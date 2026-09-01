package record

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"os"
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
	// MaxLog caps devtools.jsonl. Zero means DefaultMaxLog, and NoMaxLog lifts
	// the cap. It is not "zero means no limit" like the others, because a log
	// with no cap is a disk waiting to fill.
	MaxLog int64
}

// queueDepth is how many frames may wait for the disk.
//
// The producer is the goroutine that acknowledges a screencast frame, and
// Chrome stops the stream when an acknowledgement is late. So Write must never
// wait. A deep queue absorbs a slow write; past that the recorder drops frames
// and counts them, because a gap in a recording is a much smaller loss than a
// stream that dies.
const queueDepth = 512

// job is one thing for the recorder goroutine to store: an image, or a line
// for the log. They share a queue so that they keep their order and need one
// goroutine between them.
type job struct {
	at  time.Time
	img Image
	log *LogEvent
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

	ch       chan job
	done     chan struct{}
	once     sync.Once
	live     Live
	stopBeat chan struct{}

	result    *Manifest
	resultErr error

	// track is only touched by the recorder goroutine, so it needs no lock.
	track   *tracker
	lastRec FrameRecord

	// logw, capture and fails are only touched by the recorder goroutine and by
	// finish, which runs after that goroutine has ended.
	logw    *logWriter
	capture DevTools
	fails   *failIndex

	mu     sync.Mutex
	w      *writer
	frames []FrameRecord
	// refs counts the frames that point at each file, and fileSize is what
	// each file costs. Sharing means a name is no longer one frame, so the
	// ring buffer has to count before it deletes.
	refs     map[string]int
	fileSize map[string]int64
	events   []Event
	// dropped counts frames the queue refused, logDropped counts lines it
	// refused. They are two numbers because they end up in two places: a lost
	// frame is a gap in the picture, a lost line is a gap in the log.
	dropped    int
	logDropped int
	bytes      int64
	seq        int
	err        error
	full       bool // a limit ended the capture
}

// StartOptions describe a recording that is about to begin.
type StartOptions struct {
	Title   string
	Browser string
	Options Options
	Limits  Limits
	Change  ChangeOptions
	// Source names what started this recording, for the live marker: "cli" for
	// "atr record", "live-view" for the button in the page. It only ever
	// reaches a person reading "something else is recording", so an unknown
	// value costs nothing.
	Source string
	// Capture is what the log was allowed to keep. It is written to the
	// manifest, so a person reading the recording later knows what is in it.
	Capture DevTools
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
	logw, err := newLogWriter(dir, so.Limits.MaxLog)
	if err != nil {
		return nil, err
	}

	change := so.Change.withDefaults()
	opts := so.Options
	// Record how the scores were made, so a player can read them years later
	// without guessing which build produced them.
	opts.RefLagMs = int(change.RefLag.Milliseconds())
	opts.ChangeThreshold = change.Threshold
	opts.DedupeEpsilon = change.Epsilon
	opts.KeepEveryMs = int(change.KeepEvery.Milliseconds())
	if change.KeepAll {
		opts.DedupeEpsilon = 0
	}

	r := &Recorder{
		store:    store,
		id:       id,
		dir:      dir,
		title:    so.Title,
		opts:     opts,
		browser:  so.Browser,
		limits:   so.Limits,
		started:  now,
		track:    newTracker(change),
		w:        w,
		logw:     logw,
		capture:  so.Capture,
		fails:    newFailIndex(),
		refs:     map[string]int{},
		fileSize: map[string]int64{},
		ch:       make(chan job, queueDepth),
		done:     make(chan struct{}),
		stopBeat: make(chan struct{}),
	}
	r.live = Live{
		ID: id, Title: so.Title, PID: os.Getpid(),
		Source: so.Source, StartedAt: now,
	}
	writeLive(dir, r.live)
	go r.beat()
	go r.loop()
	return r, nil
}

// beat keeps the live marker fresh. Nothing else depends on it, so it stops as
// soon as the recording does and never holds the process open.
func (r *Recorder) beat() {
	ticker := time.NewTicker(liveBeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopBeat:
			return
		case <-ticker.C:
			writeLive(r.dir, r.live)
		}
	}
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

// Log queues one line for the log journal. Like Write it never blocks and
// never fails: the tap runs on a CDP event goroutine, and a slow disk must not
// hold up the browser.
//
// Lines and frames share one queue so that they keep the order they happened
// in, and so that one goroutine owns both journals.
func (r *Recorder) Log(ev LogEvent) {
	select {
	case r.ch <- job{at: time.Now(), log: &ev}:
	default:
		r.mu.Lock()
		r.logDropped++
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
		close(r.stopBeat)
		close(r.ch)
		<-r.done
		r.result, r.resultErr = r.finish()
		// The marker outlives a crash on purpose, so a clean stop has to take
		// it away. It goes last: until the manifest is on disk, this recording
		// really is still being written.
		clearLive(r.dir)
	})
	return r.result, r.resultErr
}

func (r *Recorder) loop() {
	defer close(r.done)
	for j := range r.ch {
		if j.log != nil {
			r.store1Log(j)
			continue
		}
		r.store1(j)
	}
}

func (r *Recorder) store1Log(j job) {
	ev := *j.log
	// The tap stamped the wall clock when the event happened. The queue time is
	// only when the recorder got to it, so the stamp wins when there is one.
	if ev.TS > 0 {
		ev.AtMs = ev.TS - r.started.UnixMilli()
	} else {
		ev.AtMs = j.at.Sub(r.started).Milliseconds()
	}
	// An event from before the recording started belongs at the start of it,
	// not at a negative point no player can draw.
	if ev.AtMs < 0 {
		ev.AtMs = 0
	}

	r.logw.write(ev)

	r.mu.Lock()
	r.events = r.fails.add(r.events, ev)
	r.mu.Unlock()
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

	// Scoring decodes the frame, which is the slow part of this path. It is
	// safe here: this goroutine sits behind the queue, so it cannot delay the
	// screencast acknowledgement that keeps Chrome streaming.
	sig, sigErr := signature(j.img.JPEG)
	score, fresh := 0.0, true
	if sigErr == nil {
		score, fresh = r.track.observe(atMs, sig)
	}
	// The first frame, and any frame after a failed write, must own its file.
	if r.lastRec.File == "" {
		fresh = true
	}

	var (
		rec  FrameRecord
		size int64
		err  error
	)
	if fresh {
		rec, err = w.write(seq, atMs, j.img.JPEG, width, height, j.img.TargetID, score)
		size = int64(len(j.img.JPEG))
	} else {
		rec, err = w.share(seq, atMs, r.lastRec.File, width, height, j.img.TargetID, score)
	}
	if err != nil {
		r.mu.Lock()
		if r.err == nil {
			r.err = err
		}
		r.mu.Unlock()
		return
	}
	r.lastRec = rec

	r.mu.Lock()
	r.frames = append(r.frames, rec)
	r.refs[rec.File]++
	if fresh {
		r.fileSize[rec.File] = size
		r.bytes += size
	}
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

	// A file is shared, so cutting a frame frees nothing until the frame that
	// goes is the last one pointing at it. release counts the pending cuts per
	// file and reports the bytes only on the last one.
	pending := map[string]int{}
	release := func() int64 {
		f := r.frames[cut]
		cut++
		pending[f.File]++
		if r.refs[f.File]-pending[f.File] == 0 {
			return r.fileSize[f.File]
		}
		return 0
	}

	if d := r.limits.KeepLast; d > 0 {
		floor := nowMs - d.Milliseconds()
		for cut < len(r.frames)-1 && r.frames[cut].AtMs < floor {
			kept -= release()
		}
	}

	if max := r.limits.MaxSize; max > 0 && kept > max {
		if r.limits.KeepLast > 0 {
			// A ring window is in force, so make room instead of stopping.
			for kept > max && cut < len(r.frames)-1 {
				kept -= release()
			}
		} else {
			// No window: the size limit is a stop limit.
			r.full = true
		}
	}

	if cut == 0 {
		return
	}
	for name, n := range pending {
		r.refs[name] -= n
		if r.refs[name] <= 0 {
			r.w.remove(name)
			delete(r.refs, name)
			delete(r.fileSize, name)
		}
	}
	r.bytes = kept
	r.frames = append(r.frames[:0], r.frames[cut:]...)
}

func (r *Recorder) finish() (*Manifest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.w.close(); err != nil && r.err == nil {
		r.err = err
	}
	if err := r.logw.close(); err != nil && r.err == nil {
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
		Shared:     len(r.frames) - len(r.refs),
		Bytes:      r.bytes,
		Frames:     r.frames,
		Events:     r.events,
	}
	if m.Events == nil {
		m.Events = []Event{}
	}

	dt := r.logw.stats()
	dt.Dropped += r.logDropped
	dt.Errors = countFailures(r.events)
	dt.Bodies = r.capture.Bodies
	dt.Headers = r.capture.Headers
	dt.RedactQuery = r.capture.RedactQuery
	m.DevTools = &dt

	if err := r.store.Save(m); err != nil {
		return nil, err
	}
	return m, r.err
}
