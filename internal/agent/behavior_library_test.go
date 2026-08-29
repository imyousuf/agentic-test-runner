package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

func putLibrary(t *testing.T, specPath, source string) {
	t.Helper()
	if err := os.WriteFile(testscript.LibraryPath(specPath), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The feature's primary use is editing login() to follow a UI change. Making
// that cost a model compile per spec in the directory would turn the cheap fix
// into the expensive one, rewrite every committed script as a whole-file diff,
// and turn CI red until somebody re-ran them all locally.
//
// A library change does not mean the script is wrong. It means the script is
// unproven against the current library — which is a replay.
func TestALibraryEditReplaysRatherThanRecompiling(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)
	putLibrary(t, specPath, "function signIn() { atr.click('#submit'); }\n")

	script := `atr.step(1, "Sign in", () => { signIn(); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`

	client := &scriptedClient{replies: []string{jsBlock(script)}}
	a := newRunAgent(t, client)

	req := RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	}

	first, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if !first.Passed() || first.ModelCalls != 1 {
		t.Fatalf("first run: passed=%v calls=%d", first.Passed(), first.ModelCalls)
	}

	// Edit the shared operation. The scripted client has no replies left, so
	// any compile at all fails the test loudly rather than silently costing
	// money.
	putLibrary(t, specPath, "function signIn() {\n  atr.click('#submit');\n}\n")

	second, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !second.Passed() {
		t.Fatalf("the replay failed: %v", second.Result.Failure)
	}
	if second.ModelCalls != 0 {
		t.Errorf("a library edit cost %d model calls; it must replay", second.ModelCalls)
	}
	if second.Compiled {
		t.Error("a library edit forced a recompile")
	}

	// And the header caught up, so the same edit is not reported for ever.
	stored, err := testscript.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := testscript.LoadLibrary(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LibraryChanged(lib.Hash()) {
		t.Error("the script was not restamped, so the edit reports as changed on every future run")
	}
}

// The progress line has to name the library, because "the spec changed" and
// "the library changed" call for different actions.
func TestALibraryChangeIsAnnouncedByName(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)
	putLibrary(t, specPath, "function signIn() { atr.click('#submit'); }\n")

	script := `atr.step(1, "Sign in", () => { signIn(); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`

	client := &scriptedClient{replies: []string{jsBlock(script)}}
	a := newRunAgent(t, client)

	base := RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	}
	if _, err := a.RunBehavior(context.Background(), base); err != nil {
		t.Fatal(err)
	}

	putLibrary(t, specPath, "function signIn() {\n  atr.click('#submit');\n}\n")

	var progress []string
	req := base
	req.Progress = func(msg string) { progress = append(progress, msg) }
	if _, err := a.RunBehavior(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	var named bool
	for _, line := range progress {
		if strings.Contains(line, testscript.LibraryName) {
			named = true
		}
		if strings.Contains(line, "the spec changed") {
			t.Errorf("a library change was reported as a spec change: %q", line)
		}
	}
	if !named {
		t.Errorf("nothing named the library that changed: %v", progress)
	}
}

// Under --no-compile the checkout belongs to CI, and a restamp there leaves a
// dirty working tree on a machine nobody is watching.
func TestNoCompileReportsTheRestampInsteadOfWriting(t *testing.T) {
	b, url := sharedRunBrowser(t)
	specPath := writeSpec(t, sampleSpec)
	putLibrary(t, specPath, "function signIn() { atr.click('#submit'); }\n")

	script := `atr.step(1, "Sign in", () => { signIn(); });
atr.step(2, "Verify status", () => { expect(atr.text("#status")).toBe("signed in"); });`

	client := &scriptedClient{replies: []string{jsBlock(script)}}
	a := newRunAgent(t, client)

	base := RunRequest{
		SpecPath:      specPath,
		Spec:          sampleSpec,
		BaseURL:       url,
		ScriptTimeout: 30 * time.Second,
		Reset:         func(ctx context.Context) error { return b.Navigate(ctx, url) },
	}
	if _, err := a.RunBehavior(context.Background(), base); err != nil {
		t.Fatal(err)
	}

	putLibrary(t, specPath, "function signIn() {\n  atr.click('#submit');\n}\n")

	before, err := os.ReadFile(testscript.ScriptPath(specPath))
	if err != nil {
		t.Fatal(err)
	}

	var progress []string
	req := base
	req.NoCompile = true
	req.Progress = func(msg string) { progress = append(progress, msg) }

	out, err := a.RunBehavior(context.Background(), req)
	if err != nil {
		t.Fatalf("--no-compile refused a legal replay: %v", err)
	}
	if !out.Passed() {
		t.Fatalf("the replay failed: %v", out.Result.Failure)
	}

	after, err := os.ReadFile(testscript.ScriptPath(specPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("--no-compile rewrote a committed script")
	}

	var told bool
	for _, line := range progress {
		if strings.Contains(line, "restamp") {
			told = true
		}
	}
	if !told {
		t.Errorf("nothing said the script would need restamping: %v", progress)
	}
}

// A library the model cannot see is a shelf nobody reaches for: the agent
// re-derives login anyway and we have added a file.
func TestBothPromptsCarryTheLibrary(t *testing.T) {
	const library = "function signIn() { atr.click('#submit'); }"

	note := libraryNote(library)
	if note == "" {
		t.Fatal("the library is not rendered for a prompt at all")
	}
	if !strings.Contains(note, "signIn") {
		t.Error("the library is not injected verbatim, so the model cannot see what the operation does")
	}

	// The API reference opens with "Nothing else is available". Without an
	// amendment, a repairing model reading a script that calls signIn() is
	// told that call is invalid — and its rational repair is to inline it,
	// after which the replay passes, the script is stamped, and the suite has
	// silently lost its library with every hash still valid.
	if !strings.Contains(note, "available") {
		t.Error("nothing amends the 'nothing else is available' rule")
	}
	if !strings.Contains(note, "Do NOT rewrite this file") {
		t.Error("nothing stops a repair from rewriting shared code that twenty tests depend on")
	}

	if libraryNote("") != "" || libraryNote("   \n") != "" {
		t.Error("a directory with no library still gets a library section")
	}
}

func TestLibraryPathIsBesideTheSpec(t *testing.T) {
	spec := filepath.Join("tests", "login.test.txt")
	if got := testscript.LibraryPath(spec); got != filepath.Join("tests", testscript.LibraryName) {
		t.Errorf("LibraryPath = %q", got)
	}
}
