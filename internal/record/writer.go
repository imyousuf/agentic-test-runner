package record

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// writer puts one recording on disk.
//
// It writes each frame as its own JPEG and appends one line to frames.jsonl.
// The journal is what makes an interrupted recording recoverable: the manifest
// only exists after a clean stop, but the journal is already complete up to
// the last frame that was written.
type writer struct {
	dir     string
	frames  string
	journal *os.File
	buf     *bufio.Writer
	bytes   int64
	count   int
}

func newWriter(dir string) (*writer, error) {
	frames := filepath.Join(dir, framesDir)
	if err := os.MkdirAll(frames, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", frames, err)
	}
	f, err := os.Create(filepath.Join(dir, journalFile))
	if err != nil {
		return nil, fmt.Errorf("failed to create the frame journal: %w", err)
	}
	return &writer{dir: dir, frames: frames, journal: f, buf: bufio.NewWriter(f)}, nil
}

// write stores one image and returns the record that describes it.
func (w *writer) write(seq int, atMs int64, jpeg []byte, width, height float64, targetID string, score float64) (FrameRecord, error) {
	name := fmt.Sprintf("%06d.jpg", seq)
	path := filepath.Join(w.frames, name)
	if err := os.WriteFile(path, jpeg, 0o644); err != nil {
		return FrameRecord{}, fmt.Errorf("failed to write %s: %w", path, err)
	}
	w.bytes += int64(len(jpeg))

	rec := FrameRecord{
		Seq: seq, File: name, AtMs: atMs,
		W: width, H: height, TargetID: targetID, Score: score,
	}
	return rec, w.journalise(rec)
}

// share records a frame that shows the same picture as one already on disk. It
// writes no image, only the journal line, so the frame keeps its own place on
// the timeline at no cost in bytes.
func (w *writer) share(seq int, atMs int64, file string, width, height float64, targetID string, score float64) (FrameRecord, error) {
	rec := FrameRecord{
		Seq: seq, File: file, AtMs: atMs,
		W: width, H: height, TargetID: targetID, Score: score,
	}
	return rec, w.journalise(rec)
}

func (w *writer) journalise(rec FrameRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("failed to encode the frame record: %w", err)
	}
	if _, err := w.buf.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("failed to append to the frame journal: %w", err)
	}

	w.count++
	// Flush often enough that a crash loses at most a handful of journal
	// lines, and rarely enough that the disk is not the bottleneck.
	if w.count%20 == 0 {
		_ = w.buf.Flush()
	}
	return nil
}

// remove deletes one frame file. The ring buffer uses this to hold the
// recording inside its size or time window.
func (w *writer) remove(name string) {
	if !ValidFrameName(name) {
		return
	}
	path := filepath.Join(w.frames, name)
	if info, err := os.Stat(path); err == nil {
		w.bytes -= info.Size()
	}
	_ = os.Remove(path)
}

func (w *writer) close() error {
	if err := w.buf.Flush(); err != nil {
		_ = w.journal.Close()
		return fmt.Errorf("failed to flush the frame journal: %w", err)
	}
	if err := w.journal.Close(); err != nil {
		return fmt.Errorf("failed to close the frame journal: %w", err)
	}
	return nil
}
