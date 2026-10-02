package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// A hand-written script is replayed whatever the specification says, because
// nothing ties the two together: the spec may have moved on, or never matched.
// But when such a script failed, the triage prompt still held the failure up
// against the specification — "test_failure: the application genuinely does
// not do what the specification requires" — and that verdict is terminal. A
// drifted script beside a diverged spec could be reported as a regression in
// the application: exit 1, for something this test never checked.

// recordingClient is a scriptedClient that keeps what it was sent.
type recordingClient struct {
	scriptedClient
	sent [][]llm.Message
}

func (c *recordingClient) Chat(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	c.mu.Lock()
	c.sent = append(c.sent, append([]llm.Message(nil), messages...))
	c.mu.Unlock()
	return c.scriptedClient.Chat(ctx, messages, tools)
}

func (c *recordingClient) ChatWithHistory(ctx context.Context, h []llm.Message, t []llm.Tool) (*llm.Response, error) {
	return c.Chat(ctx, h, t)
}

// prompt returns the system and user text of the first request sent.
func (c *recordingClient) prompt(t *testing.T) (system, user string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		t.Fatal("nothing was sent to the model")
	}
	for _, m := range c.sent[0] {
		switch m.Role {
		case llm.RoleSystem:
			system += m.Content
		case llm.RoleUser:
			user += m.Content
		}
	}
	return system, user
}

func newRecordingAgent(t *testing.T, client *recordingClient) *Agent {
	t.Helper()
	b, _ := sharedRunBrowser(t)
	return NewCompilerAgent(CompilerConfig{
		LLMClient:     client,
		Browser:       b,
		MaxIterations: 5,
		Timeout:       60 * time.Second,
	})
}

const driftedHandWritten = `// Hand-written.
atr.step(1, "Click sign in", () => { atr.click("#button-that-moved"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });
`

// A spec that asks for something the script has never checked, and the
// application does not do.
const divergedSpec = "Test: Sign in\n\nSteps:\n1. Click sign in\n2. Verify status\n3. Verify a discount banner is shown\n"

func TestTriageOfAHandWrittenScriptIsToldWhatItIsJudging(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, divergedSpec)
	putHandWritten(t, specPath, driftedHandWritten)

	client := &recordingClient{scriptedClient: scriptedClient{replies: []string{
		verdictBlock("repaired", "the sign-in button's id changed from #button-that-moved to #submit"),
	}}}
	a := newRecordingAgent(t, client)

	if _, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: divergedSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	system, user := client.prompt(t)

	// The script is the statement of intent, and the spec is only context.
	for _, want := range []string{
		"hand-written",
		"The script, not the specification",
		"appears only in the specification",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("the triage prompt for a hand-written script does not say %q", want)
		}
	}
	// Nobody will apply a rewrite, so none is asked for.
	if !strings.Contains(system, "Do not write a rewritten script") {
		t.Error("the prompt still asks for a rewritten script that will be thrown away")
	}
	// And the script is not called something it is not.
	if strings.Contains(user, "The compiled script that failed") {
		t.Error("the prompt calls a hand-written script compiled")
	}
	if !strings.Contains(user, "The hand-written script that failed") {
		t.Error("the prompt does not label the script as hand-written")
	}
	// The spec is still shown: it is context, not nothing.
	if !strings.Contains(user, "discount banner") {
		t.Error("the specification was left out of the prompt altogether")
	}
}

// A compiled script is judged against the spec it was compiled from, exactly
// as before.
func TestTriageOfACompiledScriptIsUnchanged(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	stale := `atr.step(1, "Click sign in", () => { atr.click("#button-that-moved"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`
	if _, err := testscript.Save(specPath, sampleSpec, stale, ""); err != nil {
		t.Fatal(err)
	}

	client := &recordingClient{scriptedClient: scriptedClient{replies: []string{
		verdictBlock("unresolved", "cannot tell"),
	}}}
	a := newRecordingAgent(t, client)

	if _, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	system, user := client.prompt(t)
	if strings.Contains(system, "hand-written") || strings.Contains(user, "hand-written") {
		t.Error("the prompt for a compiled script talks about hand-written scripts")
	}
	if !strings.Contains(user, "The compiled script that failed") {
		t.Error("the prompt no longer labels a compiled script as compiled")
	}
	if !strings.Contains(system, "follow it with the complete rewritten script") {
		t.Error("the prompt for a compiled script no longer asks for the rewrite")
	}
}

// With no rewrite asked for, a diagnosis of drift arrives without one — and is
// a diagnosis, not the "said it repaired but produced no code" it would be for
// a script ATR was going to rewrite.
func TestADriftDiagnosisForAHandWrittenScriptNeedsNoCode(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, divergedSpec)
	scriptPath := putHandWritten(t, specPath, driftedHandWritten)
	before := stateOf(t, scriptPath)

	const reason = "the sign-in button's id changed from #button-that-moved to #submit"
	client := &recordingClient{scriptedClient: scriptedClient{replies: []string{
		verdictBlock("repaired", reason),
	}}}
	a := newRecordingAgent(t, client)
	progress := &progressLog{}

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: divergedSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
		Progress:      progress.add,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out.Triage == nil || out.Triage.Verdict != VerdictRepaired {
		t.Fatalf("triage = %+v, want the drift diagnosis kept", out.Triage)
	}
	if out.Triage.Reason != reason {
		t.Errorf("reason = %q, want what the agent said", out.Triage.Reason)
	}
	if out.Passed() || out.Repaired {
		t.Errorf("passed=%v repaired=%v; nothing was rewritten, so nothing can have passed", out.Passed(), out.Repaired)
	}
	// It is drift, and stays reported as drift: not the application's fault.
	if out.Result.Failure.Kind != testscript.KindNotFound {
		t.Errorf("kind = %q, want %q", out.Result.Failure.Kind, testscript.KindNotFound)
	}
	if !progress.contains(reason) || !progress.contains("hand-written") {
		t.Errorf("the progress output does not carry the diagnosis:\n%s", progress)
	}
	wantUntouched(t, scriptPath, before)
}

// The verdict that the application is broken is still honoured — it is only
// what it is judged against that changed.
func TestATestFailureVerdictForAHandWrittenScriptStillCounts(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)
	scriptPath := putHandWritten(t, specPath, driftedHandWritten)
	before := stateOf(t, scriptPath)

	client := &recordingClient{scriptedClient: scriptedClient{replies: []string{
		verdictBlock("test_failure", "there is no way to sign in on this page any more"),
	}}}
	a := newRecordingAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !out.Result.Failure.Kind.IsTestFailure() {
		t.Errorf("kind = %q, want the agent's verdict to make it a test failure", out.Result.Failure.Kind)
	}
	wantUntouched(t, scriptPath, before)
}

// The note itself.
func TestHandWrittenNote(t *testing.T) {
	if handWrittenNote(false) != "" {
		t.Error("a compiled script gets a hand-written note")
	}

	note := handWrittenNote(true)
	for _, want := range []string{
		"hand-written",
		// What to judge against.
		"The script, not the specification",
		// What not to conclude.
		"appears only in the specification",
		// What to do when it cannot tell.
		"unresolved",
		// No rewrite, and where the diagnosis goes instead.
		"Do not write a rewritten script",
		"reason",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not say %q:\n%s", want, note)
		}
	}
}
