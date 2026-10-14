package browser

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// fakeDesktop is a window system whose focus moves when told to, standing in
// for a window manager in tests that cannot have one.
type fakeDesktop struct {
	mu        sync.Mutex
	focused   uint32
	readable  bool
	browsers  map[uint32]bool
	dialogs   map[uint32]bool
	requests  []uint32
	timeline  []func() // run, one per call, by the next active() after the guarded call
	closed    bool
	afterCall bool
}

const (
	usersEditor   uint32 = 101
	usersTerminal uint32 = 102
	browserWindow uint32 = 201
	browserPopup  uint32 = 202
	fileChooser   uint32 = 203
)

func newFakeDesktop(focused uint32) *fakeDesktop {
	return &fakeDesktop{
		focused:  focused,
		readable: true,
		browsers: map[uint32]bool{browserWindow: true, browserPopup: true, fileChooser: true},
		dialogs:  map[uint32]bool{fileChooser: true},
	}
}

func (d *fakeDesktop) active() (uint32, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.afterCall && len(d.timeline) > 0 {
		next := d.timeline[0]
		d.timeline = d.timeline[1:]
		next()
	}
	return d.focused, d.readable
}

func (d *fakeDesktop) ownedByBrowser(w uint32) bool { return d.browsers[w] }

func (d *fakeDesktop) isDialog(w uint32) bool { return d.dialogs[w] }

func (d *fakeDesktop) activate(w uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.requests = append(d.requests, w)
	d.focused = w
	return nil
}

func (d *fakeDesktop) close() { d.closed = true }

// focusTo returns a step that moves the focus to w.
func (d *fakeDesktop) focusTo(w uint32) func() { return func() { d.focused = w } }

func (d *fakeDesktop) nothing() func() { return func() {} }

// call is a guarded call that, once made, lets the desktop's timeline play.
func (d *fakeDesktop) call(err error) func() error {
	return func() error {
		d.mu.Lock()
		d.afterCall = true
		d.mu.Unlock()
		return err
	}
}

func fastKeeper(d *fakeDesktop) *focusKeeper {
	k := newFocusKeeper(d)
	k.watch = 150 * time.Millisecond
	k.settle = 40 * time.Millisecond
	k.poll = time.Millisecond
	return k
}

// The case all of this is for: the user is in their editor, a click makes the
// browser take the focus, and the editor gets it back.
func TestTheFocusIsGivenBackWhenTheBrowserTakesIt(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.nothing(), d.nothing(), d.focusTo(browserWindow)}

	if err := fastKeeper(d).around(d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if d.focused != usersEditor {
		t.Errorf("focus ended on %d, want the editor (%d)", d.focused, usersEditor)
	}
	if len(d.requests) != 1 || d.requests[0] != usersEditor {
		t.Errorf("activation requests = %v, want one, for the editor", d.requests)
	}
}

// A call that takes nothing changes nothing, and does not hold the caller past
// the watch.
func TestNothingIsAskedOfTheDesktopWhenTheBrowserTakesNothing(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	k := fastKeeper(d)

	start := time.Now()
	if err := k.around(d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if len(d.requests) != 0 {
		t.Errorf("activation requests = %v, want none", d.requests)
	}
	if elapsed := time.Since(start); elapsed > k.watch+100*time.Millisecond {
		t.Errorf("held the call for %v with a %v watch", elapsed, k.watch)
	}
}

// A user who is looking at the browser is not pulled out of it.
func TestAUserInTheBrowserIsLeftThere(t *testing.T) {
	d := newFakeDesktop(browserWindow)
	d.timeline = []func(){d.focusTo(browserPopup)}

	start := time.Now()
	if err := fastKeeper(d).around(d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if len(d.requests) != 0 {
		t.Errorf("activation requests = %v, want none: the user was in the browser", d.requests)
	}
	// And there was nothing to watch for.
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("watched for %v with nothing to give back", elapsed)
	}
}

// The focus moving somewhere that is not the browser is the user choosing,
// and is not undone.
func TestTheUsersOwnSwitchIsNotUndone(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(usersTerminal)}

	if err := fastKeeper(d).around(d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if d.focused != usersTerminal || len(d.requests) != 0 {
		t.Errorf("focus = %d, requests = %v; want the terminal the user chose, untouched", d.focused, d.requests)
	}
}

// A click that opens a pop-up activates twice: once for the click, once for
// the pop-up. Both are given back.
func TestASecondGrabIsGivenBackToo(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(browserWindow), d.nothing(), d.focusTo(browserPopup)}

	if err := fastKeeper(d).around(d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if d.focused != usersEditor {
		t.Errorf("focus ended on %d, want the editor", d.focused)
	}
	if len(d.requests) != 2 {
		t.Errorf("activation requests = %v, want two", d.requests)
	}
}

// A click on a file input opens the operating system's file chooser, which
// belongs to the browser and takes the focus. Whoever clicked means to use it
// next — `atr computer type` the path into it — so it keeps the focus.
func TestADialogTheCallOpenedKeepsTheFocus(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(fileChooser)}

	if err := fastKeeper(d).around(d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if d.focused != fileChooser || len(d.requests) != 0 {
		t.Errorf("focus = %d, requests = %v; want the file chooser left with the focus", d.focused, d.requests)
	}
}

// The browser window grabbing first and the dialog opening after it is the
// same: the dialog is what the call was for.
func TestADialogAfterAGrabKeepsTheFocus(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(browserWindow), d.nothing(), d.focusTo(fileChooser)}

	if err := fastKeeper(d).around(d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if d.focused != fileChooser {
		t.Errorf("focus ended on %d, want the file chooser", d.focused)
	}
}

// A browser that keeps grabbing cannot hold the caller for ever.
func TestABrowserThatKeepsGrabbingIsGivenUpOn(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	for i := 0; i < 50; i++ {
		d.timeline = append(d.timeline, d.focusTo(browserWindow))
	}

	start := time.Now()
	if err := fastKeeper(d).around(d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if len(d.requests) != focusMaxReturns {
		t.Errorf("gave the focus back %d times, want at most %d", len(d.requests), focusMaxReturns)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("held the call for %v", elapsed)
	}
}

// Two guarded calls overlapping: the second begins while the first has let the
// browser take the focus. It must still give it back to the user's window —
// read naively, the browser looks like where the user is.
func TestOverlappingCallsBothGiveTheFocusBack(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	k := fastKeeper(d)
	k.watch = 300 * time.Millisecond

	firstStarted := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = k.around(func() error {
			close(firstStarted)
			<-release
			return nil
		})
	}()
	<-firstStarted

	// The first call's grab has landed and not been given back yet.
	d.mu.Lock()
	d.focused = browserWindow
	d.mu.Unlock()

	if err := k.around(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done

	if d.focused != usersEditor {
		t.Errorf("focus ended on %d, want the editor", d.focused)
	}
}

// When the desktop cannot say which window has the focus, the call runs as it
// would have without a keeper.
func TestAnUnreadableDesktopIsLeftAlone(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.readable = false
	d.timeline = []func(){d.focusTo(browserWindow)}

	calls := 0
	if err := fastKeeper(d).around(func() error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(d.requests) != 0 {
		t.Errorf("calls = %d, requests = %v; want the call made once and nothing else", calls, d.requests)
	}
}

// No window manager, no keeper: the call is simply made.
func TestNoDesktopMeansNoKeeper(t *testing.T) {
	if k := newFocusKeeper(nil); k != nil {
		t.Fatalf("newFocusKeeper(nil) = %v, want nil", k)
	}
	var k *focusKeeper
	calls := 0
	if err := k.around(func() error { calls++; return nil }); err != nil || calls != 1 {
		t.Errorf("err = %v, calls = %d; want the call made once", err, calls)
	}
	k.close() // and closing nothing is harmless
}

// A failed call still gets its focus back, and its error is returned as is.
func TestAFailedCallStillGivesTheFocusBack(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(browserWindow)}
	boom := errors.New("element went away")

	if err := fastKeeper(d).around(d.call(boom)); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the call's own error", err)
	}
	if d.focused != usersEditor {
		t.Errorf("focus ended on %d, want the editor", d.focused)
	}
}

func TestIsBrowserClass(t *testing.T) {
	for class, want := range map[string]bool{
		"Chromium-browser chromium-browser (/tmp/rod/user-data/abc)": true,
		"Google-chrome google-chrome":                                true,
		"Google-chrome-for-testing":                                  true,
		"XLogo xlogo":                                                false,
		"Code code":                                                  false,
		"":                                                           false,
	} {
		if got := isBrowserClass(class); got != want {
			t.Errorf("isBrowserClass(%q) = %v, want %v", class, got, want)
		}
	}
}

// Only a page known to be shown is left alone by SelectPage.
func TestNeedsActivation(t *testing.T) {
	for visibility, want := range map[string]bool{
		"visible":   false,
		"hidden":    true,
		"prerender": true,
		"":          true, // could not be told: activate, as every page used to be
	} {
		if got := needsActivation(visibility); got != want {
			t.Errorf("needsActivation(%q) = %v, want %v", visibility, got, want)
		}
	}
}

// On a desktop a new page opens in a background window of its own; without
// one, as a tab, as it always has.
func TestNewPageTarget(t *testing.T) {
	const ctx proto.BrowserBrowserContextID = "ctx-1"

	windowed := newPageTarget(ctx, true)
	if !windowed.NewWindow || !windowed.Background {
		t.Errorf("windowed: %+v, want a new window, in the background", windowed)
	}
	headless := newPageTarget(ctx, false)
	if headless.NewWindow || headless.Background {
		t.Errorf("headless: %+v, want a plain tab", headless)
	}
	for _, target := range []proto.TargetCreateTarget{windowed, headless} {
		if target.URL != "about:blank" || target.BrowserContextID != ctx {
			t.Errorf("%+v: want about:blank in context %s", target, ctx)
		}
	}
}
