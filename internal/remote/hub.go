package remote

import (
	"encoding/json"
	"sync"

	"github.com/imyousuf/agentic-test-runner/internal/record"
)

// Frame is one encoded screencast image with the metadata the client needs to
// map its canvas coordinates back to page coordinates.
type Frame struct {
	Seq          int     `json:"seq"`
	DeviceWidth  float64 `json:"deviceWidth"`
	DeviceHeight float64 `json:"deviceHeight"`
	PageScale    float64 `json:"pageScale"`
	OffsetTop    float64 `json:"offsetTop"`
	ScrollX      float64 `json:"scrollX"`
	ScrollY      float64 `json:"scrollY"`
	JPEG         []byte  `json:"-"`
}

// viewer is one connected browser tab.
type viewer struct {
	mu      sync.Mutex
	pending *Frame
	text    [][]byte
	wake    chan struct{}
	closed  bool
	lastErr string
}

// repeatError reports whether this message is the same as the last one sent,
// so a failure that recurs on every action does not queue without bound.
func (v *viewer) repeatError(msg string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.lastErr == msg {
		return true
	}
	v.lastErr = msg
	return false
}

func newViewer() *viewer {
	return &viewer{wake: make(chan struct{}, 1)}
}

// put replaces the pending frame. It never queues, so a slow viewer always
// receives the newest image instead of a backlog of stale ones.
func (v *viewer) put(f *Frame) {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	v.pending = f
	v.mu.Unlock()
	v.signal()
}

// send queues a control message. Control messages are not dropped.
func (v *viewer) send(msg []byte) {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	v.text = append(v.text, msg)
	v.mu.Unlock()
	v.signal()
}

func (v *viewer) signal() {
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

// take returns everything waiting for this viewer.
func (v *viewer) take() (*Frame, [][]byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	f := v.pending
	v.pending = nil
	msgs := v.text
	v.text = nil
	return f, msgs
}

func (v *viewer) close() {
	v.mu.Lock()
	v.closed = true
	v.mu.Unlock()
	v.signal()
}

// logRing is how many log lines the hub keeps for a viewer that joins late.
//
// The dock has to show something the moment it opens, and the page reported
// most of what matters before anybody thought to look. Two thousand rows is
// about a minute of a busy application at the rate cap.
const logRing = 2000

// Hub keeps the connected viewers and fans frames out to them.
type Hub struct {
	mu      sync.RWMutex
	viewers map[*viewer]struct{}

	logMu  sync.Mutex
	recent []record.LogEvent
}

func NewHub() *Hub {
	return &Hub{viewers: make(map[*viewer]struct{})}
}

func (h *Hub) add(v *viewer) {
	h.mu.Lock()
	h.viewers[v] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(v *viewer) {
	h.mu.Lock()
	delete(h.viewers, v)
	h.mu.Unlock()
	v.close()
}

// Count reports how many viewers are connected.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.viewers)
}

// Frame hands an image to every viewer. It satisfies Sink.
func (h *Hub) Frame(f *Frame) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for v := range h.viewers {
		v.put(f)
	}
}

// Text hands a control message to every viewer. It satisfies Sink.
func (h *Hub) Text(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for v := range h.viewers {
		v.send(msg)
	}
}

// Log hands one line to every viewer and keeps it for the next one. It
// satisfies Logger.
func (h *Hub) Log(ev record.LogEvent) {
	h.logMu.Lock()
	h.recent = append(h.recent, ev)
	if n := len(h.recent) - logRing; n > 0 {
		h.recent = append(h.recent[:0], h.recent[n:]...)
	}
	h.logMu.Unlock()

	msg, err := json.Marshal(map[string]any{"t": "log", "rows": []record.LogEvent{ev}})
	if err != nil {
		return
	}
	h.Text(msg)
}

// Backlog is the log as one message, for a viewer that has just connected.
// It returns nil when the page has said nothing yet.
func (h *Hub) Backlog() []byte {
	h.logMu.Lock()
	rows := make([]record.LogEvent, len(h.recent))
	copy(rows, h.recent)
	h.logMu.Unlock()

	if len(rows) == 0 {
		return nil
	}
	msg, err := json.Marshal(map[string]any{"t": "log", "rows": rows})
	if err != nil {
		return nil
	}
	return msg
}
