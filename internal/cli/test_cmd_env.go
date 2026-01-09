package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/executor"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"

	// Import to register Gemini provider
	_ "github.com/imyousuf/agentic-test-runner/internal/llm"
)

var (
	testCmdEnvCwdFlag        string
	testCmdEnvPythonVenvFlag string
	testCmdEnvNvmVersionFlag string
	testCmdEnvNoAutoEnvFlag  bool
)

func newTestCmdEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test-cmd-env <command>",
		Short: "Test environment detection for a command",
		Long: `Analyze a command to see which environments would be activated without running it.

This is a diagnostic command to understand how ATR will detect and apply
virtual environments (Python venv, Node.js nvm/fnm) for a given command.`,
		Example: `  # Test what environments a command would use
  atr test-cmd-env "pytest tests/"
  atr test-cmd-env "npm run build"
  atr test-cmd-env "make test"

  # Test with specific working directory
  atr test-cmd-env "pytest" --cwd /path/to/project

  # Test with manual venv override
  atr test-cmd-env "python script.py" --python-venv /path/to/.venv

  # Test with nvm version override
  atr test-cmd-env "node app.js" --nvm-version 18`,
		Args: cobra.ExactArgs(1),
		RunE: runTestCmdEnv,
	}

	cmd.Flags().StringVar(&testCmdEnvCwdFlag, "cwd", "", "Working directory for environment detection")
	cmd.Flags().StringVar(&testCmdEnvPythonVenvFlag, "python-venv", "", "Override Python venv path")
	cmd.Flags().StringVar(&testCmdEnvNvmVersionFlag, "nvm-version", "", "Override Node.js version")
	cmd.Flags().BoolVar(&testCmdEnvNoAutoEnvFlag, "no-auto-env", false, "Disable automatic environment detection")

	return cmd
}

func runTestCmdEnv(cmd *cobra.Command, args []string) error {
	command := args[0]

	// Determine working directory
	cwd := testCmdEnvCwdFlag
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Build environment config with CLI overrides
	envConfig := executor.EnvironmentConfig{
		AutoDetect:       cfg.Executor.Environment.AutoDetect,
		PythonVenvPath:   cfg.Executor.Environment.PythonVenvPath,
		CondaEnvName:     cfg.Executor.Environment.CondaEnvName,
		NodeVersion:      cfg.Executor.Environment.NodeVersion,
		DisablePythonEnv: cfg.Executor.Environment.DisablePythonEnv,
		DisableNodeEnv:   cfg.Executor.Environment.DisableNodeEnv,
		UseLLMDetection:  cfg.Executor.Environment.UseLLMDetection,
	}

	// CLI flags take precedence
	if testCmdEnvPythonVenvFlag != "" {
		envConfig.PythonVenvPath = testCmdEnvPythonVenvFlag
	}
	if testCmdEnvNvmVersionFlag != "" {
		envConfig.NodeVersion = testCmdEnvNvmVersionFlag
	}
	if testCmdEnvNoAutoEnvFlag {
		envConfig.AutoDetect = false
	}

	// Create LLM client if available (for environment detection)
	ctx := context.Background()
	var llmClient llm.Client
	if err := cfg.ValidateForLLM(); err == nil {
		llmClient, _ = llm.NewClient(ctx, cfg.GetLLMConfig())
		if llmClient != nil {
			defer llmClient.Close()
		}
	}

	// Run environment detection test
	result := executor.TestEnvironmentDetection(ctx, command, cwd, envConfig, llmClient)

	// Print results
	printTestCmdEnvResult(result, command, cwd, llmClient != nil)

	return nil
}

func printTestCmdEnvResult(result *executor.EnvironmentTestResult, command, cwd string, hasLLM bool) {
	fmt.Printf("Command: %s\n", command)
	fmt.Printf("Working directory: %s\n", cwd)
	fmt.Println()

	// Detection method
	fmt.Printf("Detection method: %s\n", result.DetectionMethod)
	if result.Needs != nil {
		fmt.Println("Analysis:")
		fmt.Printf("  needs_python: %v\n", result.Needs.NeedsPython)
		fmt.Printf("  needs_node: %v\n", result.Needs.NeedsNode)
		if result.Needs.Reasoning != "" {
			fmt.Printf("  reasoning: %s\n", result.Needs.Reasoning)
		}
	} else {
		fmt.Println("Analysis: Unable to determine environment needs")
	}
	fmt.Println()

	// Python environment status
	printPythonEnvStatus(result)

	// Node.js environment status
	printNodeEnvStatus(result)

	// Final command
	fmt.Println()
	fmt.Println("Final command would be:")
	fmt.Printf("  %s\n", result.FinalCommand)

	// Warnings
	if result.Needs != nil {
		if result.Needs.NeedsPython && !hasActivePythonEnv(result) {
			fmt.Println()
			fmt.Println("WARNING: Python environment needed but not found!")
			fmt.Println("  To specify manually, use: --python-venv /path/to/.venv")
		}
		if result.Needs.NeedsNode && !hasActiveNodeEnv(result) {
			fmt.Println()
			fmt.Println("WARNING: Node.js environment needed but not found!")
			fmt.Println("  To specify manually, use: --nvm-version 18")
		}
	}

	// LLM hint
	if !hasLLM && result.DetectionMethod == "Unknown" {
		fmt.Println()
		fmt.Println("Hint: Configure an LLM API key for better command analysis.")
		fmt.Println("  Run 'atr test' to check LLM connectivity.")
	}
}

func printPythonEnvStatus(result *executor.EnvironmentTestResult) {
	// Find Python environments in detected and active lists
	var detectedPython, activePython *executor.DetectedEnvironment
	for _, env := range result.DetectedEnvs {
		if env.Type == executor.EnvTypePythonVenv || env.Type == executor.EnvTypeConda {
			detectedPython = env
			break
		}
	}
	for _, env := range result.ActiveEnvs {
		if env.Type == executor.EnvTypePythonVenv || env.Type == executor.EnvTypeConda {
			activePython = env
			break
		}
	}

	needsPython := result.Needs != nil && result.Needs.NeedsPython
	skippedPython := result.Needs != nil && !result.Needs.NeedsPython

	fmt.Println("Python environment:")
	if activePython != nil {
		fmt.Println("  Status: FOUND (will be activated)")
		fmt.Printf("  Type: %s\n", activePython.Type)
		fmt.Printf("  Path: %s\n", activePython.Path)
		if activePython.ActivatePath != "" {
			fmt.Printf("  Activate: source %s\n", activePython.ActivatePath)
		}
	} else if skippedPython && detectedPython != nil {
		fmt.Println("  Status: SKIPPED (not needed)")
		fmt.Printf("  Found at: %s\n", detectedPython.Path)
	} else if needsPython {
		fmt.Println("  Status: NOT FOUND")
		fmt.Println("  Searched: .venv, venv, env, .env (in working directory and parents)")
	} else if result.Needs == nil {
		// Unknown needs
		if detectedPython != nil {
			fmt.Println("  Status: FOUND (will be activated - unknown if needed)")
			fmt.Printf("  Path: %s\n", detectedPython.Path)
		} else {
			fmt.Println("  Status: NOT FOUND")
		}
	} else {
		fmt.Println("  Status: NOT NEEDED")
	}
}

func printNodeEnvStatus(result *executor.EnvironmentTestResult) {
	// Find Node environments in detected and active lists
	var detectedNode, activeNode *executor.DetectedEnvironment
	for _, env := range result.DetectedEnvs {
		if env.Type == executor.EnvTypeNVM || env.Type == executor.EnvTypeFNM || env.Type == executor.EnvTypeNodeModules {
			detectedNode = env
			break
		}
	}
	for _, env := range result.ActiveEnvs {
		if env.Type == executor.EnvTypeNVM || env.Type == executor.EnvTypeFNM || env.Type == executor.EnvTypeNodeModules {
			activeNode = env
			break
		}
	}

	needsNode := result.Needs != nil && result.Needs.NeedsNode
	skippedNode := result.Needs != nil && !result.Needs.NeedsNode

	fmt.Println("Node.js environment:")
	if activeNode != nil {
		fmt.Println("  Status: FOUND (will be activated)")
		fmt.Printf("  Type: %s\n", activeNode.Type)
		if activeNode.Path != "" {
			fmt.Printf("  Path: %s\n", activeNode.Path)
		}
		if activeNode.Version != "" {
			fmt.Printf("  Version: %s\n", activeNode.Version)
		}
	} else if skippedNode && detectedNode != nil {
		fmt.Println("  Status: SKIPPED (not needed)")
		if detectedNode.Path != "" {
			fmt.Printf("  Found at: %s\n", detectedNode.Path)
		}
	} else if needsNode {
		fmt.Println("  Status: NOT FOUND")
		fmt.Println("  Searched: .nvmrc, .node-version, node_modules/.bin (in working directory and parents)")
	} else if result.Needs == nil {
		// Unknown needs
		if detectedNode != nil {
			fmt.Println("  Status: FOUND (will be activated - unknown if needed)")
			if detectedNode.Path != "" {
				fmt.Printf("  Path: %s\n", detectedNode.Path)
			}
		} else {
			fmt.Println("  Status: NOT FOUND")
		}
	} else {
		fmt.Println("  Status: NOT NEEDED")
	}
}

func hasActivePythonEnv(result *executor.EnvironmentTestResult) bool {
	for _, env := range result.ActiveEnvs {
		if env.Type == executor.EnvTypePythonVenv || env.Type == executor.EnvTypeConda {
			return true
		}
	}
	return false
}

func hasActiveNodeEnv(result *executor.EnvironmentTestResult) bool {
	for _, env := range result.ActiveEnvs {
		if env.Type == executor.EnvTypeNVM || env.Type == executor.EnvTypeFNM || env.Type == executor.EnvTypeNodeModules {
			return true
		}
	}
	return false
}
