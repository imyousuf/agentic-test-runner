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
	"github.com/imyousuf/agentic-test-runner/internal/history"
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
	behaviorFlag     string
	browserURLFlag   string
	headlessFlag     bool
	recompileFlag    bool
	noCompileFlag    bool
	noRepairFlag     bool
	pruneValuesFlag  bool
	lintFlag         string
	otelEndpointFlag string
	noTriageFlag     bool
	interpretFlag    bool
	sandboxFlag      bool // opt-in to enable sandbox (default: disabled for compatibility)
	viewportFlag     string
	cdpEndpointFlag  string

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

Alternatively, run browser-based behavior tests using the --behavior flag. A spec
compiles once to a sibling .js file and afterwards replays with no model in the
loop; the agent returns only to diagnose a failure. Use --no-compile to
guarantee a run costs nothing.

Exit codes:
  0  everything passed
  1  the thing under test is broken — a failed command, or a behaviour test
     whose assertion did not hold
  2  the run could not decide — a missing input, a stale or absent compiled
     script under --no-compile, a browser that would not start, an unreachable
     model

1 means the application misbehaved and nothing else, so a red build has a
single meaning. Everything that says nothing about the application is 2, which
a CI job can retry rather than escalate.`,
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
	runCmd.Flags().BoolVar(&noTriageFlag, "no-triage", false,
		"Never ask the model why a failure happened, even to classify it")
	runCmd.Flags().StringVar(&otelEndpointFlag, "otel-endpoint", "",
		"OTLP collector for run telemetry, e.g. http://localhost:4318 (overrides OTEL_EXPORTER_OTLP_ENDPOINT)")
	runCmd.Flags().StringVar(&lintFlag, "lint", string(agent.LintModeError),
		"What to do about a compiled script that cannot fail: error, warn, off")
	runCmd.Flags().BoolVar(&pruneValuesFlag, "prune-values", false,
		"Remove inputs the compiled script no longer reads (reported, not removed, without this)")
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

	// Early validation for LLM-dependent operations.
	//
	// Skipped for a run that cannot call the model: a replay under
	// --no-compile has no use for a backend, and demanding one turns "this
	// never calls the model" into "this never calls the model, but you still
	// need credentials to find that out".
	if runNeedsModel() {
		if err := cfg.ValidateForLLM(); err != nil {
			return err
		}
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
	// --interpret is nothing but model calls and --no-compile forbids them, so
	// the two cannot both be honoured. Better to say so than to let one win
	// silently and bill for it.
	if noCompileFlag && interpretFlag {
		return exitWith(ExitInfra, fmt.Errorf(
			"--interpret drives every step with the model, which --no-compile forbids; pass one or the other"))
	}

	// Checked here rather than left to default: an unrecognised value would
	// otherwise behave as "error", so a typo in --lint=of would look like it
	// disabled the check and quietly do the opposite.
	switch agent.LintMode(lintFlag) {
	case agent.LintModeError, agent.LintModeWarn, agent.LintModeOff:
	default:
		return exitWith(ExitInfra, fmt.Errorf("--lint must be error, warn or off, not %q", lintFlag))
	}

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

	llmClient := openModel(ctx, cfg, llmCfg)
	if unusable, ok := llmClient.(*llm.Unavailable); ok && unusable.Fatal {
		return exitWith(ExitInfra, fmt.Errorf("failed to create LLM client: %w", unusable.Err))
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

	recorder := openHistory(ctx, cfg)
	defer closeHistory(ctx, recorder)

	// runSpec executes one spec and fills in its run record. It reports
	// whether the spec failed; *why* it failed lives in rec.Outcome, so the
	// exit code and the recorded history are derived from the same decision
	// and cannot drift apart.
	//
	// A closure rather than the loop body it used to be, because every early
	// exit here — an unreadable file, a missing base URL, a tab that would
	// not open — is a run that happened and used to leave no trace.
	runSpec := func(testFile string, rec *history.Run) bool {
		// Read test file content
		content, err := os.ReadFile(testFile)
		if err != nil {
			fmt.Printf("✗ Failed to read test file: %v\n", err)
			return infra(rec, "reading the spec: %v", err)
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

		// A compile has to drive the real application, so without an address
		// there is nothing to drive. Left to itself the agent navigates
		// nowhere, learns nothing, and spends its whole iteration budget
		// looking — an expensive way to discover a missing setting. A replay
		// is unaffected: a compiled script may navigate to absolute URLs.
		if testBaseURL == "" && needsLiveApp(testFile, string(content)) {
			fmt.Printf("✗ %s needs a base URL to drive the application: set base_url in %s, or pass --browser-url\n",
				filepath.Base(testFile), filepath.Base(testscript.ValuesPath(testFile)))
			return infra(rec, "no base URL: set base_url in %s, or pass --browser-url",
				filepath.Base(testscript.ValuesPath(testFile)))
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
				return infra(rec, "opening a test tab: %v", err)
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
				return infra(rec, "opening a page: %v", err)
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
				return infra(rec, "interpreting the spec: %v", err)
			}

			formatter := output.NewTextFormatter()
			formatted, err := formatter.Format(result)
			if err != nil {
				fmt.Printf("✗ Failed to format output: %v\n", err)
				return infra(rec, "formatting the result: %v", err)
			}
			fmt.Println(formatted)

			if result.IsFailure() {
				rec.Outcome = history.OutcomeTestFailure
				return true
			}
			rec.Outcome = history.OutcomePassed
			return false
		}

		// Compiled path: generate the script once, replay it for free after.
		outcome, err := ag.RunBehavior(ctx, agent.RunRequest{
			SpecPath:      testFile,
			Spec:          string(content),
			BaseURL:       testBaseURL,
			SecretFiller:  behaviorSecretFiller(b, cfg),
			Recompile:     recompileFlag,
			NoCompile:     noCompileFlag,
			NoTriage:      noTriageFlag,
			Lint:          agent.LintMode(lintFlag),
			NoRepair:      noRepairFlag,
			ScriptTimeout: cfg.Agent.Timeout,
			Reset: func(ctx context.Context) error {
				if testBaseURL == "" {
					return nil
				}
				return b.Navigate(ctx, testBaseURL)
			},
			// The script's own atr.log() output: the author's tracing, on
			// request only.
			Log: func(msg string) {
				if verbose {
					fmt.Printf("  %s\n", msg)
				}
			},
			// What the runner is doing. Shown by default, on stderr so a
			// piped stdout still carries only the result, and so --json is
			// unaffected. Without this a compile printed nothing at all
			// between opening the browser and finishing, and a wedged run
			// looked exactly like a working one.
			Progress: func(msg string) {
				fmt.Fprintf(os.Stderr, "  %s\n", msg)
			},
		})

		closeTestTab(b, reusingServer, testPageTarget)

		recordOutcome(rec, outcome)

		if err != nil {
			fmt.Printf("✗ %v\n", err)
			// The outcome carries what happened up to the point it stopped —
			// a refused lint, a compile that never finished — and the error
			// says why, so keep both.
			rec.Outcome = history.OutcomeInfra
			rec.Message = err.Error()
			return true
		}

		reportUnusedValues(testFile)
		printBehaviorOutcome(testFile, outcome)
		return !outcome.Passed()
	}

	// Run each test file
	var failedTests []string
	// sawTestFailure is true only when the application misbehaved. Everything
	// else — a missing input, a browser that would not start, an unreachable
	// model — is infrastructure, so a CI job can retry it rather than treat it
	// as a regression.
	sawTestFailure := false

	for i, testFile := range testFiles {
		if len(testFiles) > 1 {
			fmt.Printf("\n[%d/%d] %s\n", i+1, len(testFiles), filepath.Base(testFile))
		}

		rec := history.Run{ID: history.NewID(), StartedAt: time.Now()}
		rec.Spec, rec.SpecPath = history.SpecIdentity(testFile)

		failed := runSpec(testFile, &rec)

		rec.FinishedAt = time.Now()
		recordRun(ctx, recorder, rec)

		if failed {
			failedTests = append(failedTests, testFile)
		}
		if rec.Outcome == history.OutcomeTestFailure {
			sawTestFailure = true
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

// runNeedsModel reports whether this invocation cannot proceed without one.
//
// Only one shape can: replaying compiled behaviour specs under --no-compile,
// where loadOrCompile refuses instead of compiling. Command analysis is
// nothing but a model call, and --interpret drives every step with one.
//
// "Can proceed without" is not "will not use": a replay that fails still wants
// a verdict on why. See openModel.
func runNeedsModel() bool {
	if behaviorFlag == "" {
		return true
	}
	return !noCompileFlag || interpretFlag
}

// openModel returns the client this run should use.
//
// A replay needs no backend, and requiring one turned "this never calls the
// model" into "this never calls the model, but you still need credentials to
// find that out". It may still *want* one: without a verdict, a failure is
// reported under whatever kind the runtime guessed, and a regression that
// presented as a timeout reads as an infrastructure problem — so CI retries a
// broken feature instead of escalating it.
//
// So a replay uses a backend if one is configured and stays silent if not. It
// is a strict improvement on refusing to look: no new requirement, and one
// model call at most, only on a run that has already gone red.
func openModel(ctx context.Context, cfg *config.Config, llmCfg llm.Config) llm.Client {
	if runNeedsModel() {
		client, err := llm.NewClient(ctx, llmCfg)
		if err != nil {
			return &llm.Unavailable{Reason: "the model could not be reached", Fatal: true, Err: err}
		}
		return client
	}

	if noTriageFlag {
		return llm.NewUnavailable("--no-triage is set")
	}

	// Nothing configured: do not go looking, so a CI replay pays neither the
	// delay of a credential lookup nor a warning about a backend it never
	// asked for.
	if err := cfg.Validate(); err != nil {
		return llm.NewUnavailable("no LLM backend is configured")
	}

	client, err := llm.NewClient(ctx, llmCfg)
	if err != nil {
		return llm.NewUnavailable("the configured backend could not be reached")
	}
	return client
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
