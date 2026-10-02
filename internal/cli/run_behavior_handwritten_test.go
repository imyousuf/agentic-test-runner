package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

// captureStdout returns what fn prints.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()

	done := make(chan string)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

func writeHandWritten(t *testing.T, dir, name, source string) string {
	t.Helper()
	specPath := filepath.Join(dir, name+".test.txt")
	if err := os.WriteFile(specPath, []byte("Test: sign in\n\nSteps:\n1. Sign in\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testscript.ScriptPath(specPath), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return specPath
}

// A hand-written script is replayed whatever the spec says, so a missing base
// URL is no more fatal for it than for any other replay. needsLiveApp asked
// Fresh, which is never true of a script with no hash — so a run of a
// hand-written script that navigates to absolute addresses was refused before
// it started, for want of an address it would never have used.
func TestAHandWrittenScriptDoesNotDemandABaseURL(t *testing.T) {
	const spec = "Test: sign in\n\nSteps:\n1. Sign in\n"
	specPath := writeHandWritten(t, t.TempDir(), "login",
		"// mine\natr.step(1, \"Sign in\", () => { expect(atr.text(\"#status\")).toBe(\"in\"); });\n")

	if needsLiveApp(specPath, spec) {
		t.Error("a hand-written script demanded a base URL; it is only ever replayed")
	}
	// No edit to the spec changes that: there is no hash to hold it against.
	if needsLiveApp(specPath, spec+"2. And check something else\n") {
		t.Error("editing the spec made a hand-written script demand a base URL")
	}

	// --recompile is the one thing that does compile it, and a compile needs
	// somewhere to point the browser.
	recompileFlag = true
	defer func() { recompileFlag = false }()
	if !needsLiveApp(specPath, spec) {
		t.Error("--recompile on a hand-written script did not report that it needs the application")
	}
}

// "Re-run to let the agent repair it" is the advice for a drifted compiled
// script. For a hand-written one nothing will repair it however often it is
// re-run, so the advice has to be different.
func TestDriftAdviceForAHandWrittenScriptDoesNotPromiseARepair(t *testing.T) {
	failure := &testscript.Failure{
		Kind: testscript.KindNotFound, Message: `click "#gone": element not found: #gone`,
		Step: 1, StepDesc: "Sign in", Target: "#gone",
	}

	handWritten := captureStdout(t, func() {
		printBehaviorOutcome("login.test.txt", &agent.RunOutcome{
			HandWritten: true,
			ScriptPath:  "login.test.js",
			Result:      &testscript.Result{Failure: failure},
		})
	})
	if strings.Contains(handWritten, "re-run to let the agent repair it") {
		t.Errorf("a hand-written script was promised a repair:\n%s", handWritten)
	}
	if !strings.Contains(handWritten, "hand-written") || !strings.Contains(handWritten, "--recompile") {
		t.Errorf("the advice does not say the script is hand-written and how to replace it:\n%s", handWritten)
	}

	// The compiled case keeps the advice it had.
	compiled := captureStdout(t, func() {
		printBehaviorOutcome("login.test.txt", &agent.RunOutcome{
			ScriptPath: "login.test.js",
			Result:     &testscript.Result{Failure: failure},
		})
	})
	if !strings.Contains(compiled, "re-run to let the agent repair it") {
		t.Errorf("the advice for a compiled script changed:\n%s", compiled)
	}
}

// A dry run reports what a real run would hoist. A real run does not hoist
// hand-written scripts, so repetition involving one is not reported either.
func TestADryRunDoesNotOfferToHoistAHandWrittenScript(t *testing.T) {
	const journey = `atr.step(1, "Sign in", () => {
  atr.navigate("/login");
  atr.fill("#username", "someone");
  atr.click("#submit");
  atr.expectExists("#dashboard");
});
`
	header := "// atr-spec-sha256: " + testscript.SpecHash("Test: sign in\n\nSteps:\n1. Sign in\n") + "\n"

	t.Run("beside one compiled script", func(t *testing.T) {
		dir := t.TempDir()
		compiled := writeHandWritten(t, dir, "a", header+journey)
		mine := writeHandWritten(t, dir, "mine", "// mine\n"+journey)

		var err error
		out := captureStdout(t, func() { err = reportOverlaps([]string{compiled, mine}) })
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Nothing is repeated") {
			t.Errorf("repetition with a hand-written script was reported as hoistable:\n%s", out)
		}
	})

	t.Run("beside two compiled scripts that repeat each other", func(t *testing.T) {
		dir := t.TempDir()
		a := writeHandWritten(t, dir, "a", header+journey)
		b := writeHandWritten(t, dir, "b", header+journey)
		mine := writeHandWritten(t, dir, "mine", "// mine\n"+journey)

		var err error
		out := captureStdout(t, func() { err = reportOverlaps([]string{a, b, mine}) })
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "a.test.js") || !strings.Contains(out, "b.test.js") {
			t.Errorf("the compiled pair's repetition was not reported:\n%s", out)
		}
		if strings.Contains(out, "mine.test.js") {
			t.Errorf("the hand-written script was named as something to hoist from:\n%s", out)
		}
	})
}
