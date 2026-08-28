package agent

import (
	"context"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

// A failing assertion means the script is right and the application is broken.
// If a script had to pass before being trusted, every run while the app stayed
// broken would pay for a full model compile — the most expensive possible
// response to the most ordinary event this design exists to detect.
func TestABrokenApplicationDoesNotForceARecompile(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	failing := `atr.step(1, "Check the heading", () => {
		expect(atr.text("#heading")).toBe("Something the app does not say");
	});`

	client := &scriptedClient{replies: []string{jsBlock(failing)}}
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

	first, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if first.Passed() {
		t.Fatal("the script was supposed to fail its assertion")
	}
	if first.ModelCalls != 1 {
		t.Errorf("first run made %d model calls, want 1 (the compile)", first.ModelCalls)
	}

	// The script ran, so it is trusted even though the assertion did not hold.
	// The scripted client has no replies left: a recompile would error.
	second, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("second run recompiled a script that runs perfectly well: %v", err)
	}
	if second.Compiled {
		t.Error("a broken application caused a recompile")
	}
	if second.ModelCalls != 0 {
		t.Errorf("second run made %d model calls, want 0", second.ModelCalls)
	}
}

// A script that cannot run is the one case worth compiling again: it was
// stamped before it had ever executed, so it used to be replayed for ever.
func TestAScriptThatCannotRunIsCompiledAgain(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	broken := `this is not javascript at all (((`
	good := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`

	client := &scriptedClient{replies: []string{jsBlock(broken), jsBlock(good)}}
	a := newRunAgent(t, client)

	req := RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		NoRepair:      true,
		Reset: func(ctx context.Context) error {
			return b.Navigate(ctx, url)
		},
	}

	first, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if first.Passed() {
		t.Fatal("a script that is not JavaScript should not pass")
	}

	// It never ran, so it must not be trusted.
	stored, err := testscript.Load(specPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !stored.Unverified {
		t.Error("a script that could not run was marked as verified")
	}
	if stored.Fresh(sampleSpec) {
		t.Error("a script that could not run is reported fresh, so it would be replayed for ever")
	}
}
