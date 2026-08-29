package testscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLibrary(t *testing.T, specPath, source string) *Library {
	t.Helper()
	if err := os.WriteFile(LibraryPath(specPath), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := LoadLibrary(specPath)
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

// The library is not in the spec's hash, so editing it changes the behaviour
// of every script beside it while every hash stays valid: nothing recompiles,
// nothing reports stale, and --no-compile in CI replays scripts whose meaning
// just changed. A header of its own is what makes the change sayable.
func TestAScriptRecordsTheLibraryItWasCompiledAgainst(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)
	lib := writeLibrary(t, specPath, "function signIn() { atr.click('#submit'); }\n")

	if _, err := Save(specPath, spec, "atr.step(1, 'x', () => { signIn(); });", lib.Hash()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	stored, err := Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LibHash != lib.Hash() {
		t.Fatalf("LibHash = %q, want %q", stored.LibHash, lib.Hash())
	}
	if !strings.Contains(stored.Source, libHeader) {
		t.Error("the script carries no library header, so a change to the library is unsayable")
	}
	if stored.LibraryChanged(lib.Hash()) {
		t.Error("an unchanged library reports as changed")
	}
}

// Separate from the spec hash, not folded into it. A merged hash cannot say
// which of the two moved, and the two call for different actions.
func TestALibraryChangeIsNotASpecChange(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)
	lib := writeLibrary(t, specPath, "function signIn() { atr.click('#submit'); }\n")

	if _, err := Save(specPath, spec, "atr.step(1, 'x', () => { signIn(); });", lib.Hash()); err != nil {
		t.Fatal(err)
	}

	changed := writeLibrary(t, specPath, "function signIn() { atr.click('#sign-in'); }\n")

	stored, err := Load(specPath)
	if err != nil {
		t.Fatal(err)
	}

	// The spec is untouched, so the script is still fresh: replaying it is
	// legal, and legal under --no-compile too.
	if !stored.Fresh(spec) {
		t.Error("a library edit made the script stale, which would cost a model compile per spec in the directory")
	}
	if !stored.LibraryChanged(changed.Hash()) {
		t.Error("a library edit is invisible, so a changed library replays for ever unrecorded")
	}
}

// A restamp is what makes a library edit cost nothing: the run replays, passes,
// and the header catches up. Without it the same edit reports changed for ever.
func TestStampRecordsTheLibraryThatRan(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)
	lib := writeLibrary(t, specPath, "function signIn() { atr.click('#submit'); }\n")

	if _, err := SaveDraft(specPath, spec, "atr.step(1, 'x', () => { signIn(); });", lib.Hash()); err != nil {
		t.Fatal(err)
	}

	changed := writeLibrary(t, specPath, "function signIn() { atr.click('#sign-in'); }\n")

	if err := Stamp(specPath, changed.Hash()); err != nil {
		t.Fatalf("Stamp: %v", err)
	}

	stored, err := Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Unverified {
		t.Error("the script is still unverified after a completed run")
	}
	if stored.LibHash != changed.Hash() {
		t.Errorf("LibHash = %q, want the library that actually ran (%q)", stored.LibHash, changed.Hash())
	}
	if stored.LibraryChanged(changed.Hash()) {
		t.Error("the same library still reports as changed after a restamp")
	}
	if !stored.Fresh(spec) {
		t.Error("the restamp cost the script its freshness")
	}
	if strings.Count(stored.Source, libHeader) != 1 {
		t.Errorf("the header was duplicated:\n%s", stored.Source)
	}
}

// A script compiled before any library existed has no header to rewrite, and
// adding one must not disturb the rest of the block.
func TestStampAddsTheHeaderToAnOlderScript(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)

	if _, err := Save(specPath, spec, "atr.step(1, 'x', () => {});", ""); err != nil {
		t.Fatal(err)
	}

	lib := writeLibrary(t, specPath, "function signIn() {}\n")
	if err := Stamp(specPath, lib.Hash()); err != nil {
		t.Fatalf("Stamp: %v", err)
	}

	stored, err := Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LibHash != lib.Hash() {
		t.Errorf("LibHash = %q, want %q", stored.LibHash, lib.Hash())
	}
	if !stored.Fresh(spec) {
		t.Error("adding the library header cost the script its spec hash")
	}
	if !strings.Contains(stored.Source, "atr.step(1, 'x'") {
		t.Error("the script body was damaged")
	}
}

// Removing the library takes its header with it, or the script reports changed
// against a file that no longer exists.
func TestStampDropsTheHeaderWhenTheLibraryIsGone(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)
	lib := writeLibrary(t, specPath, "function signIn() {}\n")

	if _, err := Save(specPath, spec, "atr.step(1, 'x', () => {});", lib.Hash()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(LibraryPath(specPath)); err != nil {
		t.Fatal(err)
	}

	if err := Stamp(specPath, ""); err != nil {
		t.Fatalf("Stamp: %v", err)
	}

	stored, err := Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LibHash != "" {
		t.Errorf("LibHash = %q, want it removed with the library", stored.LibHash)
	}
	if stored.LibraryChanged("") {
		t.Error("a script with no library reports a changed one")
	}
}

// A key read only inside the library would otherwise be reported unused on
// every run and then deleted, after which every test in the directory fails
// with a missing input.
func TestAKeyReadOnlyInTheLibraryIsNotUnused(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Sign in\n"
	specPath := writeSpec(t, spec)

	writeLibrary(t, specPath, `
		function signIn() {
			atr.fill("#username", values.get("username"));
			atr.click("#submit");
		}
	`)
	if err := os.WriteFile(ValuesPath(specPath),
		[]byte("base_url = http://localhost\nusername = demo\nleftover = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(specPath, spec, "atr.step(1, 'Sign in', () => { signIn(); });", ""); err != nil {
		t.Fatal(err)
	}

	unused, err := UnreferencedKeys(specPath)
	if err != nil {
		t.Fatalf("UnreferencedKeys: %v", err)
	}

	for _, key := range unused {
		if key == "username" {
			t.Error("a key the library reads was reported unused; pruning it breaks every test in the directory")
		}
	}
	if len(unused) != 1 || unused[0] != "leftover" {
		t.Errorf("unused = %v, want just the genuinely unused key", unused)
	}
}

// "Referenced" stops being decidable the moment a key is built at run time,
// and the library is as good a place to do that as the script.
func TestANonLiteralCallInTheLibraryMakesTheScanInexact(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Sign in\n"
	specPath := writeSpec(t, spec)

	writeLibrary(t, specPath, `
		function fillRow(i) {
			atr.fill("#row" + i, values.get("row_" + i));
		}
	`)
	if err := os.WriteFile(ValuesPath(specPath),
		[]byte("base_url = http://localhost\nrow_1 = a\nrow_2 = b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(specPath, spec, "atr.step(1, 'x', () => { fillRow(1); });", ""); err != nil {
		t.Fatal(err)
	}

	unused, err := UnreferencedKeys(specPath)
	if err != nil {
		t.Fatalf("UnreferencedKeys: %v", err)
	}
	if len(unused) != 0 {
		t.Errorf("reported %v as unused; a computed key in the library defeats the scan, and a "+
			"confident wrong answer here deletes committed inputs", unused)
	}
}

func TestLibraryPathIsBesideTheSpec(t *testing.T) {
	got := LibraryPath(filepath.Join("tests", "e2e", "login.test.txt"))
	want := filepath.Join("tests", "e2e", LibraryName)
	if got != want {
		t.Errorf("LibraryPath = %q, want %q", got, want)
	}
}

// Removing the spec hash is how a person marks a script hand-written and
// off-limits. A header appearing in a file ATR promised to leave alone is
// exactly the surprise that stops it being trusted with the others.
func TestStampLeavesAHandWrittenScriptAlone(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)
	lib := writeLibrary(t, specPath, "function signIn() {}\n")

	handWritten := "// mine, thanks\natr.step(1, 'x', () => {});\n"
	if err := os.WriteFile(ScriptPath(specPath), []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Stamp(specPath, lib.Hash()); err != nil {
		t.Fatalf("Stamp: %v", err)
	}

	after, err := os.ReadFile(ScriptPath(specPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != handWritten {
		t.Errorf("a hand-written script was rewritten:\n%s", after)
	}
}
