package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

const origLogin = `atr.step(1, "Sign in", () => {
  atr.navigate("/login");
  atr.fill("#username", values.get("username"));
  atr.click("#submit");
  atr.expectExists("#dashboard");
});`

const origCheckout = `atr.step(1, "Sign in", () => {
  atr.navigate("/login");
  atr.fill("#username", values.get("username"));
  atr.click("#submit");
  atr.expectExists("#dashboard");
});
atr.step(2, "Check out", () => {
  atr.click("#checkout");
  expect(atr.text("#status")).toBe("Order placed");
});`

func original() map[string]string {
	return map[string]string{
		"login.test.js":    origLogin,
		"checkout.test.js": origCheckout,
	}
}

const soundLibrary = `// Signs in and leaves the browser on the dashboard.
function signIn(username) {
  atr.navigate("/login");
  atr.fill("#username", username);
  atr.click("#submit");
}`

// The shape a good extraction has: the operations move, every claim stays
// exactly where it was.
func TestASoundExtractionIsAccepted(t *testing.T) {
	ex := &Extraction{
		Library: soundLibrary,
		Scripts: map[string]string{
			"login.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  atr.expectExists("#dashboard");
});`,
			"checkout.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  atr.expectExists("#dashboard");
});
atr.step(2, "Check out", () => {
  atr.click("#checkout");
  expect(atr.text("#status")).toBe("Order placed");
});`,
		},
		Reason: "hoisted signIn",
	}

	if err := ValidateExtraction(original(), "", ex); err != nil {
		t.Errorf("a sound extraction was refused: %v", err)
	}
}

// Every way a proposal can be wrong, refused before a byte is written and
// before anything is run. Each of these would pass a replay.
func TestAnUnsoundExtractionIsRefused(t *testing.T) {
	tests := map[string]struct {
		library string
		scripts map[string]string
		says    string
	}{
		"an assertion is dropped": {
			library: soundLibrary,
			scripts: map[string]string{
				"login.test.js": `atr.step(1, "Sign in", () => { signIn(values.get("username")); });`,
			},
			says: "claims",
		},
		"an assertion is loosened": {
			library: soundLibrary,
			scripts: map[string]string{
				"checkout.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  atr.expectExists("#dashboard");
});
atr.step(2, "Check out", () => {
  atr.click("#checkout");
  expect(atr.text("#status")).toContain("Order");
});`,
			},
			says: "claims",
		},
		"an assertion is dragged into the library": {
			library: soundLibrary + `
function checkDashboard() { atr.expectExists("#dashboard"); }`,
			scripts: map[string]string{
				"login.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  checkDashboard();
});`,
			},
			says: "claims",
		},
		"the library runs code at load time": {
			library: `const HERE = atr.url();
function signIn(u) { atr.navigate("/login"); atr.fill("#username", u); atr.click("#submit"); }`,
			scripts: map[string]string{
				"login.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  atr.expectExists("#dashboard");
});`,
			},
			says: "not one",
		},
		"a script that is not in the directory": {
			library: soundLibrary,
			scripts: map[string]string{
				"invented.test.js": `atr.step(1, "x", () => { atr.expectExists("#a"); });`,
			},
			says: "not a script in this directory",
		},
		"the rewrite cannot fail": {
			library: soundLibrary,
			scripts: map[string]string{
				"login.test.js": `atr.step(1, "Sign in", () => { signIn(values.get("username")); atr.log("done"); });`,
			},
			says: "claims",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateExtraction(original(), "", &Extraction{Library: tt.library, Scripts: tt.scripts})
			if err == nil {
				t.Fatal("an unsound extraction was accepted")
			}
			if !strings.Contains(err.Error(), tt.says) {
				t.Errorf("the refusal does not say why: %v", err)
			}
		})
	}
}

// A considered refusal is an answer, not a failure: the agent is told it may
// decline when an overlap is not worth naming.
func TestADeclinedExtractionIsNotAnError(t *testing.T) {
	ex, err := parseExtraction("REASON: the two sequences differ in every argument, so naming " +
		"them would need a parameter per line.")
	if err != nil {
		t.Fatalf("a considered refusal was read as a failure: %v", err)
	}
	if !ex.Empty() {
		t.Error("a refusal produced files")
	}
	if ex.Reason == "" {
		t.Error("the refusal carries no reason")
	}
	if err := ValidateExtraction(original(), "", ex); err != nil {
		t.Errorf("validating a refusal failed: %v", err)
	}
}

// A library nothing calls, or rewrites with no library, are incoherent
// answers rather than refusals.
func TestAnIncoherentProposalIsAnError(t *testing.T) {
	only := "=== FILE: " + testscript.LibraryName + "\n```javascript\nfunction f() {}\n```\nREASON: x"
	if _, err := parseExtraction(only); err == nil {
		t.Error("a library that nothing calls was accepted")
	}

	scriptOnly := "=== FILE: login.test.js\n```javascript\natr.step(1, \"x\", () => {});\n```\nREASON: x"
	if _, err := parseExtraction(scriptOnly); err == nil {
		t.Error("rewrites with no library were accepted")
	}
}

// The reply format has to survive the shapes a model actually emits.
func TestTheReplyIsParsed(t *testing.T) {
	reply := "Here is the extraction.\n\n" +
		"=== FILE: " + testscript.LibraryName + "\n```javascript\n" + soundLibrary + "\n```\n\n" +
		"=== FILE: login.test.js\n```js\natr.step(1, \"Sign in\", () => { signIn(); });\n```\n\n" +
		"REASON: hoisted the three sign-in operations\n"

	ex, err := parseExtraction(reply)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ex.Library, "function signIn") {
		t.Errorf("the library was not read: %q", ex.Library)
	}
	if len(ex.Scripts) != 1 || !strings.Contains(ex.Scripts["login.test.js"], "signIn()") {
		t.Errorf("the rewrite was not read: %+v", ex.Scripts)
	}
	if ex.Reason != "hoisted the three sign-in operations" {
		t.Errorf("reason = %q", ex.Reason)
	}
}

// The agent is shown paths as it was given them and answers with whatever
// reads naturally. Matching the reply's file names literally rejected a sound
// extraction — every rewrite present and correct — because the agent had
// written "tests/_shared.js" where the parser wanted "_shared.js".
func TestFileNamesAreMatchedByBaseName(t *testing.T) {
	reply := "=== FILE: tests/_shared.js\n```javascript\nfunction openHome() { atr.navigate(\"/\"); }\n```\n\n" +
		"=== FILE: ./tests/a.test.js\n```javascript\natr.step(1, \"go\", () => { openHome(); expect(1).toBe(1); });\n```\n\n" +
		"REASON: hoisted the home navigation"

	ex, err := parseExtraction(reply)
	if err != nil {
		t.Fatalf("parsing a reply that used directory prefixes: %v", err)
	}
	if ex.Library == "" {
		t.Fatal("the library was not recognised through its directory prefix")
	}

	known := map[string]string{
		"/repo/tests/a.test.js": "atr.step(1, \"go\", () => { atr.navigate(\"/\"); expect(1).toBe(1); });",
	}
	if err := ex.ResolveAgainst(known); err != nil {
		t.Fatalf("resolving against the real paths: %v", err)
	}
	if _, ok := ex.Scripts["/repo/tests/a.test.js"]; !ok {
		t.Fatalf("the rewrite was not mapped onto its real path, got %v", ex.Paths())
	}
}

// A rewrite of something that is not in this directory is still refused; the
// base-name match must not turn into "accept anything".
func TestAnUnknownScriptIsStillRefused(t *testing.T) {
	ex := &Extraction{
		Library: "function f() {}",
		Scripts: map[string]string{"elsewhere.test.js": "atr.step(1, \"x\", () => {});"},
	}
	if err := ex.ResolveAgainst(map[string]string{"/repo/tests/a.test.js": "..."}); err == nil {
		t.Fatal("a script from another directory was accepted")
	}
}

// An extraction replays every rewritten script against the library it just
// wrote, which is exactly what the lib hash attests. Leaving the stamp off
// threw that proof away: the next run saw a directory whose library had
// changed and replayed all of it to rediscover what had already been shown.
//
// Every spec, not only the rewritten ones — a library now exists where none
// did, and each spec in the directory loads it whether or not it calls it.
func TestStampingRecordsTheVerifiedLibraryOnEverySpec(t *testing.T) {
	dir := t.TempDir()

	var specs []string
	for _, name := range []string{"a", "b"} {
		spec := filepath.Join(dir, name+".test.txt")
		if err := os.WriteFile(spec, []byte("Steps:\n1. Go\n\nExpected Results:\n- It worked\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		script := "// atr-spec-sha256: " + testscript.SpecHash("x") +
			"\natr.step(1, \"Go\", () => { expect(1).toBe(1); });\n"
		if err := os.WriteFile(testscript.ScriptPath(spec), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		specs = append(specs, spec)
	}

	library := "function openHome() { atr.navigate(\"/\"); }\n"
	if err := os.WriteFile(testscript.LibraryPath(specs[0]), []byte(library), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := stampDirectory(specs); err != nil {
		t.Fatalf("stamping: %v", err)
	}

	want := (&testscript.Library{Source: library}).Hash()
	for _, spec := range specs {
		stored, err := testscript.Load(spec)
		if err != nil {
			t.Fatalf("loading %s: %v", filepath.Base(spec), err)
		}
		if stored.LibraryChanged(want) {
			t.Errorf("%s was not stamped with the library it was verified against",
				filepath.Base(spec))
		}
	}
}

// A compile carries its neighbours so the parts that are genuinely the same
// come out the same. Carrying all of them makes the cost of a directory
// quadratic in its own size: sixty specs would put sixty scripts into each of
// sixty compiles, and the sixtieth example teaches nothing the third did not.
func TestTheSiblingPromptIsBounded(t *testing.T) {
	script := strings.Repeat("atr.step(1, \"x\", () => { atr.click(\"#a\"); });\n", 40)

	siblings := map[string]string{}
	for i := range 60 {
		siblings[fmt.Sprintf("s%02d.test.js", i)] = script
	}

	note := siblingNote(siblings)
	if len(note) > 32*1024 {
		t.Errorf("the note for 60 siblings is %d bytes; the whole directory is being sent", len(note))
	}

	shown, omitted := siblingsWithinBudget(siblings)
	if len(shown) > maxSiblingsShown {
		t.Errorf("showed %d siblings, want at most %d", len(shown), maxSiblingsShown)
	}
	if omitted != 60-len(shown) {
		t.Errorf("omitted = %d, want %d — the count the prompt reports must be true", omitted, 60-len(shown))
	}
	if !strings.Contains(note, "not the whole of it") {
		t.Error("the prompt does not say that what it shows is a sample")
	}

	// Same directory, same prompt: an unchanged compile must not be re-sent
	// with a different sample.
	again, _ := siblingsWithinBudget(siblings)
	if strings.Join(shown, ",") != strings.Join(again, ",") {
		t.Errorf("the sample is not deterministic: %v then %v", shown, again)
	}
}

// A small directory is still shown in full — the bound is a ceiling, not a
// sample size.
func TestASmallDirectoryIsShownWhole(t *testing.T) {
	siblings := map[string]string{
		"a.test.js": "atr.step(1, \"a\", () => {});",
		"b.test.js": "atr.step(1, \"b\", () => {});",
	}
	shown, omitted := siblingsWithinBudget(siblings)
	if len(shown) != 2 || omitted != 0 {
		t.Errorf("showed %v and omitted %d, want both siblings", shown, omitted)
	}
}

// writeExtraction promises all of it or none of it. A write that fails part
// way — a read-only checkout, a full disk — leaves a library that exists and
// some of the scripts calling it, which nothing reports and the next compile
// reasons from.
func TestAFailedWriteLeavesTheDirectoryAsItWas(t *testing.T) {
	dir := t.TempDir()

	var specs []string
	originals := map[string]string{}
	for i, name := range []string{"a", "b"} {
		spec := filepath.Join(dir, name+".test.txt")
		if err := os.WriteFile(spec, []byte("Steps:\n1. Go\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("// original %d\n", i)
		if err := os.WriteFile(testscript.ScriptPath(spec), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		originals[testscript.ScriptPath(spec)] = body
		specs = append(specs, spec)
	}

	libPath := testscript.LibraryPath(specs[0])
	if err := os.WriteFile(libPath, []byte("// original library\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	originals[libPath] = "// original library\n"

	// One script cannot be written, so the extraction fails part way through.
	unwritable := testscript.ScriptPath(specs[1])
	if err := os.Chmod(unwritable, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unwritable, 0o644) })

	ex := &Extraction{
		Library: "// new library\n",
		Scripts: map[string]string{
			testscript.ScriptPath(specs[0]): "// new a\n",
			unwritable:                      "// new b\n",
		},
	}

	restore, err := writeExtraction(specs[0], ex)
	if err == nil {
		t.Skip("this filesystem let the unwritable file be written")
	}
	// Nothing needed undoing for the file that could not be written, so this
	// must not report a failure to undo it — that alarm means a half-changed
	// directory, and raising it wrongly is how a real one gets ignored.
	if rerr := restore(); rerr != nil {
		t.Errorf("restore reported a failure for files it never changed: %v", rerr)
	}

	for path, want := range originals {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", filepath.Base(path), err)
		}
		if string(got) != want {
			t.Errorf("%s was left changed after a failed write:\n  got  %q\n  want %q",
				filepath.Base(path), got, want)
		}
	}
}

// A directory can hold a spec that has never compiled — one just added, or one
// skipped as stale. Stamping must not give up at it and leave every spec after
// it unstamped, because the cost of a missing stamp is the next run replaying
// the whole directory to rediscover what was just proved.
func TestStampingSkipsAnUncompiledSpecAndCarriesOn(t *testing.T) {
	dir := t.TempDir()

	spec := func(name string, compiled bool) string {
		p := filepath.Join(dir, name+".test.txt")
		if err := os.WriteFile(p, []byte("Steps:\n1. Go\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if compiled {
			body := "// atr-spec-sha256: " + testscript.SpecHash("x") +
				"\natr.step(1, \"Go\", () => { expect(1).toBe(1); });\n"
			if err := os.WriteFile(testscript.ScriptPath(p), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}

	// The uncompiled one sorts first, so a loop that gives up never reaches
	// the others.
	a := spec("a-never-compiled", false)
	b := spec("b-compiled", true)
	c := spec("c-compiled", true)

	library := "function openHome() { atr.navigate(\"/\"); }\n"
	if err := os.WriteFile(testscript.LibraryPath(a), []byte(library), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := stampDirectory([]string{a, b, c}); err != nil {
		t.Fatalf("stamping: %v", err)
	}

	want := (&testscript.Library{Source: library}).Hash()
	for _, s := range []string{b, c} {
		stored, err := testscript.Load(s)
		if err != nil {
			t.Fatalf("loading %s: %v", filepath.Base(s), err)
		}
		if stored == nil || stored.LibraryChanged(want) {
			t.Errorf("%s was left unstamped because an earlier spec had no script",
				filepath.Base(s))
		}
	}
}

// Two rewrites of one script is not something to resolve by picking one.
// Whichever was taken would be validated and replayed and would look entirely
// sound, and the other — possibly the one the stated reason describes — would
// be dropped without a word.
func TestTheSameScriptProposedTwiceIsRefused(t *testing.T) {
	reply := "=== FILE: _shared.js\n```javascript\nfunction a(){}\n```\n\n" +
		"=== FILE: x.test.js\n```javascript\n// one\n```\n\n" +
		"=== FILE: x.test.js\n```javascript\n// two\n```\n\nREASON: r"

	if _, err := parseExtraction(reply); err == nil {
		t.Fatal("a reply proposing one script twice was accepted")
	}
}

// The prompt carries every sequence worth hoisting and the whole text of every
// script one names. Sending the rest of the directory as well buys nothing —
// nothing was found repeated in them, so this proposal cannot rewrite them.
func TestOnlyTheScriptsAnOverlapNamesAreSent(t *testing.T) {
	req := ExtractRequest{
		Scripts: map[string]string{
			"a.test.js":         "// script a",
			"b.test.js":         "// script b",
			"untouched.test.js": "// NOT PART OF ANY OVERLAP",
		},
		Overlaps: []testscript.Overlap{{
			Steps:   []string{"atr.navigate(P)", "atr.click(Q)"},
			Scripts: []string{"a.test.js", "b.test.js"},
		}},
	}

	prompt := buildExtractPrompt(req)
	if !strings.Contains(prompt, "script a") || !strings.Contains(prompt, "script b") {
		t.Error("a script the overlap names was left out of the prompt")
	}
	if strings.Contains(prompt, "NOT PART OF ANY OVERLAP") {
		t.Error("a script no overlap names was sent anyway")
	}
}

// A refusal is not recorded anywhere, so without a second attempt the next run
// finds the same repetition, asks the same question and is refused again — a
// model call spent on every run from then on, and the duplication never
// removed. The refusal is mechanical and specific, which is exactly the kind
// of thing worth handing back.
func TestASecondAttemptIsToldWhatWasWrong(t *testing.T) {
	req := ExtractRequest{
		Scripts: map[string]string{"a.test.js": "// a"},
		Overlaps: []testscript.Overlap{{
			Steps:   []string{"atr.navigate(P)"},
			Scripts: []string{"a.test.js"},
		}},
		Refused: "_shared.js runs code at the top level",
	}

	prompt := buildExtractPrompt(req)
	if !strings.Contains(prompt, "runs code at the top level") {
		t.Error("the second attempt is not told why the first was rejected")
	}
	if !strings.Contains(prompt, "already answered this once") {
		t.Error("the second attempt is not told it is a second attempt")
	}
	if !strings.Contains(prompt, "hoist nothing and say so") {
		t.Error("the second attempt is not allowed to decline, so it will invent something")
	}

	// A first attempt says none of that.
	req.Refused = ""
	if first := buildExtractPrompt(req); strings.Contains(first, "already answered") {
		t.Error("a first attempt is told it is a retry")
	}
}

// The library is replaced whole and is shared by every spec beside it, but
// only the scripts an extraction rewrites are replayed — and those are exactly
// the ones that cannot notice a missing operation, because they were rewritten
// to call the new ones. So a proposal that hoists a journey out of two specs
// and drops the login() the other eight call verifies green and is kept.
//
// The next run is where it shows: eight specs fail on a name that is not
// defined, which is a script fault, which is repairable — so the model is
// asked to fix eight scripts calling a function that no longer exists, and the
// obvious repair is to inline it into each. The library dissolves with every
// hash along the way still valid.
func TestALibraryMayGainOperationsButNotLoseThem(t *testing.T) {
	const existing = `function login(user) {
  atr.navigate("/login");
  atr.fill("#u", user);
}
function openHome() { atr.navigate("/"); }
`
	before := map[string]string{"a.test.js": `atr.step(1, "Tags", () => {
  atr.navigate(TAGS);
  atr.expectExists("#list");
});`}
	rewritten := map[string]string{"a.test.js": `atr.step(1, "Tags", () => {
  openTags(TAGS);
  atr.expectExists("#list");
});`}

	tests := []struct {
		name    string
		library string
		wantErr string
	}{
		{
			name: "gaining one is fine",
			library: existing + `function openTags(p) { atr.navigate(p); }
`,
		},
		{
			name: "dropping one is refused",
			library: `function openHome() { atr.navigate("/"); }
function openTags(p) { atr.navigate(p); }
`,
			wantErr: "login() is gone",
		},
		{
			name: "changing what one takes is refused",
			library: `function login(user, password) { atr.navigate("/login"); }
function openHome() { atr.navigate("/"); }
function openTags(p) { atr.navigate(p); }
`,
			wantErr: "now takes 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExtraction(before, existing, &Extraction{
				Library: tt.library,
				Scripts: rewritten,
			})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("a library that only gained an operation was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("a library that other specs depend on was silently replaced")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("the refusal does not name what went missing: %v", err)
			}
		})
	}
}
