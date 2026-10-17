package browser

import (
	"errors"
	"sync"
	"sync/atomic"
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
	windows   bool
	browsers  map[uint32]bool
	dialogs   map[uint32]bool
	requests  []uint32
	timeline  []func() // run, one per active() call, once the guarded call has been made
	afterCall bool
	closed    bool
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
		windows:  true,
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
func (d *fakeDesktop) isDialog(w uint32) bool       { return d.dialogs[w] }
func (d *fakeDesktop) browserHasWindows() bool      { return d.windows }

func (d *fakeDesktop) activate(w uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.requests = append(d.requests, w)
	d.focused = w
	return nil
}

func (d *fakeDesktop) close() { d.closed = true }

func (d *fakeDesktop) focusTo(w uint32) func() { return func() { d.focused = w } }
func (d *fakeDesktop) nothing() func()         { return func() {} }

// setFocus moves the focus now, as the user or the browser would.
func (d *fakeDesktop) setFocus(w uint32) {
	d.mu.Lock()
	d.focused = w
	d.mu.Unlock()
}

func (d *fakeDesktop) state() (focused uint32, requests []uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.focused, append([]uint32(nil), d.requests...)
}

// call is a guarded call that, once made, lets the desktop's timeline play.
func (d *fakeDesktop) call(err error) func() error {
	return func() error {
		d.mu.Lock()
		d.afterCall = true
		d.mu.Unlock()
		return err
	}
}

func fastKeeper(d windowSystem) *focusKeeper {
	k := newFocusKeeper(func() windowSystem { return d })
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

	if err := fastKeeper(d).around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if focused, requests := d.state(); focused != usersEditor || len(requests) != 1 || requests[0] != usersEditor {
		t.Errorf("focus = %d, requests = %v; want one request, for the editor (%d)", focused, requests, usersEditor)
	}
}

// The page is told to hold its focus before the focus is given back, not
// after: given back first, the page would see the blur that the hold exists
// to keep from it.
func TestThePageIsHeldJustBeforeTheFocusIsGivenBack(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(browserWindow), d.nothing(), d.focusTo(browserPopup)}

	var holds int
	var requestsAtHold []uint32
	hold := func() {
		holds++
		_, requestsAtHold = d.state()
	}

	if err := fastKeeper(d).around(hold, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if holds != 1 {
		t.Errorf("the page was held %d times, want once however many grabs", holds)
	}
	if len(requestsAtHold) != 0 {
		t.Errorf("the focus had already been given back (%v) when the page was held", requestsAtHold)
	}
}

// No grab, no hold: a page that never lost the focus has nothing to keep.
func TestNoGrabMeansNoHold(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	held := false
	if err := fastKeeper(d).around(func() { held = true }, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if held {
		t.Error("the page was held though the browser took nothing")
	}
}

// A call that takes nothing changes nothing, and does not hold the caller past
// the watch.
func TestNothingIsAskedOfTheDesktopWhenTheBrowserTakesNothing(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	k := fastKeeper(d)

	start := time.Now()
	if err := k.around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if _, requests := d.state(); len(requests) != 0 {
		t.Errorf("activation requests = %v, want none", requests)
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
	if err := fastKeeper(d).around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if _, requests := d.state(); len(requests) != 0 {
		t.Errorf("activation requests = %v, want none: the user was in the browser", requests)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("watched for %v with nothing to give back", elapsed)
	}
}

// A window recorded by an earlier call is not where the user is now. With no
// other call in flight, the browser having the focus is the user's choice —
// pulling them back to a window they left minutes ago, perhaps on another
// workspace, perhaps closed, would be a theft of its own.
func TestAWindowRecordedEarlierIsNotUsedOnceTheUserHasMoved(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	k := fastKeeper(d)

	// An earlier call, from the editor, that took nothing.
	if err := k.around(nil, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	// The user has since gone into the browser, and a call there makes the
	// browser's pop-up take the focus.
	d.setFocus(browserWindow)
	d.mu.Lock()
	d.afterCall = false
	d.timeline = []func(){d.focusTo(browserPopup)}
	d.mu.Unlock()

	if err := k.around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if focused, requests := d.state(); len(requests) != 0 {
		t.Errorf("focus = %d, requests = %v; want the user left in the browser", focused, requests)
	}
}

// The user moving to another window of their own while the call runs makes
// that window theirs: the grab, landing after, goes back there — not to the
// window they started in, and not left where Chrome put it.
func TestAUsersSwitchDuringTheCallIsWhereTheFocusGoesBack(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(usersTerminal), d.nothing(), d.focusTo(browserWindow)}

	if err := fastKeeper(d).around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if focused, requests := d.state(); focused != usersTerminal || len(requests) != 1 || requests[0] != usersTerminal {
		t.Errorf("focus = %d, requests = %v; want the focus back on the terminal the user moved to", focused, requests)
	}
}

// A click on a file input opens the operating system's file chooser, which
// belongs to the browser and takes the focus. Whoever clicked means to use it
// next — `atr computer type` the path into it — so it keeps the focus.
func TestADialogTheCallOpenedKeepsTheFocus(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(fileChooser)}

	if err := fastKeeper(d).around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if focused, requests := d.state(); focused != fileChooser || len(requests) != 0 {
		t.Errorf("focus = %d, requests = %v; want the file chooser left with the focus", focused, requests)
	}
}

// The browser window grabbing first and the dialog opening after it is the
// same: the dialog is what the call was for.
func TestADialogAfterAGrabKeepsTheFocus(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(browserWindow), d.nothing(), d.focusTo(fileChooser)}

	if err := fastKeeper(d).around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if focused, _ := d.state(); focused != fileChooser {
		t.Errorf("focus ended on %d, want the file chooser", focused)
	}
}

// A click that opens a pop-up activates twice. Both are given back.
func TestASecondGrabIsGivenBackToo(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(browserWindow), d.nothing(), d.focusTo(browserPopup)}

	if err := fastKeeper(d).around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if focused, requests := d.state(); focused != usersEditor || len(requests) != 2 {
		t.Errorf("focus = %d, requests = %v; want two, ending on the editor", focused, requests)
	}
}

// A browser that keeps grabbing cannot hold the caller for ever.
func TestABrowserThatKeepsGrabbingIsGivenUpOn(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	for i := 0; i < 50; i++ {
		d.timeline = append(d.timeline, d.focusTo(browserWindow))
	}

	start := time.Now()
	if err := fastKeeper(d).around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if _, requests := d.state(); len(requests) != focusMaxReturns {
		t.Errorf("gave the focus back %d times, want at most %d", len(requests), focusMaxReturns)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("held the call for %v", elapsed)
	}
}

// Two calls overlapping. The first's grab lands while the second is starting,
// so the second starts with the browser in front — and must not read that as
// the user's choice. The first gives the focus back and finishes; the second's
// own grab lands only after that, when nothing but the second is watching.
func TestASecondCallsGrabAfterTheFirstHasFinishedIsGivenBack(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	k := fastKeeper(d)
	k.watch = 120 * time.Millisecond

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_ = k.around(nil, func() error {
			close(firstStarted)
			<-releaseFirst
			// The first call's grab.
			d.setFocus(browserWindow)
			return nil
		})
	}()
	<-firstStarted

	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	secondDone := make(chan struct{})

	// The first call's grab is in, not yet given back, when the second
	// starts.
	d.setFocus(browserWindow)
	go func() {
		defer close(secondDone)
		_ = k.around(nil, func() error {
			close(secondStarted)
			<-releaseSecond
			// The second call's grab, after the first has finished.
			d.setFocus(browserWindow)
			return nil
		})
	}()
	<-secondStarted

	close(releaseFirst)
	<-firstDone
	if focused, _ := d.state(); focused != usersEditor {
		t.Fatalf("after the first call the focus is on %d, want the editor", focused)
	}

	close(releaseSecond)
	<-secondDone
	if focused, _ := d.state(); focused != usersEditor {
		t.Errorf("after the second call's grab the focus is on %d, want the editor", focused)
	}
}

// A browser with no window on this desktop — on another machine, in a
// container, or running natively on Wayland — can take no focus from it, so
// nothing is watched and nothing is paid for watching.
func TestABrowserWithNoWindowsHereIsNotWatched(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.windows = false
	k := fastKeeper(d)
	k.watch = time.Second

	start := time.Now()
	if err := k.around(nil, d.call(nil)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("watched for %v for a browser with no windows here", elapsed)
	}
}

// When the desktop cannot say which window has the focus, the call runs as it
// would have without a keeper.
func TestAnUnreadableDesktopIsLeftAlone(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.readable = false
	d.timeline = []func(){d.focusTo(browserWindow)}

	calls := 0
	if err := fastKeeper(d).around(nil, func() error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, requests := d.state(); calls != 1 || len(requests) != 0 {
		t.Errorf("calls = %d, requests = %v; want the call made once and nothing else", calls, requests)
	}
}

// No desktop to connect to, or no keeper at all: the call is simply made.
func TestNoDesktopMeansCallsRunUnguarded(t *testing.T) {
	k := newFocusKeeper(func() windowSystem { return nil })
	calls := 0
	if err := k.around(nil, func() error { calls++; return nil }); err != nil || calls != 1 {
		t.Errorf("err = %v, calls = %d; want the call made once", err, calls)
	}
	k.close()

	var none *focusKeeper
	if err := none.around(nil, func() error { calls++; return nil }); err != nil || calls != 2 {
		t.Errorf("nil keeper: err = %v, calls = %d; want the call made", err, calls)
	}
	none.close()
}

// A desktop slow to answer is given up on after the connect timeout, once:
// the first call waits that long at most, and no call after it waits at all.
func TestASlowDesktopIsGivenUpOnOnce(t *testing.T) {
	unblock := make(chan struct{})
	var connects atomic.Int32
	k := newFocusKeeper(func() windowSystem {
		connects.Add(1)
		<-unblock
		return newFakeDesktop(usersEditor)
	})
	k.connectTimeout = 50 * time.Millisecond
	defer close(unblock)

	for i := 0; i < 3; i++ {
		start := time.Now()
		calls := 0
		if err := k.around(nil, func() error { calls++; return nil }); err != nil || calls != 1 {
			t.Fatalf("call %d: err = %v, calls = %d", i, err, calls)
		}
		limit := 30 * time.Millisecond
		if i == 0 {
			limit = 300 * time.Millisecond
		}
		if elapsed := time.Since(start); elapsed > limit {
			t.Errorf("call %d waited %v", i, elapsed)
		}
	}
	if n := connects.Load(); n != 1 {
		t.Errorf("connected %d times, want once", n)
	}
}

// Closing a keeper that was never used does not connect to the desktop just
// to disconnect from it.
func TestClosingAnUnusedKeeperDoesNotConnect(t *testing.T) {
	connected := false
	k := newFocusKeeper(func() windowSystem { connected = true; return newFakeDesktop(usersEditor) })
	k.close()
	if connected {
		t.Error("closing an unused keeper connected to the desktop")
	}
	// And it stays closed for good.
	if err := k.around(nil, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if connected {
		t.Error("a closed keeper connected on its next use")
	}
}

// A used keeper's connection is closed with it.
func TestClosingAKeeperClosesItsConnection(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	k := fastKeeper(d)
	if err := k.around(nil, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	k.close()
	if !d.closed {
		t.Error("the desktop connection was left open")
	}
}

// A failed call still gets its focus back, and its error is returned as is.
func TestAFailedCallStillGivesTheFocusBack(t *testing.T) {
	d := newFakeDesktop(usersEditor)
	d.timeline = []func(){d.focusTo(browserWindow)}
	boom := errors.New("element went away")

	if err := fastKeeper(d).around(nil, d.call(boom)); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the call's own error", err)
	}
	if focused, _ := d.state(); focused != usersEditor {
		t.Errorf("focus ended on %d, want the editor", focused)
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
	plain := newPageTarget(ctx, false)
	if plain.NewWindow || plain.Background {
		t.Errorf("not windowed: %+v, want a plain tab", plain)
	}
	for _, target := range []proto.TargetCreateTarget{windowed, plain} {
		if target.URL != "about:blank" || target.BrowserContextID != ctx {
			t.Errorf("%+v: want about:blank in context %s", target, ctx)
		}
	}
}

// What a connected browser reports decides whether ATR treats it as one on a
// desktop. Chrome's current headless mode names itself plainly "Chrome" in
// the product, and gives itself away only in the user agent.
func TestReportsHeadless(t *testing.T) {
	tests := []struct {
		name, product, userAgent string
		want                     bool
	}{
		{"headless, current mode (as Chrome for Testing 155 reports it)",
			"Chrome/155.0.8059.84",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/155.0.0.0 Safari/537.36",
			true},
		{"headless, old mode",
			"HeadlessChrome/120.0.6099.0",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/120.0.6099.0 Safari/537.36",
			true},
		{"headed",
			"Chrome/155.0.8059.84",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36",
			false},
		{"nothing reported", "", "", false},
	}
	for _, tt := range tests {
		if got := reportsHeadless(tt.product, tt.userAgent); got != tt.want {
			t.Errorf("%s: reportsHeadless = %v, want %v", tt.name, got, tt.want)
		}
	}
}
