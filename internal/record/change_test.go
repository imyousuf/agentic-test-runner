package record

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"
)

// solidJPEG paints one flat colour, then optionally repaints a rectangle in
// another. The rectangle stands in for a small local change such as a typed
// word.
func solidJPEG(t *testing.T, shade uint8, patch image.Rectangle, patchShade uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 640, 400))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	for y := patch.Min.Y; y < patch.Max.Y; y++ {
		for x := patch.Min.X; x < patch.Max.X; x++ {
			img.SetGray(x, y, color.Gray{Y: patchShade})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sigOf(t *testing.T, data []byte) []uint8 {
	t.Helper()
	s, err := signature(data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDiffIsZeroAgainstItself(t *testing.T) {
	s := sigOf(t, solidJPEG(t, 128, image.Rectangle{}, 0))
	if d := diff(s, s); d != 0 {
		t.Errorf("diff against itself = %v, want 0", d)
	}
}

func TestDiffSeesAChangeInOneTile(t *testing.T) {
	// The patch covers about a fortieth of the frame. A whole frame mean would
	// hide it; the worst tile must not.
	flat := sigOf(t, solidJPEG(t, 200, image.Rectangle{}, 0))
	poked := sigOf(t, solidJPEG(t, 200, image.Rect(40, 40, 140, 100), 20))

	d := diff(flat, poked)
	if d < DefaultThreshold {
		t.Errorf("a visible local change scored %v, below the %v threshold", d, DefaultThreshold)
	}
}

func TestDiffRejectsASignatureOfTheWrongSize(t *testing.T) {
	if d := diff([]uint8{1, 2, 3}, make([]uint8, sigW*sigH)); d != 1 {
		t.Errorf("diff on a malformed signature = %v, want 1", d)
	}
}

func TestTrackerScoresAgainstALaggedReference(t *testing.T) {
	// A caret blinks: it changes every frame and reverts within the lag. The
	// score has to stay near zero anyway, or every idle second reads as active.
	on := sigOf(t, solidJPEG(t, 200, image.Rect(40, 40, 60, 90), 20))
	off := sigOf(t, solidJPEG(t, 200, image.Rectangle{}, 0))

	tk := newTracker(ChangeOptions{})
	worst := 0.0
	for i := 0; i < 40; i++ {
		sig := off
		if i%2 == 0 {
			sig = on
		}
		// The lag is a whole second, so the reference lands on the same phase.
		score, _ := tk.observe(int64(i)*500, sig)
		if score > worst {
			worst = score
		}
	}
	if worst >= DefaultThreshold {
		t.Errorf("a blinking caret scored %v, at or above the %v threshold", worst, DefaultThreshold)
	}
}

func TestTrackerSharesAFileWhileTheFrameIsUnchanged(t *testing.T) {
	same := sigOf(t, solidJPEG(t, 128, image.Rectangle{}, 0))
	other := sigOf(t, solidJPEG(t, 128, image.Rect(0, 0, 320, 200), 40))

	tk := newTracker(ChangeOptions{KeepEvery: time.Hour})
	if _, fresh := tk.observe(0, same); !fresh {
		t.Fatal("the first frame must own its file")
	}
	if _, fresh := tk.observe(100, same); fresh {
		t.Error("an unchanged frame asked for a file of its own")
	}
	if _, fresh := tk.observe(200, other); !fresh {
		t.Error("a changed frame did not ask for a file of its own")
	}
}

func TestTrackerWritesAFrameAfterKeepEvery(t *testing.T) {
	same := sigOf(t, solidJPEG(t, 128, image.Rectangle{}, 0))
	tk := newTracker(ChangeOptions{KeepEvery: 2 * time.Second})

	tk.observe(0, same)
	if _, fresh := tk.observe(1900, same); fresh {
		t.Error("a frame inside the keep-every window wrote a file")
	}
	if _, fresh := tk.observe(3900, same); !fresh {
		t.Error("no file was written after the keep-every window passed")
	}
}

func TestKeepAllWritesEveryFrame(t *testing.T) {
	same := sigOf(t, solidJPEG(t, 128, image.Rectangle{}, 0))
	tk := newTracker(ChangeOptions{KeepAll: true})
	for i := 0; i < 5; i++ {
		if _, fresh := tk.observe(int64(i)*50, same); !fresh {
			t.Fatalf("frame %d was shared even though keep-all is on", i)
		}
	}
}
