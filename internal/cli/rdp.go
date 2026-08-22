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

	"github.com/imyousuf/agentic-test-runner/internal/rdp"
	"github.com/imyousuf/agentic-test-runner/web"
)

func newRDPCmd() *cobra.Command {
	var (
		port     int
		bind     string
		token    string
		attach   string
		viewOnly bool
		quality  int
		maxWidth int
		fps      int
	)

	cmd := &cobra.Command{
		Use:     "rdp",
		Aliases: []string{"view"},
		Short:   "Serve a live view of the browser in a web page",
		Long: `Serve a live view of the browser that ATR drives.

The command attaches to the running browser as a second DevTools session and
streams the active page to a web application. You can watch the agent and take
over for a step that needs a person, such as a login.

Examples:
  # Watch the browser that "atr browser start" launched
  atr rdp

  # Use another port, and attach to a browser by endpoint
  atr rdp --port 9000 --attach cdp://127.0.0.1:9222

  # Watch without the ability to click
  atr rdp --view-only`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if token == "" {
				token = os.Getenv("ATR_RDP_TOKEN")
			}
			if token == "" {
				token = rdp.NewToken()
			}
			if !isLoopback(bind) && token == "" {
				return fmt.Errorf("a token is required to bind %s", bind)
			}

			cdpURL, err := rdp.Discover(attach)
			if err != nil {
				return err
			}

			hub := rdp.NewHub()
			streamer := rdp.NewStreamer(hub, rdp.Options{
				Quality: quality, MaxWidth: maxWidth, FPS: fps, ViewOnly: viewOnly,
			})
			if err := streamer.Attach(cdpURL); err != nil {
				return err
			}
			defer streamer.Close()

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

			server := rdp.NewServer(hub, streamer, assets, token, viewOnly)
			addr := net.JoinHostPort(bind, fmt.Sprint(port))
			httpServer := &http.Server{
				Addr:              addr,
				Handler:           server.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
			}

			pages, _ := streamer.Pages()
			fmt.Println("ATR live view")
			fmt.Printf("  URL:     http://%s/?t=%s\n", addr, token)
			fmt.Printf("  Browser: %s  (attached, not owned)\n", streamer.Version())
			fmt.Printf("  Pages:   %d\n", len(pages))
			if viewOnly {
				fmt.Println("  Input:   disabled")
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
	cmd.Flags().StringVar(&token, "token", "", "Access token (or set ATR_RDP_TOKEN)")
	cmd.Flags().StringVar(&attach, "attach", "", "CDP endpoint, such as cdp://127.0.0.1:9222")
	cmd.Flags().BoolVar(&viewOnly, "view-only", false, "Refuse input from viewers")
	cmd.Flags().IntVar(&quality, "quality", 60, "JPEG quality, 1 to 100")
	cmd.Flags().IntVar(&maxWidth, "max-width", 1600, "Largest frame width")
	cmd.Flags().IntVar(&fps, "fps", 20, "Target frame rate")

	cmd.AddCommand(newRDPSetupCmd())

	return cmd
}

func newRDPSetupCmd() *cobra.Command {
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
It also generates an access token and stores it with owner-only permissions.

It does not install a browser. The browser belongs to ATR itself, and the live
view attaches to whichever one ATR is driving.

Examples:
  atr rdp setup                 # install, enable, and print the URL
  atr rdp setup --check         # report the state, change nothing
  atr rdp setup --port 9000     # use another port
  atr rdp setup --uninstall     # remove the service, keep the token`,
		RunE: func(_ *cobra.Command, _ []string) error {
			switch {
			case check:
				installed, running, path := rdp.Status()
				fmt.Println("ATR live view service")
				fmt.Printf("  Platform:  %s\n", runtime.GOOS)
				fmt.Printf("  File:      %s\n", path)
				fmt.Printf("  Installed: %t\n", installed)
				fmt.Printf("  Running:   %t\n", running)
				token, tokenPath, err := rdp.EnsureToken()
				if err != nil {
					return err
				}
				fmt.Printf("  Token:     %s\n", tokenPath)
				fmt.Printf("  URL:       http://%s:%d/?t=%s\n", bind, port, token)
				return nil

			case uninstall:
				path, err := rdp.Uninstall()
				if err != nil {
					return err
				}
				fmt.Println("Removed the live view service.")
				fmt.Printf("  File: %s\n", path)
				fmt.Println("  The token is kept, so a later setup gives the same URL.")
				return nil
			}

			result, err := rdp.Setup(rdp.SetupOptions{Port: port, Bind: bind, FPS: fps})
			if err != nil {
				return err
			}

			fmt.Println("ATR live view installed")
			fmt.Printf("  Platform: %s\n", result.Platform)
			fmt.Printf("  Service:  %s\n", result.ServicePath)
			fmt.Printf("  Token:    %s\n", result.TokenPath)
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
