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

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/ops"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
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

	// Wait
	s.mux.HandleFunc("/api/v1/wait", s.handleWait)

	// Styles
	s.mux.HandleFunc("/api/v1/computed-styles", s.handleComputedStyles)
	s.mux.HandleFunc("/api/v1/computed-styles-diff", s.handleComputedStylesDiff)

	// Scroll
	s.mux.HandleFunc("/api/v1/scroll", s.handleScroll)

	// Text
	s.mux.HandleFunc("/api/v1/text", s.handleText)

	// Font check
	s.mux.HandleFunc("/api/v1/font-check", s.handleFontCheck)

	// Download images
	s.mux.HandleFunc("/api/v1/download-images", s.handleDownloadImages)

	// Clean snapshot
	s.mux.HandleFunc("/api/v1/clean-snapshot", s.handleCleanSnapshot)

	// Viewport
	s.mux.HandleFunc("/api/v1/viewport", s.handleViewport)

	// AI-powered
	s.mux.HandleFunc("/api/v1/ask", s.handleAsk)

	// Recording
	s.mux.HandleFunc("/api/v1/record/start", s.handleRecordStart)
	s.mux.HandleFunc("/api/v1/record/stop", s.handleRecordStop)
	s.mux.HandleFunc("/api/v1/record/status", s.handleRecordStatus)
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
	var req ops.NavigateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	res, err := ops.Navigate(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleBack handles POST /api/v1/back
func (s *Server) handleBack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	res, err := ops.Back(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleForward handles POST /api/v1/forward
func (s *Server) handleForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	res, err := ops.Forward(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleReload handles POST /api/v1/reload
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	res, err := ops.Reload(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handlePages handles GET/POST /api/v1/pages
func (s *Server) handlePages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		res, err := ops.ListPages(r.Context(), s.browser)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, res)

	case http.MethodPost:
		var req ops.NewPageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			req.URL = "about:blank"
		}
		res, err := ops.NewPage(r.Context(), s.browser, req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, res)

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
		res, err := ops.SelectPage(r.Context(), s.browser, ops.SelectPageRequest{Index: index})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, res)

	case http.MethodDelete:
		res, err := ops.ClosePage(r.Context(), s.browser, ops.ClosePageRequest{Index: index})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, res)

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
	var req ops.ClickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}
	res, err := ops.Click(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleFill handles POST /api/v1/fill
func (s *Server) handleFill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ops.FillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}
	res, err := ops.Fill(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleHover handles POST /api/v1/hover
func (s *Server) handleHover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ops.HoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}
	res, err := ops.Hover(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handlePressKey handles POST /api/v1/press-key
func (s *Server) handlePressKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ops.PressKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	res, err := ops.PressKey(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleDrag handles POST /api/v1/drag
func (s *Server) handleDrag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ops.DragRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.From == "" || req.To == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	res, err := ops.Drag(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleSnapshot handles GET /api/v1/snapshot
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req := ops.SnapshotRequest{Verbose: r.URL.Query().Get("verbose") == "true"}
	res, err := ops.Snapshot(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleScreenshot handles GET /api/v1/screenshot
func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	req := ops.ScreenshotRequest{
		Selector:    q.Get("selector"),
		SelectorAll: q.Get("selector_all"),
		FullPage:    q.Get("full") == "true",
		OutputDir:   q.Get("output_dir"),
	}
	if t := q.Get("timeout"); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			req.TimeoutMs = n
		}
	}
	format := q.Get("format") // "file" or default base64

	res, err := ops.Screenshot(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Multi-element capture: persist all bytes to disk and return a manifest.
	if res.IsMulti() {
		dir := req.OutputDir
		if dir == "" {
			dir = os.TempDir()
		} else {
			dir = filepath.Clean(dir)
			if err := os.MkdirAll(dir, 0755); err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
				return
			}
		}

		var files []map[string]interface{}
		var skipped []map[string]interface{}
		for _, sr := range res.Multi {
			if sr.Error != "" {
				skipped = append(skipped, map[string]interface{}{
					"index": sr.Index + 1,
					"error": sr.Error,
				})
				continue
			}
			filename := fmt.Sprintf("%d.png", sr.Index+1)
			filePath := filepath.Join(dir, filename)
			if err := os.WriteFile(filePath, sr.Data, 0644); err != nil {
				skipped = append(skipped, map[string]interface{}{
					"index": sr.Index + 1,
					"error": fmt.Sprintf("failed to save: %v", err),
				})
				continue
			}
			files = append(files, map[string]interface{}{
				"index": sr.Index + 1,
				"path":  filePath,
				"size":  len(sr.Data),
			})
		}

		resp := map[string]interface{}{
			"captured": len(files),
			"total":    len(res.Multi),
			"files":    files,
		}
		if len(skipped) > 0 {
			resp["skipped"] = skipped
		}
		writeSuccess(w, resp)
		return
	}

	if format == "file" {
		filename := fmt.Sprintf("atr-screenshot-%s.png", time.Now().Format("20060102-150405"))
		fp := filepath.Join(os.TempDir(), filename)
		if err := os.WriteFile(fp, res.Data, 0644); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save screenshot: %v", err))
			return
		}
		writeSuccess(w, map[string]interface{}{
			"path": fp,
			"size": len(res.Data),
		})
		return
	}

	writeSuccess(w, map[string]interface{}{
		"data":   base64.StdEncoding.EncodeToString(res.Data),
		"format": "png",
		"size":   len(res.Data),
	})
}

// handleHTML handles GET /api/v1/html
func (s *Server) handleHTML(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	res, err := ops.HTML(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleURL handles GET /api/v1/url
func (s *Server) handleURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	res, err := ops.URL(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleTitle handles GET /api/v1/title
func (s *Server) handleTitle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	res, err := ops.Title(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleEval handles POST /api/v1/eval
func (s *Server) handleEval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ops.EvalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Script == "" {
		writeError(w, http.StatusBadRequest, "script is required")
		return
	}
	res, err := ops.Eval(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleConsole handles GET /api/v1/console
func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req := ops.ConsoleRequest{}
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			req.Limit = n
		}
	}
	res, err := ops.Console(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleNetwork handles GET /api/v1/network
func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req := ops.NetworkRequestArgs{}
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			req.Limit = n
		}
	}
	res, err := ops.Network(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleErrors handles GET /api/v1/errors
func (s *Server) handleErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	res, err := ops.Errors(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleText handles GET /api/v1/text
func (s *Server) handleText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	req := ops.TextRequest{Selector: q.Get("selector"), Mode: q.Get("mode")}
	if req.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}
	res, err := ops.Text(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleFontCheck handles GET /api/v1/font-check
func (s *Server) handleFontCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req := ops.FontCheckRequest{Family: r.URL.Query().Get("family")}
	if req.Family == "" {
		writeError(w, http.StatusBadRequest, "family is required")
		return
	}
	res, err := ops.FontCheck(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleDownloadImages handles POST /api/v1/download-images
func (s *Server) handleDownloadImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ops.DownloadImagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}

	dir := req.OutputDir
	if dir == "" {
		dir = os.TempDir()
	} else {
		dir = filepath.Clean(dir)
		if err := os.MkdirAll(dir, 0755); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create output dir: %v", err))
			return
		}
	}

	res, err := ops.DownloadImages(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var files []map[string]interface{}
	var skipped []map[string]interface{}
	for _, img := range res.Images {
		if img.Error != "" {
			skipped = append(skipped, map[string]interface{}{
				"index":  img.Index + 1,
				"error":  img.Error,
				"method": img.Method,
				"source": img.Source,
			})
			continue
		}

		ext := ".png"
		if img.Method == "download" && img.Source != "" {
			lower := strings.ToLower(img.Source)
			switch {
			case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
				ext = ".jpg"
			case strings.HasSuffix(lower, ".svg"):
				ext = ".svg"
			case strings.HasSuffix(lower, ".webp"):
				ext = ".webp"
			case strings.HasSuffix(lower, ".gif"):
				ext = ".gif"
			}
		}

		filename := fmt.Sprintf("%d%s", img.Index+1, ext)
		filePath := filepath.Join(dir, filename)
		if err := os.WriteFile(filePath, img.Data, 0644); err != nil {
			skipped = append(skipped, map[string]interface{}{
				"index": img.Index + 1,
				"error": fmt.Sprintf("failed to save: %v", err),
			})
			continue
		}

		entry := map[string]interface{}{
			"index":  img.Index + 1,
			"path":   filePath,
			"size":   len(img.Data),
			"method": img.Method,
		}
		if img.Source != "" {
			entry["source"] = img.Source
		}
		files = append(files, entry)
	}

	resp := map[string]interface{}{
		"captured": len(files),
		"total":    len(res.Images),
		"files":    files,
	}
	if len(skipped) > 0 {
		resp["skipped"] = skipped
	}
	writeSuccess(w, resp)
}

// handleComputedStylesDiff handles GET /api/v1/computed-styles-diff
func (s *Server) handleComputedStylesDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()

	againstStr := q.Get("against")
	if againstStr == "" {
		writeError(w, http.StatusBadRequest, "against (page index) is required")
		return
	}
	againstIdx, err := strconv.Atoi(againstStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "against must be a page index number")
		return
	}

	req := ops.ComputedStylesDiffRequest{
		Against:        againstIdx,
		SelectorTarget: q.Get("selector_target"),
	}
	if p := q.Get("properties"); p != "" {
		req.Properties = strings.Split(p, ",")
	}
	if sels := q.Get("selectors"); sels != "" {
		split := strings.Split(sels, ",")
		for i := range split {
			split[i] = strings.TrimSpace(split[i])
		}
		req.Selectors = split
	} else {
		req.Selector = q.Get("selector")
		if req.Selector == "" {
			writeError(w, http.StatusBadRequest, "selector or selectors is required")
			return
		}
	}

	res, err := ops.ComputedStylesDiff(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if res.Mode == "batch" {
		writeSuccess(w, map[string]interface{}{
			"results":       res.Results,
			"overall_score": res.OverallScore,
		})
		return
	}
	writeSuccess(w, map[string]interface{}{
		"selector":      res.Selector,
		"matches":       res.Matches,
		"mismatches":    res.Mismatches,
		"matchCount":    res.MatchCount,
		"mismatchCount": res.MismatchCount,
		"score":         res.Score,
	})
}

// handleScroll handles POST /api/v1/scroll
func (s *Server) handleScroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ops.ScrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}
	res, err := ops.Scroll(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Preserve historical "scrolled" key for compatibility.
	writeSuccess(w, map[string]interface{}{
		"scrolled":     res.Selector,
		"scrollTop":    res.ScrollTop,
		"scrollLeft":   res.ScrollLeft,
		"scrollHeight": res.ScrollHeight,
		"scrollWidth":  res.ScrollWidth,
		"clientHeight": res.ClientHeight,
		"clientWidth":  res.ClientWidth,
	})
}

// handleComputedStyles handles GET /api/v1/computed-styles
func (s *Server) handleComputedStyles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	req := ops.ComputedStylesRequest{
		Selector:    q.Get("selector"),
		SelectorAll: q.Get("selector_all"),
	}
	if p := q.Get("properties"); p != "" {
		req.Properties = strings.Split(p, ",")
	}
	if sels := q.Get("selectors"); sels != "" {
		split := strings.Split(sels, ",")
		for i := range split {
			split[i] = strings.TrimSpace(split[i])
		}
		req.Selectors = split
	}
	if req.Selector == "" && req.SelectorAll == "" && len(req.Selectors) == 0 {
		writeError(w, http.StatusBadRequest, "selector, selector_all, or selectors is required")
		return
	}
	res, err := ops.ComputedStyles(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	switch res.Mode {
	case "batch":
		writeSuccess(w, map[string]interface{}{"results": res.BatchResults})
	case "all":
		writeSuccess(w, map[string]interface{}{
			"selector": res.Selector,
			"count":    res.Count,
			"elements": res.Elements,
		})
	default: // "single"
		writeSuccess(w, map[string]interface{}{
			"selector": res.Selector,
			"styles":   res.Styles,
			"count":    res.Count,
		})
	}
}

// handleWait handles POST /api/v1/wait
func (s *Server) handleWait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// The request body uses "timeout" historically; map it onto TimeoutMs.
	var raw struct {
		Selector string `json:"selector"`
		Timeout  int    `json:"timeout"`
		Visible  bool   `json:"visible"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if raw.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}
	res, err := ops.Wait(r.Context(), s.browser, ops.WaitRequest{
		Selector:  raw.Selector,
		TimeoutMs: raw.Timeout,
		Visible:   raw.Visible,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleCleanSnapshot handles GET /api/v1/clean-snapshot
func (s *Server) handleCleanSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	req := ops.CleanSnapshotRequest{
		Selector: q.Get("selector"),
		SVGFull:  q.Get("svg_full") == "true",
		JSON:     q.Get("format") == "json",
	}
	if req.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}
	if d := q.Get("depth"); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			req.Depth = n
		}
	}
	if m := q.Get("max_length"); m != "" {
		if n, err := strconv.Atoi(m); err == nil {
			req.MaxLength = n
		}
	}
	res, err := ops.CleanSnapshot(r.Context(), s.browser, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.JSON {
		writeSuccess(w, map[string]interface{}{
			"selector": res.Selector,
			"tree":     res.Tree,
		})
		return
	}
	writeSuccess(w, map[string]interface{}{
		"selector": res.Selector,
		"html":     res.HTML,
	})
}

// handleViewport handles GET/POST /api/v1/viewport
func (s *Server) handleViewport(w http.ResponseWriter, r *http.Request) {
	// GET: query current viewport
	if r.Method == http.MethodGet {
		res, err := ops.GetViewport(r.Context(), s.browser)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, map[string]interface{}{
			"width":             res.Width,
			"height":            res.Height,
			"deviceScaleFactor": res.DPR,
		})
		return
	}

	// POST: set viewport
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ops.ViewportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	res, err := ops.SetViewport(r.Context(), s.browser, req)
	if err != nil {
		// Surface preset/dimensions errors as 400 to preserve historical behavior.
		msg := err.Error()
		if strings.Contains(msg, "unknown preset") || strings.Contains(msg, "width and height are required") {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		writeError(w, http.StatusInternalServerError, msg)
		return
	}
	writeSuccess(w, map[string]interface{}{
		"previous": res.Previous,
		"current":  res.Current,
	})
}

// handleAsk handles POST /api/v1/ask
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ops.AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Question == "" {
		writeError(w, http.StatusBadRequest, "question is required")
		return
	}

	if s.appConfig == nil {
		writeError(w, http.StatusInternalServerError, "LLM not configured: app config not provided to server")
		return
	}
	if err := s.appConfig.ValidateForLLM(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("LLM configuration error: %v", err))
		return
	}

	llmCfg := s.appConfig.GetLLMConfig()
	llmClient, err := llm.NewClient(r.Context(), llmCfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create LLM client: %v", err))
		return
	}
	defer llmClient.Close()

	askAgent := agent.NewAskAgent(agent.AskConfig{
		LLMClient: llmClient,
		Browser:   s.browser,
		Verbose:   true,
	})

	runner := func(ctx context.Context, q string) (string, error) {
		return askAgent.Ask(ctx, q)
	}

	res, err := ops.Ask(r.Context(), runner, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleRecordStart handles POST /api/v1/record/start
func (s *Server) handleRecordStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ops.RecordStartRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	res, err := ops.RecordStart(r.Context(), s.browser, req)
	if err != nil {
		if strings.Contains(err.Error(), "already in progress") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleRecordStop handles POST /api/v1/record/stop
func (s *Server) handleRecordStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	res, err := ops.RecordStop(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleRecordStatus handles GET /api/v1/record/status
func (s *Server) handleRecordStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	res, err := ops.RecordStatus(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}
