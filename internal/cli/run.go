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
	"time"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/api"
	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/executor"
	"github.com/imyousuf/agentic-test-runner/internal/output"
	"github.com/imyousuf/agentic-test-runner/internal/testscript"
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
	recompileFlag   bool
	noCompileFlag   bool
	noRepairFlag    bool
	interpretFlag   bool
	sandboxFlag     bool // opt-in to enable sandbox (default: disabled for compatibility)
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
	runCmd.Flags().BoolVar(&headlessFlag, "headless", false, "Run browser in headless mode (no visible window)")
	runCmd.Flags().BoolVar(&recompileFlag, "recompile", false,
		"Regenerate the compiled script even if it matches the spec")
	runCmd.Flags().BoolVar(&noCompileFlag, "no-compile", false,
		"Replay only; never call the model. Fails if a script is missing or stale (use in CI)")
	runCmd.Flags().BoolVar(&noRepairFlag, "no-repair", false,
		"Diagnose a drifted script but do not rewrite it")
	runCmd.Flags().BoolVar(&interpretFlag, "interpret", false,
		"Skip compilation and let the agent drive every step (slower, costs tokens per run)")
	runCmd.Flags().BoolVar(&sandboxFlag, "sandbox", false, "Enable Chrome sandbox (disabled by default for Ubuntu 23.10+ compatibility)")
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
	llmCfg.Verbose = verbose
	llmClient, err = llm.NewClient(ctx, llmCfg)
	if err != nil {
		// Log warning but continue - pattern matching fallback will be used for env detection
		fmt.Fprintf(os.Stderr, "Warning: LLM client creation failed: %v\n", err)
	}
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

	// 1. Save full output to file (always)
	outputFile, saveErr := output.SaveOutput(result.Stdout, result.Stderr, cmdFlag, cwd)
	if saveErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save output file: %v\n", saveErr)
	}

	// 2. Get LLM summary (uses configured model for concise one-line summary)
	summary, _ := output.SummarizeOutput(ctx, llmClient, result.Stdout, result.Stderr, result.ExitCode)

	// 3. Print concise summary
	fmt.Println(summary)

	// 4. Print output file path
	if outputFile != "" {
		fmt.Printf("Full output: %s\n", outputFile)
	}

	// If command succeeded, we're done
	if result.Success() {
		return nil
	}

	// Command failed - engage the agent for detailed analysis
	if result.TimedOut {
		fmt.Println("  (command timed out)")
	}
	fmt.Println("\nAnalyzing failure...")

	// Ensure we have an LLM client for agent analysis
	if llmClient == nil {
		return fmt.Errorf("LLM client not available for agent analysis - check your API key configuration")
	}

	// Display backend/model info appropriately
	if llmClient.Provider().IsCLI() {
		fmt.Printf("Using backend: %s\n\n", llmClient.Provider())
	} else {
		fmt.Printf("Using model: %s (%s)\n\n", llmClient.Model(), llmClient.Provider())
	}

	// Create and run agent
	ag := agent.New(agent.Config{
		LLMClient:     llmClient,
		Executor:      exec,
		MaxIterations: cfg.Agent.MaxIterations,
		Timeout:       cfg.Agent.Timeout,
		WorkingDir:    cwd,
		Verbose:       verbose,
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

	// Signal failure for scripting. Returned rather than exited, so the
	// deferred cleanup above actually runs.
	if analysisResult.IsFailure() {
		return exitWith(ExitTestFailure, nil)
	}

	return nil
}

// runBehaviorTest runs browser-based behavior tests.
func runBehaviorTest(ctx context.Context, cfg *config.Config, cwd string) error {
	// Collect test files
	testFiles, err := collectTestFiles(behaviorFlag)
	if err != nil {
		return exitWith(ExitInfra, fmt.Errorf("failed to collect test files: %w", err))
	}

	if len(testFiles) == 0 {
		return exitWith(ExitInfra, fmt.Errorf("no .test.txt files found in %s", behaviorFlag))
	}

	// Apply viewport override if specified
	browserCfg := cfg.Behavior.Browser
	if viewportFlag != "" {
		width, height, err := parseViewport(viewportFlag)
		if err != nil {
			return exitWith(ExitInfra, fmt.Errorf("invalid viewport: %w", err))
		}
		browserCfg.Viewport.Width = width
		browserCfg.Viewport.Height = height
	}

	// Create browser with CLI defaults (visible, no sandbox)
	b, err := browser.NewForCLI(browserCfg, browser.CLIOptions{
		Headless: headlessFlag,
		Sandbox:  sandboxFlag,
	})
	if err != nil {
		return exitWith(ExitInfra, fmt.Errorf("failed to create browser: %w", err))
	}

	// Check if a browser server is already running and reuse it.
	// This avoids Chrome SingletonLock conflicts when both use the same profile,
	// and is also the expected behavior when a user explicitly starts a server.
	reusingServer := false
	cdpTarget := cdpEndpointFlag
	if cdpTarget == "" {
		if serverState, err := api.GetRunningState(); err == nil && serverState != nil && serverState.CDPEndpoint != "" {
			cdpTarget = serverState.CDPEndpoint
			reusingServer = true
		}
	}

	// Launch or connect to browser
	if err := b.LaunchOrConnect(ctx, cdpTarget); err != nil {
		return exitWith(ExitInfra, fmt.Errorf("failed to start browser: %w", err))
	}
	defer b.Close()

	// Save browser state so MCP server can discover it.
	// Skip if reusing a server (it owns the state file) or if a server is running
	// (avoid overwriting its state even when launching a separate browser).
	if !reusingServer {
		if existingState, _ := api.GetRunningState(); existingState == nil {
			if cdpEndpoint := b.CDPEndpoint(); cdpEndpoint != "" {
				state := &api.BrowserState{
					PID:         os.Getpid(),
					Endpoint:    cdpEndpoint,
					CDPEndpoint: cdpEndpoint,
					StartedAt:   time.Now(),
				}
				if err := api.SaveState(state); err != nil {
					// Non-fatal - log warning but continue
					fmt.Fprintf(os.Stderr, "Warning: failed to save browser state: %v\n", err)
				}
				defer api.RemoveState() // Clean up when done
			}
		}
	}

	// Create LLM client
	llmCfg := cfg.GetLLMConfig()
	llmCfg.Verbose = verbose

	// For CLI backends, pass the browser's CDP endpoint so the MCP server can connect
	if llmCfg.Provider.IsCLI() && b.CDPEndpoint() != "" {
		llmCfg.CDPEndpoint = b.CDPEndpoint()
	}

	llmClient, err := llm.NewClient(ctx, llmCfg)
	if err != nil {
		return exitWith(ExitInfra, fmt.Errorf("failed to create LLM client: %w", err))
	}
	defer llmClient.Close()

	// Determine base URL
	baseURL := browserURLFlag
	if baseURL == "" {
		baseURL = cfg.Behavior.BaseURL
	}

	// Create behavior agent. The compiler agent carries the browser as well
	// as the tools, because it runs compiled scripts itself.
	ag := agent.NewCompilerAgent(agent.CompilerConfig{
		LLMClient:     llmClient,
		Browser:       b,
		MaxIterations: cfg.Agent.MaxIterations,
		Timeout:       cfg.Agent.Timeout,
		Verbose:       verbose,
	})

	// Run each test file
	var failedTests []string
	// sawTestFailure is true only when the application misbehaved. Everything
	// else — a missing input, a browser that would not start, an unreachable
	// model — is infrastructure, so a CI job can retry it rather than treat it
	// as a regression. See exitCodeFor.
	sawTestFailure := false
	for i, testFile := range testFiles {
		if len(testFiles) > 1 {
			fmt.Printf("\n[%d/%d] %s\n", i+1, len(testFiles), filepath.Base(testFile))
		}

		// Read test file content
		content, err := os.ReadFile(testFile)
		if err != nil {
			fmt.Printf("✗ Failed to read test file: %v\n", err)
			failedTests = append(failedTests, testFile)
			continue
		}

		// A test's own properties file can supply its base URL, which is what
		// lets the same spec run against localhost here and staging there
		// with no flags. Resolved per test, before the browser is pointed
		// anywhere, because each spec has its own values.
		testBaseURL := baseURL
		if testBaseURL == "" {
			if v, err := testscript.LoadValues(testFile); err == nil {
				if base, ok, err := v.Resolve(ctx, "base_url"); err == nil && ok && base != "" {
					testBaseURL = base
				}
			}
		}

		// When reusing a server, always open a new tab to isolate the test,
		// then close it afterward to leave the server's tabs untouched.
		testPageTarget := ""
		if reusingServer {
			tabURL := testBaseURL
			if tabURL == "" {
				tabURL = "about:blank"
			}
			if err := b.NewPage(ctx, tabURL); err != nil {
				fmt.Printf("✗ Failed to open test tab: %v\n", err)
				failedTests = append(failedTests, testFile)
				continue
			}
			// The tab's target id, not its index: closing a tab renumbers the
			// ones after it, so an index captured now can name one of the
			// user's own tabs by the time the test finishes.
			if pages := b.ListPages(); len(pages) > 0 {
				testPageTarget = pages[len(pages)-1].TargetID
			}
		} else if testBaseURL != "" {
			// Not reusing server: navigate to base URL in a new tab
			if err := b.NewPage(ctx, testBaseURL); err != nil {
				fmt.Printf("✗ Failed to open page: %v\n", err)
				failedTests = append(failedTests, testFile)
				continue
			}
		}

		if interpretFlag {
			// Legacy path: the agent drives every step, every run.
			result, err := ag.ExecuteBehaviorTest(ctx, &agent.BehaviorRequest{
				TestFile:    testFile,
				TestContent: string(content),
				BaseURL:     testBaseURL,
			})

			closeTestTab(b, reusingServer, testPageTarget)

			if err != nil {
				fmt.Printf("✗ Test execution failed: %v\n", err)
				failedTests = append(failedTests, testFile)
				continue
			}

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
			continue
		}

		// Compiled path: generate the script once, replay it for free after.
		outcome, err := ag.RunBehavior(ctx, agent.RunRequest{
			SpecPath:      testFile,
			Spec:          string(content),
			BaseURL:       testBaseURL,
			SecretFiller:  behaviorSecretFiller(b, cfg),
			Recompile:     recompileFlag,
			NoCompile:     noCompileFlag,
			NoRepair:      noRepairFlag,
			ScriptTimeout: cfg.Agent.Timeout,
			Reset: func(ctx context.Context) error {
				if testBaseURL == "" {
					return nil
				}
				return b.Navigate(ctx, testBaseURL)
			},
			Log: func(msg string) {
				if verbose {
					fmt.Printf("  %s\n", msg)
				}
			},
		})

		closeTestTab(b, reusingServer, testPageTarget)

		if err != nil {
			fmt.Printf("✗ %v\n", err)
			failedTests = append(failedTests, testFile)
			continue
		}

		printBehaviorOutcome(testFile, outcome)
		if !outcome.Passed() {
			failedTests = append(failedTests, testFile)
			if outcome.Result != nil && outcome.Result.Failure != nil &&
				outcome.Result.Failure.Kind.IsTestFailure() {
				sawTestFailure = true
			}
		}
	}

	// Summary (only show if multiple tests)
	if len(testFiles) > 1 {
		fmt.Printf("\nPassed: %d/%d\n", len(testFiles)-len(failedTests), len(testFiles))
		if len(failedTests) > 0 {
			fmt.Println("Failed:")
			for _, t := range failedTests {
				fmt.Printf("  - %s\n", t)
			}
		}
	}

	if len(failedTests) > 0 {
		// A real regression outranks an infrastructure problem: if any spec
		// says the application is broken, that is what the run means, even
		// when another spec could not be run at all.
		if sawTestFailure {
			return exitWith(ExitTestFailure, nil)
		}
		return exitWith(ExitInfra, nil)
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
