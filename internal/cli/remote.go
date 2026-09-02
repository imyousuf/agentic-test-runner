package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/record"
	"github.com/imyousuf/agentic-test-runner/internal/remote"
	"github.com/imyousuf/agentic-test-runner/web"
)

func newRemoteCmd() *cobra.Command {
	var (
		port     int
		bind     string
		attach   string
		viewOnly bool
		quality  int
		maxWidth int
		fps      int
		output   string
		redactQ  bool
	)

	cmd := &cobra.Command{
		Use:     "remote",
		Aliases: []string{"view", "rdp"},
		Short:   "Serve a live view of the browser in a web page",
		Long: `Serve a live view of the browser that ATR drives.

The command attaches to the running browser as a second DevTools session and
streams the active page to a web application. You can watch the agent and take
over for a step that needs a person, such as a login. The page also records the
session and plays back what you recorded.

Examples:
  # Watch the browser that "atr browser start" launched
  atr remote

  # Use another port, and attach to a browser by endpoint
  atr remote --port 9000 --attach cdp://127.0.0.1:9222

  # Watch without the ability to click
  atr remote --view-only`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The live view authenticates nobody. On a laptop the loopback bind
			// is the boundary; anywhere else it has to be supplied around this
			// process, so binding wider says so out loud.
			if !isLoopback(bind) {
				fmt.Fprintf(os.Stderr,
					"Warning: bound to %s with no authentication. Anyone who can reach "+
						"port %d has full control of the browser and everything it is "+
						"logged in to.\n"+
						"Put a proxy or an SSH tunnel in front of it, or keep the port "+
						"private.\n", bind, port)
			}

			cdpURL, err := remote.Discover(attach)
			if err != nil {
				return err
			}

			hub := remote.NewHub()
			streamer := remote.NewStreamer(remote.Options{
				Quality: quality, MaxWidth: maxWidth, FPS: fps, ViewOnly: viewOnly,
			})
			streamer.AddSink(hub)
			if err := streamer.Attach(cdpURL); err != nil {
				return err
			}
			defer streamer.Close()
			// Set this before Select. Select starts the stream, and the tap
			// that goes with it reads the setting once.
			streamer.SetRedactQuery(redactQ)

			if err := streamer.Select(""); err != nil {
				return err
			}

			assets, err := web.Assets()
			if err != nil {
				return fmt.Errorf("failed to load the web assets: %w", err)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go streamer.Watch(ctx)
			go streamer.Heartbeat(ctx, 5*time.Second)

			server := remote.NewServer(hub, streamer, assets, viewOnly)

			// Recording is off. The session only gives the page the ability to
			// start one, and to browse what was recorded before.
			var session *remote.Session
			store, storeErr := record.NewStore(output)
			if storeErr != nil {
				fmt.Fprintf(os.Stderr, "Recording is unavailable: %v\n", storeErr)
			} else {
				session = remote.NewSession(store, streamer, record.Limits{}, false)
				session.SetChangeOptions(changeOptions(cmd))
				server = server.WithSession(session)
				go session.Publish(ctx)
			}

			addr := net.JoinHostPort(bind, fmt.Sprint(port))
			httpServer := &http.Server{
				Addr:              addr,
				Handler:           server.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
			}

			pages, _ := streamer.Pages()
			fmt.Println("ATR live view")
			fmt.Printf("  URL:     http://%s/\n", addr)
			fmt.Printf("  Browser: %s  (attached, not owned)\n", streamer.Version())
			fmt.Printf("  Pages:   %d\n", len(pages))
			if viewOnly {
				fmt.Println("  Input:   disabled")
			}
			switch {
			case session == nil:
				fmt.Println("  Record:  unavailable")
			case viewOnly:
				fmt.Println("  Record:  disabled by --view-only")
			default:
				fmt.Printf("  Record:  off, press ● in the page  (%s)\n", store.Root())
			}

			errCh := make(chan error, 1)
			go func() {
				if err := httpServer.ListenAndServe(); err != nil &&
					err != http.ErrServerClosed {
					errCh <- err
				}
			}()

			select {
			case err := <-errCh:
				return fmt.Errorf("the live view server stopped: %w", err)
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = httpServer.Shutdown(shutdownCtx)
				fmt.Println("\nLive view stopped. The browser is still running.")
				return nil
			}
		},
	}

	cmd.Flags().IntVar(&port, "port", 7788, "HTTP port")
	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1", "Listen address")
	cmd.Flags().StringVar(&attach, "attach", "", "CDP endpoint, such as cdp://127.0.0.1:9222")
	cmd.Flags().BoolVar(&viewOnly, "view-only", false, "Refuse input from viewers")
	cmd.Flags().IntVar(&quality, "quality", 60, "JPEG quality, 1 to 100")
	cmd.Flags().IntVar(&maxWidth, "max-width", 1600, "Largest frame width")
	cmd.Flags().IntVar(&fps, "fps", 20, "Target frame rate")
	cmd.Flags().StringVarP(&output, "output", "o", "",
		"Recordings directory (default: ~/.atr/recordings)")
	cmd.Flags().BoolVar(&redactQ, "redact-query", false,
		"Drop the query string from every URL in the log")
	addChangeFlags(cmd)

	cmd.AddCommand(newRemoteSetupCmd())

	return cmd
}

func newRemoteSetupCmd() *cobra.Command {
	var (
		port      int
		bind      string
		fps       int
		check     bool
		uninstall bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install a service that keeps the live view running",
		Long: `Install a service that keeps the live view running.

The command writes a systemd user unit on Linux, or a launchd agent on macOS.

The service authenticates nobody, so it binds 127.0.0.1 by default. Reach it
from elsewhere over an SSH tunnel, or put a proxy in front of it.

It does not install a browser. The browser belongs to ATR itself, and the live
view attaches to whichever one ATR is driving.

Examples:
  atr remote setup                 # install, enable, and print the URL
  atr remote setup --check         # report the state, change nothing
  atr remote setup --port 9000     # use another port
  atr remote setup --uninstall     # remove the service`,
		RunE: func(_ *cobra.Command, _ []string) error {
			switch {
			case check:
				installed, running, path := remote.Status()
				fmt.Println("ATR live view service")
				fmt.Printf("  Platform:  %s\n", runtime.GOOS)
				fmt.Printf("  File:      %s\n", path)
				fmt.Printf("  Installed: %t\n", installed)
				fmt.Printf("  Running:   %t\n", running)
				fmt.Printf("  URL:       http://%s:%d/\n", bind, port)
				return nil

			case uninstall:
				path, err := remote.Uninstall()
				if err != nil {
					return err
				}
				fmt.Println("Removed the live view service.")
				fmt.Printf("  File: %s\n", path)
				return nil
			}

			result, err := remote.Setup(remote.SetupOptions{Port: port, Bind: bind, FPS: fps})
			if err != nil {
				return err
			}

			fmt.Println("ATR live view installed")
			fmt.Printf("  Platform: %s\n", result.Platform)
			fmt.Printf("  Service:  %s\n", result.ServicePath)
			fmt.Printf("  URL:      %s\n", result.URL)
			for _, note := range result.Notes {
				fmt.Printf("\n%s\n", note)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&port, "port", 7788, "HTTP port for the service")
	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1", "Listen address for the service")
	cmd.Flags().IntVar(&fps, "fps", 20, "Target frame rate")
	cmd.Flags().BoolVar(&check, "check", false, "Report the state and change nothing")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "Remove the service")

	return cmd
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
