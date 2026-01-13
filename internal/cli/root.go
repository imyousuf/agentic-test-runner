// Package cli implements the command-line interface for ATR.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	verbose bool
)

// rootCmd is the base command.
var rootCmd = &cobra.Command{
	Use:   "atr",
	Short: "Agentic Test Runner - AI-powered command failure analysis",
	Long: `ATR (Agentic Test Runner) executes shell commands and uses AI to analyze failures.

When a command fails, ATR engages an AI agent that can:
  - Analyze the failure output
  - Run additional diagnostic commands
  - Read source files and configuration
  - Search the codebase for related code

The agent iteratively investigates until it understands the failure,
then provides a clear explanation and actionable recommendations.

Examples:
  # Run tests and analyze failures
  atr run --cmd "go test ./..." --cwd "/path/to/project"

  # With additional context
  atr run --cmd "npm test" --context "Running integration tests for auth module"

  # Using the pro model for complex issues
  atr run --cmd "make build" --model pro`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Skip auto-update check for update and version commands
		if cmd.Name() == "update" || cmd.Name() == "version" {
			return
		}
		CheckAndAutoUpdate()
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Persistent flags (available to all subcommands)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.atr/config.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	// LLM backend flags
	rootCmd.PersistentFlags().String("backend", "", "LLM backend: claude-cli, gemini-cli, gemini-api, or vertex-ai")
	rootCmd.PersistentFlags().String("api-key", "", "Gemini API key")
	rootCmd.PersistentFlags().String("project", "", "GCP project for Vertex AI")
	rootCmd.PersistentFlags().String("location", "", "GCP location for Vertex AI")
	rootCmd.PersistentFlags().String("model", "", "Model tier to use: flash or pro")

	// Bind flags to viper - use panic since the application can't function properly without flag binding
	bindFlag := func(key, flag string) {
		if err := viper.BindPFlag(key, rootCmd.PersistentFlags().Lookup(flag)); err != nil {
			panic(fmt.Sprintf("failed to bind %s flag: %v", flag, err))
		}
	}
	bindFlag("backend", "backend")
	bindFlag("gemini.api_key", "api-key")
	bindFlag("vertex.project", "project")
	bindFlag("vertex.location", "location")
	bindFlag("model", "model")

	// Add subcommands
	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newBrowserCmd())
	rootCmd.AddCommand(newTestCmd())
	rootCmd.AddCommand(newUpdateCmd())
	rootCmd.AddCommand(newTestCmdEnvCmd())
	rootCmd.AddCommand(newMCPCmd())
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	}
}
