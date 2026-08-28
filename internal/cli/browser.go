package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/api"
	"github.com/imyousuf/agentic-test-runner/internal/config"
)

var (
	browserJSONOutput   bool
	browserEndpoint     string
	browserPort         int
	browserDataDir      string
	browserPersistFlag  bool
	browserHeadless     bool
	browserHud          bool
	browserSandbox      bool // opt-in to enable sandbox (default: disabled for compatibility)
	browserSystemChrome bool
)

func newBrowserCmd() *cobra.Command {
	browserCmd := &cobra.Command{
		Use:   "browser",
		Short: "Control a browser instance via HTTP server",
		Long: `Start and control a browser instance through CLI commands.

The browser runs as a daemon process with an HTTP server. Commands communicate
with the server to perform browser actions. This allows tools like Claude Code
to control a browser via shell commands.`,
	}

	// Global browser flags
	browserCmd.PersistentFlags().BoolVar(&browserJSONOutput, "json", false, "Output as JSON")
	browserCmd.PersistentFlags().StringVar(&browserEndpoint, "endpoint", "", "Server endpoint URL (default: from state file)")

	// Add subcommands
	browserCmd.AddCommand(newBrowserStartCmd())
	browserCmd.AddCommand(newBrowserStopCmd())
	browserCmd.AddCommand(newBrowserStatusCmd())
	browserCmd.AddCommand(newBrowserServeCmd())

	// Navigation
	browserCmd.AddCommand(newBrowserNavigateCmd())
	browserCmd.AddCommand(newBrowserBackCmd())
	browserCmd.AddCommand(newBrowserForwardCmd())
	browserCmd.AddCommand(newBrowserReloadCmd())

	// Page management
	browserCmd.AddCommand(newBrowserNewPageCmd())
	browserCmd.AddCommand(newBrowserListPagesCmd())
	browserCmd.AddCommand(newBrowserSelectPageCmd())
	browserCmd.AddCommand(newBrowserClosePageCmd())

	// Wait
	browserCmd.AddCommand(newBrowserWaitCmd())

	// Styles
	browserCmd.AddCommand(newBrowserComputedStylesCmd())
	browserCmd.AddCommand(newBrowserComputedStylesDiffCmd())

	// Scroll
	browserCmd.AddCommand(newBrowserScrollCmd())

	// Text
	browserCmd.AddCommand(newBrowserTextCmd())

	// Interaction
	browserCmd.AddCommand(newBrowserClickCmd())
	browserCmd.AddCommand(newBrowserFillCmd())
	browserCmd.AddCommand(newBrowserHoverCmd())
	browserCmd.AddCommand(newBrowserPressKeyCmd())
	browserCmd.AddCommand(newBrowserDragCmd())

	// Inspection
	browserCmd.AddCommand(newBrowserSnapshotCmd())
	browserCmd.AddCommand(newBrowserScreenshotCmd())
	browserCmd.AddCommand(newBrowserHTMLCmd())
	browserCmd.AddCommand(newBrowserURLCmd())
	browserCmd.AddCommand(newBrowserTitleCmd())
	browserCmd.AddCommand(newBrowserEvalCmd())

	// Debugging
	browserCmd.AddCommand(newBrowserConsoleCmd())
	browserCmd.AddCommand(newBrowserNetworkCmd())
	browserCmd.AddCommand(newBrowserErrorsCmd())

	// Font
	browserCmd.AddCommand(newBrowserFontCheckCmd())

	// Download images
	browserCmd.AddCommand(newBrowserDownloadImagesCmd())

	// Clean snapshot
	browserCmd.AddCommand(newBrowserCleanSnapshotCmd())

	// Viewport
	browserCmd.AddCommand(newBrowserViewportCmd())

	// Batch
	browserCmd.AddCommand(newBrowserBatchCmd())

	// AI-powered
	browserCmd.AddCommand(newBrowserAskCmd())
	browserCmd.AddCommand(newBrowserHudCmd())

	// Recording
	browserCmd.AddCommand(newBrowserRecordCmd())

	return browserCmd
}

// Server lifecycle commands

func newBrowserStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start browser daemon",
		Long:  "Start the browser as a background daemon process with an HTTP server.",
		RunE:  runBrowserStart,
	}
	cmd.Flags().IntVar(&browserPort, "port", 0, "Server port (default: 9333)")
	cmd.Flags().BoolVar(&browserPersistFlag, "persist-session", false,
		"Keep cookies/sessions after browser closes")
	cmd.Flags().StringVar(&browserDataDir, "data-dir", "",
		"Directory for browser data (default: ~/.atr/browser-data when --persist-session)")
	cmd.Flags().BoolVar(&browserHeadless, "headless", false,
		"Run browser in headless mode (no visible window)")
	cmd.Flags().BoolVar(&browserSandbox, "sandbox", false,
		"Enable Chrome sandbox (disabled by default for Ubuntu 23.10+ compatibility)")
	cmd.Flags().BoolVar(&browserSystemChrome, "system-chrome", false,
		"Use system-installed Google Chrome (falls back to bundled browser if not found)")
	cmd.Flags().BoolVar(&browserHud, "hud", false,
		"Show the in-page agent panel once the browser is up")
	return cmd
}

func runBrowserStart(cmd *cobra.Command, args []string) error {
	// Set environment variables (inherited by daemon process)
	// Note: For CLI usage, headless defaults to false (visible browser)
	// This overrides the config default of headless=true
	setEnv := func(key, value string) error {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("failed to set %s: %w", key, err)
		}
		return nil
	}

	// Use system Chrome if requested, with fallback to rod's bundled browser
	if browserSystemChrome {
		if chromePath, found := launcher.LookPath(); found {
			if err := setEnv("ATR_BEHAVIOR_BROWSER_EXECUTABLE", chromePath); err != nil {
				return err
			}
			// Implicitly enable persist so sessions are shared between system and bundled Chrome
			browserPersistFlag = true
		} else {
			fmt.Fprintln(os.Stderr, "Warning: system Chrome not found, falling back to bundled browser")
		}
	}

	if browserHeadless {
		if err := setEnv("ATR_BEHAVIOR_BROWSER_HEADLESS", "true"); err != nil {
			return err
		}
	} else {
		// Explicitly set false to override config default
		if err := setEnv("ATR_BEHAVIOR_BROWSER_HEADLESS", "false"); err != nil {
			return err
		}
	}
	if browserPersistFlag {
		if err := setEnv("ATR_BEHAVIOR_BROWSER_PERSIST_SESSION", "true"); err != nil {
			return err
		}
	}
	if browserDataDir != "" {
		if err := setEnv("ATR_BEHAVIOR_BROWSER_DATA_DIR", browserDataDir); err != nil {
			return err
		}
	}
	// For CLI usage, sandbox is disabled by default for Ubuntu 23.10+ compatibility
	// User can opt-in with --sandbox flag
	if browserSandbox {
		if err := setEnv("ATR_BEHAVIOR_BROWSER_NO_SANDBOX", "false"); err != nil {
			return err
		}
	} else {
		if err := setEnv("ATR_BEHAVIOR_BROWSER_NO_SANDBOX", "true"); err != nil {
			return err
		}
	}

	if !browserPersistFlag {
		// Losing the session sends the next run to a login page, where every
		// step times out — which reads as a hung test rather than a browser
		// that forgot who you are.
		fmt.Fprintln(os.Stderr,
			"Warning: --persist-session not set; cookies and logins are lost when this browser stops")
	}

	state, err := api.StartDaemon(browserPort)
	if err != nil {
		return err
	}

	// Before the JSON branch below returns early: --hud must work in both
	// output modes.
	if browserHud {
		enableHudAfterStart()
	}

	if browserJSONOutput {
		return outputJSON(map[string]interface{}{
			"status":     "started",
			"endpoint":   state.Endpoint,
			"pid":        state.PID,
			"started_at": state.StartedAt.Format(time.RFC3339),
		})
	}

	fmt.Println("Browser started")
	fmt.Printf("  Endpoint: %s\n", state.Endpoint)
	fmt.Printf("  PID: %d\n", state.PID)

	statePath, err := api.StateFilePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not get state file path: %v\n", err)
	} else {
		fmt.Printf("  State: %s\n", statePath)
	}

	logPath, err := api.LogFilePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not get log file path: %v\n", err)
	} else {
		fmt.Printf("  Logs: %s\n", logPath)
	}

	return nil
}

func newBrowserStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop browser daemon",
		Long:  "Stop the running browser daemon.",
		RunE:  runBrowserStop,
	}
}

func runBrowserStop(cmd *cobra.Command, args []string) error {
	if err := api.StopDaemon(); err != nil {
		return err
	}

	if browserJSONOutput {
		return outputJSON(map[string]string{"status": "stopped"})
	}

	fmt.Println("Browser stopped")
	return nil
}

func newBrowserStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check browser daemon status",
		Long:  "Check if a browser daemon is running and show its endpoint.",
		RunE:  runBrowserStatus,
	}
}

func runBrowserStatus(cmd *cobra.Command, args []string) error {
	state, err := api.GetRunningState()
	if err != nil {
		return err
	}

	if state == nil {
		if browserJSONOutput {
			return outputJSON(map[string]interface{}{
				"running": false,
			})
		}
		fmt.Println("Browser not running")
		return nil
	}

	if browserJSONOutput {
		return outputJSON(map[string]interface{}{
			"running":    true,
			"endpoint":   state.Endpoint,
			"pid":        state.PID,
			"started_at": state.StartedAt.Format(time.RFC3339),
		})
	}

	fmt.Println("Browser running")
	fmt.Printf("  Endpoint: %s\n", state.Endpoint)
	fmt.Printf("  PID: %d\n", state.PID)
	fmt.Printf("  Started: %s\n", state.StartedAt.Format(time.RFC3339))
	return nil
}

func newBrowserServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "serve",
		Short:  "Run browser server (internal)",
		Hidden: true,
		RunE:   runBrowserServe,
	}
	cmd.Flags().IntVar(&browserPort, "port", 9333, "Server port")
	return cmd
}

func runBrowserServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	server, err := api.NewServer(api.ServerConfig{
		Port:       browserPort,
		BrowserCfg: cfg.Behavior.Browser,
		AppConfig:  cfg,
	})
	if err != nil {
		return err
	}

	ctx := context.Background()
	if err := server.Start(ctx, browserPort); err != nil {
		return err
	}

	fmt.Printf("Browser server started at %s\n", server.Endpoint())
	server.Wait()
	return nil
}

// Navigation commands

func newBrowserNavigateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "navigate <url>",
		Short: "Navigate to URL",
		Args:  cobra.ExactArgs(1),
		RunE:  runBrowserNavigate,
	}
}

func runBrowserNavigate(cmd *cobra.Command, args []string) error {
	return apiPost("/navigate", map[string]interface{}{"url": args[0]})
}

func newBrowserBackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "back",
		Short: "Go back in history",
		RunE:  func(cmd *cobra.Command, args []string) error { return apiPost("/back", nil) },
	}
}

func newBrowserForwardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forward",
		Short: "Go forward in history",
		RunE:  func(cmd *cobra.Command, args []string) error { return apiPost("/forward", nil) },
	}
}

func newBrowserReloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Reload page",
		RunE:  func(cmd *cobra.Command, args []string) error { return apiPost("/reload", nil) },
	}
}

// Page management commands

func newBrowserNewPageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new-page [url]",
		Short: "Open new tab",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := "about:blank"
			if len(args) > 0 {
				url = args[0]
			}
			return apiPost("/pages", map[string]interface{}{"url": url})
		},
	}
}

func newBrowserListPagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-pages",
		Short: "List all tabs",
		RunE:  func(cmd *cobra.Command, args []string) error { return apiGet("/pages") },
	}
}

func newBrowserSelectPageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "select-page <index>",
		Short: "Switch to tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiRequest("PUT", "/pages/"+args[0], nil)
		},
	}
}

func newBrowserClosePageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close-page <index>",
		Short: "Close tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiRequest("DELETE", "/pages/"+args[0], nil)
		},
	}
}

// Text commands

func newBrowserTextCmd() *cobra.Command {
	var flat, links, headings bool
	cmd := &cobra.Command{
		Use:   "text <selector>",
		Short: "Extract text content from element",
		Long: `Extract text content from an element, structured by HTML tag hierarchy.
Use --flat for plain text, --links for anchor elements only, --headings for h1-h6 only.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := "structured"
			if flat {
				mode = "flat"
			}
			if links {
				mode = "links"
			}
			if headings {
				mode = "headings"
			}
			path := "/text?selector=" + url.QueryEscape(args[0]) + "&mode=" + mode
			return apiGet(path)
		},
	}
	cmd.Flags().BoolVar(&flat, "flat", false, "Return plain text only")
	cmd.Flags().BoolVar(&links, "links", false, "Return only link elements with href")
	cmd.Flags().BoolVar(&headings, "headings", false, "Return only heading elements (h1-h6)")
	return cmd
}

// Scroll commands

func newBrowserScrollCmd() *cobra.Command {
	var selector string
	var x, y int
	var toBottom, toTop bool
	cmd := &cobra.Command{
		Use:   "scroll",
		Short: "Scroll inside an element",
		Long: `Scroll within a specific element's scroll container.
Useful for modals, dialogs, and other elements with overflow scroll/auto.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/scroll", map[string]interface{}{
				"selector":  selector,
				"x":         x,
				"y":         y,
				"to_bottom": toBottom,
				"to_top":    toTop,
			})
		},
	}
	cmd.Flags().StringVarP(&selector, "selector", "s", "", "CSS selector of scrollable element (required)")
	_ = cmd.MarkFlagRequired("selector")
	cmd.Flags().IntVar(&x, "x", 0, "Horizontal scroll position in pixels")
	cmd.Flags().IntVar(&y, "y", 0, "Vertical scroll position in pixels")
	cmd.Flags().BoolVar(&toBottom, "to-bottom", false, "Scroll to bottom of element")
	cmd.Flags().BoolVar(&toTop, "to-top", false, "Scroll to top of element")
	return cmd
}

// Style commands

func newBrowserComputedStylesCmd() *cobra.Command {
	var properties string
	var selectorAll string
	var selectors []string
	cmd := &cobra.Command{
		Use:   "computed-styles [selector]",
		Short: "Get computed CSS styles for an element",
		Long: `Get computed CSS styles for an element identified by CSS selector.
Returns a JSON object of CSS property names to their computed values.
Without --properties, returns a default set of common layout and typography properties.

Use --selector-all to return computed styles for every matching element in an array.
Use repeated --selector flags to batch-query multiple selectors in one call.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Batch mode: repeated --selector flags
			if len(selectors) > 0 {
				if len(args) > 0 {
					return fmt.Errorf("cannot use both positional selector and --selector flags")
				}
				path := "/computed-styles?selectors=" + url.QueryEscape(strings.Join(selectors, ","))
				if properties != "" {
					path += "&properties=" + url.QueryEscape(properties)
				}
				return apiGet(path)
			}

			path := "/computed-styles?"
			if selectorAll != "" {
				path += "selector_all=" + url.QueryEscape(selectorAll)
			} else if len(args) > 0 {
				path += "selector=" + url.QueryEscape(args[0])
			} else {
				return fmt.Errorf("selector argument, --selector, or --selector-all flag is required")
			}
			if properties != "" {
				path += "&properties=" + url.QueryEscape(properties)
			}
			return apiGet(path)
		},
	}
	cmd.Flags().StringVar(&properties, "properties", "", "Comma-separated CSS properties to return (e.g., fontSize,color,fontWeight)")
	cmd.Flags().StringVar(&selectorAll, "selector-all", "", "CSS selector matching multiple elements to get styles for")
	cmd.Flags().StringArrayVar(&selectors, "selector", nil, "CSS selector (repeatable for batch mode)")
	return cmd
}

func newBrowserComputedStylesDiffCmd() *cobra.Command {
	var against string
	var properties string
	var selectorTarget string
	var selectors []string
	cmd := &cobra.Command{
		Use:   "computed-styles-diff [selector]",
		Short: "Compare computed styles between two pages",
		Long: `Compare computed CSS styles of an element on the current page against
the same (or different) element on another open page. Returns matches,
mismatches, and a similarity score.

The --against flag accepts a page index as "page:N" or just "N" (e.g., --against page:0 or --against 0).
Use repeated --selector flags to batch-diff multiple selectors with an overall score.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pageIdx := against
			if strings.HasPrefix(pageIdx, "page:") {
				pageIdx = strings.TrimPrefix(pageIdx, "page:")
			}
			if _, err := strconv.Atoi(pageIdx); err != nil {
				return fmt.Errorf("--against must be a page index (e.g., 0, 1, or page:0): %w", err)
			}

			// Batch mode
			if len(selectors) > 0 {
				if len(args) > 0 {
					return fmt.Errorf("cannot use both positional selector and --selector flags")
				}
				path := "/computed-styles-diff?selectors=" + url.QueryEscape(strings.Join(selectors, ","))
				path += "&against=" + pageIdx
				if properties != "" {
					path += "&properties=" + url.QueryEscape(properties)
				}
				if selectorTarget != "" {
					path += "&selector_target=" + url.QueryEscape(selectorTarget)
				}
				return apiGet(path)
			}

			if len(args) < 1 {
				return fmt.Errorf("selector argument or --selector flags required")
			}

			path := "/computed-styles-diff?selector=" + url.QueryEscape(args[0])
			path += "&against=" + pageIdx
			if properties != "" {
				path += "&properties=" + url.QueryEscape(properties)
			}
			if selectorTarget != "" {
				path += "&selector_target=" + url.QueryEscape(selectorTarget)
			}
			return apiGet(path)
		},
	}
	cmd.Flags().StringVar(&against, "against", "0", "Page index to compare against (e.g., 0, page:0)")
	cmd.Flags().StringVar(&properties, "properties", "", "Comma-separated CSS properties to compare")
	cmd.Flags().StringVar(&selectorTarget, "selector-target", "", "CSS selector on target page (defaults to source selector)")
	cmd.Flags().StringArrayVar(&selectors, "selector", nil, "CSS selector (repeatable for batch mode)")
	return cmd
}

// Wait commands

func newBrowserWaitCmd() *cobra.Command {
	var timeout int
	var visible bool
	cmd := &cobra.Command{
		Use:   "wait <selector>",
		Short: "Wait for element to appear",
		Long: `Wait for an element matching the selector to appear in the DOM.
Use --visible to also require the element to be visible (not display:none or opacity:0).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/wait", map[string]interface{}{
				"selector": args[0],
				"timeout":  timeout,
				"visible":  visible,
			})
		},
	}
	cmd.Flags().IntVar(&timeout, "timeout", 5000, "Timeout in milliseconds")
	cmd.Flags().BoolVar(&visible, "visible", false, "Wait for element to be visible")
	return cmd
}

// Interaction commands

func newBrowserClickCmd() *cobra.Command {
	var doubleClick bool
	cmd := &cobra.Command{
		Use:   "click <target>",
		Short: "Click element",
		Long:  "Click an element. Target can be UID (e.g., e0), text, aria-label, data-testid, or CSS selector.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/click", map[string]interface{}{
				"selector":     args[0],
				"double_click": doubleClick,
			})
		},
	}
	cmd.Flags().BoolVar(&doubleClick, "double", false, "Double click")
	return cmd
}

func newBrowserFillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fill <target> <value>",
		Short: "Type into input",
		Long:  "Fill an input field. Target can be UID (e.g., e0), text, aria-label, data-testid, or CSS selector.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/fill", map[string]interface{}{
				"selector": args[0],
				"value":    args[1],
			})
		},
	}
}

func newBrowserHoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hover <target>",
		Short: "Hover over element",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/hover", map[string]interface{}{"selector": args[0]})
		},
	}
}

func newBrowserPressKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "press-key <key>",
		Short: "Press keyboard key",
		Long:  "Press a key or key combination. Examples: Enter, Tab, Control+A, Shift+Tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/press-key", map[string]interface{}{"key": args[0]})
		},
	}
}

func newBrowserDragCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "drag <from> <to>",
		Short: "Drag element",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/drag", map[string]interface{}{
				"from": args[0],
				"to":   args[1],
			})
		},
	}
}

// Inspection commands

func newBrowserSnapshotCmd() *cobra.Command {
	var verboseSnapshot bool
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Get page elements with UIDs",
		Long:  "Get the accessibility tree of visible page elements with unique identifiers.",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/snapshot"
			if verboseSnapshot {
				path += "?verbose=true"
			}
			return apiGet(path)
		},
	}
	cmd.Flags().BoolVar(&verboseSnapshot, "verbose", false, "Include detailed attributes")
	return cmd
}

func newBrowserScreenshotCmd() *cobra.Command {
	var fullPage bool
	var saveToFile bool
	var selector string
	var selectorAll string
	var outputDir string
	var timeout int
	cmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Capture screenshot",
		Long: `Capture a screenshot of the current page or a specific element.

Use --selector to screenshot a specific element by CSS selector (e.g., "header",
"#nav", ".hero", "[data-testid='banner']"). Combine --selector with --full to capture
the element's full scrollable height (useful for modals and dialogs with overflow).

Use --selector-all to screenshot every element matching a selector, saving each as
a numbered PNG file (1.png, 2.png, etc.) in --output-dir or /tmp/. Elements that
timeout or fail are skipped and reported separately. Use --timeout to set per-element
timeout in milliseconds (default 30000).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/screenshot"
			params := []string{}
			if selectorAll != "" {
				params = append(params, "selector_all="+url.QueryEscape(selectorAll))
				if outputDir != "" {
					params = append(params, "output_dir="+url.QueryEscape(outputDir))
				}
				if timeout != 30000 {
					params = append(params, "timeout="+strconv.Itoa(timeout))
				}
			} else if selector != "" {
				params = append(params, "selector="+url.QueryEscape(selector))
			}
			if fullPage {
				params = append(params, "full=true")
			}
			if saveToFile {
				params = append(params, "format=file")
			}
			if len(params) > 0 {
				path += "?" + params[0]
				for _, p := range params[1:] {
					path += "&" + p
				}
			}
			return apiGet(path)
		},
	}
	cmd.Flags().BoolVar(&fullPage, "full", false, "Capture full scrollable page")
	cmd.Flags().BoolVar(&saveToFile, "file", false, "Save to file instead of base64")
	cmd.Flags().StringVarP(&selector, "selector", "s", "", "CSS selector of element to screenshot")
	cmd.Flags().StringVar(&selectorAll, "selector-all", "", "CSS selector matching multiple elements to screenshot")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Directory to save screenshots (used with --selector-all)")
	cmd.Flags().IntVar(&timeout, "timeout", 30000, "Per-element timeout in milliseconds (used with --selector-all)")
	return cmd
}

func newBrowserHTMLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "html",
		Short: "Get page HTML",
		RunE:  func(cmd *cobra.Command, args []string) error { return apiGet("/html") },
	}
}

func newBrowserURLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "url",
		Short: "Get current URL",
		RunE:  func(cmd *cobra.Command, args []string) error { return apiGet("/url") },
	}
}

func newBrowserTitleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "title",
		Short: "Get page title",
		RunE:  func(cmd *cobra.Command, args []string) error { return apiGet("/title") },
	}
}

func newBrowserEvalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "eval <script>",
		Short: "Run JavaScript",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/eval", map[string]interface{}{"script": args[0]})
		},
	}
}

// Debugging commands

func newBrowserConsoleCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "console",
		Short: "Get console messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiGet("/console?limit=" + strconv.Itoa(limit))
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum messages to return")
	return cmd
}

func newBrowserNetworkCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Get network requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiGet("/network?limit=" + strconv.Itoa(limit))
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum requests to return")
	return cmd
}

func newBrowserErrorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "errors",
		Short: "Get failed requests",
		RunE:  func(cmd *cobra.Command, args []string) error { return apiGet("/errors") },
	}
}

// Clean snapshot command

func newBrowserCleanSnapshotCmd() *cobra.Command {
	var depth int
	var maxLength int
	var svgFull bool
	cmd := &cobra.Command{
		Use:   "clean-snapshot <selector>",
		Short: "Get cleaned DOM subtree for an element",
		Long: `Get a cleaned, indented DOM subtree for the element matching the selector.

Cleaning rules:
- Removes data-*/aria-* attributes (except data-theme, data-variant, data-state)
- Removes inline scripts, styles, and hidden elements
- Flattens empty wrapper divs
- Collapses SVGs to tag-only (use --svg-full to include paths)
- Truncates text content to 80 characters
- Indents with 2 spaces

Use --json for a structured JSON tree instead of HTML.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/clean-snapshot?selector=" + url.QueryEscape(args[0])
			if depth > 0 {
				path += "&depth=" + strconv.Itoa(depth)
			}
			if maxLength > 0 {
				path += "&max_length=" + strconv.Itoa(maxLength)
			}
			if svgFull {
				path += "&svg_full=true"
			}
			if browserJSONOutput {
				path += "&format=json"
			}
			return apiGet(path)
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 0, "Maximum tree depth (0 = unlimited)")
	cmd.Flags().IntVar(&maxLength, "max-length", 5000, "Maximum output characters")
	cmd.Flags().BoolVar(&svgFull, "svg-full", false, "Include full SVG path data")
	return cmd
}

// Viewport command

func newBrowserViewportCmd() *cobra.Command {
	var preset string
	var dpr float64
	cmd := &cobra.Command{
		Use:   "viewport [width height]",
		Short: "Get or set browser viewport size",
		Long: `Get or set the browser viewport dimensions.

Without arguments, returns the current viewport size.
With width and height, resizes the viewport.

Presets: mobile (375x812), tablet (768x1024), desktop (1440x900), wide (1920x1080).`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Query mode
			if len(args) == 0 && preset == "" {
				return apiGet("/viewport")
			}

			body := map[string]interface{}{}
			if preset != "" {
				body["preset"] = preset
			} else if len(args) == 2 {
				w, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("width must be an integer: %w", err)
				}
				h, err := strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("height must be an integer: %w", err)
				}
				body["width"] = w
				body["height"] = h
			} else {
				return fmt.Errorf("viewport requires both width and height, or --preset")
			}

			if dpr > 0 {
				body["dpr"] = dpr
			}

			return apiPost("/viewport", body)
		},
	}
	cmd.Flags().StringVar(&preset, "preset", "", "Named preset: mobile, tablet, desktop, wide")
	cmd.Flags().Float64Var(&dpr, "dpr", 0, "Device pixel ratio (default: 1)")
	return cmd
}

// Download images command

func newBrowserDownloadImagesCmd() *cobra.Command {
	var outputDir string
	var fallbackScreenshot bool
	cmd := &cobra.Command{
		Use:   "download-images <selector>",
		Short: "Download images found within matching elements",
		Long: `Download images found within elements matching a CSS selector.

Finds all <img> elements within the selector scope and downloads their src URLs
via the browser (bypassing CORS). If --fallback-screenshot is set and no <img>
tags are found, screenshots each matching element instead.

Files are saved as numbered images (1.png, 2.jpg, etc.) in --output-dir or /tmp/.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/download-images", map[string]interface{}{
				"selector":            args[0],
				"output_dir":          outputDir,
				"fallback_screenshot": fallbackScreenshot,
			})
		},
	}
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Directory to save images (default: /tmp/)")
	cmd.Flags().BoolVar(&fallbackScreenshot, "fallback-screenshot", false, "Screenshot elements when no <img> tags found")
	return cmd
}

// Font commands

func newBrowserFontCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "font-check <font-family>",
		Short: "Check if a font is loaded and rendering",
		Long: `Check if a font family is actually loaded and rendering in the browser.
Uses the CSS Font Loading API to verify the font's real status rather than
just the declared @font-face family name. Useful for detecting CORS-blocked
fonts or failed downloads that computed-styles won't reveal.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiGet("/font-check?family=" + url.QueryEscape(args[0]))
		},
	}
}

// AI-powered commands

func newBrowserAskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask a question about the current page",
		Long:  "Ask a natural language question about the current browser page. A sub-agent inspects the page and returns a concise answer.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/ask", map[string]interface{}{"question": args[0]})
		},
	}
}

// Helper functions

func getEndpoint() (string, error) {
	if browserEndpoint != "" {
		return browserEndpoint, nil
	}

	state, err := api.GetRunningState()
	if err != nil {
		return "", err
	}
	if state == nil {
		return "", fmt.Errorf("browser not running. Start with: atr browser start")
	}

	return state.Endpoint, nil
}

func apiGet(path string) error {
	return apiRequest("GET", path, nil)
}

func apiPost(path string, body interface{}) error {
	return apiRequest("POST", path, body)
}

// apiRequestRaw executes an API request and returns the parsed response.
func apiRequestRaw(method, path string, body interface{}) (*api.APIResponse, error) {
	endpoint, err := getEndpoint()
	if err != nil {
		return nil, err
	}

	apiURL := endpoint + "/api/v1" + path

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, apiURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result api.APIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func apiRequest(method, path string, body interface{}) error {
	result, err := apiRequestRaw(method, path, body)
	if err != nil {
		return err
	}

	if !result.Success {
		return fmt.Errorf("%s", result.Error)
	}

	if browserJSONOutput {
		return outputJSON(result.Data)
	}

	return outputHuman(result.Data)
}

func outputJSON(data interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func outputHuman(data interface{}) error {
	// Convert to map for human-readable output
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return outputJSON(data)
	}

	// Format key-value pairs
	for key, value := range dataMap {
		switch v := value.(type) {
		case []interface{}:
			fmt.Printf("%s: (%d items)\n", key, len(v))
			for i, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					// Print summary for each item
					if uid, ok := m["uid"]; ok {
						fmt.Printf("  [%s] %s: %v\n", uid, getFirst(m, "text", "name", "tag_name"), getFirst(m, "value", "url", ""))
					} else {
						fmt.Printf("  %d: %v\n", i, summarizeMap(m))
					}
				} else {
					fmt.Printf("  %d: %v\n", i, item)
				}
			}
		case string:
			fmt.Printf("%s: %s\n", key, v)
		case float64:
			if v == float64(int(v)) {
				fmt.Printf("%s: %d\n", key, int(v))
			} else {
				fmt.Printf("%s: %.2f\n", key, v)
			}
		case map[string]interface{}:
			jsonBytes, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				fmt.Printf("%s: %v\n", key, v)
			} else {
				fmt.Printf("%s: %s\n", key, string(jsonBytes))
			}
		default:
			fmt.Printf("%s: %v\n", key, v)
		}
	}

	return nil
}

func getFirst(m map[string]interface{}, keys ...string) interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil && v != "" {
			return v
		}
	}
	return ""
}

func summarizeMap(m map[string]interface{}) string {
	if url, ok := m["url"].(string); ok {
		return url
	}
	if text, ok := m["text"].(string); ok {
		if len(text) > 50 {
			return text[:50] + "..."
		}
		return text
	}
	return fmt.Sprintf("%v", m)
}

// Recording command

var (
	recordOutput string
	recordURL    string
)

func newBrowserRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record browser interactions as a behavior test",
		Long: `Record user interactions in the browser and output a .test.txt behavior test file.

Captures clicks, form fills, keyboard shortcuts, navigation, and scroll events.
A recording overlay appears in the browser showing captured steps in real time.

Stop recording with Ctrl+C in the terminal or the "Stop" button in the browser overlay.

Examples:
  atr browser record --url https://example.com --output repro.test.txt
  atr browser record -o login-flow.test.txt`,
		RunE: runBrowserRecord,
	}
	cmd.Flags().StringVarP(&recordOutput, "output", "o", "", "Output file path (default: record-<timestamp>.test.txt)")
	cmd.Flags().StringVar(&recordURL, "url", "", "Initial URL to navigate to")
	return cmd
}

func runBrowserRecord(cmd *cobra.Command, args []string) error {
	// Start recording
	body := map[string]interface{}{}
	if recordURL != "" {
		body["url"] = recordURL
	}
	result, err := apiRequestRaw("POST", "/record/start", body)
	if err != nil {
		return fmt.Errorf("failed to start recording: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("failed to start recording: %s", result.Error)
	}

	fmt.Println("Recording started. Interact with the browser.")
	fmt.Println("Press Ctrl+C or click \"Stop\" in the browser overlay to finish.")

	// Wait for stop signal (Ctrl+C or overlay stop)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	stoppedByOverlay := false
	for {
		select {
		case <-sigCh:
			fmt.Println("\nStopping recording...")
			goto stop
		case <-ticker.C:
			// Check if recording was stopped via overlay
			statusResult, err := apiRequestRaw("GET", "/record/status", nil)
			if err == nil && statusResult.Success {
				if data, ok := statusResult.Data.(map[string]interface{}); ok {
					if recording, ok := data["recording"].(bool); ok && !recording {
						stoppedByOverlay = true
						goto stop
					}
				}
			}
		}
	}

stop:
	// Fetch results
	var stopResult *api.APIResponse
	if stoppedByOverlay {
		// Recording already stopped, just get the events
		stopResult, err = apiRequestRaw("POST", "/record/stop", nil)
		if err != nil {
			// If stop fails because already stopped, that's fine
			// Try to get status with events
			stopResult, err = apiRequestRaw("GET", "/record/status", nil)
		}
	} else {
		stopResult, err = apiRequestRaw("POST", "/record/stop", nil)
	}
	if err != nil {
		return fmt.Errorf("failed to stop recording: %w", err)
	}

	// Extract test content from response
	data, ok := stopResult.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected response format")
	}

	testContent, _ := data["test_content"].(string)
	eventCount := 0
	if ec, ok := data["event_count"].(float64); ok {
		eventCount = int(ec)
	}

	if testContent == "" {
		fmt.Println("No interactions were recorded.")
		return nil
	}

	// Determine output filename
	outputPath := recordOutput
	if outputPath == "" {
		outputPath = fmt.Sprintf("record-%s.test.txt", time.Now().Format("20060102-150405"))
	}

	// Write file
	if err := os.WriteFile(outputPath, []byte(testContent), 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Printf("Recorded %d steps to %s\n", eventCount, outputPath)
	return nil
}
