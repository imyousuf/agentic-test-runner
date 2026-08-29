package testscript

import (
	"errors"
	"fmt"
	"testing"
)

// renderLater schedules a node to appear after ms milliseconds, which is how
// these tests reproduce the case the old idiom got wrong: a page that is
// slower than a branch check but faster than a person's patience.
func renderLater(id string, ms int) string {
	return fmt.Sprintf(`atr.eval("(function(){setTimeout(function(){var d=document.createElement('div');d.id='%s';d.textContent='here';document.body.appendChild(d);},%d);return true})()");`, id, ms)
}

func removeLater(id string, ms int) string {
	return fmt.Sprintf(`atr.eval("(function(){setTimeout(function(){var e=document.getElementById('%s');if(e)e.remove();},%d);return true})()");`, id, ms)
}

// The bug this replaces: expect(atr.exists(x)).toBeTruthy() gave the lookup
// exists()'s 500ms branch budget and then reported the miss through expect,
// which raises KindAssertion — terminal, never retried, never triaged. A page
// that renders in 1.2s was reported as a broken application.
func TestExpectExistsWaitsPastTheBranchBudget(t *testing.T) {
	res := run(t, `
		atr.step(1, "The node is not there yet", () => {
			`+renderLater("late", 1200)+`
			expect(atr.exists("#late")).toBeFalsy();
		});
		atr.step(2, "But it arrives", () => {
			atr.expectExists("#late");
		});
	`)

	if !res.Passed {
		t.Fatalf("expected pass, got %v", res.Failure)
	}
}

func TestExpectExistsMissIsAnAssertion(t *testing.T) {
	res := run(t, `
		atr.step(1, "Look for something that is not there", () => {
			atr.expectExists("#no-such-node", {timeout: 800});
		});
	`)

	if res.Passed {
		t.Fatal("a missing target passed")
	}
	if res.Failure.Kind != KindAssertion {
		t.Errorf("kind = %s, want %s", res.Failure.Kind, KindAssertion)
	}
	if res.Failure.Target != "#no-such-node" {
		t.Errorf("target = %q, want the selector that was missing", res.Failure.Target)
	}
}

func TestExpectMissingWaitsForRemoval(t *testing.T) {
	res := run(t, `
		atr.step(1, "The heading goes away", () => {
			`+removeLater("heading", 1000)+`
			atr.expectMissing("#heading", {timeout: 5000});
		});
	`)

	if !res.Passed {
		t.Fatalf("expected pass, got %v", res.Failure)
	}
}

func TestExpectMissingStillPresentIsAnAssertion(t *testing.T) {
	res := run(t, `
		atr.step(1, "The heading is still there", () => {
			atr.expectMissing("#heading", {timeout: 800});
		});
	`)

	if res.Passed {
		t.Fatal("a target that never went away passed")
	}
	if res.Failure.Kind != KindAssertion {
		t.Errorf("kind = %s, want %s", res.Failure.Kind, KindAssertion)
	}
}

// exists() returning a bare boolean swallowed faults as answers: a dead
// renderer, a closed page or a selector that cannot parse all read as
// "absent", and whatever the script did with that false was then reported as
// the application's behaviour.
func TestExistsDoesNotSwallowFaults(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		present bool
		fatal   FailureKind
	}{
		{"found", nil, true, ""},
		{"genuinely absent", fmt.Errorf("%w: #x", ErrElementNotFound), false, ""},
		{"selector cannot parse", fmt.Errorf("%w: #x", ErrInvalidSelector), false, KindScript},
		{"browser could not answer", errors.New("websocket closed"), false, KindEnvironment},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			present, fatal := existsOutcome(tt.err)
			if present != tt.present {
				t.Errorf("present = %v, want %v", present, tt.present)
			}
			if fatal != tt.fatal {
				t.Errorf("fatal = %q, want %q", fatal, tt.fatal)
			}
		})
	}
}

// A selector the browser refuses is a defect in the script, not an answer
// about the page — and above all it must not read as a passing absence check.
func TestExistsOnAnUnparseableSelectorIsAScriptFault(t *testing.T) {
	res := run(t, `
		atr.step(1, "Branch on a broken selector", () => {
			if (atr.exists("#a:has-text(")) { atr.fail("should not get here"); }
		});
	`)

	if res.Passed {
		t.Fatal("a selector the browser cannot parse passed as an absence")
	}
	if res.Failure.Kind != KindScript {
		t.Errorf("kind = %s, want %s", res.Failure.Kind, KindScript)
	}
}
