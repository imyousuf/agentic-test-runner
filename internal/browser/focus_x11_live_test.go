//go:build linux

package browser

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/icccm"

	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// The whole point of the focus work is what a person at a desktop sees, so it
// is proved on one: a headed Chrome for Testing, a real window manager, and a
// window standing in for whatever the user is working in.
//
// It needs a display nobody is using, so it runs only when given one. A nested
// X server with its own window manager does:
//
//	Xephyr :57 -screen 1280x800 -ac &
//	DISPLAY=:57 dbus-run-session -- mutter --x11 --replace --sm-disable &
//	ATR_FOCUS_DISPLAY=:57 go test -run TestTheBrowserDoesNotTakeTheFocus ./internal/browser/
func TestTheBrowserDoesNotTakeTheFocus(t *testing.T) {
	d := newFocusDesktop(t)
	b := d.launch()
	ctx := context.Background()

	// The control first. On this display, without the keeper, an activation
	// and a click do take the focus — so everything after passes because the
	// focus was kept, not because nothing here could have taken it.
	//
	// The activation comes first because it is what makes the click count. A
	// window that has never been active does not take the focus on a click;
	// once it has been — the user clicked it, or a tab in it was activated —
	// every click takes it.
	t.Run("control: unguarded calls take the focus", func(t *testing.T) {
		if err := b.NewPage(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
			t.Fatal(err)
		}
		page, err := b.CurrentPage()
		if err != nil {
			t.Fatal(err)
		}

		d.putUserInFront(t)
		if err := (proto.TargetActivateTarget{TargetID: page.TargetID}).Call(b.browser); err != nil {
			t.Fatal(err)
		}
		if d.settled() {
			t.Error("an unguarded activation did not take the focus")
		}

		d.putUserInFront(t)
		el, err := page.Timeout(5 * time.Second).Element("#test-button")
		if err != nil {
			t.Fatal(err)
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			t.Fatal(err)
		}
		if d.settled() {
			t.Error("an unguarded click did not take the focus, so this display cannot tell a kept focus from an untested one")
		}
	})

	t.Run("every step keeps the user's focus", func(t *testing.T) {
		steps := []struct {
			name string
			do   func() error
		}{
			{"a click", func() error { return b.Click(ctx, "#test-button", false) }},
			{"another click", func() error { return b.Click(ctx, "#test-button", false) }},
			{"a double-click", func() error { return b.Click(ctx, "#test-button", true) }},
			{"a click on visible text", func() error { return b.Click(ctx, "Test Link", false) }},
			{"Tab out of the page", func() error {
				for i := 0; i < 40; i++ {
					if err := b.PressKey("Tab"); err != nil {
						return err
					}
				}
				return nil
			}},
			{"opening a second page", func() error { return b.NewPage(ctx, testFixtureURL+"/has_text.html") }},
			{"selecting the first page", func() error { return b.SelectPage(0) }},
			{"selecting the second page", func() error { return b.SelectPage(1) }},
		}
		for _, step := range steps {
			d.putUserInFront(t)
			if err := step.do(); err != nil {
				t.Fatalf("%s: %v", step.name, err)
			}
			if !d.settled() {
				t.Errorf("%s took the focus from the user's window", step.name)
			}
		}
	})

	t.Run("every page renders though none was brought forward", func(t *testing.T) {
		for i := range b.ListPages() {
			if err := b.SelectPage(i); err != nil {
				t.Fatal(err)
			}
			d.wantScreenshot(t, b, i)
		}
	})

	// Giving the focus back is, to the page, the user switching away: the
	// focused element would blur and a menu open on it would close. Headless,
	// and with the focus left where Chrome put it, neither happens — so the
	// page is held focused, and the step after a click finds the page the
	// click left.
	t.Run("a click leaves the page focused, as it was before", func(t *testing.T) {
		if err := b.NewPage(ctx, testFixtureURL+"/focus_state.html"); err != nil {
			t.Fatal(err)
		}
		page, err := b.CurrentPage()
		if err != nil {
			t.Fatal(err)
		}

		d.putUserInFront(t)
		if err := b.Click(ctx, "#name", false); err != nil {
			t.Fatal(err)
		}
		if !d.settled() {
			t.Fatal("the click took the focus from the user's window")
		}
		st := pageFocusState(t, page)
		if !st.HasFocus || st.Active != "name" || !st.MenuShown || strings.Contains(st.Log, "blur") {
			t.Errorf("after the click the page is %+v; want it focused, on the field, menu open, never blurred", st)
		}

		// The next step of a combobox spec: typing into the field.
		if err := b.PressKey("a"); err != nil {
			t.Fatal(err)
		}
		if v, _ := page.Eval(`() => document.getElementById("name").value`); v.Value.Str() != "A" {
			t.Errorf("typing after the click put %q in the field, want %q", v.Value.Str(), "A")
		}
		if !d.settled() {
			t.Error("typing took the focus from the user's window")
		}
	})

	// The hold is for a while, not for good: a page always focused tells the
	// application someone is reading it. Once ATR leaves the page alone, it
	// learns — as it would have — that nobody is looking.
	t.Run("the hold lapses once ATR leaves the page alone", func(t *testing.T) {
		b.focusHoldFor = 2 * time.Second
		defer func() { b.focusHoldFor = 0 }()

		if err := b.NewPage(ctx, testFixtureURL+"/focus_state.html"); err != nil {
			t.Fatal(err)
		}
		page, err := b.CurrentPage()
		if err != nil {
			t.Fatal(err)
		}
		d.putUserInFront(t)
		if err := b.Click(ctx, "#name", false); err != nil {
			t.Fatal(err)
		}
		if st := pageFocusState(t, page); !st.HasFocus {
			t.Fatalf("not held after the click: %+v", st)
		}

		time.Sleep(4 * time.Second)
		if st := pageFocusState(t, page); st.HasFocus || !strings.Contains(st.Log, "window-blur") {
			t.Errorf("after the hold lapsed the page is %+v; want it to know it is not focused", st)
		}
	})

	// A tab sharing a window with another is the one kind of page that still
	// needs activating — one the user opened, or one a page opened. It is
	// activated, and the focus is given back.
	t.Run("a hidden tab is activated without taking the focus", func(t *testing.T) {
		opener, err := b.CurrentPage()
		if err != nil {
			t.Fatal(err)
		}
		before := len(b.ListPages())

		// A tab in the opener's window. window.open puts it in front.
		if _, err := opener.Eval(`() => { window.open("/has_text.html", "_blank"); return true; }`); err != nil {
			t.Fatalf("opening a tab from the page: %v", err)
		}
		opened := -1
		for i := 0; i < 50 && opened < 0; i++ {
			time.Sleep(100 * time.Millisecond)
			if pages := b.ListPages(); len(pages) > before {
				opened = len(pages) - 1
			}
		}
		if opened < 0 {
			t.Fatal("the opened tab was never adopted")
		}

		// Put the opener back in front, so the new tab is hidden behind it.
		if err := (proto.TargetActivateTarget{TargetID: opener.TargetID}).Call(b.browser); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		pages := b.ListPages()
		if v := pageVisibility(pageByTarget(t, b, pages[opened].TargetID)); v != "hidden" {
			t.Fatalf("the opened tab is %q, want it hidden behind its opener for this test to mean anything", v)
		}

		d.putUserInFront(t)
		if err := b.SelectPage(opened); err != nil {
			t.Fatal(err)
		}
		if !d.settled() {
			t.Error("activating a hidden tab took the focus from the user's window")
		}
		d.wantScreenshot(t, b, opened)
	})

	// A click on a file input opens the operating system's file chooser, and
	// whoever clicked means to use it next — `atr computer type` the path
	// into it. The window manager gives a dialog the focus only if its window
	// has had it, and a page ATR opened in a background window has not; left
	// alone, the chooser opened behind the user's work, flagged as wanting
	// attention, and the typing would have gone to the user's window. So such
	// a click brings the browser forward, and the chooser has the focus.
	t.Run("a click that opens the file chooser gives it the focus", func(t *testing.T) {
		for _, target := range []string{"#upload", "Choose a file"} {
			if err := b.NewPage(ctx, testFixtureURL+"/file_input.html"); err != nil {
				t.Fatal(err)
			}
			d.putUserInFront(t)
			if err := b.Click(ctx, target, false); err != nil {
				t.Fatalf("clicking %q: %v", target, err)
			}

			ws := b.focus.desktop()
			chooser := d.waitForActive(10*time.Second, func(w xproto.Window) bool {
				return ws.ownedByBrowser(uint32(w)) && ws.isDialog(uint32(w))
			})
			if chooser == 0 {
				active, _ := ewmh.ActiveWindowGet(d.xu)
				name, _ := ewmh.WmNameGet(d.xu, active)
				t.Errorf("after clicking %q the focus is on %q, want the browser's file chooser", target, name)
				continue
			}
			time.Sleep(700 * time.Millisecond)
			if w, _ := ewmh.ActiveWindowGet(d.xu); w != chooser {
				t.Errorf("after clicking %q the file chooser had the focus and lost it", target)
			}
			_ = ewmh.CloseWindow(d.xu, chooser)
			time.Sleep(time.Second)
		}
	})

	// A recording is something the user does, on the page, so the page being
	// recorded is the one exception: it is brought forward.
	t.Run("a recording brings its page forward", func(t *testing.T) {
		d.putUserInFront(t)
		if err := b.StartRecording(""); err != nil {
			t.Fatalf("starting a recording: %v", err)
		}
		ws := b.focus.desktop()
		if w := d.waitForActive(5*time.Second, func(w xproto.Window) bool {
			return ws.ownedByBrowser(uint32(w))
		}); w == 0 {
			t.Error("starting a recording did not bring the browser forward")
		}
		if _, err := b.StopRecording(); err != nil {
			t.Fatalf("stopping the recording: %v", err)
		}
	})

	// Every X client is sent a keyboard mapping change, asked for or not, and
	// xgb queues events until they are read — a few thousand at most. The
	// keeper's connection lives as long as the browser; unread, it would
	// fill, and every click after would hang waiting on it.
	//
	// Last, because the window manager reloads its keymap for every one of
	// them and is busy for a while afterwards.
	t.Run("the desktop connection survives a flood of events", func(t *testing.T) {
		ws := b.focus.desktop()
		if ws == nil {
			t.Fatal("no desktop connection")
		}
		floodMappingNotify(t, 6000)

		answered := make(chan bool, 1)
		go func() {
			_, ok := ws.active()
			answered <- ok
		}()
		select {
		case ok := <-answered:
			if !ok {
				t.Error("the desktop connection stopped answering after the flood")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the desktop connection hung after a flood of events: its queue is not being read")
		}

		// And a guarded call still goes through rather than waiting on it.
		guarded := make(chan error, 1)
		go func() { guarded <- b.focus.around(nil, func() error { return nil }) }()
		select {
		case <-guarded:
		case <-time.After(5 * time.Second):
			t.Fatal("a guarded call hung after a flood of events")
		}
	})
}

// pageByTarget is the browser's page for a target.
func pageByTarget(t *testing.T, b *Browser, target string) *rod.Page {
	t.Helper()
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, p := range b.pages {
		if string(p.TargetID) == target {
			return p
		}
	}
	t.Fatalf("no page for target %s", target)
	return nil
}

// focusDesktop is the display a live focus test runs on, with a window
// standing in for the user's.
type focusDesktop struct {
	t     *testing.T
	xu    *xgbutil.XUtil
	users xproto.Window
}

func newFocusDesktop(t *testing.T) *focusDesktop {
	t.Helper()
	display := os.Getenv("ATR_FOCUS_DISPLAY")
	if display == "" {
		t.Skip("set ATR_FOCUS_DISPLAY to a display with a window manager to run this; see the comment")
	}
	if _, err := exec.LookPath("xlogo"); err != nil {
		t.Skip("needs xlogo to stand in for the user's window")
	}

	// Everything below — Chrome, the keeper's own connection, this test's —
	// is on that display, and the rest of the package is on none.
	t.Setenv("DISPLAY", display)
	// The file chooser must open on that display too. With a session bus to
	// reach, Chrome may ask the desktop portal for it instead, which would
	// put it on the real desktop — so there is none to reach.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/atr-focus-test")

	xu, err := xgbutil.NewConn()
	if err != nil {
		t.Fatalf("connecting to %s: %v", display, err)
	}
	drainEvents(xu)
	t.Cleanup(func() { xu.Conn().Close() })

	d := &focusDesktop{t: t, xu: xu}
	d.users = startUsersWindow(t, xu)
	d.putUserInFront(t)
	return d
}

// launch starts a headed Chrome for Testing on the display: the build ATR
// downloads, and the one that focuses its window on a mouse-down.
func (d *focusDesktop) launch() *Browser {
	t := d.t
	t.Helper()
	b, err := New(config.BrowserConfig{
		Headless:   false,
		NoSandbox:  true,
		Executable: "auto",
		Version:    "latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Launch(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if b.focus == nil {
		t.Fatal("no focus keeper on a display with a window manager")
	}
	return b
}

func (d *focusDesktop) inFront() bool {
	w, err := ewmh.ActiveWindowGet(d.xu)
	return err == nil && w == d.users
}

func (d *focusDesktop) putUserInFront(t *testing.T) {
	t.Helper()
	for i := 0; i < 20; i++ {
		_ = ewmh.ActiveWindowReq(d.xu, d.users)
		time.Sleep(100 * time.Millisecond)
		if d.inFront() {
			return
		}
	}
	t.Fatal("could not give the stand-in window the focus")
}

// settled reports whether the user's window has the focus once the browser
// has had time to take it — longer than any grab measured.
func (d *focusDesktop) settled() bool {
	time.Sleep(700 * time.Millisecond)
	return d.inFront()
}

// waitForActive waits for a window matching want to have the focus, and
// returns it, or 0 if none does in time.
func (d *focusDesktop) waitForActive(within time.Duration, want func(xproto.Window) bool) xproto.Window {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if w, err := ewmh.ActiveWindowGet(d.xu); err == nil && w != 0 && want(w) {
			return w
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0
}

// wantScreenshot checks page i renders: a screenshot of it returns an image.
// One of a page that is not rendering never returns.
func (d *focusDesktop) wantScreenshot(t *testing.T, b *Browser, i int) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		data, err := b.Screenshot(false)
		if err == nil && len(data) == 0 {
			err = os.ErrInvalid
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("screenshot of page %d: %v", i, err)
		}
	case <-time.After(15 * time.Second):
		t.Errorf("screenshot of page %d never returned: the page is not rendering", i)
	}
}

// focusState is what focus_state.html records about itself.
type focusState struct {
	Log       string `json:"log"`
	Active    string `json:"active"`
	HasFocus  bool   `json:"hasFocus"`
	MenuShown bool   `json:"menuShown"`
}

func pageFocusState(t *testing.T, page *rod.Page) focusState {
	t.Helper()
	res, err := page.Eval(`() => JSON.stringify({
		log: document.getElementById("log").textContent,
		active: document.activeElement ? document.activeElement.id : "",
		hasFocus: document.hasFocus(),
		menuShown: !document.getElementById("menu").hidden,
	})`)
	if err != nil {
		t.Fatalf("reading the page's focus state: %v", err)
	}
	var st focusState
	if err := json.Unmarshal([]byte(res.Value.Str()), &st); err != nil {
		t.Fatalf("reading the page's focus state: %v", err)
	}
	return st
}

// drainEvents reads and drops a connection's events, so that the flood this
// test sends cannot fill its own queue.
func drainEvents(xu *xgbutil.XUtil) {
	conn := xu.Conn()
	go func() {
		for {
			ev, err := conn.WaitForEvent()
			if ev == nil && err == nil {
				return
			}
		}
	}()
}

// floodMappingNotify makes the X server send every client n keyboard mapping
// notifications, by setting one key's mapping to what it already is n times.
func floodMappingNotify(t *testing.T, n int) {
	t.Helper()
	xu, err := xgbutil.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(xu)
	defer xu.Conn().Close()

	conn := xu.Conn()
	key := xproto.Setup(conn).MinKeycode
	mapping, err := xproto.GetKeyboardMapping(conn, key, 1).Reply()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		xproto.ChangeKeyboardMapping(conn, 1, key, mapping.KeysymsPerKeycode, mapping.Keysyms)
	}
	// A round trip, so that every one of them has been processed.
	if _, err := xproto.GetInputFocus(conn).Reply(); err != nil {
		t.Fatal(err)
	}
}

// startUsersWindow opens a window to stand in for the user's work, and returns
// it once the window manager has it.
func startUsersWindow(t *testing.T, xu *xgbutil.XUtil) xproto.Window {
	t.Helper()

	cmd := exec.Command("xlogo", "-geometry", "300x200+40+40", "-title", "users-window")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting xlogo: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		clients, _ := ewmh.ClientListGet(xu)
		for _, w := range clients {
			if class, err := icccm.WmClassGet(xu, w); err == nil && strings.EqualFold(class.Class, "xlogo") {
				return w
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the stand-in window never appeared")
	return 0
}
