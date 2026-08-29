package agent

import (
	"context"
	"errors"
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
