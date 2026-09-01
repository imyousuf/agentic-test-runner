package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/record"
)

// registerRecording adds the record and recordings routes. A server with no
// session serves none of them, which is what "atr remote" does when it could
// not open the recordings directory.
func (s *Server) registerRecording(mux *http.ServeMux) {
	if s.session == nil {
		return
	}
	mux.HandleFunc("/api/record/start", s.handleRecordStart)
	mux.HandleFunc("/api/record/stop", s.handleRecordStop)
	mux.HandleFunc("/api/record/status", s.handleRecordStatus)
	mux.HandleFunc("/api/recordings", s.handleRecordings)
	mux.HandleFunc("/api/recordings/", s.handleRecording)
}

func (s *Server) handleRecordStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use POST"})
		return
	}
	// A view-only server never records. Somebody who cannot click also cannot
	// decide to put the screen on the disk.
	if s.viewOnly {
		writeJSON(w, http.StatusForbidden,
			map[string]string{"error": "this live view is view-only, so it cannot record"})
		return
	}

	var body struct {
		Title string `json:"title"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	id, err := s.session.Start(body.Title)
	if err != nil {
		code := http.StatusInternalServerError
		if err == ErrAlreadyRecording {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *Server) handleRecordStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use POST"})
		return
	}
	m, err := s.session.Stop()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": m.ID, "frames": len(m.Frames), "durationMs": m.DurationMs, "bytes": m.Bytes,
	})
}

func (s *Server) handleRecordStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.session.Status())
}

func (s *Server) handleRecordings(w http.ResponseWriter, _ *http.Request) {
	list, err := s.session.Store().List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recordings": list})
}

// handleRecording serves everything under /api/recordings/{id}/.
//
// The id and the frame name are both matched against a fixed pattern before
// anything is joined to a path, and the files are served through an fs.FS
// rooted at the recordings directory. So a request cannot walk out of that
// directory even if one of those checks were wrong.
func (s *Server) handleRecording(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/recordings/")
	parts := strings.Split(rest, "/")
	id := parts[0]
	if !record.ValidID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a recording id"})
		return
	}
	store := s.session.Store()

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			m, err := store.Load(id)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, m)
		case http.MethodPatch:
			var body struct {
				Title string `json:"title"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad body"})
				return
			}
			if err := store.Rename(id, body.Title); err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case http.MethodDelete:
			if s.session.Active() && s.session.Status().ID == id {
				writeJSON(w, http.StatusConflict,
					map[string]string{"error": "that recording is still running"})
				return
			}
			if err := store.Delete(id); err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "bad method"})
		}
		return
	}

	switch {
	case len(parts) == 2 && parts[1] == "manifest.json":
		m, err := store.Load(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, m)

	case len(parts) == 2 && parts[1] == "devtools.jsonl":
		// The journal goes over as it is on the disk, one JSON object a line.
		// The player streams it and filters it, and a person can read it with
		// "less". Parsing it here would only make it bigger.
		w.Header().Set("Content-Type", "application/x-ndjson")
		serveUnder(w, r, store.Root(), path.Join(id, "devtools.jsonl"))

	case len(parts) == 3 && parts[1] == "frames":
		if !record.ValidFrameName(parts[2]) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a frame"})
			return
		}
		// A frame never changes once it is written, so let the page keep it.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		serveUnder(w, r, store.Root(), path.Join(id, "frames", parts[2]))

	case len(parts) == 2 && parts[1] == "recording.mp4":
		serveUnder(w, r, store.Root(), path.Join(id, "recording.mp4"))

	case len(parts) == 2 && parts[1] == "encode":
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use POST"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()
		if _, err := record.Encode(ctx, store, id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"url": "/api/recordings/" + id + "/recording.mp4"})

	case len(parts) == 2 && parts[1] == "repair":
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use POST"})
			return
		}
		m, err := store.Repair(id)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": m.ID, "frames": len(m.Frames)})

	default:
		http.NotFound(w, r)
	}
}

// serveUnder serves one file through a file system rooted at dir, so the path
// cannot escape it.
func serveUnder(w http.ResponseWriter, r *http.Request, dir, name string) {
	http.ServeFileFS(w, r, os.DirFS(dir), name)
}
