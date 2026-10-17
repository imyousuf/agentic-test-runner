//go:build linux

package browser

import (
	"os"
	"sync"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/icccm"
)

// keepsOutOfTheWay says whether a headed browser here is kept from taking the
// user's focus. Linux is where it has been measured.
const keepsOutOfTheWay = true

// x11Windows reads and sets the focus through the window manager, by EWMH.
type x11Windows struct {
	mu sync.Mutex
	xu *xgbutil.XUtil
	// browserPID is the browser's process. Chrome stamps it on every window
	// it opens, as _NET_WM_PID, and it is how the browser's windows are told
	// from everybody else's — including the user's own Chrome.
	browserPID int
}

// desktopConnector returns how to connect to the X11 desktop for a browser
// with this process ID, or nil when there is no way to tell its windows.
func desktopConnector(browserPID int) func() windowSystem {
	if browserPID <= 0 || os.Getenv("DISPLAY") == "" {
		return nil
	}
	return func() windowSystem { return newX11Windows(browserPID) }
}

// newX11Windows connects to the X11 desktop, or returns nil when there is no
// window manager that reports which window is active.
//
// On a Wayland session the browser may not be an X11 client at all: the
// compositor does not let it take the focus in the first place, and it has no
// windows here — so browserHasWindows says so and nothing is watched.
func newX11Windows(browserPID int) windowSystem {
	xu, err := xgbutil.NewConn()
	if err != nil {
		return nil
	}
	if _, err := ewmh.SupportingWmCheckGet(xu, xu.RootWin()); err != nil {
		xu.Conn().Close()
		return nil
	}

	// Every X client is sent some events whether it asked for them or not —
	// a keyboard mapping change goes to all of them — and xgb queues them
	// until they are read, in a buffer of a few thousand. Unread, a long-
	// lived connection fills it, and then every request on it blocks for
	// good. This connection lives as long as the browser, so its events are
	// read, and dropped, as they come. The loop ends when the connection
	// closes.
	conn := xu.Conn()
	go func() {
		for {
			ev, err := conn.WaitForEvent()
			if ev == nil && err == nil {
				return
			}
		}
	}()

	return &x11Windows{xu: xu, browserPID: browserPID}
}

func (x *x11Windows) active() (uint32, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()

	w, err := ewmh.ActiveWindowGet(x.xu)
	if err != nil {
		return 0, false
	}
	return uint32(w), true
}

func (x *x11Windows) ownedByBrowser(w uint32) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.ownedLocked(xproto.Window(w))
}

func (x *x11Windows) ownedLocked(w xproto.Window) bool {
	pid, err := ewmh.WmPidGet(x.xu, w)
	return err == nil && int(pid) == x.browserPID
}

func (x *x11Windows) browserHasWindows() bool {
	x.mu.Lock()
	defer x.mu.Unlock()

	clients, err := ewmh.ClientListGet(x.xu)
	if err != nil {
		return false
	}
	for _, w := range clients {
		if x.ownedLocked(w) {
			return true
		}
	}
	return false
}

func (x *x11Windows) isDialog(w uint32) bool {
	x.mu.Lock()
	defer x.mu.Unlock()

	win := xproto.Window(w)
	if parent, err := icccm.WmTransientForGet(x.xu, win); err == nil && parent != 0 {
		return true
	}
	if types, err := ewmh.WmWindowTypeGet(x.xu, win); err == nil {
		for _, t := range types {
			if t == "_NET_WM_WINDOW_TYPE_DIALOG" {
				return true
			}
		}
	}
	if states, err := ewmh.WmStateGet(x.xu, win); err == nil {
		for _, s := range states {
			if s == "_NET_WM_STATE_MODAL" {
				return true
			}
		}
	}
	return false
}

// activate asks the window manager to give w the focus, as a pager would.
// A pager's request is honoured where an application's may be refused, and
// giving the user back their own window is exactly what a pager is for.
func (x *x11Windows) activate(w uint32) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	return ewmh.ActiveWindowReq(x.xu, xproto.Window(w))
}

func (x *x11Windows) close() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.xu.Conn().Close()
}
