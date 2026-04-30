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
type Display struct {
	Index   int             `json:"index"`
	Bounds  image.Rectangle `json:"bounds"`
	Primary bool            `json:"primary"`
}

// ScreenSize returns the size of the primary display in pixels.
// This is a passive read; no safety gate.
func (c *Computer) ScreenSize() (width, height int) {
	return robotgo.GetScreenSize()
}

// Displays enumerates all attached displays. Index 0 is the primary
// display per platform convention.
func (c *Computer) Displays() []Display {
	n := screenshot.NumActiveDisplays()
	out := make([]Display, 0, n)
	for i := range n {
		bounds := screenshot.GetDisplayBounds(i)
		out = append(out, Display{
			Index:   i,
			Bounds:  bounds,
			Primary: i == 0,
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
