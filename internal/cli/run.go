package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/executor"
	"github.com/imyousuf/agentic-test-runner/internal/output"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"

	// Import to register Gemini provider
	_ "github.com/imyousuf/agentic-test-runner/internal/llm"
)

var (
	cmdFlag     string
	cwdFlag     string
	contextFlag string

	// Behavior testing flags
	behaviorFlag    string
	browserURLFlag  string
	headlessFlag    bool
	viewportFlag    string
	cdpEndpointFlag string

	// Environment flags
	pythonVenvFlag string
	nvmVersionFlag string
	noAutoEnvFlag  bool
)

func newRunCmd() *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a command and analyze if it fails, or run browser behavior tests",
		Long: `Execute the specified command and, if it fails, use an AI agent to analyze the failure.

The agent can run additional commands, read files, and search code to understand
what went wrong, then provides actionable recommendations.

Alternatively, run browser-based behavior tests using the --behavior flag. The AI agent
will read the test specification and execute it using browser automation tools.`,
		Example: `  # Run tests and analyze failures
  atr run --cmd "go test ./..." --cwd "/path/to/project"

  # With additional context
  atr run --cmd "npm test" --context "Running integration tests for auth module"

  # Using the pro model for complex issues
  atr run --cmd "make build" --model pro

  # Run browser behavior test
  atr run --behavior tests/e2e/login.test.txt

  # Run behavior test with options
  atr run --behavior tests/e2e/login.test.txt --browser-url http://localhost:3000 --headless=false

  # Run all behavior tests in a directory
  atr run --behavior tests/e2e/

  # Connect to existing browser
  atr run --behavior tests/e2e/login.test.txt --cdp-endpoint ws://localhost:9222`,
		RunE: runCommand,
	}

	// Command execution flags
	runCmd.Flags().StringVar(&cmdFlag, "cmd", "", "Command to execute")
	runCmd.Flags().StringVar(&cwdFlag, "cwd", "", "Working directory (default: current directory)")
	runCmd.Flags().StringVar(&contextFlag, "context", "", "Additional context for the AI agent")

	// Behavior testing flags
	runCmd.Flags().StringVar(&behaviorFlag, "behavior", "", "Path to .test.txt file or directory for browser behavior testing")
	runCmd.Flags().StringVar(&browserURLFlag, "browser-url", "", "Base URL for behavior tests (overrides config)")
	runCmd.Flags().BoolVar(&headlessFlag, "headless", true, "Run browser in headless mode")
	runCmd.Flags().StringVar(&viewportFlag, "viewport", "", "Viewport size (e.g., 1920x1080)")
	runCmd.Flags().StringVar(&cdpEndpointFlag, "cdp-endpoint", "", "Connect to existing browser via CDP endpoint")

	// Environment flags (for --cmd mode)
	runCmd.Flags().StringVar(&pythonVenvFlag, "python-venv", "", "Path to Python virtual environment to activate")
	runCmd.Flags().StringVar(&nvmVersionFlag, "nvm-version", "", "Node.js version to use via nvm (e.g., '18' or '18.17.0')")
	runCmd.Flags().BoolVar(&noAutoEnvFlag, "no-auto-env", false, "Disable automatic environment detection")

	return runCmd
}

func runCommand(cmd *cobra.Command, args []string) error {
	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, shutting down...")
		cancel()
	}()

	// Validate flags - must have either --cmd or --behavior
	if cmdFlag == "" && behaviorFlag == "" {
		return fmt.Errorf("must specify either --cmd or --behavior flag")
	}
	if cmdFlag != "" && behaviorFlag != "" {
		return fmt.Errorf("cannot specify both --cmd and --behavior flags")
	}

	// Default to current directory
	cwd := cwdFlag
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

	// Early validation for LLM-dependent operations
	if err := cfg.ValidateForLLM(); err != nil {
		return err
	}

	// Route to appropriate handler
	if behaviorFlag != "" {
		return runBehaviorTest(ctx, cfg, cwd)
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
	if pythonVenvFlag != "" {
		envConfig.PythonVenvPath = pythonVenvFlag
	}
	if nvmVersionFlag != "" {
		envConfig.NodeVersion = nvmVersionFlag
	}
	if noAutoEnvFlag {
		envConfig.AutoDetect = false
	}

	// Create LLM client for environment detection and agent (may be nil if config invalid)
	var llmClient llm.Client
	llmCfg := cfg.GetLLMConfig()
	llmClient, _ = llm.NewClient(ctx, llmCfg)
	// Note: we don't fail here if LLM client creation fails - we'll use pattern matching fallback
	if llmClient != nil {
		defer llmClient.Close()
	}

	// Create executor with optional LLM client
	exec := executor.New(&executor.Config{
		CommandTimeout: cfg.Executor.CommandTimeout,
		MaxOutputSize:  cfg.Executor.MaxOutputSize,
		Environment:    envConfig,
		LLMClient:      llmClient,
	})

	// Execute the command
	fmt.Printf("Executing: %s\n", cmdFlag)
	fmt.Printf("Directory: %s\n", cwd)
	fmt.Printf("Shell: %s\n\n", exec.Shell())

	result, err := exec.Execute(ctx, cmdFlag, cwd)
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	// Print immediate output
	if result.Stdout != "" {
		fmt.Println(result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprintln(os.Stderr, result.Stderr)
	}

	// If command succeeded, we're done
	if result.Success() {
		fmt.Printf("\n✓ Command completed successfully (exit code: %d, duration: %s)\n", result.ExitCode, result.Duration)
		return nil
	}

	// Command failed - engage the agent
	fmt.Printf("\n✗ Command failed (exit code: %d, duration: %s)\n", result.ExitCode, result.Duration)
	if result.TimedOut {
		fmt.Println("  (command timed out)")
	}
	fmt.Println("\nAnalyzing failure with AI agent...")

	// Ensure we have an LLM client for agent analysis
	if llmClient == nil {
		return fmt.Errorf("LLM client not available for agent analysis - check your API key configuration")
	}

	fmt.Printf("Using model: %s (%s)\n\n", llmClient.Model(), llmClient.Provider())

	// Create and run agent
	ag := agent.New(agent.Config{
		LLMClient:     llmClient,
		Executor:      exec,
		MaxIterations: cfg.Agent.MaxIterations,
		Timeout:       cfg.Agent.Timeout,
		WorkingDir:    cwd,
	})

	analysisResult, err := ag.AnalyzeFailure(ctx, &agent.AnalysisRequest{
		Command:    cmdFlag,
		WorkingDir: cwd,
		ExitCode:   result.ExitCode,
		Duration:   result.Duration,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Context:    contextFlag,
		TimedOut:   result.TimedOut,
	})
	if err != nil {
		return fmt.Errorf("agent analysis failed: %w", err)
	}

	// Format and print output
	formatter := output.NewTextFormatter()
	formatted, err := formatter.Format(analysisResult)
	if err != nil {
		return fmt.Errorf("failed to format output: %w", err)
	}

	fmt.Println(formatted)

	// Return error if command failed (for scripting)
	if analysisResult.IsFailure() {
		os.Exit(1)
	}

	return nil
}

// runBehaviorTest runs browser-based behavior tests.
func runBehaviorTest(ctx context.Context, cfg *config.Config, cwd string) error {
	// Collect test files
	testFiles, err := collectTestFiles(behaviorFlag)
	if err != nil {
		return fmt.Errorf("failed to collect test files: %w", err)
	}

	if len(testFiles) == 0 {
		return fmt.Errorf("no .test.txt files found in %s", behaviorFlag)
	}

	fmt.Printf("Found %d behavior test(s)\n\n", len(testFiles))

	// Apply CLI overrides to config
	browserCfg := cfg.Behavior.Browser
	if !headlessFlag {
		browserCfg.Headless = false
	}
	if viewportFlag != "" {
		width, height, err := parseViewport(viewportFlag)
		if err != nil {
			return fmt.Errorf("invalid viewport: %w", err)
		}
		browserCfg.Viewport.Width = width
		browserCfg.Viewport.Height = height
	}

	// Create browser
	b, err := browser.New(browserCfg)
	if err != nil {
		return fmt.Errorf("failed to create browser: %w", err)
	}

	// Launch or connect to browser
	if cdpEndpointFlag != "" {
		fmt.Printf("Connecting to browser at %s...\n", cdpEndpointFlag)
		if err := b.Connect(ctx, cdpEndpointFlag); err != nil {
			return fmt.Errorf("failed to connect to browser: %w", err)
		}
	} else {
		fmt.Println("Launching browser...")
		if err := b.Launch(ctx); err != nil {
			return fmt.Errorf("failed to launch browser: %w", err)
		}
	}
	defer b.Close()

	// Create LLM client
	llmCfg := cfg.GetLLMConfig()

	// For CLI backends, pass the browser's CDP endpoint so the MCP server can connect
	if llmCfg.Provider.IsCLI() && b.CDPEndpoint() != "" {
		llmCfg.CDPEndpoint = b.CDPEndpoint()
	}

	llmClient, err := llm.NewClient(ctx, llmCfg)
	if err != nil {
		return fmt.Errorf("failed to create LLM client: %w", err)
	}
	defer llmClient.Close()

	fmt.Printf("Using model: %s (%s)\n", llmClient.Model(), llmClient.Provider())

	// Determine base URL
	baseURL := browserURLFlag
	if baseURL == "" {
		baseURL = cfg.Behavior.BaseURL
	}

	// Create behavior agent
	ag := agent.NewBehaviorAgent(agent.BehaviorConfig{
		LLMClient:     llmClient,
		Browser:       b,
		MaxIterations: cfg.Agent.MaxIterations,
		Timeout:       cfg.Agent.Timeout,
	})

	// Run each test file
	var failedTests []string
	for i, testFile := range testFiles {
		fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("Test %d/%d: %s\n", i+1, len(testFiles), filepath.Base(testFile))
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		// Read test file content
		content, err := os.ReadFile(testFile)
		if err != nil {
			fmt.Printf("✗ Failed to read test file: %v\n", err)
			failedTests = append(failedTests, testFile)
			continue
		}

		// Navigate to base URL first if specified
		if baseURL != "" {
			fmt.Printf("Opening %s...\n", baseURL)
			if err := b.NewPage(ctx, baseURL); err != nil {
				fmt.Printf("✗ Failed to open page: %v\n", err)
				failedTests = append(failedTests, testFile)
				continue
			}
		}

		// Execute behavior test
		result, err := ag.ExecuteBehaviorTest(ctx, &agent.BehaviorRequest{
			TestFile:    testFile,
			TestContent: string(content),
			BaseURL:     baseURL,
		})
		if err != nil {
			fmt.Printf("✗ Test execution failed: %v\n", err)
			failedTests = append(failedTests, testFile)
			continue
		}

		// Format and print result
		formatter := output.NewTextFormatter()
		formatted, err := formatter.Format(result)
		if err != nil {
			fmt.Printf("✗ Failed to format output: %v\n", err)
			failedTests = append(failedTests, testFile)
			continue
		}

		fmt.Println(formatted)

		if result.IsFailure() {
			failedTests = append(failedTests, testFile)
		}
	}

	// Summary
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Summary: %d/%d tests passed\n", len(testFiles)-len(failedTests), len(testFiles))
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	if len(failedTests) > 0 {
		fmt.Println("\nFailed tests:")
		for _, t := range failedTests {
			fmt.Printf("  - %s\n", t)
		}
		os.Exit(1)
	}

	return nil
}

// collectTestFiles collects .test.txt files from a path.
func collectTestFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		// Single file
		return []string{path}, nil
	}

	// Directory - find all .test.txt files
	var files []string
	err = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(p, ".test.txt") {
			files = append(files, p)
		}
		return nil
	})

	return files, err
}

// parseViewport parses a viewport string like "1920x1080".
func parseViewport(s string) (int, int, error) {
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected format WIDTHxHEIGHT (e.g., 1920x1080)")
	}

	width, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid width: %w", err)
	}

	height, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid height: %w", err)
	}

	return width, height, nil
}
