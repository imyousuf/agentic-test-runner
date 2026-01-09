// Package browser provides browser lifecycle and control for behavior testing.
package browser

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// Browser manages browser lifecycle and page interactions.
type Browser struct {
	browser *rod.Browser
	pages   []*rod.Page
	current int // index of current page
	config  config.BrowserConfig
	mu      sync.RWMutex

	// Event tracking
	consoleMessages []ConsoleMessage
	networkRequests []NetworkRequest
	consoleMu       sync.Mutex
	networkMu       sync.Mutex

	// Target tracking for detecting manually opened tabs
	targetIDs map[proto.TargetTargetID]*rod.Page
}

// ConsoleMessage represents a browser console message.
type ConsoleMessage struct {
	Level     string    `json:"level"`
	Text      string    `json:"text"`
	URL       string    `json:"url,omitempty"`
	Line      int       `json:"line,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// NetworkRequest represents a captured network request.
type NetworkRequest struct {
	ID          string            `json:"id"`
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	Status      int               `json:"status"`
	StatusText  string            `json:"status_text"`
	ResourceType string           `json:"resource_type"`
	StartTime   time.Time         `json:"start_time"`
	Duration    time.Duration     `json:"duration,omitempty"`
	Failed      bool              `json:"failed"`
	ErrorText   string            `json:"error_text,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// New creates a new browser instance with the given configuration.
func New(cfg config.BrowserConfig) (*Browser, error) {
	return &Browser{
		config:          cfg,
		pages:           make([]*rod.Page, 0),
		current:         -1,
		consoleMessages: make([]ConsoleMessage, 0),
		networkRequests: make([]NetworkRequest, 0),
		targetIDs:       make(map[proto.TargetTargetID]*rod.Page),
	}, nil
}

// Launch starts a new browser instance.
func (b *Browser) Launch(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var l *launcher.Launcher

	if b.config.Executable == "auto" || b.config.Executable == "" {
		// Check if a specific version is requested
		if b.config.Version != "" {
			browserPath, err := b.resolveBrowserVersion()
			if err != nil {
				return fmt.Errorf("failed to resolve browser version: %w", err)
			}
			l = launcher.New().Bin(browserPath)
		} else {
			// Use rod's auto-download feature (default bundled version)
			l = launcher.New()
		}
	} else {
		// Use specified browser path
		l = launcher.New().Bin(b.config.Executable)
	}

	// Set cache directory if specified
	if b.config.CacheDir != "" {
		l = l.UserDataDir(b.config.CacheDir)
	}

	// Set headless mode
	l = l.Headless(b.config.Headless)

	// Ignore HTTPS certificate errors if configured (useful for local dev with self-signed certs)
	if b.config.IgnoreHTTPSErrors {
		l = l.Set("ignore-certificate-errors")
	}

	// Launch and get control URL
	controlURL, err := l.Launch()
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}

	// Connect to browser
	browser := rod.New().ControlURL(controlURL)
	if b.config.SlowMotion > 0 {
		browser = browser.SlowMotion(b.config.SlowMotion)
	}

	if err := browser.Connect(); err != nil {
		return fmt.Errorf("failed to connect to browser: %w", err)
	}

	b.browser = browser

	// Start listening for new tabs opened manually
	b.startTargetListener()

	return nil
}

// resolveBrowserVersion resolves and downloads the requested browser version.
// Returns the path to the browser executable.
func (b *Browser) resolveBrowserVersion() (string, error) {
	atrDir, err := GetATRDir()
	if err != nil {
		return "", fmt.Errorf("failed to get ATR directory: %w", err)
	}

	// Resolve version from config
	versionInfo, err := ResolveVersion(b.config.Version, atrDir)
	if err != nil {
		return "", err
	}
	if versionInfo == nil {
		return "", fmt.Errorf("no version info resolved")
	}

	// Check if browser is already downloaded
	browserDir := filepath.Join(atrDir, "browsers", versionInfo.Version)
	browserPath := getBrowserExecutable(browserDir, versionInfo.Platform)

	if _, err := os.Stat(browserPath); err == nil {
		// Browser already exists
		fmt.Printf("Using Chrome %s (%s)\n", versionInfo.Version, versionInfo.Channel)
		return browserPath, nil
	}

	// Download and extract browser
	fmt.Printf("Downloading Chrome %s (%s)...\n", versionInfo.Version, versionInfo.Channel)
	if err := downloadAndExtractBrowser(versionInfo.URL, browserDir); err != nil {
		return "", fmt.Errorf("failed to download browser: %w", err)
	}

	// Verify browser executable exists
	if _, err := os.Stat(browserPath); err != nil {
		return "", fmt.Errorf("browser executable not found after extraction: %s", browserPath)
	}

	fmt.Printf("Chrome %s ready\n", versionInfo.Version)
	return browserPath, nil
}

// getBrowserExecutable returns the path to the browser executable for a platform.
func getBrowserExecutable(browserDir, platform string) string {
	switch {
	case strings.HasPrefix(platform, "mac"):
		// macOS: chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing
		return filepath.Join(browserDir, "chrome-"+platform, "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing")
	case strings.HasPrefix(platform, "linux"):
		// Linux: chrome-linux64/chrome
		return filepath.Join(browserDir, "chrome-linux64", "chrome")
	case strings.HasPrefix(platform, "win"):
		// Windows: chrome-win64/chrome.exe
		return filepath.Join(browserDir, "chrome-"+platform, "chrome.exe")
	default:
		return ""
	}
}

// downloadAndExtractBrowser downloads and extracts the browser from URL.
func downloadAndExtractBrowser(url, destDir string) error {
	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "chrome-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Download
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}
	tmpFile.Close()

	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Extract zip
	if err := extractZip(tmpFile.Name(), destDir); err != nil {
		return fmt.Errorf("failed to extract: %w", err)
	}

	// Make executable on Unix
	if runtime.GOOS != "windows" {
		// Find and chmod the chrome executable
		filepath.Walk(destDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			name := filepath.Base(path)
			if name == "chrome" || name == "Google Chrome for Testing" {
				os.Chmod(path, 0755)
			}
			return nil
		})
	}

	return nil
}

// extractZip extracts a zip file to destination directory.
func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(destDir, f.Name)

		// Security check: prevent zip slip
		if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

// Connect connects to an existing browser instance via CDP endpoint.
func (b *Browser) Connect(ctx context.Context, cdpEndpoint string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	browser := rod.New().ControlURL(cdpEndpoint)
	if b.config.SlowMotion > 0 {
		browser = browser.SlowMotion(b.config.SlowMotion)
	}

	if err := browser.Connect(); err != nil {
		return fmt.Errorf("failed to connect to browser at %s: %w", cdpEndpoint, err)
	}

	// Ignore HTTPS certificate errors if configured (useful for local dev with self-signed certs)
	if b.config.IgnoreHTTPSErrors {
		browser.MustIgnoreCertErrors(true)
	}

	b.browser = browser

	// Sync existing pages from the connected browser
	b.syncExistingPages()

	// Start listening for new tabs opened manually
	b.startTargetListener()

	return nil
}

// Close closes the browser and all pages.
func (b *Browser) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// The target listener goroutine will naturally stop when browser is closed
	if b.browser != nil {
		return b.browser.Close()
	}
	return nil
}

// NewPage creates a new page/tab and navigates to the URL.
func (b *Browser) NewPage(ctx context.Context, url string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.browser == nil {
		return fmt.Errorf("browser not launched")
	}

	page, err := b.browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return fmt.Errorf("failed to create page: %w", err)
	}

	// Set viewport
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:  b.config.Viewport.Width,
		Height: b.config.Viewport.Height,
	}); err != nil {
		return fmt.Errorf("failed to set viewport: %w", err)
	}

	// Set up event listeners
	b.setupEventListeners(page)

	// Register in target ID map for tracking
	info := page.MustInfo()
	b.targetIDs[proto.TargetTargetID(info.TargetID)] = page

	b.pages = append(b.pages, page)
	b.current = len(b.pages) - 1

	// Wait for page load
	if err := page.WaitLoad(); err != nil {
		return fmt.Errorf("failed to wait for page load: %w", err)
	}

	return nil
}

// setupEventListeners sets up console and network event listeners.
func (b *Browser) setupEventListeners(page *rod.Page) {
	// Listen for console messages
	go page.EachEvent(func(e *proto.RuntimeConsoleAPICalled) {
		b.consoleMu.Lock()
		defer b.consoleMu.Unlock()

		level := "log"
		switch e.Type {
		case proto.RuntimeConsoleAPICalledTypeWarning:
			level = "warning"
		case proto.RuntimeConsoleAPICalledTypeError:
			level = "error"
		case proto.RuntimeConsoleAPICalledTypeInfo:
			level = "info"
		case proto.RuntimeConsoleAPICalledTypeDebug:
			level = "debug"
		}

		text := ""
		for _, arg := range e.Args {
			text += fmt.Sprintf("%v ", arg.Value.Val())
		}

		b.consoleMessages = append(b.consoleMessages, ConsoleMessage{
			Level:     level,
			Text:      text,
			Timestamp: time.Now(),
		})
	})()

	// Listen for network requests
	go page.EachEvent(func(e *proto.NetworkRequestWillBeSent) {
		b.networkMu.Lock()
		defer b.networkMu.Unlock()

		b.networkRequests = append(b.networkRequests, NetworkRequest{
			ID:           string(e.RequestID),
			URL:          e.Request.URL,
			Method:       e.Request.Method,
			ResourceType: string(e.Type),
			StartTime:    time.Now(),
		})
	})()

	// Listen for network responses
	go page.EachEvent(func(e *proto.NetworkResponseReceived) {
		b.networkMu.Lock()
		defer b.networkMu.Unlock()

		for i := range b.networkRequests {
			if b.networkRequests[i].ID == string(e.RequestID) {
				b.networkRequests[i].Status = e.Response.Status
				b.networkRequests[i].StatusText = e.Response.StatusText
				b.networkRequests[i].Duration = time.Since(b.networkRequests[i].StartTime)
				break
			}
		}
	})()

	// Listen for network failures
	go page.EachEvent(func(e *proto.NetworkLoadingFailed) {
		b.networkMu.Lock()
		defer b.networkMu.Unlock()

		for i := range b.networkRequests {
			if b.networkRequests[i].ID == string(e.RequestID) {
				b.networkRequests[i].Failed = true
				b.networkRequests[i].ErrorText = e.ErrorText
				break
			}
		}
	})()
}

// CurrentPage returns the currently selected page.
func (b *Browser) CurrentPage() (*rod.Page, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.current < 0 || b.current >= len(b.pages) {
		return nil, fmt.Errorf("no page selected")
	}
	return b.pages[b.current], nil
}

// ListPages returns information about all open pages.
func (b *Browser) ListPages() []PageInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	infos := make([]PageInfo, len(b.pages))
	for i, page := range b.pages {
		info := page.MustInfo()
		infos[i] = PageInfo{
			Index:   i,
			URL:     info.URL,
			Title:   info.Title,
			Current: i == b.current,
		}
	}
	return infos
}

// PageInfo contains information about a page.
type PageInfo struct {
	Index   int    `json:"index"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Current bool   `json:"current"`
}

// SelectPage switches to the page at the given index.
func (b *Browser) SelectPage(index int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if index < 0 || index >= len(b.pages) {
		return fmt.Errorf("invalid page index: %d (have %d pages)", index, len(b.pages))
	}

	b.current = index
	_, err := b.pages[index].Activate()
	return err
}

// ClosePage closes the page at the given index.
func (b *Browser) ClosePage(index int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if index < 0 || index >= len(b.pages) {
		return fmt.Errorf("invalid page index: %d", index)
	}

	if len(b.pages) == 1 {
		return fmt.Errorf("cannot close the last page")
	}

	if err := b.pages[index].Close(); err != nil {
		return fmt.Errorf("failed to close page: %w", err)
	}

	// Remove from slice
	b.pages = append(b.pages[:index], b.pages[index+1:]...)

	// Adjust current index
	if b.current >= len(b.pages) {
		b.current = len(b.pages) - 1
	}

	return nil
}

// Navigate navigates the current page to the given URL.
func (b *Browser) Navigate(ctx context.Context, url string) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	if err := page.Navigate(url); err != nil {
		return fmt.Errorf("navigation failed: %w", err)
	}

	return page.WaitLoad()
}

// GoBack navigates back in history.
func (b *Browser) GoBack() error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}
	return page.NavigateBack()
}

// GoForward navigates forward in history.
func (b *Browser) GoForward() error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}
	return page.NavigateForward()
}

// Reload reloads the current page.
func (b *Browser) Reload() error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}
	return page.Reload()
}

// CurrentURL returns the current page URL.
func (b *Browser) CurrentURL() string {
	page, err := b.CurrentPage()
	if err != nil {
		return ""
	}
	info := page.MustInfo()
	return info.URL
}

// PageTitle returns the current page title.
func (b *Browser) PageTitle() string {
	page, err := b.CurrentPage()
	if err != nil {
		return ""
	}
	info := page.MustInfo()
	return info.Title
}

// Screenshot captures a screenshot of the current page.
func (b *Browser) Screenshot(fullPage bool) ([]byte, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	if fullPage {
		return page.Screenshot(true, nil)
	}
	return page.Screenshot(false, nil)
}

// HTML returns the current page HTML.
func (b *Browser) HTML() (string, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return "", err
	}
	return page.HTML()
}

// GetConsoleMessages returns captured console messages.
func (b *Browser) GetConsoleMessages(limit int) []ConsoleMessage {
	b.consoleMu.Lock()
	defer b.consoleMu.Unlock()

	if limit <= 0 || limit > len(b.consoleMessages) {
		limit = len(b.consoleMessages)
	}

	// Return most recent messages
	start := len(b.consoleMessages) - limit
	if start < 0 {
		start = 0
	}

	result := make([]ConsoleMessage, limit)
	copy(result, b.consoleMessages[start:])
	return result
}

// GetNetworkRequests returns captured network requests.
func (b *Browser) GetNetworkRequests(limit int) []NetworkRequest {
	b.networkMu.Lock()
	defer b.networkMu.Unlock()

	if limit <= 0 || limit > len(b.networkRequests) {
		limit = len(b.networkRequests)
	}

	// Return most recent requests
	start := len(b.networkRequests) - limit
	if start < 0 {
		start = 0
	}

	result := make([]NetworkRequest, limit)
	copy(result, b.networkRequests[start:])
	return result
}

// GetFailedRequests returns network requests that failed.
func (b *Browser) GetFailedRequests() []NetworkRequest {
	b.networkMu.Lock()
	defer b.networkMu.Unlock()

	var failed []NetworkRequest
	for _, req := range b.networkRequests {
		if req.Failed || req.Status >= 400 {
			failed = append(failed, req)
		}
	}
	return failed
}

// ClearEvents clears captured console messages and network requests.
func (b *Browser) ClearEvents() {
	b.consoleMu.Lock()
	b.consoleMessages = make([]ConsoleMessage, 0)
	b.consoleMu.Unlock()

	b.networkMu.Lock()
	b.networkRequests = make([]NetworkRequest, 0)
	b.networkMu.Unlock()
}

// SetViewport sets the viewport dimensions.
func (b *Browser) SetViewport(width, height int) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	return page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:  width,
		Height: height,
	})
}

// Evaluate executes JavaScript in the current page.
func (b *Browser) Evaluate(script string) (interface{}, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	result, err := page.Eval(script)
	if err != nil {
		return nil, fmt.Errorf("script evaluation failed: %w", err)
	}

	return result.Value.Val(), nil
}

// WaitForText waits for text to appear on the page.
func (b *Browser) WaitForText(text string, timeout time.Duration) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	page = page.Timeout(timeout)
	el := page.MustElementR("*", text)
	if el == nil {
		return fmt.Errorf("text not found: %s", text)
	}
	return nil
}

// startTargetListener starts listening for target events including:
// - New tabs opened manually
// - Tabs closed
// - Tab switches (when user clicks on a different tab)
func (b *Browser) startTargetListener() {
	if b.browser == nil {
		return
	}

	// Register existing pages in target map
	for _, page := range b.pages {
		info := page.MustInfo()
		b.targetIDs[proto.TargetTargetID(info.TargetID)] = page
	}

	// Listen for target events - EachEvent returns a wait function that must be called
	wait := b.browser.EachEvent(
		func(e *proto.TargetTargetCreated) {
			// Filter: only track actual pages, not iframes or service workers
			if e.TargetInfo.Type != proto.TargetTargetInfoTypePage {
				return
			}

			b.mu.Lock()
			defer b.mu.Unlock()

			// Check if we already have this page
			if _, exists := b.targetIDs[e.TargetInfo.TargetID]; exists {
				return
			}

			// Get page from target ID
			page, err := b.browser.PageFromTarget(e.TargetInfo.TargetID)
			if err != nil {
				// Could be a transient target, skip silently
				return
			}

			// Set up event listeners for the new page
			b.setupEventListeners(page)

			// Set viewport if configured
			if b.config.Viewport.Width > 0 && b.config.Viewport.Height > 0 {
				_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
					Width:  b.config.Viewport.Width,
					Height: b.config.Viewport.Height,
				})
			}

			// Add to tracked pages
			b.pages = append(b.pages, page)
			b.targetIDs[e.TargetInfo.TargetID] = page

			// If this is the first page, set it as current
			if b.current < 0 {
				b.current = len(b.pages) - 1
			}
		},
		func(e *proto.TargetTargetDestroyed) {
			b.mu.Lock()
			defer b.mu.Unlock()

			// Find and remove the destroyed page
			if page, exists := b.targetIDs[e.TargetID]; exists {
				delete(b.targetIDs, e.TargetID)

				// Find and remove from pages slice
				for i, p := range b.pages {
					if p == page {
						b.pages = append(b.pages[:i], b.pages[i+1:]...)

						// Adjust current index
						if b.current >= len(b.pages) {
							b.current = len(b.pages) - 1
						}
						break
					}
				}
			}
		},
		func(e *proto.TargetTargetInfoChanged) {
			// Track tab switches - when a page becomes the active/focused tab
			if e.TargetInfo.Type != proto.TargetTargetInfoTypePage {
				return
			}

			b.mu.Lock()
			defer b.mu.Unlock()

			// Find the page in our tracked pages
			if page, exists := b.targetIDs[e.TargetInfo.TargetID]; exists {
				// Update current to this page if it's now attached (focused)
				if e.TargetInfo.Attached {
					for i, p := range b.pages {
						if p == page {
							b.current = i
							break
						}
					}
				}
			}
		},
	)

	// Start the event loop in a background goroutine
	go wait()
}

// syncExistingPages discovers and tracks all existing pages in a connected browser.
func (b *Browser) syncExistingPages() {
	if b.browser == nil {
		return
	}

	pages, err := b.browser.Pages()
	if err != nil {
		return
	}

	for _, page := range pages {
		info := page.MustInfo()

		// Skip non-page targets (though Pages() should only return pages)
		if info.Type != "page" {
			continue
		}

		// Skip if already tracked
		targetID := proto.TargetTargetID(info.TargetID)
		if _, exists := b.targetIDs[targetID]; exists {
			continue
		}

		// Set up event listeners
		b.setupEventListeners(page)

		// Set viewport
		if b.config.Viewport.Width > 0 && b.config.Viewport.Height > 0 {
			_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
				Width:  b.config.Viewport.Width,
				Height: b.config.Viewport.Height,
			})
		}

		b.pages = append(b.pages, page)
		b.targetIDs[targetID] = page
	}

	if len(b.pages) > 0 && b.current < 0 {
		b.current = 0
	}
}
