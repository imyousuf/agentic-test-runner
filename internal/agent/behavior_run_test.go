package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/testscript"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// scriptedClient replies with a canned sequence, so the recovery policy can
// be tested without a model. Each reply is returned once, in order.
type scriptedClient struct {
	mu      sync.Mutex
	replies []string
	calls   int
}

func (c *scriptedClient) Chat(_ context.Context, _ []llm.Message, _ []llm.Tool) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.calls >= len(c.replies) {
		return nil, fmt.Errorf("scriptedClient: unexpected call %d (only %d replies configured)", c.calls+1, len(c.replies))
	}
	reply := c.replies[c.calls]
	c.calls++
	return &llm.Response{Content: reply}, nil
}

func (c *scriptedClient) ChatWithHistory(ctx context.Context, h []llm.Message, t []llm.Tool) (*llm.Response, error) {
	return c.Chat(ctx, h, t)
}
func (c *scriptedClient) Model() string          { return "scripted" }
func (c *scriptedClient) Provider() llm.Provider { return llm.Provider("scripted") }
func (c *scriptedClient) Close() error           { return nil }
func (c *scriptedClient) callCount() int         { c.mu.Lock(); defer c.mu.Unlock(); return c.calls }

func jsBlock(src string) string {
	return "Here you go.\n\n```javascript\n" + src + "\n```\n"
}

func verdictBlock(verdict, reason string) string {
	return fmt.Sprintf("```json\n{\"verdict\": %q, \"reason\": %q}\n```\n", verdict, reason)
}

// --- fixture -----------------------------------------------------------------

var (
	runTestBrowser *browser.Browser
	runFixtureURL  string
	runFixtureOnce sync.Once
	runFixtureErr  error
)

const runFixture = `<!doctype html><html><head><title>Fixture</title></head><body>
<h1 id="heading">Welcome back</h1>
<button id="submit" type="button">Sign in</button>
<div id="status">idle</div>
<script>document.getElementById('submit').onclick = function(){
  document.getElementById('status').textContent = 'signed in';
};</script>
</body></html>`

// sharedRunBrowser launches one browser for this file, and only if a test
// actually needs it.
func sharedRunBrowser(t *testing.T) (*browser.Browser, string) {
	t.Helper()

	runFixtureOnce.Do(func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, runFixture)
		}))
		runFixtureURL = ts.URL

		b, err := browser.New(config.BrowserConfig{Headless: true, NoSandbox: true})
		if err != nil {
			runFixtureErr = err
			return
		}
		if err := b.Launch(context.Background()); err != nil {
			runFixtureErr = err
			return
		}
		if err := b.NewPage(context.Background(), runFixtureURL); err != nil {
			runFixtureErr = err
			return
		}
		runTestBrowser = b
	})

	if runFixtureErr != nil {
		t.Fatalf("launching the shared browser: %v", runFixtureErr)
	}
	return runTestBrowser, runFixtureURL
}

// newRunAgent wires an agent around the scripted client and shared browser.
func newRunAgent(t *testing.T, client *scriptedClient) *Agent {
	t.Helper()
	b, _ := sharedRunBrowser(t)

	a := NewCompilerAgent(CompilerConfig{
		LLMClient:     client,
		Browser:       b,
		MaxIterations: 5,
		Timeout:       60 * time.Second,
	})
	return a
}

// writeSpec puts a spec in a temp dir and returns its path.
func writeSpec(t *testing.T, spec string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.test.txt")
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const sampleSpec = "Test: Sign in\n\nSteps:\n1. Click sign in\n2. Verify status\n"

// --- tests -------------------------------------------------------------------

// The saving the whole feature exists for: once a script is compiled and the
// spec has not changed, a passing run must not call the model at all.
func TestPassingReplayCostsNoModelCalls(t *testing.T) {
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

	// First run compiles: one model call.
	first, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if !first.Passed() {
		t.Fatalf("first run failed: %v", first.Result.Failure)
	}
	if !first.Compiled {
		t.Error("the first run should have compiled the script")
	}
	if first.ModelCalls != 1 {
		t.Errorf("first run made %d model calls, want 1", first.ModelCalls)
	}

	// Second run replays: zero model calls. The scripted client has no
	// replies left, so any call at all would error out.
	second, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !second.Passed() {
		t.Fatalf("second run failed: %v", second.Result.Failure)
	}
	if second.Compiled {
		t.Error("the second run should have reused the compiled script")
	}
	if second.ModelCalls != 0 {
		t.Errorf("second run made %d model calls, want 0", second.ModelCalls)
	}
}

// An assertion failure means the application is wrong. Triaging it would
// spend the tokens this design exists to save, and risks the agent editing
// away the assertion that caught the regression.
func TestAssertionFailureNeverReachesTheModel(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	failing := `atr.step(1, "Check the heading", () => {
		expect(atr.text("#heading")).toBe("Something the app does not say");
	});`
	if _, err := testscript.Save(specPath, sampleSpec, failing); err != nil {
		t.Fatal(err)
	}

	// No replies at all: any model call fails the test.
	client := &scriptedClient{}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out.Passed() {
		t.Fatal("expected the test to fail")
	}
	if out.ModelCalls != 0 {
		t.Errorf("made %d model calls for an assertion failure, want 0", out.ModelCalls)
	}
	if out.Repaired {
		t.Error("an assertion failure must never be repaired")
	}
	if out.Result.Failure.Kind != testscript.KindAssertion {
		t.Errorf("kind = %q, want assertion", out.Result.Failure.Kind)
	}
}

// Drift is the case repair exists for: the spec is still satisfied, the
// script just names something that moved.
func TestDriftIsRepairedAndRerun(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	stale := `atr.step(1, "Click sign in", () => { atr.click("#button-that-moved"); });`
	if _, err := testscript.Save(specPath, sampleSpec, stale); err != nil {
		t.Fatal(err)
	}

	repaired := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`

	client := &scriptedClient{replies: []string{
		verdictBlock("repaired", "the submit button was renamed") + jsBlock(repaired),
	}}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !out.Passed() {
		t.Fatalf("expected the repaired script to pass, got %v", out.Result.Failure)
	}
	if !out.Repaired {
		t.Error("Repaired should be set")
	}
	if out.ModelCalls != 1 {
		t.Errorf("made %d model calls, want 1", out.ModelCalls)
	}

	// The repair must be persisted, or the next run pays for it again.
	stored, err := testscript.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored.Source, "#submit") {
		t.Error("the repaired script was not written back to disk")
	}
	if !stored.Fresh(sampleSpec) {
		t.Error("the saved repair should still be stamped with the spec hash")
	}
}

// When the agent inspects a drift-looking failure and finds the application
// genuinely broken, that verdict has to win over the mechanical
// classification.
func TestAgentCanOverrideDriftIntoTestFailure(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	stale := `atr.step(1, "Click sign in", () => { atr.click("#checkout-button"); });`
	if _, err := testscript.Save(specPath, sampleSpec, stale); err != nil {
		t.Fatal(err)
	}

	client := &scriptedClient{replies: []string{
		verdictBlock("test_failure", "checkout is missing from the page entirely"),
	}}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out.Passed() {
		t.Fatal("expected a failure")
	}
	if out.Repaired {
		t.Error("nothing should have been repaired")
	}
	if out.Triage == nil || out.Triage.Verdict != VerdictTestFailure {
		t.Fatalf("verdict = %v, want test_failure", out.Triage)
	}
	if !strings.Contains(out.Result.Failure.Message, "checkout is missing") {
		t.Errorf("the triage reason should be carried into the failure, got %q", out.Result.Failure.Message)
	}
}

// A repair verdict with no code is not a repair, and must not be treated as
// one.
func TestRepairWithoutCodeIsUnresolved(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	stale := `atr.step(1, "Click", () => { atr.click("#gone"); });`
	if _, err := testscript.Save(specPath, sampleSpec, stale); err != nil {
		t.Fatal(err)
	}

	client := &scriptedClient{replies: []string{
		verdictBlock("repaired", "fixed it") + "(but no code block)",
	}}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out.Passed() {
		t.Fatal("expected a failure")
	}
	if out.Repaired {
		t.Error("nothing was actually repaired")
	}
	if out.Triage.Verdict != VerdictUnresolved {
		t.Errorf("verdict = %q, want unresolved", out.Triage.Verdict)
	}
}

// --no-compile is the CI mode: never spend tokens, never accept an unreviewed
// repair.
func TestNoCompileRefusesToCompile(t *testing.T) {
	_, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	client := &scriptedClient{}
	a := newRunAgent(t, client)

	_, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		NoCompile:     true,
		ScriptTimeout: 30 * time.Second,
	})
	if err == nil {
		t.Fatal("expected an error when no script exists and --no-compile is set")
	}
	if !strings.Contains(err.Error(), "no-compile") {
		t.Errorf("the error should explain the flag, got: %v", err)
	}
	if client.callCount() != 0 {
		t.Error("--no-compile must not call the model")
	}
}

// A spec edit has to invalidate the compiled script, or the suite silently
// keeps testing the old requirements.
func TestChangedSpecForcesRecompile(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	original := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });`
	if _, err := testscript.Save(specPath, sampleSpec, original); err != nil {
		t.Fatal(err)
	}

	newSpec := sampleSpec + "3. And check the heading\n"
	recompiled := original + "\natr.step(3, \"And check the heading\", () => { expect(atr.text(\"#heading\")).toBe(\"Welcome back\"); });"

	client := &scriptedClient{replies: []string{jsBlock(recompiled)}}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          newSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !out.Compiled {
		t.Error("a changed spec should have forced a recompile")
	}
	if !out.Passed() {
		t.Fatalf("expected pass, got %v", out.Result.Failure)
	}
}

// Whitespace-only edits must not trigger a paid recompile.
func TestReflowingSpecDoesNotRecompile(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	src := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });`
	if _, err := testscript.Save(specPath, sampleSpec, src); err != nil {
		t.Fatal(err)
	}

	reflowed := strings.ReplaceAll(sampleSpec, "\n", "\n\n") + "   \n"

	client := &scriptedClient{}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath:      specPath,
		Spec:          reflowed,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Compiled {
		t.Error("a whitespace-only edit should not force a recompile")
	}
	if out.ModelCalls != 0 {
		t.Errorf("made %d model calls, want 0", out.ModelCalls)
	}
}
