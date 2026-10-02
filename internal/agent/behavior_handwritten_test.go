package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

// A script with no atr-spec-sha256 line is hand-written, and the documented
// promise is that ATR leaves it alone. It did not: "no hash" was read as "the
// hash does not match", so a plain run announced that the spec had changed,
// spent a compile, and overwrote the file a person had taken ownership of.
// Under --no-compile it refused to run it at all, calling it stale.
//
// These hold every route by which ATR writes a script to that promise:
// compile, repair, stamp and hoist.

const handWrittenPassing = `// Hand-written: no atr-spec-sha256 line, on purpose.
atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });
`

// putHandWritten writes a script beside the spec exactly as given, with no
// header, and returns its path.
//
// The file is backdated. A filesystem stamps a write with a clock that ticks
// every few milliseconds, so a file rewritten straight after it was created
// can keep the same modification time — and "it was not written to" would
// pass for a file that was. Against a time in the past any write shows.
func putHandWritten(t *testing.T, specPath, source string) string {
	t.Helper()
	path := testscript.ScriptPath(specPath)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	longAgo := time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	return path
}

// fileState is what "left alone" means: the same bytes in the same file, not
// an identical copy renamed over it.
type fileState struct {
	content string
	modTime time.Time
}

func stateOf(t *testing.T, path string) fileState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fileState{content: string(data), modTime: info.ModTime()}
}

func wantUntouched(t *testing.T, path string, before fileState) {
	t.Helper()
	after := stateOf(t, path)
	if after.content != before.content {
		t.Errorf("the hand-written script was rewritten on disk:\n%s", after.content)
	}
	if !after.modTime.Equal(before.modTime) {
		t.Errorf("the hand-written script was written to (mtime %v → %v) although its content is the same",
			before.modTime, after.modTime)
	}
}

// progressLog collects what the runner says it is doing.
type progressLog struct{ lines []string }

func (p *progressLog) add(msg string) { p.lines = append(p.lines, msg) }
func (p *progressLog) String() string { return strings.Join(p.lines, "\n") }
func (p *progressLog) contains(sub string) bool {
	return strings.Contains(p.String(), sub)
}

// The repro from the report.
func TestAHandWrittenScriptIsReplayedNotRecompiled(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)
	scriptPath := putHandWritten(t, specPath, handWrittenPassing)
	before := stateOf(t, scriptPath)

	client := &scriptedClient{} // no replies: any model call fails the run
	a := newRunAgent(t, client)
	progress := &progressLog{}

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
		Progress:      progress.add,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Compiled || client.callCount() != 0 || out.ModelCalls != 0 {
		t.Errorf("hand-written script: compiled=%v, model calls=%d (%d counted); want neither",
			out.Compiled, client.callCount(), out.ModelCalls)
	}
	if !out.Passed() {
		t.Errorf("the hand-written script did not pass: %v", out.Result.Failure)
	}
	if !out.HandWritten {
		t.Error("the outcome does not say the script was hand-written")
	}
	if out.ScriptPath != scriptPath {
		t.Errorf("ScriptPath = %q, want %q", out.ScriptPath, scriptPath)
	}
	wantUntouched(t, scriptPath, before)

	// The run has to say why it did not compile, and must not say the one
	// thing that was untrue.
	if !progress.contains("hand-written") {
		t.Errorf("the progress output never says the script is hand-written:\n%s", progress)
	}
	if progress.contains("the spec changed") || progress.contains("recompiling") {
		t.Errorf("the progress output still talks about recompiling:\n%s", progress)
	}
}

// --no-compile means "replay what is committed". A hand-written script is the
// one script nobody can recompile, so refusing it there left no way to run it
// in CI at all — and the refusal called it stale, which it is not.
func TestAHandWrittenScriptReplaysUnderNoCompile(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)
	scriptPath := putHandWritten(t, specPath, handWrittenPassing)
	before := stateOf(t, scriptPath)

	client := &scriptedClient{}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		NoCompile:     true,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("--no-compile refused a hand-written script: %v", err)
	}
	if !out.Passed() {
		t.Errorf("the hand-written script did not pass: %v", out.Result.Failure)
	}
	if client.callCount() != 0 {
		t.Errorf("--no-compile made %d model call(s)", client.callCount())
	}
	wantUntouched(t, scriptPath, before)
}

// The spec is not consulted at all: there is no hash to hold it against, so no
// edit to the spec — any edit — can make a hand-written script "stale".
func TestEditingTheSpecDoesNotRecompileAHandWrittenScript(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)
	scriptPath := putHandWritten(t, specPath, handWrittenPassing)
	before := stateOf(t, scriptPath)

	client := &scriptedClient{}
	a := newRunAgent(t, client)

	for _, spec := range []string{
		sampleSpec,
		sampleSpec + "3. And something the script does not do\n",
		"Test: rewritten from scratch\n\nSteps:\n1. Something else entirely\n",
	} {
		out, err := a.RunBehavior(context.Background(), RunRequest{
			SpecPath: specPath, Spec: spec, BaseURL: url,
			ScriptTimeout: 30 * time.Second,
			Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
		})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if out.Compiled || !out.Passed() {
			t.Errorf("compiled=%v passed=%v; want a plain replay", out.Compiled, out.Passed())
		}
	}
	if client.callCount() != 0 {
		t.Errorf("made %d model call(s)", client.callCount())
	}
	wantUntouched(t, scriptPath, before)
}

// The repair path is the other way a script gets rewritten. A drifted
// hand-written script is still diagnosed — the verdict is worth having — but
// the rewrite the agent proposes is not applied: it would overwrite a file a
// person owns, and stamp it with a header that makes it ATR's.
func TestADriftedHandWrittenScriptIsDiagnosedButNotRewritten(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	drifted := `// Hand-written.
atr.step(1, "Click sign in", () => { atr.click("#button-that-moved"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });
`
	scriptPath := putHandWritten(t, specPath, drifted)
	before := stateOf(t, scriptPath)

	repaired := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`

	// One reply, and it is exactly the one that used to get the file
	// overwritten.
	client := &scriptedClient{replies: []string{
		verdictBlock("repaired", "the submit button was renamed") + jsBlock(repaired),
	}}
	a := newRunAgent(t, client)
	progress := &progressLog{}

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
		Progress:      progress.add,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out.Passed() {
		t.Error("the run passed, so the proposed rewrite must have been executed")
	}
	if out.Repaired {
		t.Error("Repaired is set for a script ATR may not rewrite")
	}
	if out.Result.Failure == nil || out.Result.Failure.Kind != testscript.KindNotFound {
		t.Errorf("failure = %+v, want the drift reported as it was found", out.Result.Failure)
	}
	if out.Triage == nil || out.Triage.Verdict != VerdictRepaired {
		t.Errorf("triage = %+v, want the agent's diagnosis kept", out.Triage)
	}
	if client.callCount() != 1 {
		t.Errorf("made %d model calls, want exactly the one diagnosis", client.callCount())
	}
	if len(out.Attempts) != 1 {
		t.Errorf("ran the script %d times, want once — nothing changed that could make a second run differ", len(out.Attempts))
	}
	wantUntouched(t, scriptPath, before)

	if !progress.contains("hand-written") {
		t.Errorf("the progress output does not say why the repair was not applied:\n%s", progress)
	}
}

// The same, for the other repairable kind: a script that is itself broken.
func TestABrokenHandWrittenScriptIsNotRewritten(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	broken := `// Hand-written, and it calls something that does not exist.
atr.step(1, "Click sign in", () => { atr.clickTheButton("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });
`
	scriptPath := putHandWritten(t, specPath, broken)
	before := stateOf(t, scriptPath)

	fixed := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`
	client := &scriptedClient{replies: []string{
		verdictBlock("repaired", "atr.clickTheButton is not an API") + jsBlock(fixed),
	}}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Passed() || out.Repaired {
		t.Errorf("passed=%v repaired=%v; a hand-written script must fail as written", out.Passed(), out.Repaired)
	}
	if out.Result.Failure == nil || out.Result.Failure.Kind != testscript.KindScript {
		t.Errorf("failure = %+v, want a script fault", out.Result.Failure)
	}
	wantUntouched(t, scriptPath, before)
}

// An assertion in a hand-written script is an assertion: the application is
// wrong, no model is asked, and nothing is written.
func TestAFailingAssertionInAHandWrittenScriptIsATestFailure(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	failing := `// Hand-written.
atr.step(1, "Check the heading", () => {
	expect(atr.text("#heading")).toBe("Something the app does not say");
});
`
	scriptPath := putHandWritten(t, specPath, failing)
	before := stateOf(t, scriptPath)

	client := &scriptedClient{}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Passed() {
		t.Fatal("a failing assertion passed")
	}
	if !out.Result.Failure.Kind.IsTestFailure() {
		t.Errorf("kind = %s, want a test failure", out.Result.Failure.Kind)
	}
	if client.callCount() != 0 {
		t.Errorf("made %d model call(s) for an assertion failure", client.callCount())
	}
	wantUntouched(t, scriptPath, before)
}

// A shared library beside the spec is loaded for a hand-written script as for
// any other — and is the one place a stamp would otherwise be written: the
// script has no library header, so every run sees the library as "changed".
func TestAHandWrittenScriptIsNotStampedWithTheLibrary(t *testing.T) {
	b, url := sharedRunBrowser(t)

	for _, noCompile := range []bool{false, true} {
		name := "plain run"
		if noCompile {
			name = "--no-compile"
		}
		t.Run(name, func(t *testing.T) {
			specPath := writeSpec(t, sampleSpec)
			putLibrary(t, specPath, "function signIn() { atr.click('#submit'); }\n")

			usesLibrary := `// Hand-written, calling the shared library.
atr.step(1, "Sign in", () => { signIn(); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });
`
			scriptPath := putHandWritten(t, specPath, usesLibrary)
			before := stateOf(t, scriptPath)

			client := &scriptedClient{}
			a := newRunAgent(t, client)
			progress := &progressLog{}

			out, err := a.RunBehavior(context.Background(), RunRequest{
				SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
				NoCompile:     noCompile,
				ScriptTimeout: 30 * time.Second,
				Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
				Progress:      progress.add,
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if !out.Passed() {
				t.Fatalf("the script did not pass: %v", out.Result.Failure)
			}
			wantUntouched(t, scriptPath, before)

			// Nothing will ever be stamped, so nothing should be promised.
			if progress.contains("restamp") {
				t.Errorf("the run offered to restamp a script it never stamps:\n%s", progress)
			}
		})
	}
}

// The hash line is the marker, and the only one. A script that has lost it is
// hand-written whatever else is in its header — including the note saying it
// was compiled and has not run yet, which would otherwise send it straight
// back to the compiler.
func TestNoHashLineMeansHandWrittenWhateverElseTheHeaderSays(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	// Compile-shaped, then the hash line removed, as the docs describe.
	if _, err := testscript.SaveDraft(specPath, sampleSpec, handWrittenPassing, ""); err != nil {
		t.Fatal(err)
	}
	scriptPath := testscript.ScriptPath(specPath)
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "atr-spec-sha256") {
			continue
		}
		kept = append(kept, line)
	}
	scriptPath = putHandWritten(t, specPath, strings.Join(kept, "\n"))
	before := stateOf(t, scriptPath)
	if !strings.Contains(before.content, "atr-unverified") {
		t.Fatal("the fixture lost its unverified marker; this test is not exercising the case")
	}

	client := &scriptedClient{}
	a := newRunAgent(t, client)

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Compiled || !out.Passed() || client.callCount() != 0 {
		t.Errorf("compiled=%v passed=%v calls=%d; want a plain replay", out.Compiled, out.Passed(), client.callCount())
	}
	wantUntouched(t, scriptPath, before)
}

// The marker is a hash line that is gone, not one that has moved. A compiled
// script with something added above its header — a licence comment, here — is
// unreadable as a header but is nobody's hand-written script, and replaying it
// for ever would mean a later spec edit never took effect while every run
// stayed green. It is compiled again, as is a file with nothing in it.
func TestAScriptThatOnlyLostItsHeaderPositionIsCompiledAgain(t *testing.T) {
	b, url := sharedRunBrowser(t)

	compiled := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { atr.expectText("#status", "signed in"); });`

	tests := []struct {
		name   string
		source func(specPath string) string
	}{
		{"a licence comment above the header", func(specPath string) string {
			if _, err := testscript.Save(specPath, sampleSpec, "atr.step(1, \"old\", () => { expect(1).toBe(2); });", ""); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(testscript.ScriptPath(specPath))
			if err != nil {
				t.Fatal(err)
			}
			return "/* Copyright someone */\n" + string(body)
		}},
		{"an empty file", func(string) string { return "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specPath := writeSpec(t, sampleSpec)
			putHandWritten(t, specPath, tt.source(specPath))

			client := &scriptedClient{replies: []string{jsBlock(compiled)}}
			a := newRunAgent(t, client)

			out, err := a.RunBehavior(context.Background(), RunRequest{
				SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
				ScriptTimeout: 30 * time.Second,
				Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if out.HandWritten {
				t.Error("the script was taken for hand-written")
			}
			if !out.Compiled || !out.Passed() {
				t.Errorf("compiled=%v passed=%v; want it compiled again and passing", out.Compiled, out.Passed())
			}

			// And under --no-compile it is refused, not quietly replayed.
			putHandWritten(t, specPath, tt.source(specPath))
			_, err = a.RunBehavior(context.Background(), RunRequest{
				SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
				NoCompile:     true,
				ScriptTimeout: 30 * time.Second,
			})
			if err == nil || !strings.Contains(err.Error(), "--no-compile") {
				t.Errorf("err = %v, want a refusal under --no-compile", err)
			}
		})
	}
}

// --recompile is the explicit way to hand a script back: it replaces a
// hand-written one with a compiled one, because that is what was asked for.
func TestRecompileReplacesAHandWrittenScript(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)
	scriptPath := putHandWritten(t, specPath, handWrittenPassing)

	compiled := `atr.step(1, "Click sign in", () => { atr.click("#submit"); });
atr.step(2, "Verify status", () => { atr.expectText("#status", "signed in"); });`
	client := &scriptedClient{replies: []string{jsBlock(compiled)}}
	a := newRunAgent(t, client)
	progress := &progressLog{}

	out, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		Recompile:     true,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
		Progress:      progress.add,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !out.Compiled || !out.Passed() {
		t.Fatalf("compiled=%v passed=%v; --recompile should have compiled and passed", out.Compiled, out.Passed())
	}
	if out.HandWritten {
		t.Error("the outcome still calls the script hand-written after compiling over it")
	}

	stored, err := testscript.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.HandWritten() || !stored.Fresh(sampleSpec) {
		t.Errorf("after --recompile the script at %s is not a verified compile:\n%s", scriptPath, stored.Source)
	}
	// Replacing someone's file is worth a line saying so.
	if !progress.contains("hand-written") {
		t.Errorf("the progress output does not say a hand-written script was replaced:\n%s", progress)
	}
}

// --recompile and --no-compile cannot both be honoured. The refusal has to say
// that, not that the script is stale: for a hand-written script it is not, and
// for any script the flags are what is wrong.
func TestRecompileUnderNoCompileSaysWhichFlagsDisagree(t *testing.T) {
	_, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)
	scriptPath := putHandWritten(t, specPath, handWrittenPassing)
	before := stateOf(t, scriptPath)

	client := &scriptedClient{}
	a := newRunAgent(t, client)

	_, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		Recompile: true, NoCompile: true,
		ScriptTimeout: 30 * time.Second,
	})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(err.Error(), "stale") {
		t.Errorf("the refusal calls the script stale: %v", err)
	}
	if !strings.Contains(err.Error(), "--recompile") || !strings.Contains(err.Error(), "--no-compile") {
		t.Errorf("the refusal does not name both flags: %v", err)
	}
	if client.callCount() != 0 {
		t.Errorf("made %d model call(s)", client.callCount())
	}
	wantUntouched(t, scriptPath, before)
}

// A hand-written script that cannot fail is refused like any other — replaying
// a test that passes whatever happens is the harm, whoever wrote it — but the
// advice cannot be "recompile", which would replace it.
func TestTheLintDoesNotTellAnAuthorToRecompileTheirOwnScript(t *testing.T) {
	_, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)

	cannotFail := `// Hand-written, and it asserts nothing.
atr.step(1, "Click sign in", () => { atr.click("#submit"); });
`
	scriptPath := putHandWritten(t, specPath, cannotFail)
	before := stateOf(t, scriptPath)

	a := newRunAgent(t, &scriptedClient{})

	_, err := a.RunBehavior(context.Background(), RunRequest{
		SpecPath: specPath, Spec: sampleSpec, BaseURL: url,
		ScriptTimeout: 30 * time.Second,
	})
	if !errors.Is(err, ErrScriptCannotFail) {
		t.Fatalf("err = %v, want ErrScriptCannotFail", err)
	}
	if strings.Contains(err.Error(), "--recompile") {
		t.Errorf("the advice is to recompile a hand-written script, which would replace it: %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Base(scriptPath)) {
		t.Errorf("the advice does not name the script to edit: %v", err)
	}
	wantUntouched(t, scriptPath, before)
}

// The hoist is the third thing that rewrites scripts, and it runs on its own
// after a compile. A hand-written script is not offered to it: it is neither
// compared for repetition nor available to be rewritten.
func TestTheHoistDoesNotSeeHandWrittenScripts(t *testing.T) {
	dir := t.TempDir()

	write := func(name, body string) string {
		t.Helper()
		spec := filepath.Join(dir, name+".test.txt")
		if err := os.WriteFile(spec, []byte("Steps:\n1. Sign in\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(testscript.ScriptPath(spec), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return spec
	}

	header := "// atr-spec-sha256: " + testscript.SpecHash("Steps:\n1. Sign in\n") + "\n"
	compiledA := write("a", header+origLogin+"\n")
	compiledB := write("b", header+origCheckout+"\n")
	mine := write("mine", "// Hand-written.\n"+origCheckout+"\n")

	scripts, skipped, err := loadScripts([]string{compiledA, compiledB, mine})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := scripts[testscript.ScriptPath(mine)]; ok {
		t.Error("a hand-written script was loaded as a candidate for rewriting")
	}
	if len(scripts) != 2 {
		t.Errorf("loaded %d scripts, want the two compiled ones", len(scripts))
	}
	if len(skipped) != 1 || skipped[0] != testscript.ScriptPath(mine) {
		t.Errorf("skipped = %v, want the hand-written script named", skipped)
	}

	// And so a proposal that rewrites it is refused, as a rewrite of any file
	// the agent was not shown is.
	ex := &Extraction{
		Library: soundLibrary,
		Scripts: map[string]string{"mine.test.js": "atr.step(1, \"Sign in\", () => { signIn(); });"},
	}
	if err := ex.ResolveAgainst(scripts); err == nil {
		t.Error("a proposal rewriting a hand-written script was accepted")
	}
}

// With only one compiled script beside a hand-written one there is nothing to
// hoist between, however much the two repeat each other: no model call, no
// write.
func TestAHandWrittenScriptIsNotHoistedWithItsOnlyNeighbour(t *testing.T) {
	dir := t.TempDir()

	spec := func(name string) string { return filepath.Join(dir, name+".test.txt") }
	for _, name := range []string{"a", "mine"} {
		if err := os.WriteFile(spec(name), []byte("Steps:\n1. Sign in\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	header := "// atr-spec-sha256: " + testscript.SpecHash("Steps:\n1. Sign in\n") + "\n"
	if err := os.WriteFile(testscript.ScriptPath(spec("a")), []byte(header+origCheckout+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	minePath := putHandWritten(t, spec("mine"), "// Hand-written.\n"+origCheckout+"\n")
	before := stateOf(t, minePath)

	client := &scriptedClient{} // any proposal request fails the test
	a := newRunAgent(t, client)

	out, err := a.RefactorOperations(context.Background(), RefactorRequest{
		Specs: []string{spec("a"), spec("mine")},
		Mode:  ExtractAlways,
	})
	if err != nil {
		t.Fatalf("refactor: %v", err)
	}
	if out.Applied || len(out.Overlaps) != 0 || client.callCount() != 0 {
		t.Errorf("applied=%v overlaps=%d calls=%d; want nothing found and nothing asked",
			out.Applied, len(out.Overlaps), client.callCount())
	}
	wantUntouched(t, minePath, before)
	if _, err := os.Stat(testscript.LibraryPath(spec("a"))); !os.IsNotExist(err) {
		t.Errorf("a library was written (stat err = %v)", err)
	}
}

// Stamping a directory after a hoist walks every spec in it. The hand-written
// ones are walked past.
func TestStampingADirectoryLeavesHandWrittenScriptsAlone(t *testing.T) {
	dir := t.TempDir()

	compiledSpec := filepath.Join(dir, "a.test.txt")
	mineSpec := filepath.Join(dir, "mine.test.txt")
	for _, s := range []string{compiledSpec, mineSpec} {
		if err := os.WriteFile(s, []byte("Steps:\n1. Go\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compiled := "// atr-spec-sha256: " + testscript.SpecHash("x") +
		"\natr.step(1, \"Go\", () => { expect(1).toBe(1); });\n"
	if err := os.WriteFile(testscript.ScriptPath(compiledSpec), []byte(compiled), 0o644); err != nil {
		t.Fatal(err)
	}
	minePath := putHandWritten(t, mineSpec, "// Hand-written.\natr.step(1, \"Go\", () => { expect(1).toBe(1); });\n")
	before := stateOf(t, minePath)

	putLibrary(t, compiledSpec, "function openHome() { atr.navigate(\"/\"); }\n")

	if err := stampDirectory([]string{compiledSpec, mineSpec}); err != nil {
		t.Fatalf("stamping: %v", err)
	}

	wantUntouched(t, minePath, before)

	// The compiled neighbour was still stamped: skipping one must not stop
	// the walk.
	lib, err := testscript.LoadLibrary(compiledSpec)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := testscript.Load(compiledSpec)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LibraryChanged(lib.Hash()) {
		t.Error("the compiled script beside it was not stamped")
	}
}
