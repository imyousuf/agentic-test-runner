package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

func writeSpecDir(t *testing.T, spec, script, library string) string {
	t.Helper()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "login.test.txt")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	var libHash string
	if library != "" {
		if err := os.WriteFile(testscript.LibraryPath(specPath), []byte(library), 0o644); err != nil {
			t.Fatal(err)
		}
		lib, err := testscript.LoadLibrary(specPath)
		if err != nil {
			t.Fatal(err)
		}
		libHash = lib.Hash()
	}

	if script != "" {
		if _, err := testscript.Save(specPath, spec, script, libHash); err != nil {
			t.Fatal(err)
		}
	}
	return specPath
}

// needsLiveApp decides whether a missing base URL is fatal, and it asks
// `Fresh` — which deliberately knows nothing about the shared library.
//
// This is the landmine the separate atr-lib-sha256 header exists to avoid. If
// a library change made a script stale, a directory run would decide it must
// compile, demand a base URL nobody needs for a replay, and refuse to run at
// all. Keeping the two dimensions apart is what makes a library edit free.
func TestALibraryEditDoesNotDemandABaseURL(t *testing.T) {
	const spec = "Test: sign in\n\nSteps:\n1. Sign in\n"
	const script = `atr.step(1, "Sign in", () => { signIn(); expect(atr.text("#status")).toBe("in"); });`

	specPath := writeSpecDir(t, spec, script, "function signIn() { atr.click('#submit'); }\n")

	if needsLiveApp(specPath, spec) {
		t.Fatal("a fresh script wanted a live application before the library was even touched")
	}

	// Edit the shared operation, the way anyone tracking a UI change would.
	if err := os.WriteFile(testscript.LibraryPath(specPath),
		[]byte("function signIn() {\n  atr.click('#sign-in');\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if needsLiveApp(specPath, spec) {
		t.Error("a library edit made the run demand a base URL; a replay does not need one, " +
			"and requiring it would refuse the whole directory over a one-line fix")
	}
}

// The spec dimension still works: editing the spec does mean a compile, and a
// compile does need somewhere to point the browser.
func TestASpecEditStillDemandsABaseURL(t *testing.T) {
	const spec = "Test: sign in\n\nSteps:\n1. Sign in\n"
	const script = `atr.step(1, "Sign in", () => { expect(atr.text("#status")).toBe("in"); });`

	specPath := writeSpecDir(t, spec, script, "")

	if needsLiveApp(specPath, spec) {
		t.Fatal("a fresh script wanted a live application")
	}
	if !needsLiveApp(specPath, spec+"2. And check something else\n") {
		t.Error("an edited spec did not report that it needs the application")
	}
}

// A spec with nothing compiled yet has to drive the application, whatever else
// is or is not beside it.
func TestAnUncompiledSpecDemandsABaseURL(t *testing.T) {
	const spec = "Test: sign in\n\nSteps:\n1. Sign in\n"
	specPath := writeSpecDir(t, spec, "", "function signIn() {}\n")

	if !needsLiveApp(specPath, spec) {
		t.Error("a spec with no compiled script did not report that it needs the application")
	}
}
