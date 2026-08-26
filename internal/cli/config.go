package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/llm"
	pkgllm "github.com/imyousuf/agentic-test-runner/pkg/llm"
)

func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management",
		Long:  "Commands for managing ATR configuration.",
	}

	configCmd.AddCommand(newConfigShowCmd())
	configCmd.AddCommand(newConfigInitCmd())
	configCmd.AddCommand(newConfigValidateCmd())

	return configCmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			fmt.Println("Current Configuration:")
			fmt.Println("======================")
			fmt.Printf("Backend: %s\n", cfg.Backend)

			if cfg.IsCLIBackend() {
				if cfg.Model != "" {
					fmt.Printf("Model: %s\n", cfg.Model)
				} else {
					fmt.Printf("Model: (default)\n")
				}
				fmt.Printf("CLI Timeout: %s\n", cfg.GetCLITimeout())
			} else {
				fmt.Printf("Model tier: %s\n", cfg.Model)
				fmt.Printf("Model name: %s\n", cfg.GetModelName())
			}
			fmt.Println()

			switch cfg.Backend {
			case "gemini-api":
				fmt.Println("Gemini API:")
				if cfg.Gemini.APIKey != "" {
					fmt.Println("  API Key: [set]")
				} else {
					fmt.Println("  API Key: [not set]")
				}
			case "vertex-ai":
				fmt.Println("Vertex AI:")
				fmt.Printf("  Project: %s\n", cfg.Vertex.Project)
				fmt.Printf("  Location: %s\n", cfg.Vertex.Location)
			case "claude-cli", "gemini-cli":
				fmt.Println("CLI Backend:")
				clis := llm.DetectAvailableCLIs()
				for _, cli := range clis {
					status := "not installed"
					if cli.Available {
						status = "installed"
						if cli.Version != "" {
							status = fmt.Sprintf("installed (%s)", cli.Version)
						}
					}
					fmt.Printf("  %s: %s\n", cli.Provider, status)
				}
			}

			fmt.Println()
			fmt.Println("Agent Settings:")
			fmt.Printf("  Max Iterations: %d\n", cfg.Agent.MaxIterations)
			fmt.Printf("  Timeout: %s\n", cfg.Agent.Timeout)
			fmt.Printf("  Temperature: %.2f\n", cfg.Agent.Temperature)

			fmt.Println()
			fmt.Println("Executor Settings:")
			fmt.Printf("  Command Timeout: %s\n", cfg.Executor.CommandTimeout)
			fmt.Printf("  Max Output Size: %d bytes\n", cfg.Executor.MaxOutputSize)

			// Show available CLIs if not using CLI backend
			if !cfg.IsCLIBackend() {
				fmt.Println()
				fmt.Println("Available CLI Backends:")
				clis := llm.DetectAvailableCLIs()
				if len(clis) == 0 {
					fmt.Println("  (none detected)")
				}
				for _, cli := range clis {
					version := ""
					if cli.Version != "" {
						version = fmt.Sprintf(" (%s)", cli.Version)
					}
					fmt.Printf("  %s%s\n", cli.Provider, version)
				}
			}

			return nil
		},
	}
}

func newConfigInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a default configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.EnsureConfigDir(); err != nil {
				return fmt.Errorf("failed to create config directory: %w", err)
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				return err
			}

			configPath := filepath.Join(configDir, "config.yaml")

			// Check if file already exists
			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf("config file already exists at %s", configPath)
			}

			// Detect available CLIs
			clis := llm.DetectAvailableCLIs()
			var detectedBackend string
			var detectedCLIInfo string
			var defaultModel string

			if len(clis) > 0 {
				detectedBackend = string(clis[0].Provider)
				detectedCLIInfo = fmt.Sprintf("# Detected CLI: %s", clis[0].Provider)
				if clis[0].Version != "" {
					detectedCLIInfo += fmt.Sprintf(" (%s)", clis[0].Version)
				}
				detectedCLIInfo += "\n"
				if detectedBackend == "claude-cli" {
					defaultModel = "sonnet"
				} else {
					defaultModel = "flash"
				}
			} else {
				detectedBackend = "gemini-api"
				detectedCLIInfo = "# No CLI backends detected. Using API backend.\n"
				defaultModel = "flash"
			}

			defaultConfig := fmt.Sprintf(`# ATR Configuration
# See https://github.com/imyousuf/agentic-test-runner for documentation

%s
# LLM Backend: "gemini-api", "vertex-ai", "claude-cli", or "gemini-cli"
# CLI backends don't require API keys - they use the CLI's authentication
backend: "%s"

# Gemini API Configuration (when backend: gemini-api)
gemini:
  api_key: ""  # Or set GEMINI_API_KEY environment variable

# Vertex AI Configuration (when backend: vertex-ai)
vertex:
  project: ""           # Or set GOOGLE_CLOUD_PROJECT environment variable
  location: "global"    # Use "global" for Gemini 3 preview models
  credentials_file: ""  # Path to service account JSON key file

# CLI Backend Configuration (when backend: claude-cli or gemini-cli)
cli:
  auto_detect: true     # Automatically detect available CLIs
  timeout: "5m"         # Timeout for CLI execution

# Model Selection:
#   API backends: "flash" (faster, cheaper) or "pro" (more capable)
#   claude-cli: "sonnet", "opus", or "haiku"
#   gemini-cli: "flash" or "pro"
model: "%s"

# Model name overrides (optional)
models:
  flash: "gemini-3-flash-preview"
  pro: "gemini-3-pro-preview"

# Agent Configuration
agent:
  max_iterations: 100   # Maximum tool calls before giving up
  timeout: "5m"         # Total timeout for agent analysis
  temperature: 0.3      # LLM temperature (0.0 - 1.0)

# Command Executor Configuration
executor:
  command_timeout: "2m"    # Timeout for individual commands
  max_output_size: 10485760 # Max bytes to capture (10MB)
  environment:
    auto_detect: true      # Automatically detect Python venv and Node.js environments
    use_llm_detection: true  # Use LLM to analyze commands and determine env needs (pattern matching fallback)
    python_venv_path: ""   # Manual path to Python virtual environment
    conda_env_name: ""     # Manual conda environment name
    node_version: ""       # Manual Node.js version for nvm/fnm
    disable_python_env: false  # Disable Python environment detection
    disable_node_env: false    # Disable Node.js environment detection

# Browser Behavior Testing Configuration
behavior:
  base_url: ""           # Base URL for behavior tests
  browser:
    executable: "auto"   # Browser binary path ("auto" for auto-download)
    version: "latest"    # Browser version: "latest", "stable", "beta", "dev", "canary"
    cache_dir: ""        # Browser cache directory (empty for default)
    headless: true       # Run browser in headless mode
    ignore_https_errors: false  # Ignore SSL certificate errors (useful for local dev)
    viewport:
      width: 1920
      height: 1080
    page_timeout: "30s"  # Page load timeout
    action_timeout: "10s"  # Timeout for actions (click, type, etc.)
    slow_motion: "0s"    # Delay between actions for debugging
  capture:
    screenshots: true
    full_page_screenshot: false
    console_logs: true
    network_har: true
    dom_snapshot: true
    max_console_entries: 100
    max_network_requests: 50

# Browser Server Configuration
server:
  port: 9333            # HTTP server port
  read_timeout: "30s"
  write_timeout: "30s"

# Secret References (used by the in-page agent HUD)
#
# Each entry maps a name to a command that prints the secret on stdout. ATR
# runs the command and types the output straight into the field; the value is
# never shown to the agent and never enters the conversation history.
#
# secrets:
#   timeout: "60s"       # How long to wait for the password manager
#   refs:
#     github/password: "secret-tool lookup service atr account github/password"
#     work/vpn: "pass show work/vpn"
#     aws/key: "op read op://Private/aws/credential"

# Update Configuration
update:
  auto_update_dev: true  # Auto-update dev versions every 2 days
  disabled: false        # Disable all update checking
`, detectedCLIInfo, detectedBackend, defaultModel)

			if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}

			fmt.Printf("Created config file at: %s\n", configPath)
			fmt.Println()

			// Show detected CLIs
			if len(clis) > 0 {
				fmt.Println("Detected CLI backends:")
				for _, cli := range clis {
					version := ""
					if cli.Version != "" {
						version = fmt.Sprintf(" (%s)", cli.Version)
					}
					fmt.Printf("  - %s%s\n", cli.Provider, version)
				}
				fmt.Printf("\nUsing '%s' as default backend.\n", detectedBackend)
			} else {
				fmt.Println("No CLI backends detected.")
				fmt.Println("Using 'gemini-api' as default backend.")
			}

			fmt.Println("\nNext steps:")
			if len(clis) > 0 {
				fmt.Println("1. Run 'atr config validate' to verify your configuration")
				fmt.Println("2. Run 'atr run --cmd <command>' to test")
			} else {
				fmt.Println("1. Edit the config file to add your API key or Vertex AI settings")
				fmt.Println("   Or install claude or gemini CLI for no-API-key usage")
				fmt.Println("2. Run 'atr config validate' to verify your configuration")
				fmt.Println("3. Run 'atr run --cmd <command>' to test")
			}

			return nil
		},
	}
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if err := cfg.Validate(); err != nil {
				fmt.Printf("Configuration is invalid: %v\n", err)
				return err
			}

			// For CLI backends, check if the CLI is available
			if cfg.IsCLIBackend() {
				var provider string
				switch cfg.Backend {
				case "claude-cli":
					provider = "claude-cli"
				case "gemini-cli":
					provider = "gemini-cli"
				}

				if !llm.IsCLIAvailable(pkgllm.Provider(provider)) {
					fmt.Printf("Warning: CLI backend '%s' is configured but the CLI is not installed\n", cfg.Backend)
					fmt.Println("Install the CLI or change backend to 'gemini-api' or 'vertex-ai'")
					return fmt.Errorf("CLI '%s' is not available in PATH", cfg.Backend)
				}
			}

			fmt.Println("Configuration is valid")
			fmt.Printf("  Backend: %s\n", cfg.Backend)

			if cfg.IsCLIBackend() {
				// Show model for CLI backends
				if cfg.Model != "" {
					fmt.Printf("  Model: %s\n", cfg.Model)
				} else {
					fmt.Printf("  Model: (default)\n")
				}
				// Show CLI info
				cliInfo := llm.DetectAvailableCLIs()
				for _, cli := range cliInfo {
					if string(cli.Provider) == cfg.Backend {
						if cli.Version != "" {
							fmt.Printf("  CLI Version: %s\n", cli.Version)
						}
						break
					}
				}
			} else {
				fmt.Printf("  Model: %s\n", cfg.GetModelName())
			}

			return nil
		},
	}
}
