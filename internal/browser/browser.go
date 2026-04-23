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
	browser    *rod.Browser
	pages      []*rod.Page
	current    int // index of current page
	config     config.BrowserConfig
	controlURL string // CDP WebSocket URL for connecting to this browser
	connected  bool   // true when connected to an external browser (don't close it)
	mu         sync.RWMutex

	// Track pages created by this instance (for cleanup when connected)
	ownedPages map[*rod.Page]bool

	// pageSwitchMu protects compound operations that switch pages and switch back.
	// Separate from mu to avoid deadlock since SelectPage/GetComputedStyles acquire mu.
	pageSwitchMu sync.Mutex

	// Event tracking
	consoleMessages []ConsoleMessage
	networkRequests []NetworkRequest
	consoleMu       sync.Mutex
	networkMu       sync.Mutex

	// Target tracking for detecting manually opened tabs
	targetIDs map[proto.TargetTargetID]*rod.Page

	// Spoofed user agent derived from the actual browser version
	spoofedUA       string
	spoofedPlatform string

	// Recording session (nil when not recording)
	recording *RecordingSession
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
	ID           string            `json:"id"`
	URL          string            `json:"url"`
	Method       string            `json:"method"`
	Status       int               `json:"status"`
	StatusText   string            `json:"status_text"`
	ResourceType string            `json:"resource_type"`
	StartTime    time.Time         `json:"start_time"`
	Duration     time.Duration     `json:"duration,omitempty"`
	Failed       bool              `json:"failed"`
	ErrorText    string            `json:"error_text,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}

// New creates a new browser instance with the given configuration.
func New(cfg config.BrowserConfig) (*Browser, error) {
	return &Browser{
		config:          cfg,
		pages:           make([]*rod.Page, 0),
		current:         -1,
		ownedPages:      make(map[*rod.Page]bool),
		consoleMessages: make([]ConsoleMessage, 0),
		networkRequests: make([]NetworkRequest, 0),
		targetIDs:       make(map[proto.TargetTargetID]*rod.Page),
	}, nil
}

// CLIOptions configures browser for CLI usage with sensible defaults.
type CLIOptions struct {
	Headless    bool   // default: false (visible browser)
	Sandbox     bool   // default: false (disabled for Ubuntu 23.10+ compatibility)
	CDPEndpoint string // if set, connect to existing browser instead of launching
}

// NewForCLI creates a browser configured for CLI usage.
// It applies CLI-friendly defaults: visible browser, no sandbox.
func NewForCLI(baseCfg config.BrowserConfig, opts CLIOptions) (*Browser, error) {
	// Apply CLI defaults
	baseCfg.Headless = opts.Headless
	baseCfg.NoSandbox = !opts.Sandbox
	return New(baseCfg)
}

// LaunchOrConnect either connects to an existing browser via CDP endpoint,
// or launches a new browser instance.
func (b *Browser) LaunchOrConnect(ctx context.Context, cdpEndpoint string) error {
	if cdpEndpoint != "" {
		return b.Connect(ctx, cdpEndpoint)
	}
	return b.Launch(ctx)
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

	// Determine user data directory
	userDataDir := b.config.DataDir
	if userDataDir == "" {
		userDataDir = b.config.CacheDir // Backward compatibility
	}
	if userDataDir == "" && b.config.PersistSession {
		// Use default location when persist is enabled but no dir specified
		atrDir, err := GetATRDir()
		if err != nil {
			return fmt.Errorf("failed to get ATR directory: %w", err)
		}
		userDataDir = filepath.Join(atrDir, "browser-data")
	}

	// Set user data directory if specified
	// Note: When UserDataDir is set, rod does NOT delete it on close
	// (it only cleans up temp directories when UserDataDir is not set)
	if userDataDir != "" {
		var err error
		userDataDir, err = expandPath(userDataDir)
		if err != nil {
			return fmt.Errorf("failed to expand data directory path: %w", err)
		}
		// Create directory with restrictive permissions (user-only) for security
		// Browser data contains sensitive cookies and session data
		if err := os.MkdirAll(userDataDir, 0700); err != nil {
			return fmt.Errorf("failed to create browser data directory %s: %w", userDataDir, err)
		}
		l = l.UserDataDir(userDataDir)
	}

	// Set headless mode
	l = l.Headless(b.config.Headless)

	// Disable sandbox if configured (needed on Ubuntu 23.10+ with AppArmor)
	if b.config.NoSandbox {
		l = l.Set("no-sandbox")
	}

	// Ignore HTTPS certificate errors if configured (useful for local dev with self-signed certs)
	if b.config.IgnoreHTTPSErrors {
		l = l.Set("ignore-certificate-errors")
	}

	// In headless mode, set explicit window size since there's no physical window
	if b.config.Headless && b.config.Viewport.Width > 0 && b.config.Viewport.Height > 0 {
		l = l.Set("window-size", fmt.Sprintf("%d,%d", b.config.Viewport.Width, b.config.Viewport.Height))
	}

	// Launch and get control URL
	controlURL, err := l.Launch()
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}

	// Store the control URL for external access (e.g., MCP servers)
	b.controlURL = controlURL

	// Connect to browser
	browser := rod.New().ControlURL(controlURL)
	if b.config.SlowMotion > 0 {
		browser = browser.SlowMotion(b.config.SlowMotion)
	}

	if err := browser.Connect(); err != nil {
		return fmt.Errorf("failed to connect to browser: %w", err)
	}

	b.browser = browser

	// Build a realistic user agent from the actual browser version
	b.initUserAgent()

	// In headed mode, maximize the window via CDP so it fills the screen.
	// The page will naturally reflow if the user resizes the window.
	if !b.config.Headless {
		b.maximizeWindow()
	}

	// Start listening for new tabs opened manually
	b.startTargetListener()

	return nil
}

// maximizeWindow maximizes the browser window via CDP.
// This is used in headed mode so the page fills the screen naturally.
func (b *Browser) maximizeWindow() {
	pages, err := b.browser.Pages()
	if err != nil || len(pages) == 0 {
		return
	}

	// Get the window ID from the first page
	result, err := proto.BrowserGetWindowForTarget{}.Call(pages[0])
	if err != nil {
		return
	}

	// Maximize the window
	_ = proto.BrowserSetWindowBounds{
		WindowID: result.WindowID,
		Bounds:   &proto.BrowserBounds{WindowState: proto.BrowserWindowStateMaximized},
	}.Call(b.browser)
}

// clearViewportOverride clears any EmulationSetDeviceMetricsOverride on a page,
// restoring the natural window-based viewport.
func (b *Browser) clearViewportOverride(page *rod.Page) {
	_ = proto.EmulationClearDeviceMetricsOverride{}.Call(page)
}

// initUserAgent queries the browser for its real version and builds a realistic
// user agent string that matches a normal Chrome installation on the current OS.
func (b *Browser) initUserAgent() {
	ver, err := proto.BrowserGetVersion{}.Call(b.browser)
	if err != nil {
		return
	}

	// Extract Chrome version from Product (e.g., "Chrome/114.0.5735.199" or "HeadlessChrome/...")
	chromeVersion := ""
	product := ver.Product
	if idx := strings.Index(product, "/"); idx >= 0 {
		chromeVersion = product[idx+1:]
	}
	if chromeVersion == "" {
		return
	}

	// Build a platform-appropriate user agent
	var platformToken string
	switch runtime.GOOS {
	case "darwin":
		platformToken = "Macintosh; Intel Mac OS X 10_15_7"
		b.spoofedPlatform = "macOS"
	case "windows":
		platformToken = "Windows NT 10.0; Win64; x64"
		b.spoofedPlatform = "Windows"
	default:
		platformToken = "X11; Linux x86_64"
		b.spoofedPlatform = "Linux"
	}

	b.spoofedUA = fmt.Sprintf(
		"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
		platformToken, chromeVersion,
	)
}

// applyUserAgent sets the spoofed user agent on a page via CDP.
func (b *Browser) applyUserAgent(page *rod.Page) {
	if b.spoofedUA == "" {
		return
	}
	_ = proto.NetworkSetUserAgentOverride{
		UserAgent: b.spoofedUA,
		Platform:  b.spoofedPlatform,
	}.Call(page)
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
		return browserPath, nil
	}

	// Download and extract browser (only print during first-time download)
	fmt.Fprintf(os.Stderr, "Downloading Chrome %s...\n", versionInfo.Version)
	if err := downloadAndExtractBrowser(versionInfo.URL, browserDir); err != nil {
		return "", fmt.Errorf("failed to download browser: %w", err)
	}

	// Verify browser executable exists
	if _, err := os.Stat(browserPath); err != nil {
		return "", fmt.Errorf("browser executable not found after extraction: %s", browserPath)
	}

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
				fmt.Fprintf(os.Stderr, "Warning: failed to walk %s: %v\n", path, err)
				return nil
			}
			name := filepath.Base(path)
			if name == "chrome" || name == "Google Chrome for Testing" {
				if err := os.Chmod(path, 0755); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to chmod %s: %v\n", path, err)
				}
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

	// Store the control URL for external access
	b.controlURL = cdpEndpoint

	// Ignore HTTPS certificate errors if configured (useful for local dev with self-signed certs)
	if b.config.IgnoreHTTPSErrors {
		browser.MustIgnoreCertErrors(true)
	}

	b.browser = browser
	b.connected = true

	// Build a realistic user agent from the actual browser version
	b.initUserAgent()

	// Sync existing pages from the connected browser
	b.syncExistingPages()

	// Start listening for new tabs opened manually
	b.startTargetListener()

	return nil
}

// CDPEndpoint returns the CDP WebSocket URL for this browser.
// This can be used by external tools (like MCP servers) to connect to this browser.
func (b *Browser) CDPEndpoint() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.controlURL
}

// Close closes the browser and all pages.
// When connected to an external browser (e.g., a running server), only pages
// created by this instance are closed; the browser process is left running.
func (b *Browser) Close() error {
	// Stop recording if active (before acquiring lock since StopRecording acquires it)
	if b.recording != nil && b.recording.active {
		b.StopRecording()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.browser == nil {
		return nil
	}

	if b.connected {
		// Only close pages we created, leave the browser running
		for page := range b.ownedPages {
			page.Close()
		}
		b.ownedPages = make(map[*rod.Page]bool)
		return nil
	}

	// The target listener goroutine will naturally stop when browser is closed
	return b.browser.Close()
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

	if b.config.Headless {
		// In headless mode, override the viewport since there's no physical window
		if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
			Width:  b.config.Viewport.Width,
			Height: b.config.Viewport.Height,
		}); err != nil {
			return fmt.Errorf("failed to set viewport: %w", err)
		}
	} else {
		// In headed mode, clear any viewport override so the page uses the real window size
		b.clearViewportOverride(page)
	}

	// Apply spoofed user agent
	b.applyUserAgent(page)

	// Set up event listeners
	b.setupEventListeners(page)

	// Register in target ID map for tracking
	info := page.MustInfo()
	b.targetIDs[proto.TargetTargetID(info.TargetID)] = page

	b.pages = append(b.pages, page)
	b.current = len(b.pages) - 1
	b.ownedPages[page] = true

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

// HasPage returns true if the browser has at least one page/tab.
func (b *Browser) HasPage() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.pages) > 0
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
// ViewportSize represents the browser viewport dimensions.
type ViewportSize struct {
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	DeviceScaleFactor float64 `json:"deviceScaleFactor"`
}

// GetViewport returns the current viewport dimensions.
func (b *Browser) GetViewport() (*ViewportSize, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	result, err := page.Eval(`() => ({
		width: window.innerWidth,
		height: window.innerHeight,
		deviceScaleFactor: window.devicePixelRatio
	})`)
	if err != nil {
		return nil, fmt.Errorf("failed to get viewport: %w", err)
	}

	raw := result.Value.Map()
	return &ViewportSize{
		Width:             int(raw["width"].Num()),
		Height:            int(raw["height"].Num()),
		DeviceScaleFactor: raw["deviceScaleFactor"].Num(),
	}, nil
}

// SetViewport resizes the browser viewport and returns previous and new sizes.
func (b *Browser) SetViewport(width, height int, dpr ...float64) (*ViewportSize, *ViewportSize, error) {
	if width < 320 || width > 3840 {
		return nil, nil, fmt.Errorf("width must be between 320 and 3840, got %d", width)
	}
	if height < 480 || height > 2160 {
		return nil, nil, fmt.Errorf("height must be between 480 and 2160, got %d", height)
	}

	scaleFactor := 1.0
	if len(dpr) > 0 && dpr[0] > 0 {
		scaleFactor = dpr[0]
	}

	page, err := b.CurrentPage()
	if err != nil {
		return nil, nil, err
	}

	prev, _ := b.GetViewport()
	if prev == nil {
		prev = &ViewportSize{}
	}

	err = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             width,
		Height:            height,
		DeviceScaleFactor: scaleFactor,
		Mobile:            width < 768,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to set viewport: %w", err)
	}

	current := &ViewportSize{
		Width:             width,
		Height:            height,
		DeviceScaleFactor: scaleFactor,
	}

	return prev, current, nil
}

// Evaluate executes JavaScript in the current page.
// Scripts are auto-wrapped so bare expressions (e.g. "document.title") work
// like a browser dev console without needing an explicit function wrapper.
func (b *Browser) Evaluate(script string) (interface{}, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	js := wrapJSExpression(script)
	result, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("script evaluation failed: %w", err)
	}

	return result.Value.Val(), nil
}

// wrapJSExpression wraps a script so that bare expressions return their value,
// mimicking browser dev-console behaviour. If the script is already a function
// expression or contains a return statement it is left as-is.
func wrapJSExpression(script string) string {
	s := strings.TrimSpace(script)

	// Already a function expression — pass through
	if strings.HasPrefix(s, "function") || strings.HasPrefix(s, "()") ||
		strings.HasPrefix(s, "(function") || strings.HasPrefix(s, "async") {
		return script
	}

	// Contains flow-control / multiple statements — wrap with implicit return
	// of the last expression is not reliable, so leave as-is if it has return.
	if strings.Contains(s, "return ") || strings.Contains(s, "return\n") {
		return script
	}

	// Bare expression: wrap as arrow function so rod evaluates and returns it
	return "() => (" + script + ")"
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

			if b.config.Headless {
				// In headless mode, override viewport since there's no physical window
				if b.config.Viewport.Width > 0 && b.config.Viewport.Height > 0 {
					_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
						Width:  b.config.Viewport.Width,
						Height: b.config.Viewport.Height,
					})
				}
			} else {
				// In headed mode, clear any stale viewport override so the page uses the real window size
				b.clearViewportOverride(page)
			}

			// Apply spoofed user agent
			b.applyUserAgent(page)

			// Inject recorder into new tabs if recording is active
			if b.recording != nil && b.recording.active {
				b.injectRecorder(page)
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

		if b.config.Headless {
			// In headless mode, override viewport since there's no physical window
			if b.config.Viewport.Width > 0 && b.config.Viewport.Height > 0 {
				_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
					Width:  b.config.Viewport.Width,
					Height: b.config.Viewport.Height,
				})
			}
		} else {
			// In headed mode, clear any stale viewport override so the page uses the real window size
			b.clearViewportOverride(page)
		}

		// Apply spoofed user agent
		b.applyUserAgent(page)

		// Inject recorder into synced pages if recording is active
		if b.recording != nil && b.recording.active {
			b.injectRecorder(page)
		}

		b.pages = append(b.pages, page)
		b.targetIDs[targetID] = page
	}

	if len(b.pages) > 0 && b.current < 0 {
		b.current = 0
	}
}
