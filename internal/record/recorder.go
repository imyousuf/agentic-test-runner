package record

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"sync"
	"time"
)

// jpegSize reads the dimensions from a JPEG header. It parses the header only,
// not the image, so it is cheap enough for the write path.
func jpegSize(data []byte) (float64, float64) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return float64(cfg.Width), float64(cfg.Height)
}

// Image is one captured frame handed to a Recorder.
//
// The recorder takes an image, not a screencast frame, so this package never
// imports the CDP layer. That keeps the dependency pointing one way: the
// server layer knows about recordings, recordings know nothing about servers.
type Image struct {
	JPEG     []byte
	TargetID string
}

// Limits stop a recording that would otherwise run until the disk is full.
type Limits struct {
	MaxDuration time.Duration // zero means no limit
	MaxSize     int64         // bytes; zero means no limit
	KeepLast    time.Duration // keep only this much of the tail; zero keeps everything
}

// queueDepth is how many frames may wait for the disk.
//
// The producer is the goroutine that acknowledges a screencast frame, and
// Chrome stops the stream when an acknowledgement is late. So Write must never
// wait. A deep queue absorbs a slow write; past that the recorder drops frames
// and counts them, because a gap in a recording is a much smaller loss than a
// stream that dies.
const queueDepth = 512

type job struct {
	at  time.Time
	img Image
}

// Recorder writes one recording. Create it with Start and finish it with Stop.
type Recorder struct {
	store   *Store
	id      string
	dir     string
	title   string
	opts    Options
	browser string
	limits  Limits
	started time.Time

	ch   chan job
	done chan struct{}
	once sync.Once

	result    *Manifest
	resultErr error

	mu      sync.Mutex
	w       *writer
	frames  []FrameRecord
	sizes   []int64
	events  []Event
	dropped int
	bytes   int64
	seq     int
	err     error
	full    bool // a limit ended the capture
}

// StartOptions describe a recording that is about to begin.
type StartOptions struct {
	Title   string
	Browser string
	Options Options
	Limits  Limits
}

// Start creates a recording directory and begins accepting frames.
func Start(store *Store, so StartOptions) (*Recorder, error) {
	now := time.Now()
	id := NewID(now, so.Title)
	dir, err := store.Create(id)
	if err != nil {
		return nil, err
	}
	w, err := newWriter(dir)
	if err != nil {
		return nil, err
	}

	r := &Recorder{
		store:   store,
		id:      id,
		dir:     dir,
		title:   so.Title,
		opts:    so.Options,
		browser: so.Browser,
		limits:  so.Limits,
		started: now,
		w:       w,
		ch:      make(chan job, queueDepth),
		done:    make(chan struct{}),
	}
	go r.loop()
	return r, nil
}

// ID is the identifier of this recording.
func (r *Recorder) ID() string { return r.id }

// Dir is the directory of this recording.
func (r *Recorder) Dir() string { return r.dir }

// Write queues one image. It never blocks and it never returns an error: a
// frame that cannot be queued is counted as dropped.
func (r *Recorder) Write(img Image) {
	select {
	case r.ch <- job{at: time.Now(), img: img}:
	default:
		r.mu.Lock()
		r.dropped++
		r.mu.Unlock()
	}
}

// Note records something that happened, for the player timeline. The time is
// taken from the clock, so the caller only fills in what happened.
func (r *Recorder) Note(e Event) {
	e.AtMs = time.Since(r.started).Milliseconds()
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

// Status reports the progress of the recording.
func (r *Recorder) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Status{
		Recording: true,
		ID:        r.id,
		Title:     r.title,
		ElapsedMs: time.Since(r.started).Milliseconds(),
		Frames:    len(r.frames),
		Bytes:     r.bytes,
		Dropped:   r.dropped,
	}
}

// Full reports whether a limit ended the capture. The caller polls this so it
// can stop the recording and tell the person why.
func (r *Recorder) Full() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.full
}

// Stop drains the queue, writes the manifest, and returns it. It is safe to
// call more than once; later calls return the same manifest.
func (r *Recorder) Stop() (*Manifest, error) {
	r.once.Do(func() {
		close(r.ch)
		<-r.done
		r.result, r.resultErr = r.finish()
	})
	return r.result, r.resultErr
}

func (r *Recorder) loop() {
	defer close(r.done)
	for j := range r.ch {
		r.store1(j)
	}
}

func (r *Recorder) store1(j job) {
	atMs := j.at.Sub(r.started).Milliseconds()

	r.mu.Lock()
	if r.full {
		r.mu.Unlock()
		return
	}
	r.seq++
	seq := r.seq
	w := r.w
	r.mu.Unlock()

	// Read the size from the JPEG itself. The screencast metadata reports CSS
	// pixels, but --max-width rescales the image, and both the player canvas
	// and the ffmpeg pad filter need the real pixels.
	width, height := jpegSize(j.img.JPEG)

	rec, err := w.write(seq, atMs, j.img.JPEG, width, height, j.img.TargetID)
	if err != nil {
		r.mu.Lock()
		if r.err == nil {
			r.err = err
		}
		r.mu.Unlock()
		return
	}

	r.mu.Lock()
	r.frames = append(r.frames, rec)
	r.sizes = append(r.sizes, int64(len(j.img.JPEG)))
	r.bytes += int64(len(j.img.JPEG))
	r.trimLocked(atMs)
	if r.limits.MaxDuration > 0 && j.at.Sub(r.started) >= r.limits.MaxDuration {
		r.full = true
	}
	r.mu.Unlock()
}

// trimLocked applies the ring limits by dropping the oldest frames.
//
// KeepLast and MaxSize both hold a window on the tail. A long unattended
// recording therefore keeps the part somebody will actually want, instead of
// filling the disk with the part before anything went wrong.
func (r *Recorder) trimLocked(nowMs int64) {
	cut := 0
	kept := r.bytes

	if d := r.limits.KeepLast; d > 0 {
		floor := nowMs - d.Milliseconds()
		for cut < len(r.frames)-1 && r.frames[cut].AtMs < floor {
			kept -= r.sizes[cut]
			cut++
		}
	}

	if max := r.limits.MaxSize; max > 0 && kept > max {
		if r.limits.KeepLast > 0 {
			// A ring window is in force, so make room instead of stopping.
			for kept > max && cut < len(r.frames)-1 {
				kept -= r.sizes[cut]
				cut++
			}
		} else {
			// No window: the size limit is a stop limit.
			r.full = true
		}
	}

	if cut == 0 {
		return
	}
	for _, f := range r.frames[:cut] {
		r.w.remove(f.File)
	}
	r.bytes = kept
	r.frames = append(r.frames[:0], r.frames[cut:]...)
	r.sizes = append(r.sizes[:0], r.sizes[cut:]...)
}

func (r *Recorder) finish() (*Manifest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.w.close(); err != nil && r.err == nil {
		r.err = err
	}
	if len(r.frames) == 0 {
		return nil, fmt.Errorf("the recording captured no frame; the page may have been in a background tab")
	}

	// The ring may have dropped the head, so the first kept frame is the
	// origin, not the moment the recording started.
	origin := r.frames[0].AtMs
	last := r.frames[len(r.frames)-1].AtMs

	m := &Manifest{
		Version:    Version,
		ID:         r.id,
		Title:      r.title,
		StartedAt:  r.started.Add(time.Duration(origin) * time.Millisecond),
		StoppedAt:  time.Now(),
		DurationMs: last - origin,
		Browser:    r.browser,
		Options:    r.opts,
		Dropped:    r.dropped,
		Bytes:      r.bytes,
		Frames:     r.frames,
		Events:     r.events,
	}
	if m.Events == nil {
		m.Events = []Event{}
	}
	if err := r.store.Save(m); err != nil {
		return nil, err
	}
	return m, r.err
}
