package agent

import (
	"context"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

// A run is not one execution, and RunOutcome used to pretend it was: Result
// was overwritten on every pass through the loop, so a run that failed,
// repaired and then passed recorded a pass and nothing else. The evidence that
// a spec keeps needing help is exactly what was being thrown away.
func TestEveryAttemptIsRecorded(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	// Compiles to a script with a bad call, which is a script fault; the
	// agent then repairs it into one that works.
	broken := `atr.step(1, "Click sign in", () => { atr.clickk("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`
	fixed := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`

	client := &scriptedClient{replies: []string{
		jsBlock(broken),
		verdictBlock("repaired", "the script called a method that does not exist") + jsBlock(fixed),
	}}
	a := newRunAgent(t, client)

	outcome, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset: func(ctx context.Context) error {
			return b.Navigate(ctx, url)
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !outcome.Passed() {
		t.Fatalf("the run did not recover: %v", outcome.Result.Failure)
	}

	if len(outcome.Attempts) != 2 {
		t.Fatalf("recorded %d attempts, want 2 — the first one is the whole point",
			len(outcome.Attempts))
	}

	first, second := outcome.Attempts[0], outcome.Attempts[1]

	if first.Passed {
		t.Error("the first attempt is recorded as passing")
	}
	if first.Kind != testscript.KindScript {
		t.Errorf("first attempt kind = %q, want %q", first.Kind, testscript.KindScript)
	}
	if first.Message == "" {
		t.Error("the first attempt's failure message was not kept")
	}
	if first.AfterRepair {
		t.Error("the first attempt is marked as following a repair")
	}

	if !second.Passed {
		t.Error("the second attempt is recorded as failing")
	}
	if !second.AfterRepair {
		t.Error("the second attempt is not marked as following a repair, so it looks like a flake")
	}
	if second.Number != 2 {
		t.Errorf("second attempt numbered %d", second.Number)
	}
	if second.Started.Before(first.Started) {
		t.Error("attempts are out of order")
	}

	// Result is still the last attempt, because that is what the printer and
	// the exit code are about.
	if outcome.Result == nil || !outcome.Result.Passed {
		t.Error("Result is no longer the last attempt")
	}
}

// A passing replay is one attempt, and it still has to be recorded — a
// database with only the failures cannot produce a failure *rate*.
func TestAPassingRunRecordsOneAttempt(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	good := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`

	client := &scriptedClient{replies: []string{jsBlock(good)}}
	a := newRunAgent(t, client)

	req := RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset: func(ctx context.Context) error {
			return b.Navigate(ctx, url)
		},
	}

	outcome, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(outcome.Attempts) != 1 {
		t.Fatalf("recorded %d attempts, want 1", len(outcome.Attempts))
	}
	if !outcome.Attempts[0].Passed || outcome.Attempts[0].Kind != "" {
		t.Errorf("a passing attempt carries a failure: %+v", outcome.Attempts[0])
	}
	if outcome.Attempts[0].Duration <= 0 {
		t.Error("the attempt has no duration, so a duration trend has nothing to plot")
	}

	// The compile is the expensive thing ATR does, and nothing measured it.
	//
	// Not asserted as strictly positive: a scripted compile does no real work,
	// and a clock with coarse granularity — Windows' is about 15ms — measures
	// it as zero. What can be checked is that it is bounded by the run it sits
	// inside, and that a replay reports nothing at all. That a compile is
	// *visible* is asserted where it matters, on the trace shape, which keys
	// off Compiled rather than off the duration.
	if outcome.CompileDuration < 0 {
		t.Errorf("compile duration = %s", outcome.CompileDuration)
	}
	if outcome.Result != nil && outcome.CompileDuration > time.Minute {
		t.Errorf("compile duration = %s, longer than this run could possibly have taken",
			outcome.CompileDuration)
	}

	// A replay is not a compile, and must not inherit its cost.
	replay, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.CompileDuration != 0 {
		t.Errorf("a replay reports a compile duration of %s", replay.CompileDuration)
	}
	if replay.ModelCalls != 0 {
		t.Errorf("a replay cost %d model calls", replay.ModelCalls)
	}
}
