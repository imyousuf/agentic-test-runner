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
	"strconv"
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

	// Scroll
	browserCmd.AddCommand(newBrowserScrollCmd())

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

	// AI-powered
	browserCmd.AddCommand(newBrowserAskCmd())

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

	state, err := api.StartDaemon(browserPort)
	if err != nil {
		return err
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
	cmd := &cobra.Command{
		Use:   "computed-styles <selector>",
		Short: "Get computed CSS styles for an element",
		Long: `Get computed CSS styles for an element identified by CSS selector.
Returns a JSON object of CSS property names to their computed values.
Without --properties, returns a default set of common layout and typography properties.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/computed-styles?selector=" + url.QueryEscape(args[0])
			if properties != "" {
				path += "&properties=" + url.QueryEscape(properties)
			}
			return apiGet(path)
		},
	}
	cmd.Flags().StringVar(&properties, "properties", "", "Comma-separated CSS properties to return (e.g., fontSize,color,fontWeight)")
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
				"target":       args[0],
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
				"target": args[0],
				"value":  args[1],
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
			return apiPost("/hover", map[string]interface{}{"target": args[0]})
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
	cmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Capture screenshot",
		Long: `Capture a screenshot of the current page or a specific element.

Use --selector to screenshot a specific element by CSS selector (e.g., "header",
"#nav", ".hero", "[data-testid='banner']"). When --selector is used, --full is ignored.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/screenshot"
			params := []string{}
			if selector != "" {
				params = append(params, "selector="+url.QueryEscape(selector))
			} else if fullPage {
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
	cmd.Flags().StringVarP(&selector, "selector", "s", "", "CSS selector of element to screenshot (e.g., \"header\", \"#nav\", \".hero\")")
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

func apiRequest(method, path string, body interface{}) error {
	endpoint, err := getEndpoint()
	if err != nil {
		return err
	}

	url := endpoint + "/api/v1" + path

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result api.APIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
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
