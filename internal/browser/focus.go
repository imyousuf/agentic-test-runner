package browser

import (
	"sync"
	"sync/atomic"
	"time"
)

// A headed browser takes the keyboard focus away from whatever the user is
// working in, and on X11 that raises its window over their work. ATR mostly
// drives a browser while somebody does something else — an agent working
// through a spec while its user writes code — so every time that happens the
// user's next keystrokes land in the browser.
//
// Three things make Chrome activate its window, measured against Chrome for
// Testing under a real window manager:
//
//   - Target.createTarget for a foreground tab. A background tab, or a
//     background window, does not.
//   - Target.activateTarget. Chrome activates the window as well as the tab,
//     and there is no variant that switches tabs without it.
//   - A trusted mouse-down. Chrome focuses the page's contents on every
//     mouse-down, wherever it came from, and focusing the contents activates
//     the window. No protocol setting turns that off — focus emulation
//     included. (The older Chromium rod bundles does not do this, which is
//     why it does not reproduce there.) Tab past the last focusable element
//     does the same, by moving the focus into the browser's toolbar.
//
// The first is avoided: new pages open in a background window of their own.
// The second is avoided for ATR's own pages, which are each the visible tab
// of their own window and so never need activating. The third cannot be
// avoided, so the focus is given back: focusKeeper notes the window the user
// was in before a call that may take the focus, and if one of the browser's
// windows has it afterwards, asks for the user's window to have it again.
// Chrome's grab lands within 5–65ms of the call returning, so the user may see
// the browser flash; they are not left typing into it.
//
// Giving the focus back is, to the page, the user switching away from it: the
// focused element blurs, a menu that closes on blur closes, a field validated
// on blur is validated. Headless, and with the focus left where Chrome put it,
// none of that happens — so a test would pass headless and fail headed while
// its author was in another window. Just before giving the focus back, the
// caller is asked to hold the page's focus (Browser.holdFocus: Chrome's focus
// emulation, released after a while without ATR touching the page).
//
// All of this is Linux-only for now: it is what has been measured. Elsewhere
// the browser behaves as it always has.

// windowSystem is what keeping the user's focus needs from the desktop.
type windowSystem interface {
	// active returns the window that has the focus. False means it cannot be
	// told, and 0 that no window has it.
	active() (uint32, bool)
	// ownedByBrowser reports whether w is one of the browser's own windows.
	ownedByBrowser(w uint32) bool
	// isDialog reports whether w is a dialog — transient for another window,
	// typed as a dialog, or modal. Chrome's file chooser is all three.
	isDialog(w uint32) bool
	// browserHasWindows reports whether the browser has any window on this
	// desktop at all. A browser on another machine, in a container, or
	// running natively on Wayland has none here, and can take no focus from
	// it — so there is nothing to watch for.
	browserHasWindows() bool
	// activate asks for w to be given the focus.
	activate(w uint32) error
	// close releases the connection to the desktop.
	close()
}

// How long focusKeeper watches for the browser to take the focus.
const (
	// focusWatch is how long it waits for a grab after the call returns.
	// Measured grabs land within 65ms; this leaves a slow machine room.
	focusWatch = 300 * time.Millisecond
	// focusSettle is how long it keeps watching after giving the focus back,
	// for a second grab: a click that opens a pop-up activates twice.
	focusSettle = 100 * time.Millisecond
	// focusPoll is how often it looks.
	focusPoll = 10 * time.Millisecond
	// focusMaxReturns bounds how often one call gives the focus back, so a
	// browser that keeps grabbing cannot hold the caller for ever.
	focusMaxReturns = 3
	// desktopConnectTimeout bounds connecting to the desktop. A DISPLAY that
	// names a host which drops packets would otherwise hold the first click
	// for the operating system's connect timeout.
	desktopConnectTimeout = time.Second
)

// focusKeeper gives the focus back to the user's window when the browser
// takes it.
type focusKeeper struct {
	// connect opens the connection to the desktop, or returns nil when there
	// is none to keep the focus on. Called once, on first use, so that
	// launching a browser never waits on the desktop.
	connect func() windowSystem

	watch, settle, poll, connectTimeout time.Duration

	ready sync.Once
	ws    windowSystem

	// hasWindows records that the browser has been seen with a window on
	// this desktop. Once true it stays true.
	hasWindows atomic.Bool

	// guarding counts calls in flight that are watching for a grab to give
	// back: those that started with the focus on a user window, and those
	// that started while one of them was in flight and took its window over.
	// While any is, the browser having the focus may be a grab rather than
	// the user's choice — see usersWindowNow.
	guarding atomic.Int32

	mu sync.Mutex
	// usersWindow is the user's window as last recorded by a call being
	// guarded. Meaningful only while guarding is above zero.
	usersWindow uint32
}

// newFocusKeeper returns a keeper that connects to the desktop with connect
// when first used.
func newFocusKeeper(connect func() windowSystem) *focusKeeper {
	return &focusKeeper{
		connect:        connect,
		watch:          focusWatch,
		settle:         focusSettle,
		poll:           focusPoll,
		connectTimeout: desktopConnectTimeout,
	}
}

// desktop returns the connection to the desktop, opening it on first use, or
// nil when there is none to be had.
func (k *focusKeeper) desktop() windowSystem {
	k.ready.Do(func() {
		done := make(chan windowSystem, 1)
		go func() { done <- k.connect() }()
		select {
		case ws := <-done:
			k.ws = ws
		case <-time.After(k.connectTimeout):
			// Given up on, for good: this desktop cannot answer in time, and
			// asking again would make every later call wait too. If it
			// connects after all, that connection is closed.
			go func() {
				if ws := <-done; ws != nil {
					ws.close()
				}
			}()
		}
	})
	return k.ws
}

// around runs fn, a call that may make the browser take the focus, and gives
// the focus back to the user's window if it does. holdPage, when not nil, is
// called once, just before the focus is first given back, to keep the page
// behaving as focused when it no longer is.
//
// Nothing is given back when the user was in the browser already: they are
// watching it, and pulling them out of it would be its own kind of theft. When
// the focus moves to some other window that is not the browser's, that was the
// user, choosing — and that window is where it goes back to. When it moves to
// a dialog of the browser's, it stays there: a click on a file input opens the
// operating system's file chooser, and whoever clicked means to use it next —
// taking the focus off it is what would make `atr computer type` land in the
// wrong window.
func (k *focusKeeper) around(holdPage func(), fn func() error) error {
	if k == nil {
		return fn()
	}
	ws := k.desktop()
	if ws == nil {
		return fn()
	}
	if !k.hasWindows.Load() {
		if !ws.browserHasWindows() {
			return fn()
		}
		k.hasWindows.Store(true)
	}

	users, guarding := k.usersWindowNow(ws)
	if guarding {
		defer k.guarding.Add(-1)
	}

	err := fn()
	if users != 0 {
		k.giveBack(ws, users, holdPage)
	}
	return err
}

// usersWindowNow is the window to give the focus back to after this call, or
// 0 when there is none. guarding is true when this call now counts in
// k.guarding and must be taken off it when done.
func (k *focusKeeper) usersWindowNow(ws windowSystem) (users uint32, guarding bool) {
	cur, ok := ws.active()
	if !ok || cur == 0 {
		return 0, false
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	if !ws.ownedByBrowser(cur) {
		k.usersWindow = cur
		k.guarding.Add(1)
		return cur, true
	}
	// The browser has the focus. If another call that started from the
	// user's window is still in flight, that is most likely its grab, not
	// yet given back — and this call's own grab may land after that call has
	// stopped watching, so this one has to watch for it too. With no such
	// call in flight it is the user's choice, and the window recorded long
	// ago is not where they are any more.
	if k.guarding.Load() > 0 {
		k.guarding.Add(1)
		return k.usersWindow, true
	}
	return 0, false
}

// giveBack watches for the browser to take the focus from users, and gives it
// back when it does.
func (k *focusKeeper) giveBack(ws windowSystem, users uint32, holdPage func()) {
	deadline := time.Now().Add(k.watch)
	returns := 0
	held := false

	for time.Now().Before(deadline) {
		if cur, ok := ws.active(); ok && cur != users && cur != 0 {
			if !ws.ownedByBrowser(cur) {
				// The user went to another window themselves. That is now
				// theirs: the grab may still be on its way, and if it
				// lands it goes back there.
				users = cur
				k.mu.Lock()
				k.usersWindow = cur
				k.mu.Unlock()
				time.Sleep(k.poll)
				continue
			}
			if ws.isDialog(cur) {
				// The call opened a dialog, which is the point of it.
				return
			}
			if !held && holdPage != nil {
				holdPage()
				held = true
			}
			_ = ws.activate(users)
			returns++
			if returns >= focusMaxReturns {
				return
			}
			// The window manager takes a moment to act on that, and until it
			// has, the browser still has the focus — the same grab, not a
			// second one to give back again.
			k.waitWhileActive(ws, cur, k.watch)
			deadline = time.Now().Add(k.settle)
		}
		time.Sleep(k.poll)
	}
}

// waitWhileActive waits until w no longer has the focus, or for at most d.
func (k *focusKeeper) waitWhileActive(ws windowSystem, w uint32, d time.Duration) {
	for until := time.Now().Add(d); time.Now().Before(until); {
		if cur, ok := ws.active(); !ok || cur != w {
			return
		}
		time.Sleep(k.poll)
	}
}

// close releases the keeper's connection to the desktop, if it opened one.
// A keeper never used never connects, here or later.
func (k *focusKeeper) close() {
	if k == nil {
		return
	}
	// Marks the connection settled without opening one, or waits for one
	// being opened.
	k.ready.Do(func() {})
	if k.ws != nil {
		k.ws.close()
	}
}
