package agent

import (
	"strings"
	"testing"
)

// The rule these guard: if the compiler can prevent a mistake, the fix belongs
// in the binary rather than in a skill an author may never load. The prompt is
// the compiler's half of that, so a rule that has moved into it must not
// silently move back out.
func TestPromptPrescribesTheAssertionsThatWait(t *testing.T) {
	if !strings.Contains(scriptAPIReference, "atr.expectExists") {
		t.Error("the prompt does not offer expectExists, so the model cannot use it")
	}
	if !strings.Contains(scriptAPIReference, "atr.expectMissing") {
		t.Error("the prompt does not offer expectMissing, so absence is asserted through exists() again")
	}

	// The old prescription gave the lookup exists()'s 500ms branch budget and
	// then reported the miss as KindAssertion: terminal, never retried, never
	// triaged. A page rendering in 800ms was called a broken application.
	if strings.Contains(scriptAPIReference, `expect(atr.exists("...")).toBeTruthy()`) {
		t.Error("the prompt still prescribes asserting through exists()")
	}
}

func TestPromptWarnsAgainstAssertingOnWholePageText(t *testing.T) {
	if !strings.Contains(scriptAPIReference, "atr.text() with no selector") {
		t.Error("nothing tells the model that a bare atr.text() matches anything on the page")
	}
}

// exists() is the branch point for optional furniture. Calling it an assertion
// is what produced the misclassification in the first place.
func TestPromptMarksExistsAsBranchingOnly(t *testing.T) {
	line := ""
	for l := range strings.SplitSeq(scriptAPIReference, "\n") {
		if strings.Contains(l, "atr.exists(target)") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatal("atr.exists is not listed in the API reference at all")
	}
	if !strings.Contains(strings.ToUpper(line), "BRANCH") {
		t.Errorf("exists() is not marked as branching-only: %q", line)
	}
}

// A spec that asks for an operation by name — "using openFirstPost() from the
// shared library" — does not fail when no library declares it. The compiler
// writes a local function of that name, calls it, and the script passes,
// reading as though it shares code when it does not.
//
// The lint reports it afterwards; this is the half that stops it happening.
func TestPromptRefusesToInventALocalOperation(t *testing.T) {
	if !strings.Contains(scriptAPIReference, "Do not declare functions of your own") {
		t.Error("nothing tells the model to write the steps out rather than name an operation")
	}
	if !strings.Contains(scriptAPIReference, "that operation does not exist") {
		t.Error("the prompt does not say what to do when the spec names an operation no library declares")
	}
}

// The journey rule exists so that two specs making the same journey can ever
// share it: a defensive assertion between two operations means they sit either
// side of something that never moves, and no hoist can gather them.
func TestPromptKeepsAJourneyStepFreeOfItsOwnAssertions(t *testing.T) {
	if !strings.Contains(scriptAPIReference, "carries no assertions of its own") {
		t.Error("the prompt no longer says a journey step asserts nothing of its own")
	}
}
