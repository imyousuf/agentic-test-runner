package computer

import (
	"image"

	"github.com/vcaesar/screenshot"
)

// coords translates between two coordinate systems present on this stack:
//
//   - "primary-centric" (used by github.com/vcaesar/screenshot and the
//     underlying github.com/kbinani/screenshot): the primary monitor's
//     top-left is (0, 0); monitors to the left or above have negative
//     coordinates.
//
//   - "root" (used by robotgo, xrandr, X11 root window, and EWMH window
//     bounds): the bounding box of all monitors starts at (0, 0); every
//     visible coordinate is non-negative.
//
// On single-monitor setups, or multi-monitor setups where every monitor
// is to the right of and below primary, the two systems are identical.
// They diverge whenever a monitor sits to the left or above primary —
// e.g., a portrait secondary mounted on the left of a landscape primary.
//
// The public API of internal/computer uses root coords throughout.
// Internal screenshot calls translate to primary-centric before invoking
// the library.
type coords struct {
	// offsetX, offsetY are added to a primary-centric point to get the
	// equivalent root point. Equivalently: -offset is the position of
	// root (0, 0) in primary-centric coords.
	offsetX, offsetY int
}

// detectCoords queries the screenshot library to compute the offset that
// shifts the most-negative monitor edge to (0, 0) in root coords.
func detectCoords() coords {
	n := screenshot.NumActiveDisplays()
	minX, minY := 0, 0
	for i := range n {
		b := screenshot.GetDisplayBounds(i)
		if b.Min.X < minX {
			minX = b.Min.X
		}
		if b.Min.Y < minY {
			minY = b.Min.Y
		}
	}
	return coords{offsetX: -minX, offsetY: -minY}
}

// primaryToRoot converts a point from primary-centric to root coords.
func (c coords) primaryToRoot(x, y int) (int, int) {
	return x + c.offsetX, y + c.offsetY
}

// rootToPrimary converts a point from root to primary-centric coords.
func (c coords) rootToPrimary(x, y int) (int, int) {
	return x - c.offsetX, y - c.offsetY
}

// primaryRectToRoot converts an image.Rectangle from primary-centric to root.
func (c coords) primaryRectToRoot(r image.Rectangle) image.Rectangle {
	return image.Rect(
		r.Min.X+c.offsetX, r.Min.Y+c.offsetY,
		r.Max.X+c.offsetX, r.Max.Y+c.offsetY,
	)
}
