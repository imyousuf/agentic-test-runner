package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

// A compile that produces a script asserting nothing is the false pass in its
// purest form: it costs a full model run, writes a committed file, replays
// green for ever, and tests nothing. It has to be refused, and refused without
// spending a second model call trying to talk the model into assertions it
// would only invent.
func TestAScriptThatCannotFailIsRefused(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	// Every step acts; nothing asserts.
	toothless := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { atr.log(atr.text("#status")); });`

	client := &scriptedClient{replies: []string{jsBlock(toothless)}}
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

	if err == nil {
		t.Fatal("a script that asserts nothing was accepted")
	}
	if !errors.Is(err, ErrScriptCannotFail) {
		t.Errorf("error = %v, want it to wrap ErrScriptCannotFail", err)
	}
	if outcome.Result != nil {
		t.Error("the script was run anyway; nothing can be learned from running it")
	}
	if client.callCount() != 1 {
		t.Errorf("made %d model calls, want 1 — the findings must not be handed back to the model",
			client.callCount())
	}

	// The expensive part of the work is still on disk: it is a draft, so it
	// will not be replayed, but throwing it away helps nobody who is about to
	// look at why it came out toothless.
	stored, loadErr := testscript.Load(specPath)
	if loadErr != nil || stored == nil {
		t.Fatalf("the compiled draft was not written: %v", loadErr)
	}
	if !stored.Unverified {
		t.Error("a refused script was stamped as verified")
	}

	// The message has to say what to do, and where.
	if !strings.Contains(err.Error(), specPath) {
		t.Errorf("the error does not name the spec to edit: %v", err)
	}
}

// The check can turn a suite red the first time it ships, so there has to be a
// way to land it gradually — reported, not enforced.
func TestLintWarnReportsAndRunsAnyway(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	toothless := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { atr.log(atr.text("#status")); });`

	client := &scriptedClient{replies: []string{jsBlock(toothless)}}
	a := newRunAgent(t, client)

	var progress []string
	outcome, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Lint:          LintModeWarn,
		Progress:      func(msg string) { progress = append(progress, msg) },
		Reset: func(ctx context.Context) error {
			return b.Navigate(ctx, url)
		},
	})
	if err != nil {
		t.Fatalf("warn mode refused the run: %v", err)
	}
	if !outcome.Passed() {
		t.Fatalf("the script did not run: %v", outcome.Result)
	}
	if len(outcome.Lint) == 0 {
		t.Error("warn mode reported no findings, so nobody learns the test is toothless")
	}

	var reported bool
	for _, line := range progress {
		if strings.HasPrefix(line, "lint:") {
			reported = true
		}
	}
	if !reported {
		t.Error("the findings were never shown")
	}
}

// A script with assertions is not the lint's business, and the check must not
// cost a run that was already correct anything.
func TestAnAssertingScriptIsUntouched(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	good := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`

	client := &scriptedClient{replies: []string{jsBlock(good)}}
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
		t.Fatalf("failed: %v", outcome.Result.Failure)
	}
	if len(outcome.Lint) != 0 {
		t.Errorf("findings on a sound script: %v", outcome.Lint)
	}
}

// off is an escape hatch, not a default that drifts into place.
func TestLintOffSkipsTheCheckEntirely(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	toothless := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { atr.log(atr.text("#status")); });`

	client := &scriptedClient{replies: []string{jsBlock(toothless)}}
	a := newRunAgent(t, client)

	outcome, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Lint:          LintModeOff,
		Reset: func(ctx context.Context) error {
			return b.Navigate(ctx, url)
		},
	})
	if err != nil {
		t.Fatalf("off mode refused the run: %v", err)
	}
	if !outcome.Passed() {
		t.Fatalf("the script did not run: %v", outcome.Result)
	}
	if len(outcome.Lint) != 0 {
		t.Errorf("off mode still linted: %v", outcome.Lint)
	}
}

// Compiling generates a script; triage only classifies one. Conflating them
// meant a CI job could never learn that its red run was the application
// breaking rather than the box being slow — so it reported a regression as
// infrastructure and retried it.
func TestNoCompileStillGetsAVerdict(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	// Waits for a state the page never reaches: a timeout by classification,
	// a broken application in fact.
	waiting := `atr.step(1, "Wait for the confirmation", () => {
	atr.waitForText("Order placed", {timeout: 300});
	expect(atr.text("#status")).toBe("Order placed");
});`
	if _, err := testscript.Save(specPath, sampleSpec, waiting, ""); err != nil {
		t.Fatal(err)
	}

	client := &scriptedClient{replies: []string{
		verdictBlock("test_failure", "the page never reaches the confirmed state"),
	}}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		NoCompile:     true,
		MaxRetries:    1,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out.Result.Failure.Kind != testscript.KindAssertion {
		t.Errorf("kind = %q, want %q — CI would report a broken feature as infrastructure",
			out.Result.Failure.Kind, testscript.KindAssertion)
	}
	if client.callCount() != 1 {
		t.Errorf("spent %d model calls; a verdict on an already-red run is worth exactly one",
			client.callCount())
	}
}

// --no-triage keeps the old guarantee for anyone who wants it absolutely.
func TestNoTriageSpendsNothing(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	waiting := `atr.step(1, "Wait", () => {
	atr.waitForText("never", {timeout: 300});
	expect(atr.text("#status")).toBe("never");
});`
	if _, err := testscript.Save(specPath, sampleSpec, waiting, ""); err != nil {
		t.Fatal(err)
	}

	// No replies at all: any model call fails the test loudly.
	client := &scriptedClient{}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		NoCompile:     true,
		NoTriage:      true,
		MaxRetries:    1,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if client.callCount() != 0 {
		t.Errorf("--no-triage spent %d model calls", client.callCount())
	}
	if out.Result.Failure.Kind != testscript.KindTimeout {
		t.Errorf("kind = %q, want the runtime's own guess", out.Result.Failure.Kind)
	}
}

// CI asked for a replay. A script rewritten on a machine nobody is watching is
// a change nobody reviewed, so a verdict of "repaired" must not be applied
// even though the verdict itself is now allowed.
func TestNoCompileRefusesTheRewriteItAsksAbout(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	stale := `atr.step(1, "Click", () => { atr.click("#gone"); });
atr.step(2, "Verify", () => { expect(atr.text("#status")).toBe("signed in"); });`
	if _, err := testscript.Save(specPath, sampleSpec, stale, ""); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(testscript.ScriptPath(specPath))
	if err != nil {
		t.Fatal(err)
	}

	repaired := `atr.step(1, "Click", () => { atr.click("#submit"); });
atr.step(2, "Verify", () => { expect(atr.text("#status")).toBe("signed in"); });`
	client := &scriptedClient{replies: []string{
		verdictBlock("repaired", "the button was renamed") + jsBlock(repaired),
	}}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		NoCompile:     true,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out.Repaired {
		t.Error("--no-compile applied a repair")
	}
	after, err := os.ReadFile(testscript.ScriptPath(specPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("--no-compile rewrote a committed script")
	}
	if out.Triage == nil {
		t.Error("the diagnosis was not kept, so nobody learns what the agent found")
	}
}

// The repair budget bounds rewrites, and it used to be checked only for
// repairable kinds — which left it applying to almost nothing. A timeout is
// not repairable, so a triage that kept answering "repaired" rewrote the
// committed script and asked again: twelve rewrites and thirteen model calls
// against a budget of one, on a run nobody was watching.
func TestTheRepairBudgetHoldsWhateverTheFailureKind(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	// A timeout: not repairable, and so previously outside the budget.
	waiting := `atr.step(1, "Wait for something that never arrives", () => {
	atr.waitForText("nope-nope-nope", {timeout: 200});
});
atr.step(2, "Check", () => { expect(atr.text("#status")).toBe("idle"); });`
	if _, err := testscript.Save(specPath, sampleSpec, waiting, ""); err != nil {
		t.Fatal(err)
	}

	// The agent answers "repaired" every time it is asked. Nothing about the
	// verdict bounds the loop; only the budget can.
	replies := make([]string, 12)
	for i := range replies {
		replies[i] = verdictBlock("repaired", "try again") + jsBlock(waiting)
	}
	client := &scriptedClient{replies: replies}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		MaxRepairs:    1,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if client.callCount() > 2 {
		t.Errorf("the agent was asked %d times against a budget of 1", client.callCount())
	}
	if out.ModelCalls > 2 {
		t.Errorf("the run recorded %d model calls against a budget of 1", out.ModelCalls)
	}
}
