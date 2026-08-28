package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/secret"
	"github.com/imyousuf/agentic-test-runner/internal/testscript"
	"github.com/imyousuf/agentic-test-runner/pkg/behavior"
)

// closeTestTab drops the isolated tab a test ran in, when one was opened.
//
// The tab is found by target id and closed at whatever index it holds now. An
// index captured when the tab was opened does not survive the run: any tab
// closing before this point renumbers the ones after it, and closing a stale
// index would take one of the user's own tabs instead.
func closeTestTab(b *browser.Browser, reusingServer bool, testPageTarget string) {
	if !reusingServer || testPageTarget == "" {
		return
	}
	for _, p := range b.ListPages() {
		if p.TargetID != testPageTarget {
			continue
		}
		b.ClosePage(p.Index)
		if len(b.ListPages()) > 0 {
			b.SelectPage(0)
		}
		return
	}
}

// printBehaviorOutcome reports one compiled-behaviour run.
//
// The line about how the run was serviced is not decoration: the whole point
// of compiling is that most runs cost nothing, and a user needs to be able to
// see when that stopped being true.
func printBehaviorOutcome(testFile string, outcome *agent.RunOutcome) {
	name := filepath.Base(testFile)

	switch {
	case outcome.Compiled:
		fmt.Printf("  compiled → %s\n", outcome.ScriptPath)
	case outcome.Repaired:
		fmt.Printf("  repaired → %s\n", outcome.ScriptPath)
	}
	if outcome.ValuesPath != "" {
		fmt.Printf("  inputs   → %s\n", outcome.ValuesPath)
	}

	if outcome.Result != nil {
		for _, step := range outcome.Result.Steps {
			mark := "✓"
			switch step.Status {
			case behavior.StepStatusFailed:
				mark = "✗"
			case behavior.StepStatusSkipped, behavior.StepStatusPending:
				mark = "-"
			}
			fmt.Printf("  %s %d. %s\n", mark, step.Number, step.Description)
			if step.Error != "" {
				fmt.Printf("      %s\n", step.Error)
			}
		}
	}

	if outcome.Passed() {
		fmt.Printf("✓ %s (%s, %d model call(s))\n",
			name, outcome.Result.Duration.Round(1e6), outcome.ModelCalls)
		return
	}

	fmt.Printf("✗ %s (%d model call(s))\n", name, outcome.ModelCalls)

	if outcome.Result != nil && outcome.Result.Failure != nil {
		f := outcome.Result.Failure
		fmt.Printf("  %s\n", f.Error())
		// Say what the classification means, because the right next action
		// differs completely between these.
		switch {
		case f.Kind.IsTestFailure():
			fmt.Println("  → the application did not behave as the spec requires")
		case f.Kind.Repairable():
			fmt.Println("  → the script no longer matches the page; re-run to let the agent repair it")
		case f.Kind == testscript.KindConfig:
			fmt.Println("  → this checkout is missing an input; add it to the properties file")
		case f.Kind.Retryable():
			fmt.Println("  → looks environmental rather than a real failure")
		}
	}

	if outcome.Triage != nil && outcome.Triage.Reason != "" {
		fmt.Printf("  triage: %s\n", outcome.Triage.Reason)
	}
}

// behaviorSecretFiller lets a compiled script fill a credential with
// atr.fillSecret without the value passing through the script, the compiled
// file, or any later triage prompt.
//
// The fetch and the fill happen inside one call: the vault produces the
// value, the browser types it, and nothing in between ever returns it. That
// is the same guarantee browser_fill_secret gives the HUD agent, and it is
// why a behaviour test can log in without a password appearing anywhere it
// could be committed or transmitted.
func behaviorSecretFiller(b *browser.Browser, cfg *config.Config) testscript.SecretFiller {
	vault := secret.New(cfg.Secrets)

	return func(ctx context.Context, target, ref, command string) error {
		value, err := vault.Fetch(ctx, secret.Request{Ref: ref, Command: command})
		if err != nil {
			return err
		}
		return b.Fill(ctx, target, value)
	}
}

// reportUnusedValues names inputs the compiled script no longer reads, and
// removes them when asked.
//
// Reported rather than removed by default. Repeated compiles accumulate
// aliases — one real spec ended up with seven keys for two inputs — but the
// values file is the part of a compiled test a person is expected to edit, and
// "referenced" is not decidable: a key built at run time defeats any scan, and
// deleting one a test actually reads fails on somebody else's machine as a
// missing input. So the scan reports nothing at all unless it is certain.
func reportUnusedValues(specPath string) {
	unused, err := testscript.UnreferencedKeys(specPath)
	if err != nil || len(unused) == 0 {
		return
	}

	if !pruneValuesFlag {
		fmt.Printf("  unused   → %d input(s) the script does not read: %s (remove with --prune-values)\n",
			len(unused), strings.Join(unused, ", "))
		return
	}

	removed, err := testscript.PruneValues(specPath, unused)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not prune inputs: %v\n", err)
		return
	}
	if len(removed) > 0 {
		fmt.Printf("  pruned   → %s\n", strings.Join(removed, ", "))
	}
}

// needsLiveApp reports whether this run will drive the application through the
// model, rather than replaying a script that already knows what to do.
//
// Used to decide whether a missing base URL is fatal. Compiling and
// interpreting both need somewhere to point the browser; a replay does not,
// because a compiled script may navigate to absolute addresses.
func needsLiveApp(specPath, spec string) bool {
	if interpretFlag || recompileFlag {
		return true
	}
	if noCompileFlag {
		return false
	}
	stored, err := testscript.Load(specPath)
	return err != nil || stored == nil || !stored.Fresh(spec)
}
