// Package browser provides browser lifecycle and control for behavior testing.
package browser

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/process"
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

	// In-page agent HUD (nil when not enabled)
	hud *hudSession

	// targetEvents serialises target bookkeeping off rod's event-dispatch
	// goroutine. See queueTargetEvent.
	targetEvents chan func()
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

		// A persisted profile keeps Chrome's single-instance lock. Stopping
		// the daemon kills Chrome outright, so that lock routinely survives
		// pointing at a process that no longer exists — and the next Chrome
		// sees it, tries to hand the profile to an instance that is gone, and
		// exits before rod can talk to it. The daemon then comes up with no
		// browser behind it and the first command fails with a bare EOF.
		clearStaleProfileLock(userDataDir)
		markProfileCleanExit(userDataDir)
		clearSessionRestore(userDataDir)
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
		// Not the Must form: a browser that refuses this is still usable for
		// everything that is not a self-signed certificate, and panicking
		// here would take down a daemon that was about to work fine.
		if err := browser.IgnoreCertErrors(true); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not disable certificate checking: %v\n", err)
		}
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

// defaultPageLoadTimeout bounds waiting for a page to finish loading when the
// config does not say.
const defaultPageLoadTimeout = 45 * time.Second

// waitLoad waits for a page to finish loading, bounded.
//
// rod's WaitLoad has no deadline of its own, and Chrome does occasionally
// never answer the Runtime.evaluate it issues — a wedged renderer, or a
// target created while the browser is under load. Unbounded, that turns a
// stalled page into a hung process: the caller waits forever and there is
// nothing to say why. Bounded, it is an ordinary error that can be reported
// or retried.
func (b *Browser) waitLoad(page *rod.Page) error {
	timeout := b.config.PageTimeout
	if timeout <= 0 {
		timeout = defaultPageLoadTimeout
	}
	if err := page.Timeout(timeout).WaitLoad(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("page did not finish loading within %s", timeout)
		}
		return err
	}
	return nil
}

// NewPage creates a new page/tab and navigates to the URL.
func (b *Browser) NewPage(ctx context.Context, url string) error {
	// The CDP setup below runs without b.mu held, deliberately.
	//
	// setupEventListeners enables the Runtime and Network domains, and those
	// calls block until Chrome answers. Chrome can be slow to answer, or not
	// answer at all when the renderer is wedged. Holding the browser lock
	// across that would freeze every other caller — and the target-event
	// worker with them — on a single unlucky page. The lock exists to guard
	// the page bookkeeping, so it is taken only for that.
	b.mu.RLock()
	rodBrowser := b.browser
	headless := b.config.Headless
	viewport := b.config.Viewport
	hudOn := b.hud != nil
	b.mu.RUnlock()

	if rodBrowser == nil {
		return fmt.Errorf("browser not launched")
	}

	page, err := rodBrowser.Page(proto.TargetCreateTarget{URL: normalizeURL(url)})
	if err != nil {
		return fmt.Errorf("failed to create page: %w", err)
	}

	if headless {
		// In headless mode, override the viewport since there's no physical window
		if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
			Width:  viewport.Width,
			Height: viewport.Height,
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

	// Install the agent HUD into new tabs if it is enabled
	if hudOn {
		b.mu.RLock()
		hud := b.hud
		b.mu.RUnlock()
		_ = b.attachHudSession(hud, page)
	}

	info, err := page.Info()
	if err != nil {
		// The tab exists even though it cannot be identified, and nothing has
		// taken ownership of it: it is not in b.ownedPages, so Close() will
		// not clean it up, and without a target id no accessor can reach it.
		// Left alone it stays open for the life of the browser.
		_ = page.Close()
		return fmt.Errorf("failed to read the new page's target info: %w", err)
	}

	b.mu.Lock()
	// The target listener may have registered this page already; either way
	// the map and the slice must agree.
	if _, exists := b.targetIDs[proto.TargetTargetID(info.TargetID)]; !exists {
		b.pages = append(b.pages, page)
		b.targetIDs[proto.TargetTargetID(info.TargetID)] = page
	}
	for i, p := range b.pages {
		if p == page {
			b.current = i
			break
		}
	}
	b.ownedPages[page] = true
	b.mu.Unlock()

	// Wait for page load
	if err := b.waitLoad(page); err != nil {
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
//
// The Info() probes run without b.mu held, for the reason NewPage documents:
// each one is a CDP round trip that Chrome can be slow to answer, or never
// answer at all when the renderer is wedged, and holding the browser lock
// across them freezes every other caller and the target-event worker with
// them. The lock is taken twice and briefly: once to copy the slice, once to
// commit what the probes found.
func (b *Browser) ListPages() []PageInfo {
	b.mu.RLock()
	pages := make([]*rod.Page, len(b.pages))
	copy(pages, b.pages)
	b.mu.RUnlock()

	// A page can die without us hearing about it: the tab is closed from
	// inside the browser, the renderer crashes, or a persisted profile is
	// reopened and the old targets are gone. Asking such a page for its Info
	// fails, and the Must form of that call panics — which, on the daemon,
	// takes the whole server down and surfaces to the client as an
	// unexplained EOF.
	//
	// A page that fails for any other reason is not proven dead. It keeps its
	// place in the bookkeeping and is merely absent from this listing, so a
	// timeout or a dropped message cannot strand a tab that is still open --
	// unreachable ever after, and no longer closed by Close().
	live := make(map[*rod.Page]*proto.TargetTargetInfo, len(pages))
	var gone []*rod.Page
	for _, page := range pages {
		info, err := page.Info()
		switch {
		case err == nil:
			live[page] = info
		case targetGone(err):
			gone = append(gone, page)
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.forgetPages(gone)

	// Index is the page's real position in b.pages, not its position in this
	// slice, so every index reported here is one SelectPage and ClosePage can
	// still be given. A page that was opened while the probes were running has
	// no info to report and is left for the next call, which is the only way
	// this listing can have a gap in it.
	infos := make([]PageInfo, 0, len(b.pages))
	for i, page := range b.pages {
		info, ok := live[page]
		if !ok {
			continue
		}
		infos = append(infos, PageInfo{
			Index:    i,
			TargetID: string(info.TargetID),
			URL:      info.URL,
			Title:    info.Title,
			Current:  i == b.current,
		})
	}
	return infos
}

// forgetPages drops pages that are known to be gone, exactly as
// handleTargetDestroyed does for the ones the browser tells us about. Callers
// must hold b.mu.
//
// The selected tab moving is the reason this is a step of its own rather than
// something ListPages does in passing: a caller that asked for a listing has
// no reason to expect the daemon's idea of the current tab to change under it.
func (b *Browser) forgetPages(gone []*rod.Page) {
	for _, dead := range gone {
		for i, page := range b.pages {
			if page != dead {
				continue
			}
			b.pages = append(b.pages[:i], b.pages[i+1:]...)
			delete(b.targetIDs, proto.TargetTargetID(dead.TargetID))
			delete(b.ownedPages, dead)
			switch {
			case b.current == i:
				// The selected tab is the one that died. Fall back to another
				// rather than leaving the browser with nothing selected,
				// which would turn every later command into "no page
				// selected" until the user happened to run select-page.
				b.current = 0
			case b.current > i:
				b.current--
			}
			break
		}
	}
	if b.current >= len(b.pages) {
		b.current = len(b.pages) - 1
	}
}

// targetGone reports whether an error from a page means the tab itself is
// gone, rather than a browser that is merely slow or briefly unreachable.
//
// Only the first kind justifies forgetting a page. Chrome answers a request
// for a target it no longer has with a CDP error saying so, and that is the
// signal; a timeout, a dropped websocket or a renderer that has not replied
// yet says nothing about whether the tab is still open.
func targetGone(err error) bool {
	var cdpErr *cdp.Error
	if !errors.As(err, &cdpErr) {
		return false
	}
	for _, phrase := range []string{
		"No target with given id",
		"Session with given id not found",
		"Target closed",
	} {
		if strings.Contains(cdpErr.Message, phrase) {
			return true
		}
	}
	return false
}

// PageInfo contains information about a page.
//
// Index is a position, and positions move: closing a tab renumbers the ones
// after it. TargetID does not, so anything that has to name the same tab twice
// — across a test run, across a REST call — should hold on to that instead.
type PageInfo struct {
	Index    int    `json:"index"`
	TargetID string `json:"target_id"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Current  bool   `json:"current"`
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

// normalizeURL supplies a scheme when the caller left one out.
//
// Chrome rejects "example.com" outright with "Cannot navigate to invalid
// URL", which is a poor answer to something every address bar accepts. A
// scheme is only ever added when there is none: anything already carrying one
// — including about:, file:, data: and chrome: — passes through untouched.
//
// Loopback hosts get http rather than https. A dev server on localhost:3000
// is overwhelmingly plain HTTP, and defaulting it to https produces a TLS
// error that reads as if the server itself were broken.
func normalizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	if strings.Contains(trimmed, "://") {
		return trimmed
	}
	// A scheme-like prefix carrying no "//" — about:blank, data:text/html,...
	// The digits check keeps "localhost:3000" a host:port rather than a scheme.
	if i := strings.Index(trimmed, ":"); i > 0 {
		head := trimmed[:i]
		if !strings.ContainsAny(head, "./") && !startsWithPort(trimmed[i+1:]) {
			return trimmed
		}
	}

	host := trimmed
	if i := strings.IndexAny(host, "/:"); i >= 0 {
		host = host[:i]
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "0.0.0.0", "[::1]":
		return "http://" + trimmed
	}

	// A path is not a host, and giving one a scheme turns a local file into a
	// request to a stranger: "login.html" would have fetched https://login.html,
	// a name anyone may register, and "/tmp/x.html" the nonsense
	// "https:///tmp/x.html". Chrome refusing a bare path says something true;
	// both of those say something false and one of them leaves the machine.
	if looksLikePath(trimmed, host) {
		return trimmed
	}

	return "https://" + trimmed
}

// looksLikePath reports whether the input is a filesystem path rather than a
// host name. host is the first segment, already split off by the caller.
//
// Anchored paths are unambiguous. A first segment with no dot in it cannot be
// a public host, and one ending in .html or .htm is a file — neither is a
// delegated top-level domain, so this is exact rather than a guess. It stops
// there deliberately: .zip, .mov, .app and .dev all are real domains, and a
// longer list of "obviously not a TLD" suffixes would start being wrong.
func looksLikePath(trimmed, host string) bool {
	switch {
	case strings.HasPrefix(trimmed, "/"),
		strings.HasPrefix(trimmed, "./"),
		strings.HasPrefix(trimmed, "../"),
		trimmed == "." || trimmed == "..":
		return true
	}
	if !strings.Contains(host, ".") {
		// A port makes it a host after all: "buildbox:8080" is a machine on
		// the network, while "buildbox" on its own is far more likely to name
		// a file in the tree.
		rest := trimmed[len(host):]
		return !strings.HasPrefix(rest, ":") || !startsWithPort(rest[1:])
	}
	switch strings.ToLower(host[strings.LastIndex(host, ".")+1:]) {
	case "html", "htm":
		return true
	}
	return false
}

// startsWithPort reports whether s begins with a bare port number.
func startsWithPort(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= '0' && r <= '9' {
			continue
		}
		return r == '/' || r == '?' || r == '#'
	}
	return true
}

// Navigate navigates the current page to the given URL.
func (b *Browser) Navigate(ctx context.Context, url string) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	if err := page.Navigate(normalizeURL(url)); err != nil {
		return fmt.Errorf("navigation failed: %w", err)
	}

	return b.waitLoad(page)
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
	info, err := page.Info()
	if err != nil {
		return ""
	}
	return info.URL
}

// PageTitle returns the current page title.
func (b *Browser) PageTitle() string {
	page, err := b.CurrentPage()
	if err != nil {
		return ""
	}
	info, err := page.Info()
	if err != nil {
		return ""
	}
	return info.Title
}

// Screenshot captures a screenshot of the current page.
func (b *Browser) Screenshot(fullPage bool) ([]byte, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	defer b.hideHud(page)()

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

	// ElementR rather than MustElementR: the Must form panics when the text
	// never appears, which is the ordinary outcome of a wait that times out.
	//
	// ElementR matches a regular expression, and every caller of this passes
	// text a person typed. Quoting it keeps ordinary punctuation — "Sign up
	// (free)", "20% off" — from being read as syntax and reported as text
	// that never appeared, or as an invalid-pattern error nobody expected.
	el, err := page.Timeout(timeout).ElementR("*", regexp.QuoteMeta(text))
	if err != nil {
		return fmt.Errorf("waiting for text %q: %w", text, err)
	}
	if el == nil {
		return fmt.Errorf("text not found: %s", text)
	}
	return nil
}

// startTargetListener starts listening for target events including:
// - New tabs opened manually
// - Tabs closed
// - Tab switches (when user clicks on a different tab)
// targetEventQueueSize bounds the backlog of target bookkeeping waiting to be
// applied. Deep enough that the overflow path below is unreachable in
// practice.
const targetEventQueueSize = 256

// queueTargetEvent hands work to the single worker started by
// startTargetListener.
//
// Target callbacks must not run on rod's event-dispatch goroutine. They take
// b.mu, and the created-target path additionally makes blocking CDP calls
// (PageFromTarget, EnableDomain, SetViewport). Anything that blocks there
// stops rod from reading further CDP messages — including the responses the
// blocked call is waiting for. That deadlocks against any code holding b.mu
// across a CDP call, which NewPage does: it holds the lock while
// setupEventListeners enables the Runtime and Network domains.
//
// One worker rather than a goroutine per event, so that created/destroyed
// pairs for the same target are still applied in the order they arrived.
func (b *Browser) queueTargetEvent(fn func()) {
	select {
	case b.targetEvents <- fn:
	default:
		// Backlog full. Running it on its own goroutine can reorder events,
		// which is a far smaller problem than blocking the dispatcher.
		go fn()
	}
}

func (b *Browser) startTargetListener() {
	if b.browser == nil {
		return
	}

	// Never closed: sends can come from rod's dispatcher at any time, and a
	// send on a closed channel would panic. The worker parks on an empty
	// channel and costs nothing.
	b.targetEvents = make(chan func(), targetEventQueueSize)
	go func() {
		for fn := range b.targetEvents {
			fn()
		}
	}()

	// Register existing pages in target map. A page that cannot be identified
	// is already gone; tracking it would only produce a panic later.
	for _, page := range b.pages {
		info, err := page.Info()
		if err != nil {
			continue
		}
		b.targetIDs[proto.TargetTargetID(info.TargetID)] = page
	}

	// Listen for target events - EachEvent returns a wait function that must be called
	wait := b.browser.EachEvent(
		func(e *proto.TargetTargetCreated) {
			// Filter: only track actual pages, not iframes or service workers
			if e.TargetInfo.Type != proto.TargetTargetInfoTypePage {
				return
			}

			info := e.TargetInfo
			b.queueTargetEvent(func() {
				b.handleTargetCreated(info)
			})
		},
		func(e *proto.TargetTargetDestroyed) {
			id := e.TargetID
			b.queueTargetEvent(func() {
				b.handleTargetDestroyed(id)
			})
		},
		func(e *proto.TargetTargetInfoChanged) {
			// Track tab switches - when a page becomes the active/focused tab
			if e.TargetInfo.Type != proto.TargetTargetInfoTypePage {
				return
			}

			info := e.TargetInfo
			b.queueTargetEvent(func() {
				b.handleTargetInfoChanged(info)
			})
		},
	)

	// Start the event loop in a background goroutine
	go wait()
}

// handleTargetCreated registers a page opened outside NewPage — a target the
// browser created itself, or a tab the user opened by hand. Runs on the
// target-event worker, never on rod's dispatcher.
func (b *Browser) handleTargetCreated(info *proto.TargetTargetInfo) {
	// As in NewPage, the CDP setup runs without b.mu held: those calls block
	// on Chrome, and the lock only guards the bookkeeping at the end.
	b.mu.RLock()
	rodBrowser := b.browser
	headless := b.config.Headless
	viewport := b.config.Viewport
	_, known := b.targetIDs[info.TargetID]
	recording := b.recording != nil && b.recording.active
	hud := b.hud
	b.mu.RUnlock()

	if known || rodBrowser == nil {
		return
	}

	// Get page from target ID
	page, err := rodBrowser.PageFromTarget(info.TargetID)
	if err != nil {
		// Could be a transient target, skip silently
		return
	}

	// Set up event listeners for the new page
	b.setupEventListeners(page)

	if headless {
		// In headless mode, override viewport since there's no physical window
		if viewport.Width > 0 && viewport.Height > 0 {
			_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
				Width:  viewport.Width,
				Height: viewport.Height,
			})
		}
	} else {
		// In headed mode, clear any stale viewport override so the page uses the real window size
		b.clearViewportOverride(page)
	}

	// Apply spoofed user agent
	b.applyUserAgent(page)

	// Inject recorder into new tabs if recording is active
	if recording {
		b.injectRecorder(page)
	}

	// Install the agent HUD into new tabs if it is enabled
	if hud != nil {
		_ = b.attachHudSession(hud, page)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Re-check: NewPage may have registered this target while the setup above
	// was talking to Chrome.
	if _, exists := b.targetIDs[info.TargetID]; exists {
		return
	}

	// Add to tracked pages
	b.pages = append(b.pages, page)
	b.targetIDs[info.TargetID] = page

	// If this is the first page, set it as current
	if b.current < 0 {
		b.current = len(b.pages) - 1
	}
}

// handleTargetDestroyed drops a closed page from tracking.
func (b *Browser) handleTargetDestroyed(id proto.TargetTargetID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Find and remove the destroyed page
	if page, exists := b.targetIDs[id]; exists {
		delete(b.targetIDs, id)

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
}

// handleTargetInfoChanged follows tab switches so CurrentPage tracks the tab
// the user is actually looking at.
func (b *Browser) handleTargetInfoChanged(info *proto.TargetTargetInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Find the page in our tracked pages
	if page, exists := b.targetIDs[info.TargetID]; exists {
		// Update current to this page if it's now attached (focused)
		if info.Attached {
			for i, p := range b.pages {
				if p == page {
					b.current = i
					break
				}
			}
		}
	}
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
		info, err := page.Info()
		if err != nil {
			continue
		}

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

		// Install the agent HUD into synced pages if it is enabled
		if b.hudActive() {
			_ = b.attachHud(page)
		}

		b.pages = append(b.pages, page)
		b.targetIDs[targetID] = page
	}

	if len(b.pages) > 0 && b.current < 0 {
		b.current = 0
	}
}

// singletonFiles are the markers Chrome uses to enforce one instance per
// profile. SingletonLock is a symlink whose name encodes the owning host and
// pid, e.g. "myhost-12345".
var singletonFiles = []string{"SingletonLock", "SingletonCookie", "SingletonSocket"}

// clearStaleProfileLock removes Chrome's single-instance markers when the
// process that created them is gone.
//
// Deliberately conservative: if the recorded pid is still alive, or the lock
// was created on another host, or it cannot be read at all, the markers are
// left exactly as they are. A real second instance must keep working, and
// deleting a live lock would corrupt the profile of whoever owns it.
func clearStaleProfileLock(userDataDir string) {
	lock := filepath.Join(userDataDir, "SingletonLock")

	target, err := os.Readlink(lock)
	if err != nil {
		// No lock, or not a symlink: nothing that can be reasoned about.
		return
	}

	// "host-pid" — the pid is everything after the final dash.
	dash := strings.LastIndex(target, "-")
	if dash < 0 {
		return
	}
	host, pidText := target[:dash], target[dash+1:]

	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return
	}
	if name, err := os.Hostname(); err != nil || name != host {
		// Another machine wrote this lock — a profile on shared storage.
		// Its owner may well still be running.
		return
	}
	if process.Alive(pid) {
		return
	}

	for _, name := range singletonFiles {
		_ = os.Remove(filepath.Join(userDataDir, name))
	}
}

// markProfileCleanExit tells Chrome the profile was closed properly.
//
// Stopping the daemon kills Chrome rather than closing it, so a persisted
// profile is left recorded as "Crashed" every single time. On the next launch
// Chrome goes into crash recovery and session restore, and that machinery
// tears down the target rod has just created — the page is made, its CDP
// session dies underneath it, and the first command comes back as a bare EOF.
// A fresh profile works, which is what makes this look like corruption rather
// than a startup mode.
//
// Rewriting the two exit fields is the same thing every browser-automation
// harness does. Nothing else in Preferences is touched, and any problem
// reading or parsing the file means leaving it exactly as it was: a profile
// that cannot be adjusted is still a profile the user may want to open.
func markProfileCleanExit(userDataDir string) {
	prefs := filepath.Join(userDataDir, "Default", "Preferences")

	raw, err := os.ReadFile(prefs)
	if err != nil {
		return
	}

	// UseNumber, because this round-trips the whole file: Chrome stores
	// microsecond timestamps well above 2^53, and decoding those into float64
	// would quietly round them on every start. json.Number carries the
	// original text through untouched.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		return
	}

	profile, ok := doc["profile"].(map[string]any)
	if !ok {
		return
	}
	if profile["exit_type"] == "Normal" && profile["exited_cleanly"] == true {
		return
	}
	profile["exit_type"] = "Normal"
	profile["exited_cleanly"] = true

	updated, err := json.Marshal(doc)
	if err != nil {
		return
	}

	// Write through a temporary file so an interrupted write cannot leave the
	// profile with truncated Preferences, which Chrome treats as a reset.
	tmp := prefs + ".atr-tmp"
	if err := os.WriteFile(tmp, updated, 0600); err != nil {
		return
	}
	if err := os.Rename(tmp, prefs); err != nil {
		_ = os.Remove(tmp)
	}
}

// clearSessionRestore drops Chrome's saved open-tab state from a persisted
// profile before launching.
//
// Chrome writes Sessions/ and Tabs/ on every run and replays them on the
// next. In a normal browser that restores your tabs; under automation it
// races the target rod has just created, and the created page's CDP session
// is torn down underneath it — the caller gets a bare EOF from what looks
// like a perfectly ordinary navigate, and it recurs on every start because
// each successful run writes the state again.
//
// Only the open-tab bookkeeping is removed. Cookies, localStorage, saved
// logins and every other reason to run --persist-session live elsewhere in
// the profile and are left untouched, so the session the user actually cares
// about survives.
func clearSessionRestore(userDataDir string) {
	for _, dir := range []string{"Sessions", "Tabs"} {
		_ = os.RemoveAll(filepath.Join(userDataDir, "Default", dir))
	}
}
