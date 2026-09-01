package record

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"time"
)

// A signature is a tiny greyscale thumbnail of one frame, sigW by sigH.
//
// Comparing whole JPEGs is useless: the encoder makes every frame differ, so
// byte equality found exactly one static pair in a 1421 frame recording. A
// downsampled thumbnail throws that noise away and keeps the layout, which is
// the thing that actually changes when something happens on the page.
const (
	sigW = 32
	sigH = 20
	// tiles is the grid a difference is measured over, tiles by tiles.
	tiles = 4
)

// The defaults come from measuring a real session against what was on the
// screen at the time. See docs/session-recording.md.
const (
	// DefaultRefLag is how far back the activity reference frame sits.
	//
	// A caret blinks, so it differs from the frame before it and matches the
	// frame a second earlier. Measuring against a lagged reference cancels
	// anything that reverts, and keeps anything cumulative such as typing.
	DefaultRefLag = 1000 * time.Millisecond
	// DefaultThreshold is the activity score a player treats as "something
	// happened". Typing scores about 0.002 to 0.01, a blinking caret about
	// 0.0002 to 0.0005.
	DefaultThreshold = 0.002
	// DefaultEpsilon is the largest difference that still counts as the same
	// picture, so the frame can point at the file already on disk.
	DefaultEpsilon = 0.0005
	// DefaultKeepEvery forces a frame of its own after this long, so a drift
	// that stays under the epsilon cannot accumulate unseen.
	DefaultKeepEvery = 2 * time.Second
)

// ChangeOptions tune the activity detector.
type ChangeOptions struct {
	RefLag    time.Duration
	Threshold float64
	Epsilon   float64
	KeepEvery time.Duration
	// KeepAll writes every frame to its own file, at the cost of the disk.
	KeepAll bool
}

// withDefaults fills in anything the caller left at zero.
func (o ChangeOptions) withDefaults() ChangeOptions {
	if o.RefLag <= 0 {
		o.RefLag = DefaultRefLag
	}
	if o.Threshold <= 0 {
		o.Threshold = DefaultThreshold
	}
	if o.Epsilon <= 0 {
		o.Epsilon = DefaultEpsilon
	}
	if o.KeepEvery <= 0 {
		o.KeepEvery = DefaultKeepEvery
	}
	return o
}

// signature decodes one frame and reduces it to a greyscale thumbnail.
//
// This is the expensive part of the capture path, about 13 ms for a 1280 by
// 800 frame. It runs on the recorder goroutine, which already sits behind a
// queue, so it cannot delay the screencast acknowledgement Chrome waits for.
func signature(data []byte) ([]uint8, error) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode the frame: %w", err)
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("the frame has no pixels")
	}

	// Box filter: every output cell is the mean of the source pixels that fall
	// in it. A nearest neighbour sample would alias, and aliasing on a page of
	// text reads as change when nothing moved.
	sum := make([]uint64, sigW*sigH)
	n := make([]uint32, sigW*sigH)

	// A JPEG almost always decodes to YCbCr, whose Y plane is already the luma
	// we want. Reading it directly avoids a colour conversion per pixel.
	if yc, ok := img.(*image.YCbCr); ok {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			row := (y - b.Min.Y) * sigH / b.Dy() * sigW
			off := yc.YOffset(b.Min.X, y)
			for x := 0; x < b.Dx(); x++ {
				i := row + x*sigW/b.Dx()
				sum[i] += uint64(yc.Y[off+x])
				n[i]++
			}
		}
	} else {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			row := (y - b.Min.Y) * sigH / b.Dy() * sigW
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, _ := img.At(x, y).RGBA()
				// Rec. 601 luma, on the 0 to 65535 range RGBA returns.
				lum := (299*uint64(r) + 587*uint64(g) + 114*uint64(bl)) / 1000 >> 8
				i := row + (x-b.Min.X)*sigW/b.Dx()
				sum[i] += lum
				n[i]++
			}
		}
	}

	sig := make([]uint8, sigW*sigH)
	for i := range sig {
		if n[i] > 0 {
			sig[i] = uint8(sum[i] / uint64(n[i]))
		}
	}
	return sig, nil
}

// diff reports how much two signatures differ, from 0 to 1.
//
// It is the worst tile, not the mean of the whole frame. One changed word in a
// large page moves the frame mean by almost nothing, so a frame mean calls
// typing idle. Splitting the thumbnail into a grid and taking the loudest tile
// keeps a small local change visible.
func diff(a, b []uint8) float64 {
	if len(a) != sigW*sigH || len(b) != sigW*sigH {
		return 1
	}
	tw, th := sigW/tiles, sigH/tiles
	worst := 0.0
	for ty := 0; ty < tiles; ty++ {
		for tx := 0; tx < tiles; tx++ {
			var total uint64
			for y := ty * th; y < (ty+1)*th; y++ {
				for x := tx * tw; x < (tx+1)*tw; x++ {
					i := y*sigW + x
					if a[i] > b[i] {
						total += uint64(a[i] - b[i])
					} else {
						total += uint64(b[i] - a[i])
					}
				}
			}
			if v := float64(total) / float64(tw*th) / 255; v > worst {
				worst = v
			}
		}
	}
	return worst
}

// stamped is one signature and the moment it was captured.
type stamped struct {
	atMs int64
	sig  []uint8
}

// tracker turns a stream of frames into an activity score per frame, and
// decides which frames need a file of their own.
//
// The two questions use different references on purpose. Activity asks "does
// this look different from a second ago", which is what a viewer perceives.
// Storage asks "is this the same picture as the file I last wrote", which is
// the only safe basis for sharing a file.
type tracker struct {
	opts ChangeOptions

	// ring holds the signatures inside the reference lag, oldest first.
	ring []stamped

	written   []uint8
	writtenAt int64
	started   bool
}

func newTracker(opts ChangeOptions) *tracker {
	return &tracker{opts: opts.withDefaults()}
}

// observe takes one frame and reports its activity score, and whether it needs
// a file of its own.
func (t *tracker) observe(atMs int64, sig []uint8) (score float64, fresh bool) {
	// Activity is measured against the newest signature that is at least the
	// reference lag old. Anything younger is dropped from the ring first.
	floor := atMs - t.opts.RefLag.Milliseconds()
	// Advance while the next entry is still old enough, so the head ends up as
	// the newest signature that qualifies.
	for len(t.ring) >= 2 && t.ring[1].atMs <= floor {
		t.ring = t.ring[1:]
	}
	if len(t.ring) > 0 && t.ring[0].atMs <= floor {
		score = diff(t.ring[0].sig, sig)
	}
	t.ring = append(t.ring, stamped{atMs: atMs, sig: sig})

	if !t.started {
		t.started = true
		t.written, t.writtenAt = sig, atMs
		return score, true
	}
	if t.opts.KeepAll {
		t.written, t.writtenAt = sig, atMs
		return score, true
	}
	stale := atMs-t.writtenAt >= t.opts.KeepEvery.Milliseconds()
	if stale || diff(t.written, sig) >= t.opts.Epsilon {
		t.written, t.writtenAt = sig, atMs
		return score, true
	}
	return score, false
}
