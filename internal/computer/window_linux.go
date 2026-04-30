//go:build linux

package computer

import (
	"fmt"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/icccm"
	"github.com/jezek/xgbutil/xwindow"
)

func platformXUtil() (*xgbutil.XUtil, error) {
	xu, err := xgbutil.NewConn()
	if err != nil {
		return nil, fmt.Errorf("connect to X11: %w", err)
	}
	return xu, nil
}

func platformListWindows() ([]Window, error) {
	xu, err := platformXUtil()
	if err != nil {
		return nil, err
	}
	defer xu.Conn().Close()

	wins, err := ewmh.ClientListGet(xu)
	if err != nil {
		return nil, fmt.Errorf("list client windows: %w", err)
	}
	out := make([]Window, 0, len(wins))
	for _, w := range wins {
		win := windowFromX(xu, w)
		out = append(out, win)
	}
	return out, nil
}

func platformActiveWindow() (Window, error) {
	xu, err := platformXUtil()
	if err != nil {
		return Window{}, err
	}
	defer xu.Conn().Close()

	w, err := ewmh.ActiveWindowGet(xu)
	if err != nil {
		return Window{}, fmt.Errorf("get active window: %w", err)
	}
	return windowFromX(xu, w), nil
}

func platformFocusWindow(id uint32) error {
	xu, err := platformXUtil()
	if err != nil {
		return err
	}
	defer xu.Conn().Close()

	if err := ewmh.ActiveWindowReq(xu, xproto.Window(id)); err != nil {
		return fmt.Errorf("focus window %d: %w", id, err)
	}
	return nil
}

func platformSetWindowState(id uint32, state WindowState) error {
	xu, err := platformXUtil()
	if err != nil {
		return err
	}
	defer xu.Conn().Close()
	win := xproto.Window(id)

	switch state {
	case WindowMinimize:
		if err := icccm.WmStateSet(xu, win, &icccm.WmState{State: icccm.StateIconic}); err != nil {
			// Fallback: send WM_CHANGE_STATE client message
			return ewmh.ClientEvent(xu, win, "WM_CHANGE_STATE", 3)
		}
		return nil
	case WindowMaximize:
		const stateAdd = 1
		const sourceUser = 2
		return ewmh.WmStateReqExtra(xu, win, stateAdd,
			"_NET_WM_STATE_MAXIMIZED_VERT", "_NET_WM_STATE_MAXIMIZED_HORZ", sourceUser)
	case WindowRestore:
		const stateRemove = 0
		const sourceUser = 2
		// Remove maximize state and ensure window is mapped (un-iconified).
		_ = ewmh.WmStateReqExtra(xu, win, stateRemove,
			"_NET_WM_STATE_MAXIMIZED_VERT", "_NET_WM_STATE_MAXIMIZED_HORZ", sourceUser)
		xwindow.New(xu, win).Map()
		return nil
	case WindowClose:
		return ewmh.CloseWindow(xu, win)
	default:
		return fmt.Errorf("unknown window state %q", state)
	}
}

func platformMoveWindow(id uint32, x, y int) error {
	xu, err := platformXUtil()
	if err != nil {
		return err
	}
	defer xu.Conn().Close()
	if err := ewmh.MoveWindow(xu, xproto.Window(id), x, y); err != nil {
		return fmt.Errorf("move window %d: %w", id, err)
	}
	return nil
}

func platformResizeWindow(id uint32, w, h int) error {
	xu, err := platformXUtil()
	if err != nil {
		return err
	}
	defer xu.Conn().Close()
	w2 := xwindow.New(xu, xproto.Window(id))
	if err := w2.WMResize(w, h); err != nil {
		return fmt.Errorf("resize window %d: %w", id, err)
	}
	return nil
}

// windowFromX builds a Window from an X11 window ID, fetching name, PID,
// state, and bounds. Errors on individual fields are tolerated and surface
// as zero values rather than failing the whole listing.
// platformActiveAppID returns the WM_CLASS of the currently focused window,
// or "" if none can be determined.
func platformActiveAppID() (string, error) {
	xu, err := platformXUtil()
	if err != nil {
		return "", err
	}
	defer xu.Conn().Close()

	w, err := ewmh.ActiveWindowGet(xu)
	if err != nil {
		return "", err
	}
	if class, err := icccm.WmClassGet(xu, w); err == nil && class != nil {
		if class.Class != "" {
			return class.Class, nil
		}
		return class.Instance, nil
	}
	return "", nil
}

func windowFromX(xu *xgbutil.XUtil, w xproto.Window) Window {
	out := Window{ID: uint32(w)}

	if name, err := ewmh.WmNameGet(xu, w); err == nil && name != "" {
		out.Title = name
	} else if name, err := icccm.WmNameGet(xu, w); err == nil {
		out.Title = name
	}

	if pid, err := ewmh.WmPidGet(xu, w); err == nil {
		out.PID = uint32(pid)
	}

	if states, err := ewmh.WmStateGet(xu, w); err == nil {
		for _, st := range states {
			switch st {
			case "_NET_WM_STATE_HIDDEN":
				out.Minimized = true
			case "_NET_WM_STATE_MAXIMIZED_VERT", "_NET_WM_STATE_MAXIMIZED_HORZ":
				out.Maximized = true
			}
		}
	}

	if g, err := xwindow.New(xu, w).DecorGeometry(); err == nil {
		out.Bounds = [4]int{g.X(), g.Y(), g.Width(), g.Height()}
	}

	if class, err := icccm.WmClassGet(xu, w); err == nil && class != nil {
		out.AppName = class.Class
	}

	return out
}
