package computer

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	"github.com/go-vgo/robotgo"
	"github.com/vcaesar/screenshot"
)

// Display describes a single physical or virtual display.
//
// Bounds are in root coords (the same coordinate system used by mouse
// clicks, window positions, and xrandr). On a single-monitor setup or
// any layout where every monitor is to the right of and below primary,
// these values match what the screenshot library reports natively;
// when a monitor sits to the left or above primary, the screenshot
// library would report a negative origin while these bounds are shifted
// so the bounding box of all monitors starts at (0, 0).
type Display struct {
	Index   int             `json:"index"`
	Bounds  image.Rectangle `json:"bounds"`
	Primary bool            `json:"primary"`
}

// ScreenSize returns the size of the virtual desktop bounding box in
// pixels (the union of every monitor's area). This is a passive read.
func (c *Computer) ScreenSize() (width, height int) {
	return robotgo.GetScreenSize()
}

// Displays enumerates all attached displays in root coords. The display
// whose primary-centric origin is (0, 0) is marked Primary=true.
func (c *Computer) Displays() []Display {
	n := screenshot.NumActiveDisplays()
	out := make([]Display, 0, n)
	for i := range n {
		pc := screenshot.GetDisplayBounds(i)
		out = append(out, Display{
			Index:   i,
			Bounds:  c.coords.primaryRectToRoot(pc),
			Primary: pc.Min.X == 0 && pc.Min.Y == 0,
		})
	}
	return out
}

// Screenshot captures the given display and returns PNG-encoded bytes.
// displayIndex < 0 means use the configured default display.
func (c *Computer) Screenshot(displayIndex int) ([]byte, error) {
	if displayIndex < 0 {
		displayIndex = c.cfg.DefaultDisplay
	}
	n := screenshot.NumActiveDisplays()
	if displayIndex >= n {
		return nil, fmt.Errorf("display %d out of range (have %d)", displayIndex, n)
	}
	bounds := screenshot.GetDisplayBounds(displayIndex)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, fmt.Errorf("capture display %d: %w", displayIndex, err)
	}
	return encodePNG(img)
}

// ScreenshotRegion captures (x, y, w, h) in screen coordinates of the given
// display and returns PNG-encoded bytes. displayIndex < 0 uses the default.
func (c *Computer) ScreenshotRegion(displayIndex, x, y, w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("region width and height must be positive (got %d, %d)", w, h)
	}
	if x < 0 || y < 0 {
		return nil, fmt.Errorf("region x and y must be non-negative (got %d, %d)", x, y)
	}
	if displayIndex < 0 {
		displayIndex = c.cfg.DefaultDisplay
	}
	n := screenshot.NumActiveDisplays()
	if displayIndex >= n {
		return nil, fmt.Errorf("display %d out of range (have %d)", displayIndex, n)
	}
	dispBounds := screenshot.GetDisplayBounds(displayIndex)
	dispW := dispBounds.Dx()
	dispH := dispBounds.Dy()
	if x+w > dispW || y+h > dispH {
		return nil, fmt.Errorf("region (%d, %d, %d, %d) exceeds display %d bounds (%dx%d)", x, y, w, h, displayIndex, dispW, dispH)
	}
	region := image.Rect(
		dispBounds.Min.X+x,
		dispBounds.Min.Y+y,
		dispBounds.Min.X+x+w,
		dispBounds.Min.Y+y+h,
	)
	img, err := screenshot.CaptureRect(region)
	if err != nil {
		return nil, fmt.Errorf("capture region: %w", err)
	}
	return encodePNG(img)
}

func encodePNG(img *image.RGBA) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := png.Encode(buf, img); err != nil {
		return nil, fmt.Errorf("encode PNG: %w", err)
	}
	return buf.Bytes(), nil
}
