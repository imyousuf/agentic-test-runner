package record

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Encode turns a recording into an MP4 next to its frames.
//
// The frames arrive at whatever rate the page produced them, so the concat
// demuxer carries an explicit duration per frame and ffmpeg writes a variable
// frame rate file. The frames also change size when the window is resized, so
// every one is scaled and padded into a single canvas.
func Encode(ctx context.Context, s *Store, id string) (string, error) {
	if c := checkFFmpeg(); !c.OK {
		return "", c.Err
	}

	m, err := s.Load(id)
	if err != nil {
		return "", err
	}
	if len(m.Frames) == 0 {
		return "", fmt.Errorf("%s has no frame to encode", id)
	}
	dir, err := s.Dir(id)
	if err != nil {
		return "", err
	}

	listPath := filepath.Join(dir, "concat.txt")
	if err := os.WriteFile(listPath, ConcatList(m), 0o644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", listPath, err)
	}
	defer func() { _ = os.Remove(listPath) }()

	w, h := canvasSize(m)
	out := filepath.Join(dir, mp4File)
	filter := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,"+
			"pad=%d:%d:(ow-iw)/2:(oh-ih)/2,format=yuv420p", w, h, w, h)

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-vf", filter,
		"-fps_mode", "vfr",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "28",
		"-movflags", "+faststart",
		out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg failed to encode %s: %w\n%s",
			id, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// ConcatList builds the body of concat.txt.
//
// The last file is listed twice, and the second entry carries no duration.
// The concat demuxer ends the stream at the start of the final entry, so
// without the repeat the last frame is dropped. The spike measured exactly
// that: six frames became five, and 15.96 s became 14.96 s.
func ConcatList(m *Manifest) []byte {
	var b strings.Builder
	b.WriteString("ffconcat version 1.0\n")

	frames := m.Frames
	for i, f := range frames {
		var d float64
		if i+1 < len(frames) {
			d = float64(frames[i+1].AtMs-f.AtMs) / 1000
		} else {
			d = 1.0 / 30
		}
		if d < 1.0/60 {
			d = 1.0 / 60
		}
		fmt.Fprintf(&b, "file '%s/%s'\n", framesDir, f.File)
		fmt.Fprintf(&b, "duration %.3f\n", d)
	}
	fmt.Fprintf(&b, "file '%s/%s'\n", framesDir, frames[len(frames)-1].File)
	return []byte(b.String())
}

// canvasSize picks one size that holds every frame. H.264 needs even numbers.
func canvasSize(m *Manifest) (int, int) {
	var w, h float64
	for _, f := range m.Frames {
		w = math.Max(w, f.W)
		h = math.Max(h, f.H)
	}
	if w <= 0 || h <= 0 {
		w, h = 1280, 720
	}
	return even(w), even(h)
}

func even(v float64) int {
	n := int(math.Round(v))
	if n < 2 {
		n = 2
	}
	if n%2 == 1 {
		n++
	}
	return n
}
