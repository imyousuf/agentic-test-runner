package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/api"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP (Model Context Protocol) commands",
		Long:  "Commands for running ATR as an MCP server, allowing CLI tools like claude and gemini to use ATR's browser automation.",
	}

	mcpCmd.AddCommand(newMCPServeCmd())

	return mcpCmd
}

func newMCPServeCmd() *cobra.Command {
	var headless bool
	var sandbox bool
	var ignoreHTTPSErrors bool
	var cdpEndpoint string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server for browser automation",
		Long: `Start an MCP (Model Context Protocol) server that exposes ATR's browser tools.

This allows CLI tools like claude and gemini to control a browser via ATR:

  # Register with Claude CLI (inline config)
  claude -p "navigate to example.com" \
    --mcp-config '{"mcpServers":{"atr-browser":{"command":"atr","args":["mcp","serve"]}}}' \
    --allowedTools "mcp__atr-browser__*"

  # Or add to Claude settings (~/.claude.json or project .mcp.json)
  {
    "mcpServers": {
      "atr-browser": {"command": "atr", "args": ["mcp", "serve"]}
    }
  }

  # Register with Gemini CLI (project settings .gemini/settings.json)
  {
    "mcpServers": {
      "atr-browser": {"command": "atr", "args": ["mcp", "serve"], "trust": true}
    }
  }

  # Connect to existing browser via CDP endpoint
  atr mcp serve --cdp-endpoint ws://localhost:9222

The server communicates via JSON-RPC 2.0 over stdio and exposes browser tools:
  - browser_navigate: Navigate to a URL
  - browser_click: Click on an element
  - browser_fill: Fill a form field
  - browser_screenshot: Take a screenshot
  - browser_get_url: Get current URL
  - browser_get_title: Get page title
  - browser_get_html: Get page HTML
  - browser_snapshot: Get accessibility tree
  - browser_console: Get console messages
  - browser_network: Get network requests
  - browser_press_key: Press a key
  - browser_hover: Hover over an element
  - browser_go_back: Navigate back
  - browser_go_forward: Navigate forward
  - browser_reload: Reload the page
  - browser_ask: Ask a question about the current page (AI-powered)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load configuration
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Build browser config from behavior settings
			browserCfg := cfg.Behavior.Browser

			// Apply CLI defaults via factory options
			browserCfg.Headless = headless
			browserCfg.NoSandbox = !sandbox

			// Override with other flags if provided
			if cmd.Flags().Changed("ignore-https-errors") {
				browserCfg.IgnoreHTTPSErrors = ignoreHTTPSErrors
			}

			// Open debug log file
			homeDir, _ := os.UserHomeDir()
			logPath := filepath.Join(homeDir, ".atr", "mcp-debug.log")
			os.MkdirAll(filepath.Dir(logPath), 0700)
			logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err == nil {
				defer logFile.Close()
				fmt.Fprintf(logFile, "\n=== MCP Server Starting at %s ===\n", time.Now().Format(time.RFC3339))
			}
			mcpLog := func(format string, args ...interface{}) {
				msg := fmt.Sprintf(format, args...)
				if logFile != nil {
					fmt.Fprintf(logFile, "[%s] %s\n", time.Now().Format("15:04:05.000"), msg)
				}
			}

			// Check for CDP endpoint from flag, environment, or state file
			endpoint := cdpEndpoint
			mcpLog("Checking for CDP endpoint...")
			mcpLog("From flag: %q", endpoint)
			if endpoint == "" {
				endpoint = os.Getenv("ATR_CDP_ENDPOINT")
				mcpLog("From env ATR_CDP_ENDPOINT: %q", endpoint)
			}
			if endpoint == "" {
				// Try to read from browser state file (written by atr run --behavior or atr browser start)
				mcpLog("Checking state file...")
				if state, err := api.GetRunningState(); err == nil && state != nil {
					endpoint = state.Endpoint
					mcpLog("From state file: %q", endpoint)
				} else if err != nil {
					mcpLog("State file error: %v", err)
				} else {
					mcpLog("No state file found")
				}
			}
			mcpLog("Final endpoint: %q", endpoint)

			// Create MCP server with options
			var opts []mcp.ServerOption
			if endpoint != "" {
				opts = append(opts, mcp.WithCDPEndpoint(endpoint))
			}
			opts = append(opts, mcp.WithAppConfig(cfg))

			server := mcp.NewServer(browserCfg, opts...)
			defer server.Close()

			// Set up signal handling for graceful shutdown
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			go func() {
				<-sigChan
				cancel()
			}()

			// Run the MCP server
			return server.Run(ctx)
		},
	}

	cmd.Flags().BoolVar(&headless, "headless", false, "Run browser in headless mode (no visible window)")
	cmd.Flags().BoolVar(&sandbox, "sandbox", false, "Enable Chrome sandbox (disabled by default for Ubuntu 23.10+ compatibility)")
	cmd.Flags().BoolVar(&ignoreHTTPSErrors, "ignore-https-errors", false, "Ignore HTTPS certificate errors")
	cmd.Flags().StringVar(&cdpEndpoint, "cdp-endpoint", "", "Connect to existing browser via CDP endpoint (or set ATR_CDP_ENDPOINT)")

	return cmd
}
