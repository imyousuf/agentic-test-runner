package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/record"
)

// RecorderSink feeds a record.Recorder from a Streamer.
//
// It is the only place that knows about both packages, which is what keeps
// record free of any CDP or HTTP type.
type RecorderSink struct {
	r        *record.Recorder
	targetID string
}

// NewRecorderSink wraps a recorder as a Sink.
func NewRecorderSink(r *record.Recorder) *RecorderSink {
	return &RecorderSink{r: r}
}

// Frame hands the image to the recorder. record.Recorder.Write never blocks,
// so the screencast acknowledgement is never held up by the disk.
func (k *RecorderSink) Frame(f *Frame) {
	if f == nil || len(f.JPEG) == 0 {
		return
	}
	k.r.Write(record.Image{JPEG: f.JPEG, TargetID: k.targetID})
}

// Text watches the control messages for a tab change, so the player can put a
// tick on the timeline where the recording moved to another page.
func (k *RecorderSink) Text(msg []byte) {
	var m struct {
		T     string     `json:"t"`
		Pages []PageInfo `json:"pages"`
	}
	if err := json.Unmarshal(msg, &m); err != nil || m.T != "pages" {
		return
	}
	for _, p := range m.Pages {
		if !p.Active {
			continue
		}
		if p.ID != k.targetID {
			k.targetID = p.ID
			k.r.Note(record.Event{T: "tab", TargetID: p.ID, URL: p.URL})
		}
		return
	}
}

// Session ties one running recording to the streamer that feeds it. The
// server holds at most one, because a second recording of the same browser
// would only duplicate the frames.
type Session struct {
	mu       sync.Mutex
	store    *record.Store
	streamer *Streamer
	rec      *record.Recorder
	sink     *RecorderSink
	limits   record.Limits
	encode   bool
}

// NewSession prepares the recording control for a server.
func NewSession(store *record.Store, streamer *Streamer, limits record.Limits, encode bool) *Session {
	return &Session{store: store, streamer: streamer, limits: limits, encode: encode}
}

// Store exposes the recordings library.
func (s *Session) Store() *record.Store { return s.store }

// Active reports whether a recording is running.
func (s *Session) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec != nil
}

// ErrAlreadyRecording says a second start arrived while one was running.
var ErrAlreadyRecording = fmt.Errorf("a recording is already running")

// Start begins a recording and attaches it to the stream.
func (s *Session) Start(title string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec != nil {
		return "", ErrAlreadyRecording
	}

	pages, _ := s.streamer.Pages()
	pre := record.Preflight{Root: s.store.Root(), Encode: s.encode, Pages: len(pages)}
	if err := record.FirstError(pre.Run()); err != nil {
		return "", err
	}

	rec, err := record.Start(s.store, record.StartOptions{
		Title:   title,
		Browser: s.streamer.Version(),
		Options: record.Options{
			Quality:  s.streamer.opts.Quality,
			MaxWidth: s.streamer.opts.MaxWidth,
			FPS:      s.streamer.opts.FPS,
			Policy:   s.streamer.Policy(),
		},
		Limits: s.limits,
	})
	if err != nil {
		return "", err
	}

	sink := NewRecorderSink(rec)
	// Seed the recording with what is on the screen now. Without this a still
	// page would produce nothing until the heartbeat came round.
	if f, err := s.streamer.Snapshot(); err == nil {
		sink.Frame(f)
	} else if f := s.streamer.LastFrame(); f != nil {
		sink.Frame(f)
	}
	s.streamer.AddSink(sink)

	s.rec, s.sink = rec, sink
	return rec.ID(), nil
}

// Stop ends the recording and writes its manifest.
func (s *Session) Stop() (*record.Manifest, error) {
	s.mu.Lock()
	rec, sink := s.rec, s.sink
	s.rec, s.sink = nil, nil
	s.mu.Unlock()

	if rec == nil {
		return nil, fmt.Errorf("no recording is running")
	}
	// Detach first. Stopping a recording must not interrupt the people who
	// are still watching the live view.
	s.streamer.RemoveSink(sink)
	return rec.Stop()
}

// Status reports the recording in progress, or an idle status.
func (s *Session) Status() record.Status {
	s.mu.Lock()
	rec := s.rec
	s.mu.Unlock()
	if rec == nil {
		return record.Status{}
	}
	return rec.Status()
}

// Publish broadcasts the recording status once a second, so the button in the
// page shows the elapsed time and the frame count without polling.
//
// It also enforces the limits: a recording that reaches --max-duration or
// --max-size stops itself and keeps what it captured.
func (s *Session) Publish(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var wasActive bool
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			rec := s.rec
			s.mu.Unlock()

			if rec != nil && rec.Full() {
				if _, err := s.Stop(); err != nil {
					s.broadcastRecord(record.Status{}, err.Error())
					continue
				}
				s.broadcastRecord(record.Status{}, "the recording reached its limit and stopped")
				wasActive = false
				continue
			}

			st := s.Status()
			if !st.Recording && !wasActive {
				continue
			}
			wasActive = st.Recording
			s.broadcastRecord(st, "")
		}
	}
}

func (s *Session) broadcastRecord(st record.Status, note string) {
	body := map[string]any{
		"t":         "record",
		"recording": st.Recording,
		"id":        st.ID,
		"title":     st.Title,
		"elapsedMs": st.ElapsedMs,
		"frames":    st.Frames,
		"bytes":     st.Bytes,
		"dropped":   st.Dropped,
	}
	if note != "" {
		body["note"] = note
	}
	msg, err := json.Marshal(body)
	if err != nil {
		return
	}
	s.streamer.emitText(msg)
}
