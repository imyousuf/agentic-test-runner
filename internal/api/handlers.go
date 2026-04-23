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

	"github.com/imyousuf/agentic-test-runner/internal/browser"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
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
	selector := r.URL.Query().Get("selector")
	selectorAll := r.URL.Query().Get("selector_all")
	outputDir := r.URL.Query().Get("output_dir")
	format := r.URL.Query().Get("format") // "file" or "base64", default base64

	// Multiple element screenshots
	if selectorAll != "" {
		timeoutMs := 30000
		if t := r.URL.Query().Get("timeout"); t != "" {
			if n, err := strconv.Atoi(t); err == nil && n > 0 {
				timeoutMs = n
			}
		}

		results, err := s.browser.GetMultipleElementScreenshots(selectorAll, time.Duration(timeoutMs)*time.Millisecond)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("screenshot failed: %v", err))
			return
		}

		dir := outputDir
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
		for _, sr := range results {
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
			"total":    len(results),
			"files":    files,
		}
		if len(skipped) > 0 {
			resp["skipped"] = skipped
		}

		writeSuccess(w, resp)
		return
	}

	var data []byte
	var err error

	if selector != "" && fullPage {
		data, err = s.browser.GetElementFullHeightScreenshot(selector)
	} else if selector != "" {
		data, err = s.browser.GetElementScreenshotByCSS(selector)
	} else {
		data, err = s.browser.Screenshot(fullPage)
	}
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

// handleText handles GET /api/v1/text
func (s *Server) handleText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	selector := r.URL.Query().Get("selector")
	if selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}

	mode := r.URL.Query().Get("mode")

	result, err := s.browser.GetTextContent(selector, mode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("text extraction failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"selector": result.Selector,
		"mode":     result.Mode,
		"groups":   result.Groups,
		"count":    len(result.Groups),
	})
}

// handleFontCheck handles GET /api/v1/font-check
func (s *Server) handleFontCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	family := r.URL.Query().Get("family")
	if family == "" {
		writeError(w, http.StatusBadRequest, "family is required")
		return
	}

	result, err := s.browser.CheckFont(family)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("font check failed: %v", err))
		return
	}

	resp := map[string]interface{}{
		"family":   result.Family,
		"declared": result.Declared,
		"loaded":   result.Loaded,
		"status":   result.Status,
	}
	if result.Reason != "" {
		resp["reason"] = result.Reason
	}
	if result.Fallback != "" {
		resp["fallback"] = result.Fallback
	}

	writeSuccess(w, resp)
}

// handleDownloadImages handles POST /api/v1/download-images
func (s *Server) handleDownloadImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Selector           string `json:"selector"`
		OutputDir          string `json:"output_dir"`
		FallbackScreenshot bool   `json:"fallback_screenshot"`
	}
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

	images, err := s.browser.DownloadImages(req.Selector, req.FallbackScreenshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("download images failed: %v", err))
		return
	}

	var files []map[string]interface{}
	var skipped []map[string]interface{}
	for _, img := range images {
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
			// Detect extension from source URL
			if strings.HasSuffix(strings.ToLower(img.Source), ".jpg") || strings.HasSuffix(strings.ToLower(img.Source), ".jpeg") {
				ext = ".jpg"
			} else if strings.HasSuffix(strings.ToLower(img.Source), ".svg") {
				ext = ".svg"
			} else if strings.HasSuffix(strings.ToLower(img.Source), ".webp") {
				ext = ".webp"
			} else if strings.HasSuffix(strings.ToLower(img.Source), ".gif") {
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
		"total":    len(images),
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

	againstStr := r.URL.Query().Get("against")
	if againstStr == "" {
		writeError(w, http.StatusBadRequest, "against (page index) is required")
		return
	}
	againstIdx, err := strconv.Atoi(againstStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "against must be a page index number")
		return
	}

	var properties []string
	if p := r.URL.Query().Get("properties"); p != "" {
		properties = strings.Split(p, ",")
	}

	selectorTarget := r.URL.Query().Get("selector_target")

	// Batch selectors mode
	selectors := r.URL.Query().Get("selectors")
	if selectors != "" {
		selectorList := strings.Split(selectors, ",")
		for i := range selectorList {
			selectorList[i] = strings.TrimSpace(selectorList[i])
		}
		result, err := s.browser.GetBatchComputedStylesDiff(selectorList, againstIdx, properties, selectorTarget)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("batch diff failed: %v", err))
			return
		}

		writeSuccess(w, map[string]interface{}{
			"results":       result.Results,
			"overall_score": result.OverallScore,
		})
		return
	}

	// Single selector mode
	selector := r.URL.Query().Get("selector")
	if selector == "" {
		writeError(w, http.StatusBadRequest, "selector or selectors is required")
		return
	}

	result, err := s.browser.GetComputedStylesDiff(selector, againstIdx, properties, selectorTarget)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("style diff failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"selector":      result.Selector,
		"matches":       result.Matches,
		"mismatches":    result.Mismatches,
		"matchCount":    result.MatchCount,
		"mismatchCount": result.MismatchCount,
		"score":         result.Score,
	})
}

// handleScroll handles POST /api/v1/scroll
func (s *Server) handleScroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Selector string `json:"selector"`
		X        int    `json:"x"`
		Y        int    `json:"y"`
		ToBottom bool   `json:"to_bottom"`
		ToTop    bool   `json:"to_top"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}

	result, err := s.browser.ScrollElement(req.Selector, req.X, req.Y, req.ToBottom, req.ToTop)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("scroll failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"scrolled":     req.Selector,
		"scrollTop":    result.ScrollTop,
		"scrollLeft":   result.ScrollLeft,
		"scrollHeight": result.ScrollHeight,
		"scrollWidth":  result.ScrollWidth,
		"clientHeight": result.ClientHeight,
		"clientWidth":  result.ClientWidth,
	})
}

// handleComputedStyles handles GET /api/v1/computed-styles
func (s *Server) handleComputedStyles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var properties []string
	if p := r.URL.Query().Get("properties"); p != "" {
		properties = strings.Split(p, ",")
	}

	// Batch selectors mode (repeated selectors, comma-separated)
	selectors := r.URL.Query().Get("selectors")
	selector := r.URL.Query().Get("selector")
	selectorAll := r.URL.Query().Get("selector_all")

	if selector == "" && selectorAll == "" && selectors == "" {
		writeError(w, http.StatusBadRequest, "selector, selector_all, or selectors is required")
		return
	}
	if selectors != "" {
		selectorList := strings.Split(selectors, ",")
		for i := range selectorList {
			selectorList[i] = strings.TrimSpace(selectorList[i])
		}
		results, err := s.browser.GetBatchComputedStyles(selectorList, properties)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("batch styles failed: %v", err))
			return
		}

		writeSuccess(w, map[string]interface{}{
			"results": results,
		})
		return
	}

	// Multiple elements mode
	if selectorAll != "" {
		entries, err := s.browser.GetMultipleComputedStyles(selectorAll, properties)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get computed styles: %v", err))
			return
		}

		writeSuccess(w, map[string]interface{}{
			"selector": selectorAll,
			"count":    len(entries),
			"elements": entries,
		})
		return
	}

	// Single element mode
	styles, err := s.browser.GetComputedStyles(selector, properties)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get computed styles: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"selector": selector,
		"styles":   styles,
		"count":    len(styles),
	})
}

// handleWait handles POST /api/v1/wait
func (s *Server) handleWait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Selector string `json:"selector"`
		Timeout  int    `json:"timeout"` // milliseconds, default 5000
		Visible  bool   `json:"visible"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}

	timeout := 5 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	}

	ctx := context.Background()
	var err error
	if req.Visible {
		err = s.browser.WaitForElementVisible(ctx, req.Selector, timeout)
	} else {
		err = s.browser.WaitForElement(ctx, req.Selector, timeout)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("wait failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"found":    true,
		"selector": req.Selector,
		"visible":  req.Visible,
	})
}

// handleCleanSnapshot handles GET /api/v1/clean-snapshot
func (s *Server) handleCleanSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	selector := r.URL.Query().Get("selector")
	if selector == "" {
		writeError(w, http.StatusBadRequest, "selector is required")
		return
	}

	depth := 0
	if d := r.URL.Query().Get("depth"); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			depth = n
		}
	}

	maxLength := 0
	if m := r.URL.Query().Get("max_length"); m != "" {
		if n, err := strconv.Atoi(m); err == nil {
			maxLength = n
		}
	}

	svgFull := r.URL.Query().Get("svg_full") == "true"
	jsonOutput := r.URL.Query().Get("format") == "json"

	html, tree, err := s.browser.GetCleanSnapshot(selector, browser.CleanSnapshotOptions{
		Depth:     depth,
		SVGFull:   svgFull,
		MaxLength: maxLength,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("clean snapshot failed: %v", err))
		return
	}

	if jsonOutput {
		writeSuccess(w, map[string]interface{}{
			"selector": selector,
			"tree":     tree,
		})
	} else {
		writeSuccess(w, map[string]interface{}{
			"selector": selector,
			"html":     html,
		})
	}
}

// handleViewport handles GET/POST /api/v1/viewport
func (s *Server) handleViewport(w http.ResponseWriter, r *http.Request) {
	// GET: query current viewport
	if r.Method == http.MethodGet {
		vp, err := s.browser.GetViewport()
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get viewport: %v", err))
			return
		}
		writeSuccess(w, map[string]interface{}{
			"width":             vp.Width,
			"height":            vp.Height,
			"deviceScaleFactor": vp.DeviceScaleFactor,
		})
		return
	}

	// POST: set viewport
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Width  int     `json:"width"`
		Height int     `json:"height"`
		DPR    float64 `json:"dpr"`
		Preset string  `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Apply preset if specified
	if req.Preset != "" {
		switch req.Preset {
		case "mobile":
			req.Width, req.Height = 375, 812
		case "tablet":
			req.Width, req.Height = 768, 1024
		case "desktop":
			req.Width, req.Height = 1440, 900
		case "wide":
			req.Width, req.Height = 1920, 1080
		default:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown preset: %s (use mobile, tablet, desktop, or wide)", req.Preset))
			return
		}
	}

	if req.Width == 0 || req.Height == 0 {
		writeError(w, http.StatusBadRequest, "width and height are required")
		return
	}

	prev, current, err := s.browser.SetViewport(req.Width, req.Height, req.DPR)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("viewport resize failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"previous": prev,
		"current":  current,
	})
}

// handleAsk handles POST /api/v1/ask
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Question string `json:"question"`
	}
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

	answer, err := askAgent.Ask(r.Context(), req.Question)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("ask failed: %v", err))
		return
	}

	writeSuccess(w, map[string]interface{}{
		"answer": answer,
	})
}

// handleRecordStart handles POST /api/v1/record/start
func (s *Server) handleRecordStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		URL string `json:"url"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	if err := s.browser.StartRecording(req.URL); err != nil {
		if strings.Contains(err.Error(), "already in progress") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"recording": true,
	})
}

// handleRecordStop handles POST /api/v1/record/stop
func (s *Server) handleRecordStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	events, err := s.browser.StopRecording()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	testContent := browser.FormatTestFile(events, "Recorded Session")

	writeSuccess(w, map[string]interface{}{
		"event_count":  len(events),
		"test_content": testContent,
		"events":       events,
	})
}

// handleRecordStatus handles GET /api/v1/record/status
func (s *Server) handleRecordStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeSuccess(w, map[string]interface{}{
		"recording":   s.browser.IsRecording(),
		"event_count": s.browser.RecordingEventCount(),
	})
}
