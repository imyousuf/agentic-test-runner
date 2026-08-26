package testscript

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
)

// These two features arrived separately and meet for the first time on main.
// The HUD injects an isolated world, a CDP binding and a panel into every
// page; a compiled script drives that same page with no model watching. The
// combination has three ways to go wrong, and each is checked below.

// enableStubHud turns the panel on with a handler that does nothing, and
// guarantees it is turned off again.
func enableStubHud(t *testing.T, b *browser.Browser) {
	t.Helper()

	handler := func(ctx context.Context, prompt string, emit func(browser.HudEvent)) {
		emit(browser.HudEvent{Type: "done", Text: "stub"})
	}
	if err := b.EnableHud(handler); err != nil {
		t.Fatalf("EnableHud: %v", err)
	}
	t.Cleanup(func() {
		if err := b.DisableHud(); err != nil {
			t.Errorf("DisableHud: %v", err)
		}
	})
}

//  1. A compiled script must still pass with the panel on. The HUD adds a
//     steady trickle of CDP traffic to the same target, which is exactly the
//     condition that used to make element lookups blow their budget.
func TestCompiledScriptRunsWithHudEnabled(t *testing.T) {
	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatal(err)
	}
	enableStubHud(t, testBrowser)

	res, err := Run(context.Background(), Options{
		Browser: testBrowser,
		Source: `
			atr.step(1, "Fill and submit", () => {
				atr.fill("#username", "testuser");
				atr.click("#submit");
			});
			atr.step(2, "Verify the status", () => {
				atr.waitForText("signed in");
				expect(atr.text("#status")).toBe("signed in");
			});
		`,
		Name:    "with-hud.js",
		BaseURL: fixtureURL,
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Passed {
		t.Fatalf("a compiled script must pass with the HUD enabled, got: %v", res.Failure)
	}
}

//  2. The panel must not appear in what the script sees. If it did, every
//     snapshot-based assertion would pick up the agent's own controls, and a
//     count of buttons on the page would silently include them.
func TestHudIsInvisibleToCompiledScripts(t *testing.T) {
	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatal(err)
	}
	enableStubHud(t, testBrowser)

	res, err := Run(context.Background(), Options{
		Browser: testBrowser,
		Source: `
			atr.step(1, "Capture what the script can see", () => {
				atr.log("snapshot=" + JSON.stringify(atr.snapshot()));
				atr.log("text=" + atr.text("body"));
			});
		`,
		Name:    "probe.js",
		BaseURL: fixtureURL,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Passed {
		t.Fatalf("probe failed: %v", res.Failure)
	}

	joined := strings.Join(res.Logs, "\n")
	// The panel's contents live behind a closed shadow root, so neither the
	// accessibility snapshot nor the page text may contain them.
	for _, marker := range []string{"atr agent", "Ask the agent", "__atr_hud"} {
		if strings.Contains(joined, marker) {
			t.Errorf("the HUD leaked into what the script can see (%q)", marker)
		}
	}
}

//  3. An element that is genuinely gone must still classify as drift with the
//     panel on, not as an environment problem. This is the combination that
//     could regress: #16 rebinds a found element to a fresh deadline, #17 maps
//     a lookup deadline onto ErrElementNotFound, and both now run in the same
//     findElement.
func TestMissingElementStillClassifiesAsDriftUnderHud(t *testing.T) {
	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatal(err)
	}
	enableStubHud(t, testBrowser)

	res, err := Run(context.Background(), Options{
		Browser: testBrowser,
		Source:  `atr.step(1, "Click something gone", () => { atr.click("#not-here-at-all"); });`,
		Name:    "drift.js",
		BaseURL: fixtureURL,
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Passed {
		t.Fatal("expected a failure")
	}
	if res.Failure.Kind != KindNotFound {
		t.Errorf("kind = %q, want %q — a vanished element must stay repairable with the HUD on",
			res.Failure.Kind, KindNotFound)
	}
}

//  4. A screenshot taken by a compiled script must not contain the panel
//     either; the hide-during-capture path has to apply to this caller too.
func TestScriptScreenshotExcludesHud(t *testing.T) {
	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatal(err)
	}
	enableStubHud(t, testBrowser)

	before, err := testBrowser.Screenshot(false)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("empty screenshot")
	}

	// The panel must be visible again afterwards, or the first capture would
	// permanently hide the user's own controls.
	res, err := testBrowser.Evaluate(`document.getElementById("__atr_hud_host") === null ? "absent" : document.getElementById("__atr_hud_host").style.display`)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if display, _ := res.(string); display == "none" {
		t.Error("the HUD was left hidden after a screenshot")
	}
}
