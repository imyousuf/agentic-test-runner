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

// x11Windows reads and sets the focus through the window manager, by EWMH.
type x11Windows struct {
	mu sync.Mutex
	xu *xgbutil.XUtil
	// browserPID is the browser's process. Chrome stamps it on every window
	// it opens, as _NET_WM_PID. Zero for a browser ATR connected to.
	browserPID int
}

// newWindowSystem connects to the X11 desktop the browser shows on, or
// returns nil when there is none to keep the focus on: no display, or no
// window manager that reports which window is active.
//
// On a Wayland session the browser is not an X11 client, the compositor does
// not let it take the focus in the first place, and the active window read
// here is never one of its windows — so nothing is ever given back.
func newWindowSystem(browserPID int) windowSystem {
	if os.Getenv("DISPLAY") == "" {
		return nil
	}
	xu, err := xgbutil.NewConn()
	if err != nil {
		return nil
	}
	if _, err := ewmh.SupportingWmCheckGet(xu, xu.RootWin()); err != nil {
		xu.Conn().Close()
		return nil
	}
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

	if x.browserPID > 0 {
		pid, err := ewmh.WmPidGet(x.xu, xproto.Window(w))
		return err == nil && int(pid) == x.browserPID
	}
	class, err := icccm.WmClassGet(x.xu, xproto.Window(w))
	return err == nil && isBrowserClass(class.Class+" "+class.Instance)
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
