package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// registerRoutes registers all HTTP routes.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/api/v1/health", s.handleHealth)
	s.mux.HandleFunc("/api/v1/shutdown", s.handleShutdown)

	// Navigation
	s.mux.HandleFunc("/api/v1/navigate", s.handleNavigate)
	s.mux.HandleFunc("/api/v1/back", s.handleBack)
	s.mux.HandleFunc("/api/v1/forward", s.handleForward)
	s.mux.HandleFunc("/api/v1/reload", s.handleReload)

	// Page management
	s.mux.HandleFunc("/api/v1/pages", s.handlePages)
	s.mux.HandleFunc("/api/v1/pages/", s.handlePageByIndex)

	// Interaction
	s.mux.HandleFunc("/api/v1/click", s.handleClick)
	s.mux.HandleFunc("/api/v1/fill", s.handleFill)
	s.mux.HandleFunc("/api/v1/hover", s.handleHover)
	s.mux.HandleFunc("/api/v1/press-key", s.handlePressKey)
	s.mux.HandleFunc("/api/v1/drag", s.handleDrag)

	// Inspection
	s.mux.HandleFunc("/api/v1/snapshot", s.handleSnapshot)
	s.mux.HandleFunc("/api/v1/screenshot", s.handleScreenshot)
	s.mux.HandleFunc("/api/v1/html", s.handleHTML)
	s.mux.HandleFunc("/api/v1/url", s.handleURL)
	s.mux.HandleFunc("/api/v1/title", s.handleTitle)
	s.mux.HandleFunc("/api/v1/eval", s.handleEval)

	// Debugging
	s.mux.HandleFunc("/api/v1/console", s.handleConsole)
	s.mux.HandleFunc("/api/v1/network", s.handleNetwork)
	s.mux.HandleFunc("/api/v1/errors", s.handleErrors)
}

// handleHealth handles GET /api/v1/health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, map[string]interface{}{
		"status":    "ok",
		"url":       s.browser.CurrentURL(),
		"title":     s.browser.PageTitle(),
		"pages":     len(s.browser.ListPages()),
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// handleShutdown handles POST /api/v1/shutdown
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeSuccess(w, map[string]string{"message": "shutting down"})

	// Shutdown in background
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Shutdown()
		os.Exit(0)
	}()
}

// handleNavigate handles POST /api/v1/navigate
func (s *Server) handleNavigate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	ctx := context.Background()

	// Check if we need to create a new page first
	pages := s.browser.ListPages()
	if len(pages) == 0 {
		if err := s.browser.NewPage(ctx, req.URL); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create page: %v", err))
			return
		}
	} else {
		if err := s.browser.Navigate(ctx, req.URL); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("navigation failed: %v", err))
			return
		}
	}

	writeSuccess(w, map[string]interface{}{
		"url":   s.browser.CurrentURL(),
		"title": s.browser.PageTitle(),
	})
}

// handleBack handles POST /api/v1/back
func (s *Server) handleBack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if err := s.browser.GoBack(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to go back: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"url":   s.browser.CurrentURL(),
		"title": s.browser.PageTitle(),
	})
}

// handleForward handles POST /api/v1/forward
func (s *Server) handleForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if err := s.browser.GoForward(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to go forward: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"url":   s.browser.CurrentURL(),
		"title": s.browser.PageTitle(),
	})
}

// handleReload handles POST /api/v1/reload
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if err := s.browser.Reload(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to reload: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"url":   s.browser.CurrentURL(),
		"title": s.browser.PageTitle(),
	})
}

// handlePages handles GET/POST /api/v1/pages
func (s *Server) handlePages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		pages := s.browser.ListPages()
		writeSuccess(w, map[string]interface{}{
			"pages": pages,
			"count": len(pages),
		})

	case http.MethodPost:
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			req.URL = "about:blank"
		}

		ctx := context.Background()
		if err := s.browser.NewPage(ctx, req.URL); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create page: %v", err))
			return
		}

		pages := s.browser.ListPages()
		writeSuccess(w, map[string]interface{}{
			"pages": pages,
			"count": len(pages),
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handlePageByIndex handles PUT/DELETE /api/v1/pages/{index}
func (s *Server) handlePageByIndex(w http.ResponseWriter, r *http.Request) {
	// Extract index from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/pages/")
	index, err := strconv.Atoi(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid page index")
		return
	}

	switch r.Method {
	case http.MethodPut:
		if err := s.browser.SelectPage(index); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to select page: %v", err))
			return
		}

		pages := s.browser.ListPages()
		writeSuccess(w, map[string]interface{}{
			"pages":   pages,
			"current": index,
		})

	case http.MethodDelete:
		if err := s.browser.ClosePage(index); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to close page: %v", err))
			return
		}

		pages := s.browser.ListPages()
		writeSuccess(w, map[string]interface{}{
			"pages": pages,
			"count": len(pages),
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleClick handles POST /api/v1/click
func (s *Server) handleClick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Target      string `json:"target"`
		DoubleClick bool   `json:"double_click"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}

	ctx := context.Background()
	if err := s.browser.Click(ctx, req.Target, req.DoubleClick); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("click failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"clicked": req.Target,
		"url":     s.browser.CurrentURL(),
		"title":   s.browser.PageTitle(),
	})
}

// handleFill handles POST /api/v1/fill
func (s *Server) handleFill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Target string `json:"target"`
		Value  string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}

	ctx := context.Background()
	if err := s.browser.Fill(ctx, req.Target, req.Value); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("fill failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"filled": req.Target,
		"value":  req.Value,
	})
}

// handleHover handles POST /api/v1/hover
func (s *Server) handleHover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}

	ctx := context.Background()
	if err := s.browser.Hover(ctx, req.Target); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("hover failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"hovered": req.Target,
	})
}

// handlePressKey handles POST /api/v1/press-key
func (s *Server) handlePressKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	if err := s.browser.PressKey(req.Key); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("press key failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"pressed": req.Key,
	})
}

// handleDrag handles POST /api/v1/drag
func (s *Server) handleDrag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.From == "" || req.To == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}

	ctx := context.Background()
	if err := s.browser.Drag(ctx, req.From, req.To); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("drag failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"from": req.From,
		"to":   req.To,
	})
}

// handleSnapshot handles GET /api/v1/snapshot
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	verbose := r.URL.Query().Get("verbose") == "true"

	elements, err := s.browser.Snapshot(verbose)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("snapshot failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"elements": elements,
		"count":    len(elements),
		"url":      s.browser.CurrentURL(),
		"title":    s.browser.PageTitle(),
	})
}

// handleScreenshot handles GET /api/v1/screenshot
func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	fullPage := r.URL.Query().Get("full") == "true"
	format := r.URL.Query().Get("format") // "file" or "base64", default base64

	data, err := s.browser.Screenshot(fullPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("screenshot failed: %v", err))
		return
	}

	if format == "file" {
		// Save to temp file
		filename := fmt.Sprintf("atr-screenshot-%s.png", time.Now().Format("20060102-150405"))
		filepath := filepath.Join(os.TempDir(), filename)
		if err := os.WriteFile(filepath, data, 0644); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save screenshot: %v", err))
			return
		}

		writeSuccess(w, map[string]interface{}{
			"path": filepath,
			"size": len(data),
		})
	} else {
		// Return base64 encoded
		writeSuccess(w, map[string]interface{}{
			"data":   base64.StdEncoding.EncodeToString(data),
			"format": "png",
			"size":   len(data),
		})
	}
}

// handleHTML handles GET /api/v1/html
func (s *Server) handleHTML(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	html, err := s.browser.HTML()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get HTML: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"html": html,
		"url":  s.browser.CurrentURL(),
	})
}

// handleURL handles GET /api/v1/url
func (s *Server) handleURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeSuccess(w, map[string]interface{}{
		"url": s.browser.CurrentURL(),
	})
}

// handleTitle handles GET /api/v1/title
func (s *Server) handleTitle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeSuccess(w, map[string]interface{}{
		"title": s.browser.PageTitle(),
	})
}

// handleEval handles POST /api/v1/eval
func (s *Server) handleEval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Script string `json:"script"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Script == "" {
		writeError(w, http.StatusBadRequest, "script is required")
		return
	}

	result, err := s.browser.Evaluate(req.Script)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("eval failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"result": result,
	})
}

// handleConsole handles GET /api/v1/console
func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	messages := s.browser.GetConsoleMessages(limit)

	writeSuccess(w, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

// handleNetwork handles GET /api/v1/network
func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	requests := s.browser.GetNetworkRequests(limit)

	writeSuccess(w, map[string]interface{}{
		"requests": requests,
		"count":    len(requests),
	})
}

// handleErrors handles GET /api/v1/errors
func (s *Server) handleErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	failedRequests := s.browser.GetFailedRequests()

	writeSuccess(w, map[string]interface{}{
		"failed_requests": failedRequests,
		"count":           len(failedRequests),
	})
}
