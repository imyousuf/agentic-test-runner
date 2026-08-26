package cli

import (
	"fmt"
	"path/filepath"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/testscript"
	"github.com/imyousuf/agentic-test-runner/pkg/behavior"
)

// closeTestTab drops the isolated tab a test ran in, when one was opened.
func closeTestTab(b *browser.Browser, reusingServer bool, testPageIndex int) {
	if !reusingServer || testPageIndex < 0 || testPageIndex >= len(b.ListPages()) {
		return
	}
	b.ClosePage(testPageIndex)
	if len(b.ListPages()) > 0 {
		b.SelectPage(0)
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
