//go:build !linux

package browser

// keepsOutOfTheWay says whether a headed browser here is kept from taking the
// user's focus. Not yet outside Linux: background windows, skipped
// activations and giving the focus back have only been measured there, and a
// Chrome the user started themselves may not render a background window that
// another window covers. Here the browser behaves as it always has.
const keepsOutOfTheWay = false

// desktopConnector has no desktop to read the focus from outside Linux.
func desktopConnector(int) func() windowSystem { return nil }
