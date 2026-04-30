//go:build !linux

package computer

import "errors"

var errWindowsNotImplemented = errors.New("window management is only implemented on Linux X11 in v1")

func platformListWindows() ([]Window, error)               { return nil, errWindowsNotImplemented }
func platformActiveWindow() (Window, error)                { return Window{}, errWindowsNotImplemented }
func platformFocusWindow(id uint32) error                  { return errWindowsNotImplemented }
func platformSetWindowState(id uint32, s WindowState) error { return errWindowsNotImplemented }
func platformMoveWindow(id uint32, x, y int) error         { return errWindowsNotImplemented }
func platformResizeWindow(id uint32, w, h int) error       { return errWindowsNotImplemented }
func platformActiveAppID() (string, error)                 { return "", errWindowsNotImplemented }
