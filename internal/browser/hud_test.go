package browser

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// hudWorldContext returns a live isolated-world context to evaluate in. The
// map is populated when a panel says hello, which happens on mount, so this
// polls rather than assuming the handshake has already landed.
func hudWorldContext(t *testing.T) (proto.RuntimeExecutionContextID, *rod.Page, error) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		testBrowser.mu.RLock()
		hud := testBrowser.hud
		testBrowser.mu.RUnlock()
		if hud == nil {
			return 0, nil, fmt.Errorf("hud is not enabled")
		}

		hud.mu.Lock()
		for id, page := range hud.contexts {
			hud.mu.Unlock()
			return id, page, nil
		}
		hud.mu.Unlock()

		time.Sleep(50 * time.Millisecond)
	}
	return 0, nil, fmt.Errorf("no isolated-world context registered; the panel never called the binding")
}

// hudEvalResultInWorld evaluates script inside the HUD's isolated world and
// returns its value rendered as a string.
func hudEvalResultInWorld(t *testing.T, script string) (string, error) {
	t.Helper()

	id, page, err := hudWorldContext(t)
	if err != nil {
		return "", err
	}

	res, err := (proto.RuntimeEvaluate{
		Expression:    script,
		ContextID:     id,
		ReturnByValue: true,
		AwaitPromise:  true,
	}).Call(page)
	if err != nil {
		return "", err
	}
	if res.ExceptionDetails != nil {
		return "", fmt.Errorf("script threw: %s", res.ExceptionDetails.Text)
	}
	if res.Result == nil {
		return "", nil
	}
	return res.Result.Value.String(), nil
}

// hudEvalInWorld evaluates script inside the HUD's isolated world, discarding
// the result.
func hudEvalInWorld(t *testing.T, script string) error {
	t.Helper()
	_, err := hudEvalResultInWorld(t, script)
	return err
}

// hudHostPresent reports whether the panel's host element is in the page.
func hudHostPresent(t *testing.T) bool {
	t.Helper()
	res, err := testBrowser.Evaluate(`document.getElementById("` + hudHostID + `") !== null`)
	if err != nil {
		t.Fatalf("evaluating for the hud host: %v", err)
	}
	present, ok := res.(bool)
	if !ok {
		t.Fatalf("expected a boolean, got %T (%v)", res, res)
	}
	return present
}

// waitForHudHost polls until the panel mounts. Injection is asynchronous:
// the script waits for DOMContentLoaded before it touches the DOM.
func waitForHudHost(t *testing.T, want bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if hudHostPresent(t) == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for hud host present=%v", want)
}

// enableTestHud installs a HUD whose handler records the prompts it receives,
// and guarantees teardown.
func enableTestHud(t *testing.T) chan string {
	t.Helper()
	prompts := make(chan string, 8)

	handler := func(ctx context.Context, prompt string, emit func(HudEvent)) {
		prompts <- prompt
		emit(HudEvent{Type: "done", Text: "handled: " + prompt})
	}
	if err := testBrowser.EnableHud(handler); err != nil {
		t.Fatalf("EnableHud: %v", err)
	}
	t.Cleanup(func() {
		if err := testBrowser.DisableHud(); err != nil {
			t.Errorf("DisableHud: %v", err)
		}
	})
	return prompts
}

func TestHudMountsAndUnmounts(t *testing.T) {
	resetFixture(t)

	if testBrowser.HudEnabled() {
		t.Fatal("hud should start disabled")
	}

	enableTestHud(t)
	if !testBrowser.HudEnabled() {
		t.Error("HudEnabled() should report true after EnableHud")
	}

	// EnableHud only registers the injection; the panel appears on the next
	// document, or immediately via RunImmediately.
	waitForHudHost(t, true)

	if err := testBrowser.DisableHud(); err != nil {
		t.Fatalf("DisableHud: %v", err)
	}
	waitForHudHost(t, false)
	if testBrowser.HudEnabled() {
		t.Error("HudEnabled() should report false after DisableHud")
	}
}

// The panel must come back by itself after a navigation, otherwise it would
// vanish the first time the user clicked a link.
func TestHudSurvivesNavigation(t *testing.T) {
	resetFixture(t)
	enableTestHud(t)
	waitForHudHost(t, true)

	ctx := context.Background()
	if err := testBrowser.Navigate(ctx, testFixtureURL+"/test_fixture.html?second"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	waitForHudHost(t, true)
}

// The panel lives behind a closed shadow root specifically so the agent does
// not see its own controls as page content. If this fails, every snapshot the
// agent takes is polluted with the HUD's own buttons.
func TestHudIsInvisibleToSnapshot(t *testing.T) {
	resetFixture(t)
	enableTestHud(t)
	waitForHudHost(t, true)

	elements, err := testBrowser.Snapshot(false)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	for _, el := range elements {
		for _, marker := range []string{"atr agent", "Send", hudHostID} {
			if strings.Contains(el.Name, marker) || strings.Contains(el.Text, marker) {
				t.Errorf("snapshot exposed hud content: uid=%s tag=%s name=%q text=%q",
					el.UID, el.TagName, el.Name, el.Text)
			}
		}
	}
}

// Page script must not be able to reach the bridge. If it could, any site
// could drive the agent by calling it.
func TestHudBridgeIsNotReachableFromPageScript(t *testing.T) {
	resetFixture(t)
	enableTestHud(t)
	waitForHudHost(t, true)

	for _, global := range []string{"__atrHudSend", "__atrHudDeliver", "__atrHudMounted"} {
		res, err := testBrowser.Evaluate(`typeof window["` + global + `"]`)
		if err != nil {
			t.Fatalf("evaluating typeof %s: %v", global, err)
		}
		if got, _ := res.(string); got != "undefined" {
			t.Errorf("page script can see %s (typeof %q); the isolated world is not isolating", global, got)
		}
	}
}

// The host element must not be reachable through the shadow root either.
func TestHudShadowRootIsClosed(t *testing.T) {
	resetFixture(t)
	enableTestHud(t)
	waitForHudHost(t, true)

	res, err := testBrowser.Evaluate(
		`document.getElementById("` + hudHostID + `").shadowRoot === null`)
	if err != nil {
		t.Fatalf("evaluating shadowRoot: %v", err)
	}
	if closed, _ := res.(bool); !closed {
		t.Error("shadow root is open; page script can read and restyle the panel")
	}
}

// A screenshot is what the agent and the user look at to decide what to do
// next. The agent's own panel must not be baked into it.
func TestHudHiddenDuringScreenshot(t *testing.T) {
	resetFixture(t)
	enableTestHud(t)
	waitForHudHost(t, true)

	if _, err := testBrowser.Screenshot(false); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}

	// The panel must be visible again afterwards, or it would disappear the
	// first time the agent took a screenshot.
	res, err := testBrowser.Evaluate(
		`document.getElementById("` + hudHostID + `").style.display`)
	if err != nil {
		t.Fatalf("evaluating display: %v", err)
	}
	if display, _ := res.(string); display == "none" {
		t.Error("hud was left hidden after the screenshot")
	}
}

// The full round trip: the panel calls the binding, Go dispatches to the
// handler, and the handler's events are delivered back into the page.
func TestHudRoundTripThroughTheBridge(t *testing.T) {
	resetFixture(t)
	prompts := enableTestHud(t)
	waitForHudHost(t, true)

	// Capture what the panel renders. The transcript is inside a closed
	// shadow root, so the assertion goes through a hook installed in the
	// isolated world rather than through the DOM.
	if err := hudEvalInWorld(t, `
		globalThis.__testSeen = [];
		const original = globalThis.__atrHudDeliver;
		globalThis.__atrHudDeliver = (ev) => { globalThis.__testSeen.push(ev); original(ev); };
		globalThis.__atrHudSend(JSON.stringify({op: "ask", text: "fill the password"}));
	`); err != nil {
		t.Fatalf("driving the panel: %v", err)
	}

	select {
	case got := <-prompts:
		if got != "fill the password" {
			t.Errorf("handler received %q, want %q", got, "fill the password")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the handler was never called; the JS→Go binding is not working")
	}

	// And the handler's reply must arrive back in the page.
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, err := hudEvalResultInWorld(t,
			`JSON.stringify((globalThis.__testSeen || []).map(e => e.t + ":" + (e.text || "")))`)
		if err == nil && strings.Contains(out, "done:handled: fill the password") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the handler's reply never reached the page; last saw: %s", out)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The panel must actually render at a usable size. A CSS mistake that
// collapsed it to zero height would leave every other test passing — the host
// element would still be present and the bridge would still work — while the
// user saw nothing.
func TestHudPanelRendersAtUsableSize(t *testing.T) {
	resetFixture(t)
	enableTestHud(t)
	waitForHudHost(t, true)

	// Geometry has to be read from inside the closed shadow root, so this
	// goes through the isolated world rather than the DOM.
	out, err := hudEvalResultInWorld(t, `
		(() => {
			const host = document.getElementById(`+"\""+hudHostID+"\""+`);
			const root = globalThis.__atrHudTestRoot;
			const panel = root && root.querySelector('.panel');
			if (!panel) return "no panel";
			const r = panel.getBoundingClientRect();
			return JSON.stringify({w: Math.round(r.width), h: Math.round(r.height), host: !!host});
		})()
	`)
	if err != nil {
		t.Fatalf("reading panel geometry: %v", err)
	}
	if strings.Contains(out, "no panel") {
		t.Fatalf("panel element missing from the shadow root")
	}
	if !strings.Contains(out, `"w":380`) {
		t.Errorf("panel is not at its designed width, got %s", out)
	}
	if strings.Contains(out, `"h":0`) {
		t.Errorf("panel collapsed to zero height, got %s", out)
	}
}

// The transcript is replayed into every panel that appears, so it must not
// grow without bound over a long session.
func TestHudTranscriptIsCapped(t *testing.T) {
	hud := &hudSession{}

	for i := 0; i < hudMaxHistory+50; i++ {
		hudRecord(hud, HudEvent{Type: "done", Text: fmt.Sprintf("event %d", i)})
	}

	hud.mu.Lock()
	defer hud.mu.Unlock()

	if len(hud.history) != hudMaxHistory {
		t.Errorf("history holds %d events, want %d", len(hud.history), hudMaxHistory)
	}
	// The newest events are the ones worth keeping.
	if last := hud.history[len(hud.history)-1].Text; last != fmt.Sprintf("event %d", hudMaxHistory+49) {
		t.Errorf("newest event was dropped, last is %q", last)
	}
}
