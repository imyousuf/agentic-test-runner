//go:build !linux

package browser

// newWindowSystem has no desktop to read the focus from outside Linux yet, so
// calls that may take the focus run unguarded there.
func newWindowSystem(int) windowSystem { return nil }
