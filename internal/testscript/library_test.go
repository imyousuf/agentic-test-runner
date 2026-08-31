package testscript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runWithLibrary executes a script against the fixture with a library loaded.
func runWithLibrary(t *testing.T, library, source string) *Result {
	t.Helper()

	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatalf("navigate to fixture: %v", err)
	}

	res, err := Run(context.Background(), Options{
		Browser:     testBrowser,
		Source:      source,
		Name:        t.Name() + ".js",
		BaseURL:     fixtureURL,
		Timeout:     30 * time.Second,
		Library:     library,
		LibraryName: LibraryName,
	})
	if err != nil {
		t.Fatalf("Run returned an error (it should report failures in Result): %v", err)
	}
	return res
}

// The whole point: eight steps of signing in stop being rediscovered by every
// compile and re-typed into every script.
func TestALibraryOperationIsInScope(t *testing.T) {
	res := runWithLibrary(t, `
		function signIn() {
			atr.click("#submit");
		}
	`, `
		atr.step(1, "Sign in", () => { signIn(); });
		atr.step(2, "Verify status", () => {
			expect(atr.text("#status")).toBe("signed in");
		});
	`)

	if !res.Passed {
		t.Fatalf("expected pass, got %v", res.Failure)
	}
}

// A library defect is KindConfig: not repairable, not retryable, never sent to
// the model. Classifying it as a script fault would point the repair loop at
// code that every test in the directory depends on.
func TestALibraryDefectIsConfigNotScript(t *testing.T) {
	tests := []struct {
		name    string
		library string
		wants   string
	}{
		{
			name:    "does not parse",
			library: `function signIn( { atr.click("#submit"); }`,
			wants:   "does not parse",
		},
		{
			// Runs before step 1 of every spec in the directory, producing a
			// step-0 failure nobody can diagnose.
			name:    "drives the browser at the top level",
			library: "atr.navigate(\"/\");\nfunction signIn() {}",
			wants:   "top level",
		},
		{
			name:    "assigns the result of a call at the top level",
			library: `const here = atr.url();`,
			wants:   "top level",
		},
		{
			// Renumbers every spec that loads it, differently.
			name:    "declares a step",
			library: `function x() {} atr.step(1, "mine", () => {});`,
			wants:   "steps belong to a spec",
		},
		{
			name:    "declares setup",
			library: `function x() { } atr.setup("mine", () => {});`,
			wants:   "steps belong to a spec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runWithLibrary(t, tt.library, `
				atr.step(1, "Verify status", () => {
					expect(atr.text("#heading")).toBe("Welcome back");
				});`)

			if res.Passed {
				t.Fatal("a broken library passed")
			}
			if res.Failure.Kind != KindConfig {
				t.Errorf("kind = %q, want %q — anything repairable points the model at shared code",
					res.Failure.Kind, KindConfig)
			}
			if !strings.Contains(res.Failure.Message, tt.wants) {
				t.Errorf("message does not say what is wrong (%q): %s", tt.wants, res.Failure.Message)
			}
		})
	}
}

// Declarations are what a library is for; the check must not fire on them.
func TestOrdinaryDeclarationsAreAllowed(t *testing.T) {
	res := runWithLibrary(t, `
		const SUBMIT = "#submit";
		let attempts = 0;
		class Helper {}
		function signIn() {
			attempts++;
			atr.click(SUBMIT);
		}
		const alsoSignIn = () => signIn();
	`, `
		atr.step(1, "Sign in", () => { alsoSignIn(); });
		atr.step(2, "Verify status", () => {
			expect(atr.text("#status")).toBe("signed in");
		});
	`)

	if !res.Passed {
		t.Fatalf("a library of plain declarations was rejected: %v", res.Failure)
	}
}

// Once a test's assertions live in shared code you can no longer read the test
// and know what it checks, and one edit can weaken twenty tests. That is the
// same objection ATR already makes to repairing an assertion automatically.
func TestAnAssertionFromTheLibraryIsRefused(t *testing.T) {
	tests := []struct {
		name    string
		library string
	}{
		{
			name: "expect",
			library: `function checkSignedIn() {
				expect(atr.text("#status")).toBe("signed in");
			}`,
		},
		{
			name: "atr.fail",
			library: `function checkSignedIn() {
				atr.fail("the library decided this test failed");
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runWithLibrary(t, tt.library, `
				atr.step(1, "Sign in", () => { atr.click("#submit"); });
				atr.step(2, "Check", () => { checkSignedIn(); });
			`)

			if res.Passed {
				t.Fatal("a shared assertion ran")
			}
			if res.Failure.Kind != KindConfig {
				t.Errorf("kind = %q, want %q", res.Failure.Kind, KindConfig)
			}
			if !strings.Contains(res.Failure.Message, "operations, not assertions") {
				t.Errorf("the message does not explain the boundary: %s", res.Failure.Message)
			}
		})
	}
}

// The boundary must not cost the spec its own assertions.
func TestTheSpecMayStillAssertWhileALibraryIsLoaded(t *testing.T) {
	res := runWithLibrary(t, `
		function signIn() { atr.click("#submit"); }
	`, `
		atr.step(1, "Sign in", () => { signIn(); });
		atr.step(2, "Check", () => {
			expect(atr.text("#status")).toBe("signed in");
			atr.expectExists("#heading");
		});
	`)

	if !res.Passed {
		t.Fatalf("the spec's own assertions were refused: %v", res.Failure)
	}
}

// A spec with no _shared.js beside it behaves exactly as it did before any of
// this existed.
func TestNoLibraryChangesNothing(t *testing.T) {
	res := run(t, `
		atr.step(1, "Check the heading", () => {
			expect(atr.text("#heading")).toBe("Welcome back");
		});
	`)
	if !res.Passed {
		t.Fatalf("expected pass, got %v", res.Failure)
	}
}

func TestLoadLibraryFindsTheSiblingFile(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.test.txt")

	if lib, err := LoadLibrary(spec); err != nil || lib != nil {
		t.Fatalf("a directory without %s should load nothing: %v, %v", LibraryName, lib, err)
	}

	if err := os.WriteFile(filepath.Join(dir, LibraryName),
		[]byte("function signIn() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lib, err := LoadLibrary(spec)
	if err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	if lib == nil || !strings.Contains(lib.Source, "signIn") {
		t.Fatalf("the library was not read: %+v", lib)
	}
	if lib.Hash() == "" {
		t.Error("the library has no hash, so a change to it cannot be noticed")
	}
}

// The hash normalises like a spec's does, or reformatting shared code
// invalidates every script in the directory for nothing.
func TestAWhitespaceOnlyLibraryEditKeepsTheSameHash(t *testing.T) {
	a := &Library{Source: "function signIn() {\n  atr.click(\"#submit\");\n}\n"}
	b := &Library{Source: "function signIn() {\n\n  atr.click(\"#submit\");   \n}\n\n"}

	if a.Hash() != b.Hash() {
		t.Error("reflowing the library changed its hash, which would recompile a whole directory")
	}
}

// callerIsLibrary captures a bounded number of stack frames, so a deep call
// chain inside the library is worth checking: if the boundary can be walked
// around by nesting helpers, it is not a boundary.
func TestADeepChainInsideTheLibraryIsStillRefused(t *testing.T) {
	res := runWithLibrary(t, `
		function a() { expect(atr.text("#status")).toBe("signed in"); }
		function b() { a(); }
		function c() { b(); }
		function d() { c(); }
		function e() { d(); }
		function f() { e(); }
		function checkSignedIn() { f(); }
	`, `
		atr.step(1, "Sign in", () => { atr.click("#submit"); });
		atr.step(2, "Check", () => { checkSignedIn(); });
	`)

	if res.Passed {
		t.Fatal("nesting helpers walked around the assertion boundary")
	}
	if res.Failure.Kind != KindConfig {
		t.Errorf("kind = %q, want %q", res.Failure.Kind, KindConfig)
	}
}

// The mirror case, which must NOT be refused: the assertion is written in the
// spec, and the library merely reaches it. The boundary is about where the
// assertion lives, not about which frame happens to be on the stack below it.
func TestAScriptAssertionReachedThroughTheLibraryIsAllowed(t *testing.T) {
	res := runWithLibrary(t, `
		function runCheck(check) { check(); }
	`, `
		atr.step(1, "Sign in", () => { atr.click("#submit"); });
		atr.step(2, "Check", () => {
			runCheck(() => { expect(atr.text("#status")).toBe("signed in"); });
		});
	`)

	if !res.Passed {
		t.Fatalf("an assertion written in the spec was refused: %v", res.Failure)
	}
}

// A declaration can hide a program. `const boot = (function () { … })()` is a
// variable declaration whose initialiser drives the browser at load time, and
// a check that looks for atr.* calls outside a function body walks straight
// past it — the call *is* inside a function, it is just called immediately.
//
// So the rule is every call, not every interesting call.
func TestALibraryMayNotCallAnythingAtTheTopLevel(t *testing.T) {
	tests := []struct {
		name    string
		library string
	}{
		{"an immediately-invoked function", `const boot = (function () { atr.navigate("/login"); return 1; })();`},
		{"an immediately-invoked arrow", `const here = (() => atr.url())();`},
		{"optional chaining, which hides the name", `const here = atr?.url();`},
		{"a plain helper call", `function f() { return 1; } const x = f();`},
		{"a class static block", `class Boot { static { atr.navigate("/x"); } }`},
		{"reading an input", `const user = values.get("username");`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateLibrary(tt.library, LibraryName); err == nil {
				t.Error("a library that runs code at load time was accepted")
			}
		})
	}
}

// The rule must not reject the thing a library is for.
func TestALibraryOfPlainDeclarationsIsAccepted(t *testing.T) {
	const library = `
		const SUBMIT = "#submit";
		const TIMEOUT = 5000;
		// A call to a host global is how a person writes a constant, and a
		// rule that rejects it is a rule that gets worked around.
		const SELECTORS = Object.freeze({user: "#username"});
		const STEPS = "one,two".split(",");
		const ORDER = new RegExp("^ORD-");
		let attempts = 0;
		class Helper {}
		function signIn(user) {
			attempts++;
			atr.fill("#username", user);
			atr.click(SUBMIT);
			atr.waitFor("#dashboard", {timeout: TIMEOUT});
		}
		const alsoSignIn = (user) => signIn(user);
	`
	if err := ValidateLibrary(library, LibraryName); err != nil {
		t.Errorf("a library of declarations was rejected: %v", err)
	}
}

// Compiling each spec in isolation is why extraction had nothing to find: two
// specs that make the same journey derived it independently and wrote it
// differently — the same link as `TAGS_PATH` in one script and `tagsPath` in
// the other — and no matcher can undo that after the fact. Showing a compile
// what its neighbours already settled on is what makes the repetition
// detectable at all.
func TestSiblingScriptsAreOfferedToTheCompiler(t *testing.T) {
	dir := t.TempDir()

	write := func(name, body string) string {
		spec := filepath.Join(dir, name+".test.txt")
		if err := os.WriteFile(spec, []byte("Steps:\n1. Go\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if err := os.WriteFile(ScriptPath(spec), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return spec
	}

	mine := write("mine", "atr.step(1, \"mine\", () => {});")
	write("neighbour", "atr.step(1, \"neighbour\", () => {});")
	write("uncompiled", "")

	siblings, err := SiblingScripts(mine)
	if err != nil {
		t.Fatalf("reading siblings: %v", err)
	}

	if _, ok := siblings["mine.test.js"]; ok {
		t.Error("a spec was shown its own script, which teaches it nothing")
	}
	if _, ok := siblings["neighbour.test.js"]; !ok {
		t.Errorf("the compiled neighbour was not offered, got %d sibling(s)", len(siblings))
	}
	if len(siblings) != 1 {
		t.Errorf("got %d siblings, want only the one that is compiled", len(siblings))
	}
}
