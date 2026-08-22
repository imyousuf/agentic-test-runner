package rdp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	msgFrame byte = 0x01
)

// Server serves the live view: the web assets, a small REST API, and the
// WebSocket that carries frames out and input in.
type Server struct {
	hub      *Hub
	streamer *Streamer
	assets   fs.FS
	token    string
	viewOnly bool
	upgrader websocket.Upgrader
}

// NewToken makes a random token for a session that did not supply one.
func NewToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "atr-live-view"
	}
	return hex.EncodeToString(buf)
}

func NewServer(hub *Hub, streamer *Streamer, assets fs.FS, token string, viewOnly bool) *Server {
	s := &Server{
		hub:      hub,
		streamer: streamer,
		assets:   assets,
		token:    token,
		viewOnly: viewOnly,
	}
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 64 * 1024,
		CheckOrigin:     s.checkOrigin,
	}
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

const cookieName = "atr_rdp"

func (s *Server) matches(value string) bool {
	return subtle.ConstantTimeCompare([]byte(value), []byte(s.token)) == 1
}

// authorized reports whether the request carries the token. The token can
// arrive in three ways, and all three are needed: the header suits an API
// client, the query parameter suits the first click on a printed URL, and the
// cookie suits everything the page loads afterwards, because a stylesheet
// request carries no query string.
func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		if s.matches(strings.TrimPrefix(h, "Bearer ")) {
			return true
		}
	}
	if s.matches(r.URL.Query().Get("t")) {
		return true
	}
	if c, err := r.Cookie(cookieName); err == nil && s.matches(c.Value) {
		return true
	}
	return false
}

// Handler builds the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pages", s.handlePages)
	mux.HandleFunc("/api/select", s.handleSelect)
	mux.HandleFunc("/api/navigate", s.handleNavigate)
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/", http.FileServer(http.FS(s.assets)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Hand the page a cookie, so its stylesheet and script requests are
		// authorised too.
		if s.token != "" && s.matches(r.URL.Query().Get("t")) {
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    s.token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
		mux.ServeHTTP(w, r)
	})
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
	Kind   string  `json:"kind"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Button string  `json:"button"`
	Clicks int     `json:"clicks"`
	DX     float64 `json:"dx"`
	DY     float64 `json:"dy"`

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
		"t": "status", "streaming": true, "viewers": s.hub.Count(), "viewOnly": s.viewOnly,
	}); err == nil {
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
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg inbound
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		s.dispatch(msg)
	}
}

func (s *Server) dispatch(msg inbound) {
	// A view-only server drops input here, on the server, not in the client.
	if s.viewOnly {
		switch msg.T {
		case "mouse", "wheel", "key", "text", "navigate":
			return
		}
	}

	switch msg.T {
	case "mouse":
		_ = s.streamer.Mouse(MouseMsg{
			Kind: msg.Kind, X: msg.X, Y: msg.Y,
			Button: msg.Button, Clicks: msg.Clicks, Mod: msg.Mod,
		})
	case "wheel":
		_ = s.streamer.Wheel(WheelMsg{X: msg.X, Y: msg.Y, DX: msg.DX, DY: msg.DY, Mod: msg.Mod})
	case "key":
		_ = s.streamer.Key(KeyMsg{
			Kind: msg.Kind, Key: msg.Key, Code: msg.Code,
			VK: msg.VK, Text: msg.Text, Mod: msg.Mod,
		})
	case "text":
		_ = s.streamer.Text(msg.Value)
	case "selectPage":
		_ = s.streamer.Select(msg.ID)
	case "navigate":
		_ = s.streamer.Navigate(msg.URL)
	case "policy":
		s.streamer.SetPolicy(msg.Foreground)
	}
}

// writeLoop is the only writer for this connection. A viewer holds one
// pending frame, so a slow client drops stale frames instead of lagging.
func (s *Server) writeLoop(ctx context.Context, conn *websocket.Conn, v *viewer) {
	ping := time.NewTicker(20 * time.Second)
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
