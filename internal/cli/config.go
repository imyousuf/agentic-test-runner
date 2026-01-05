package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/config"
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
			fmt.Printf("Model tier: %s\n", cfg.Model)
			fmt.Printf("Model name: %s\n", cfg.GetModelName())
			fmt.Println()

			if cfg.Backend == "gemini-api" {
				fmt.Println("Gemini API:")
				if cfg.Gemini.APIKey != "" {
					fmt.Println("  API Key: [set]")
				} else {
					fmt.Println("  API Key: [not set]")
				}
			} else {
				fmt.Println("Vertex AI:")
				fmt.Printf("  Project: %s\n", cfg.Vertex.Project)
				fmt.Printf("  Location: %s\n", cfg.Vertex.Location)
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

			defaultConfig := `# ATR Configuration
# See https://github.com/imyousuf/agentic-test-runner for documentation

# LLM Backend: "gemini-api" or "vertex-ai"
backend: "gemini-api"

# Gemini API Configuration (when backend: gemini-api)
gemini:
  api_key: ""  # Or set GEMINI_API_KEY environment variable

# Vertex AI Configuration (when backend: vertex-ai)
vertex:
  project: ""           # Or set GOOGLE_CLOUD_PROJECT environment variable
  location: "global"    # Use "global" for Gemini 3 preview models
  credentials_file: ""  # Path to service account JSON key file

# Model Selection: "flash" (faster, cheaper) or "pro" (more capable)
model: "flash"

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
`

			if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}

			fmt.Printf("Created config file at: %s\n", configPath)
			fmt.Println("\nNext steps:")
			fmt.Println("1. Edit the config file to add your API key or Vertex AI settings")
			fmt.Println("2. Run 'atr config validate' to verify your configuration")
			fmt.Println("3. Run 'atr run --cmd <command>' to test")

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
				fmt.Printf("❌ Configuration is invalid: %v\n", err)
				return err
			}

			fmt.Println("✓ Configuration is valid")
			fmt.Printf("  Backend: %s\n", cfg.Backend)
			fmt.Printf("  Model: %s\n", cfg.GetModelName())

			return nil
		},
	}
}
