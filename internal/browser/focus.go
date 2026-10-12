package browser

import (
	"strings"
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
//     why it does not reproduce there.)
//
// The first is avoided: new pages open in a background window of their own.
// The second is avoided for ATR's own pages, which are each the visible tab
// of their own window and so never need activating. The third cannot be
// avoided, so the focus is given back: focusKeeper notes the window the user
// was in before a call that may take the focus, and if one of the browser's
// windows has it afterwards, asks for the user's window to have it again.
// Chrome's grab lands within 5–65ms of the call returning, so the user may see
// the browser flash; they are not left typing into it.

// windowSystem is what keeping the user's focus needs from the desktop.
type windowSystem interface {
	// active returns the window that has the focus. False means it cannot be
	// told, and 0 that no window has it.
	active() (uint32, bool)
	// ownedByBrowser reports whether w is one of the browser's own windows.
	ownedByBrowser(w uint32) bool
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
)

// focusKeeper gives the focus back to the user's window when the browser
// takes it.
type focusKeeper struct {
	ws windowSystem

	watch, settle, poll time.Duration

	// inFlight counts calls being guarded now. When one of them has let the
	// browser take the focus and not yet given it back, the next one starts
	// with the browser in front — and must not read that as the user having
	// chosen it.
	inFlight atomic.Int32

	mu sync.Mutex
	// usersWindow is the last window seen with the focus at the start of a
	// guarded call that was not the browser's.
	usersWindow uint32
}

// newFocusKeeper returns a keeper for ws, or nil when there is no desktop to
// keep the focus on. A nil keeper runs calls unguarded.
func newFocusKeeper(ws windowSystem) *focusKeeper {
	if ws == nil {
		return nil
	}
	return &focusKeeper{ws: ws, watch: focusWatch, settle: focusSettle, poll: focusPoll}
}

// around runs fn, a call that may make the browser take the focus, and gives
// the focus back to the user's window if it does.
//
// Nothing is given back when the user was in the browser already: they are
// watching it, and pulling them out of it would be its own kind of theft.
// Nor when the focus moves to some window that is not the browser's — that
// was the user, choosing.
func (k *focusKeeper) around(fn func() error) error {
	if k == nil {
		return fn()
	}

	k.inFlight.Add(1)
	defer k.inFlight.Add(-1)

	users := k.usersWindowNow()
	err := fn()
	if users != 0 {
		k.giveBack(users)
	}
	return err
}

// usersWindowNow is the window to give the focus back to after this call, or
// 0 when there is none.
func (k *focusKeeper) usersWindowNow() uint32 {
	cur, ok := k.ws.active()
	if !ok {
		return 0
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	if cur != 0 && !k.ws.ownedByBrowser(cur) {
		k.usersWindow = cur
		return cur
	}
	// The browser has the focus. Another guarded call may have let it take
	// it a moment ago and be about to give it back; this call's grab must be
	// undone too, or it would land after that one and stay.
	if cur != 0 && k.inFlight.Load() > 1 {
		return k.usersWindow
	}
	return 0
}

// giveBack watches for the browser to take the focus from users, and gives it
// back when it does.
func (k *focusKeeper) giveBack(users uint32) {
	deadline := time.Now().Add(k.watch)
	returns := 0

	for time.Now().Before(deadline) {
		if cur, ok := k.ws.active(); ok && cur != users {
			if cur == 0 || !k.ws.ownedByBrowser(cur) {
				// The user went somewhere else themselves.
				return
			}
			_ = k.ws.activate(users)
			returns++
			if returns >= focusMaxReturns {
				return
			}
			deadline = time.Now().Add(k.settle)
		}
		time.Sleep(k.poll)
	}
}

// close releases the keeper's connection to the desktop.
func (k *focusKeeper) close() {
	if k != nil {
		k.ws.close()
	}
}

// isBrowserClass reports whether an X11 window class names a Chromium-based
// browser, for a browser ATR connected to rather than launched and so has no
// process ID for.
func isBrowserClass(class string) bool {
	return strings.Contains(strings.ToLower(class), "chrom")
}
