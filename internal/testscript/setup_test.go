package testscript

import (
	"strings"
	"testing"
)

// A test that consumes its own precondition has to be able to rebuild it.
// Setup runs before the steps, so the script is replayable rather than
// working once and failing ever after.
func TestSetupRunsBeforeTheSteps(t *testing.T) {
	res := run(t, `
		atr.setup("prepare the page", () => {
			atr.eval("window.__fixture = 'ready'");
		});
		atr.step(1, "Use what setup prepared", () => {
			expect(atr.eval("window.__fixture")).toBe("ready");
		});
	`)

	if !res.Passed {
		t.Fatalf("setup did not run before the step: %v", res.Failure)
	}
}

// Building a fixture is not something the specification claims about the
// application, so it must not appear as a numbered step.
func TestSetupIsNotAStep(t *testing.T) {
	res := run(t, `
		atr.setup("prepare", () => { atr.eval("1"); });
		atr.step(1, "Only step", () => { atr.eval("1"); });
	`)

	if !res.Passed {
		t.Fatalf("run failed: %v", res.Failure)
	}
	if len(res.Steps) != 1 {
		t.Errorf("got %d steps, want 1 — setup should not be counted", len(res.Steps))
	}
	if res.Steps[0].Description != "Only step" {
		t.Errorf("step 1 is %q", res.Steps[0].Description)
	}
}

// A fixture that cannot be built stops the run, and says it was the setup so
// nobody reads it as the application misbehaving.
func TestAFailedSetupStopsTheRunAndSaysSo(t *testing.T) {
	res := run(t, `
		atr.setup("open the missing thing", () => {
			atr.click("#no-such-fixture-control");
		});
		atr.step(1, "Never reached", () => { atr.fail("should not run"); });
	`)

	if res.Passed {
		t.Fatal("expected the run to fail in setup")
	}
	if !strings.Contains(res.Failure.StepDesc, "setup:") {
		t.Errorf("StepDesc = %q, want it to name the setup", res.Failure.StepDesc)
	}
	if res.Failure.Kind == KindAssertion {
		t.Error("a fixture that could not be built was reported as the application misbehaving")
	}
	if len(res.Steps) != 0 {
		t.Errorf("got %d steps, want none — the steps never ran", len(res.Steps))
	}
}

// Setup runs every time the script does, which is the property that makes a
// destructive test survive the replay a compile performs straight afterwards.
func TestSetupRunsOnEveryExecution(t *testing.T) {
	const script = `
		atr.setup("count", () => {
			atr.eval("window.__runs = (window.__runs || 0) + 1");
		});
		atr.step(1, "Check", () => {
			expect(atr.eval("window.__runs")).toBe(1);
		});
	`
	// Each run() navigates afresh, so a setup that runs every time always
	// sees exactly one.
	for i := 0; i < 2; i++ {
		if res := run(t, script); !res.Passed {
			t.Fatalf("execution %d: %v", i+1, res.Failure)
		}
	}
}
