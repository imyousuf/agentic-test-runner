package remote

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

const (
	msgFrame byte = 0x01

	// pingInterval is how often the server pings; pongWait is how long it will
	// wait for any read before declaring the peer gone. pongWait must exceed
	// pingInterval comfortably.
	pingInterval = 20 * time.Second
	pongWait     = 60 * time.Second
)

// Server serves the live view: the web assets, a small REST API, and the
// WebSocket that carries frames out and input in.
type Server struct {
	hub      *Hub
	streamer *Streamer
	session  *Session // nil when this server cannot record or browse recordings
	assets   fs.FS
	viewOnly bool
	upgrader websocket.Upgrader
}

/*
NewServer builds the live view.

It authenticates nobody. The live view used to mint a token and put it in the
URL, and that cost more than it bought: the token changed on every restart, so
the URL people had bookmarked and the cookie their browser was holding both
went stale at once, and the symptom was a page that worked a minute ago
answering 401.

Authentication belongs to whoever is running this. On a laptop that is the
loopback bind. Anywhere else it is a reverse proxy, an SSO gateway, an SSH
tunnel, or a port that is simply not published.
*/
func NewServer(hub *Hub, streamer *Streamer, assets fs.FS, viewOnly bool) *Server {
	s := &Server{
		hub:      hub,
		streamer: streamer,
		assets:   assets,
		viewOnly: viewOnly,
	}
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 64 * 1024,
		CheckOrigin:     s.checkOrigin,
	}
	return s
}

// WithSession gives the server the ability to record and to browse
// recordings.
func (s *Server) WithSession(session *Session) *Server {
	s.session = session
	return s
}

// checkOrigin rejects a page from another site that tries to drive the browser.
func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // a non-browser client, such as a test
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Host
	}
	return host == "127.0.0.1" || host == "localhost" || host == "[::1]" || host == "::1"
}

// Handler builds the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pages", s.handlePages)
	mux.HandleFunc("/api/select", s.handleSelect)
	mux.HandleFunc("/api/navigate", s.handleNavigate)
	s.registerRecording(mux)
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/", http.FileServer(http.FS(s.assets)))

	return mux
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) handlePages(w http.ResponseWriter, _ *http.Request) {
	pages, err := s.streamer.Pages()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
}

func (s *Server) handleSelect(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if err := s.streamer.Select(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleNavigate(w http.ResponseWriter, r *http.Request) {
	// The WebSocket path is not the only way in: a read-only server has to
	// refuse here too, or reaching the port at all is enough to drive it.
	if s.viewOnly {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": ErrViewOnly.Error()})
		return
	}
	target := r.URL.Query().Get("url")
	if target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}
	if err := s.streamer.Navigate(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// inbound is one control message from a viewer.
type inbound struct {
	T string `json:"t"`

	// mouse and wheel
	Kind    string  `json:"kind"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Button  string  `json:"button"`
	Clicks  int     `json:"clicks"`
	Buttons int     `json:"buttons"`
	DX      float64 `json:"dx"`
	DY      float64 `json:"dy"`

	// keyboard
	Key  string `json:"key"`
	Code string `json:"code"`
	VK   int    `json:"vk"`
	Text string `json:"text"`

	Mod int `json:"mod"`

	// control
	Value      string `json:"value"`
	ID         string `json:"id"`
	URL        string `json:"url"`
	Foreground string `json:"foreground"`
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	v := newViewer()
	s.hub.add(v)
	defer s.hub.remove(v)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go s.writeLoop(ctx, conn, v)

	// Send the current tab list at once, so the client is not empty.
	if pages, err := s.streamer.Pages(); err == nil {
		if msg, err := json.Marshal(map[string]any{"t": "pages", "pages": pages}); err == nil {
			v.send(msg)
		}
	}
	if msg, err := json.Marshal(map[string]any{
		"t":         "status",
		"streaming": s.streamer.Live(),
		"viewers":   s.hub.Count(),
		"viewOnly":  s.viewOnly,
		"canRecord": s.session != nil && !s.viewOnly,
	}); err == nil {
		v.send(msg)
	}
	// Tell a viewer that joins mid-recording that one is running, so the
	// button shows the right state at once. That includes a recording started
	// by another process, such as "atr record".
	if s.session != nil {
		st := s.session.Status()
		if msg, err := json.Marshal(map[string]any{
			"t": "record", "recording": st.Recording, "id": st.ID, "title": st.Title,
			"elapsedMs": st.ElapsedMs, "frames": st.Frames, "bytes": st.Bytes,
			"dropped": st.Dropped, "elsewhere": elsewhereJSON(s.session.Elsewhere()),
		}); err == nil {
			v.send(msg)
		}
	}
	// Give the dock what the page said before anybody was watching. Somebody
	// opens the live view because something went wrong, and the error is
	// already behind them.
	if msg := s.hub.Backlog(); msg != nil {
		v.send(msg)
	}
	// A still page emits no frame at all. Capture one on demand so the viewer
	// sees the page immediately instead of a blank canvas.
	if f, err := s.streamer.Snapshot(); err == nil {
		v.put(f)
	} else if f := s.streamer.LastFrame(); f != nil {
		v.put(f)
	}

	conn.SetReadLimit(1 << 20)
	// The pings in writeLoop only detect a dead peer if a missing pong
	// eventually fails the read. Without this a closed laptop lid leaks the
	// viewer and its goroutine for the life of the process.
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		// A client that is talking is alive, whether or not it answers pings.
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		var msg inbound
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		s.dispatch(msg, v)
	}
}

// dispatch applies one viewer message. View-only is enforced by the Streamer,
// so there is no second list here to forget a new primitive from.
func (s *Server) dispatch(msg inbound, v *viewer) {
	var err error

	switch msg.T {
	case "mouse":
		err = s.streamer.Mouse(MouseMsg{
			Kind: msg.Kind, X: msg.X, Y: msg.Y,
			Button: msg.Button, Clicks: msg.Clicks, Mod: msg.Mod,
			Buttons: msg.Buttons,
		})
	case "wheel":
		err = s.streamer.Wheel(WheelMsg{
			X: msg.X, Y: msg.Y, DX: msg.DX, DY: msg.DY, Mod: msg.Mod, Buttons: msg.Buttons,
		})
	case "key":
		err = s.streamer.Key(KeyMsg{
			Kind: msg.Kind, Key: msg.Key, Code: msg.Code,
			VK: msg.VK, Text: msg.Text, Mod: msg.Mod,
		})
	case "text":
		err = s.streamer.Text(msg.Value)
	case "selectPage":
		err = s.streamer.Select(msg.ID)
	case "newPage":
		err = s.streamer.NewPage(msg.URL)
	case "closePage":
		err = s.streamer.ClosePage(msg.ID)
		err = s.streamer.Select(msg.ID)
	case "navigate":
		err = s.streamer.Navigate(msg.URL)
	case "policy":
		s.streamer.SetPolicy(msg.Foreground)
	}

	// A silently dropped click looks identical to a broken stream, so tell the
	// viewer -- but only for discrete actions. Pointer moves arrive once per
	// animation frame, and reporting each failure would queue ~60 messages a
	// second and re-render the client just as often.
	if err == nil || v == nil || !worthReporting(msg) {
		return
	}
	if v.repeatError(err.Error()) {
		return
	}
	if out, mErr := json.Marshal(map[string]any{
		"t": "error", "message": err.Error(),
	}); mErr == nil {
		v.send(out)
	}
}

// worthReporting keeps continuous motion out of the error channel.
func worthReporting(msg inbound) bool {
	switch msg.T {
	case "wheel":
		return false
	case "mouse":
		return msg.Kind != "moved"
	}
	return true
}

// writeLoop is the only writer for this connection. A viewer holds one
// pending frame, so a slow client drops stale frames instead of lagging.
func (s *Server) writeLoop(ctx context.Context, conn *websocket.Conn, v *viewer) {
	// Closing here unblocks the read loop in handleWS. Returning without it
	// left the viewer registered in the hub, so every later broadcast appended
	// to a queue that nothing drained.
	defer func() { _ = conn.Close() }()

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-v.wake:
			frame, msgs := v.take()

			for _, m := range msgs {
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, m); err != nil {
					return
				}
			}

			if frame == nil {
				continue
			}
			payload, err := encodeFrame(frame)
			if err != nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
				return
			}
		}
	}
}

// encodeFrame lays out one frame: a type byte, the header length, the JSON
// header, then the JPEG bytes.
func encodeFrame(f *Frame) ([]byte, error) {
	header, err := json.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the frame header: %w", err)
	}
	out := make([]byte, 0, 5+len(header)+len(f.JPEG))
	out = append(out, msgFrame)
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(header)))
	out = append(out, size[:]...)
	out = append(out, header...)
	out = append(out, f.JPEG...)
	return out, nil
}
