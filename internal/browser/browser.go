// Package browser provides browser lifecycle and control for behavior testing.
package browser

import (
	"context"
	"fmt"
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
	}, nil
}

// Launch starts a new browser instance.
func (b *Browser) Launch(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var l *launcher.Launcher

	if b.config.Executable == "auto" || b.config.Executable == "" {
		// Use rod's auto-download feature
		l = launcher.New()
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

	b.browser = browser
	return nil
}

// Close closes the browser and all pages.
func (b *Browser) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

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
