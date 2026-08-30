package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/api"
	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/testscript"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

var refactorDryRun bool

func newRefactorOpsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refactor-ops <directory>",
		Short: "Hoist repeated operations into a directory's shared library",
		Long: `Hoist the operations a directory's specs keep repeating into _shared.js.

Compiling a spec re-derives whatever the application makes it re-derive, so
several specs in one directory end up carrying their own copy of the same
sign-in. This finds those sequences, names them once, rewrites the scripts to
call them, and proves the rewrites before keeping them.

Nothing is kept unless all of it holds:

  - the library declares operations only, and runs nothing at load time
  - every rewritten script still lints
  - every rewritten script claims exactly what it claimed before, character
    for character — an extraction may move a click, never an assertion
  - every rewritten script still passes against the live application

Fail any of those and every file goes back exactly as it was.

A compile does this on its own unless behavior.extract_operations is set to
"on-demand" or "off", which is when this command is the way to run it.`,
		Args: cobra.ExactArgs(1),
		RunE: runRefactorOps,
	}

	cmd.Flags().BoolVar(&refactorDryRun, "dry-run", false,
		"Report what would be hoisted and change nothing")
	cmd.Flags().StringVar(&browserURLFlag, "browser-url", "", "Base URL for the verification replays")
	cmd.Flags().BoolVar(&headlessFlag, "headless", false, "Run the browser headless")

	return cmd
}

func runRefactorOps(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	dir := args[0]
	info, err := os.Stat(dir)
	if err != nil {
		return exitWith(ExitInfra, fmt.Errorf("reading %s: %w", dir, err))
	}
	if !info.IsDir() {
		return exitWith(ExitInfra, fmt.Errorf("%s is not a directory; refactoring is per directory, "+
			"because a shared library is", dir))
	}

	specs, err := collectTestFiles(dir)
	if err != nil {
		return exitWith(ExitInfra, err)
	}
	if len(specs) < 2 {
		fmt.Printf("%s has %d spec(s); there is nothing to share between them.\n", dir, len(specs))
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return exitWith(ExitInfra, fmt.Errorf("loading config: %w", err))
	}
	if err := cfg.ValidateForLLM(); err != nil {
		return err
	}

	mode := agent.ExtractAlways
	if refactorDryRun {
		mode = agent.ExtractOnDemand
	}

	// A dry run reads scripts and never drives the browser, so it needs
	// neither a browser nor a base URL.
	if refactorDryRun {
		return reportOverlaps(specs)
	}

	baseURL, err := refactorBaseURL(ctx, cfg, specs)
	if err != nil {
		return exitWith(ExitInfra, err)
	}

	b, llmClient, cleanup, err := browserAndModel(ctx, cfg)
	if err != nil {
		return exitWith(ExitInfra, err)
	}
	defer cleanup()

	ag := agent.NewCompilerAgent(agent.CompilerConfig{
		LLMClient:     llmClient,
		Browser:       b,
		MaxIterations: cfg.Agent.MaxIterations,
		Timeout:       cfg.Agent.Timeout,
		Verbose:       verbose,
	})

	outcome, err := ag.RefactorOperations(ctx, agent.RefactorRequest{
		Specs:         specs,
		Mode:          mode,
		BaseURL:       baseURL,
		ScriptTimeout: cfg.Agent.Timeout,
		SecretFiller:  behaviorSecretFiller(b, cfg),
		Reset: func(ctx context.Context) error {
			if baseURL == "" {
				return nil
			}
			return b.Navigate(ctx, baseURL)
		},
		Progress: func(msg string) { fmt.Fprintf(os.Stderr, "  %s\n", msg) },
	})
	if err != nil {
		return exitWith(ExitInfra, err)
	}

	printRefactorOutcome(dir, outcome)
	return nil
}

// reportOverlaps says what could be hoisted without opening a browser.
func reportOverlaps(specs []string) error {
	scripts := map[string]string{}
	for _, spec := range specs {
		stored, err := testscript.Load(spec)
		if err != nil {
			return exitWith(ExitInfra, err)
		}
		if stored != nil {
			scripts[stored.Path] = stored.Source
		}
	}

	overlaps, err := testscript.FindOverlaps(scripts)
	if err != nil {
		return exitWith(ExitInfra, err)
	}
	if len(overlaps) == 0 {
		fmt.Println("Nothing is repeated across these scripts.")
		return nil
	}

	for _, o := range overlaps {
		names := make([]string, 0, len(o.Scripts))
		for _, p := range o.Scripts {
			names = append(names, filepath.Base(p))
		}
		fmt.Printf("\n%d operations repeated across %s:\n", len(o.Steps), strings.Join(names, ", "))
		for _, step := range o.Steps {
			fmt.Printf("    %s\n", step)
		}
	}
	fmt.Println("\nRun without --dry-run to hoist these into a shared operation.")
	return nil
}

func printRefactorOutcome(dir string, o *agent.RefactorOutcome) {
	switch {
	case len(o.Overlaps) == 0:
		fmt.Printf("Nothing is repeated across the scripts in %s.\n", dir)
	case o.Applied:
		fmt.Printf("\nHoisted into %s and verified.\n", testscript.LibraryName)
		if o.Reason != "" {
			fmt.Printf("  %s\n", o.Reason)
		}
		for _, p := range o.Changed {
			fmt.Printf("  rewrote → %s\n", filepath.Base(p))
		}
		fmt.Printf("  %d model call(s)\n", o.ModelCalls)
		fmt.Println("\nThe rewritten scripts assert exactly what they asserted before, and\n" +
			"each one was replayed against the application before being kept.")
	case o.RolledBack:
		fmt.Printf("\nAn extraction was proposed but did not verify, so every file was put back.\n")
	default:
		fmt.Printf("\nFound %d repeated sequence(s) but hoisted nothing.\n", len(o.Overlaps))
		if o.Reason != "" {
			fmt.Printf("  %s\n", o.Reason)
		}
	}
}

// refactorBaseURL finds where the verification replays should point.
func refactorBaseURL(ctx context.Context, cfg *config.Config, specs []string) (string, error) {
	if browserURLFlag != "" {
		return browserURLFlag, nil
	}
	for _, spec := range specs {
		v, err := testscript.LoadValues(spec)
		if err != nil {
			continue
		}
		if base, ok, err := v.Resolve(ctx, "base_url"); err == nil && ok && base != "" {
			return base, nil
		}
	}
	if cfg.Behavior.BaseURL != "" {
		return cfg.Behavior.BaseURL, nil
	}
	return "", fmt.Errorf("no base URL: the rewritten scripts have to be replayed against the " +
		"application before they can be kept, so set base_url or pass --browser-url")
}

// browserAndModel opens what a refactor needs to verify its own work: a
// browser to replay the rewritten scripts in, and a model to propose them.
//
// Deliberately simpler than the one `atr run` builds. A refactor never
// compiles, so it does not reuse a running daemon's tabs or publish state for
// one — it opens a browser of its own, replays into it, and closes it.
func browserAndModel(ctx context.Context, cfg *config.Config) (*browser.Browser, llm.Client, func(), error) {
	b, err := browser.NewForCLI(cfg.Behavior.Browser, browser.CLIOptions{
		Headless: headlessFlag,
		Sandbox:  sandboxFlag,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating the browser: %w", err)
	}

	cdpTarget := cdpEndpointFlag
	if cdpTarget == "" {
		if state, err := api.GetRunningState(); err == nil && state != nil && state.CDPEndpoint != "" {
			cdpTarget = state.CDPEndpoint
		}
	}
	if err := b.LaunchOrConnect(ctx, cdpTarget); err != nil {
		return nil, nil, nil, fmt.Errorf("starting the browser: %w", err)
	}

	llmCfg := cfg.GetLLMConfig()
	llmCfg.Verbose = verbose
	client, err := llm.NewClient(ctx, llmCfg)
	if err != nil {
		b.Close()
		return nil, nil, nil, fmt.Errorf("creating the LLM client: %w", err)
	}

	return b, client, func() {
		client.Close()
		b.Close()
	}, nil
}

// runExtraction hoists what a run's compiles kept re-deriving.
//
// Returns nil when there is nothing to say — no repetition, extraction turned
// off, or a run that compiled nothing. A replay-only run reaches here with
// every script already on disk and finds whatever was left duplicated by
// earlier compiles, which is the right time to notice it.
//
// A failure here is reported and never fatal. The specs the user asked to run
// have already run; refusing to report their result because a refactor of
// something else went wrong would be answering a question nobody asked.
func runExtraction(
	ctx context.Context,
	cfg *config.Config,
	ag *agent.Agent,
	specs []string,
	baseURL string,
	b *browser.Browser,
) *agent.RefactorOutcome {
	mode := agent.ExtractionMode(cfg.Behavior.ExtractOperations)
	switch mode {
	case agent.ExtractAlways, agent.ExtractOnDemand, agent.ExtractOff:
	default:
		mode = agent.ExtractAlways
	}
	if noExtractFlag {
		mode = agent.ExtractOff
	}
	if mode == agent.ExtractOff || len(specs) < 2 {
		return nil
	}

	outcome, err := ag.RefactorOperations(ctx, agent.RefactorRequest{
		Specs:         specs,
		Mode:          mode,
		BaseURL:       baseURL,
		ScriptTimeout: cfg.Agent.Timeout,
		SecretFiller:  behaviorSecretFiller(b, cfg),
		Reset: func(ctx context.Context) error {
			if baseURL == "" {
				return nil
			}
			return b.Navigate(ctx, baseURL)
		},
		Progress: func(msg string) { fmt.Fprintf(os.Stderr, "  %s\n", msg) },
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not hoist shared operations: %v\n", err)
		return nil
	}
	if len(outcome.Overlaps) == 0 {
		return nil
	}
	return outcome
}
