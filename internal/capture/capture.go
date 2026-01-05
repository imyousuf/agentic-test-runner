package capture

import (
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// Capturer handles browser state capture for failure analysis.
type Capturer struct {
	browser *browser.Browser
	options CaptureOptions
}

// New creates a new Capturer with the given browser and options.
func New(b *browser.Browser, opts CaptureOptions) *Capturer {
	return &Capturer{
		browser: b,
		options: opts,
	}
}

// NewFromConfig creates a new Capturer from configuration.
func NewFromConfig(b *browser.Browser, cfg config.CaptureConfig) *Capturer {
	return New(b, CaptureOptions{
		Screenshots:        cfg.Screenshots,
		FullPageScreenshot: cfg.FullPageScreenshot,
		ConsoleLogs:        cfg.ConsoleLogs,
		NetworkRequests:    cfg.NetworkHAR,
		DOMSnapshot:        cfg.DOMSnapshot,
		AccessibilityTree:  true,
		MaxConsoleEntries:  cfg.MaxConsoleEntries,
		MaxNetworkRequests: cfg.MaxNetworkRequests,
	})
}

// CaptureFailure captures browser state at the time of failure.
func (c *Capturer) CaptureFailure(stepDesc, errorMsg string) *FailureContext {
	ctx := &FailureContext{
		Timestamp:       time.Now(),
		StepDescription: stepDesc,
		ErrorMessage:    errorMsg,
		URL:             c.browser.CurrentURL(),
		Title:           c.browser.PageTitle(),
	}

	// Capture screenshot
	if c.options.Screenshots {
		if screenshot, err := c.browser.Screenshot(false); err == nil {
			ctx.Screenshot = screenshot
		}
	}

	// Capture full page screenshot
	if c.options.FullPageScreenshot {
		if screenshot, err := c.browser.Screenshot(true); err == nil {
			ctx.FullPageScreenshot = screenshot
		}
	}

	// Capture console logs
	if c.options.ConsoleLogs {
		consoleMsgs := c.browser.GetConsoleMessages(c.options.MaxConsoleEntries)
		ctx.ConsoleLogs = convertConsoleMessages(consoleMsgs)
	}

	// Capture network requests
	if c.options.NetworkRequests {
		networkReqs := c.browser.GetNetworkRequests(c.options.MaxNetworkRequests)
		ctx.NetworkRequests = convertNetworkRequests(networkReqs)

		// Capture failed requests separately
		failedReqs := c.browser.GetFailedRequests()
		ctx.FailedRequests = convertNetworkRequests(failedReqs)
	}

	// Capture DOM snapshot
	if c.options.DOMSnapshot {
		if html, err := c.browser.HTML(); err == nil {
			ctx.DOMSnapshot = html
		}
	}

	// Capture accessibility tree
	if c.options.AccessibilityTree {
		if elements, err := c.browser.Snapshot(true); err == nil {
			ctx.AccessibilityTree = elements
		}
	}

	return ctx
}

// CaptureSuccess captures minimal browser state for successful steps.
func (c *Capturer) CaptureSuccess() *FailureContext {
	return &FailureContext{
		Timestamp: time.Now(),
		URL:       c.browser.CurrentURL(),
		Title:     c.browser.PageTitle(),
	}
}

// CaptureScreenshot captures just a screenshot.
func (c *Capturer) CaptureScreenshot(fullPage bool) ([]byte, error) {
	return c.browser.Screenshot(fullPage)
}

// convertConsoleMessages converts browser console messages to capture entries.
func convertConsoleMessages(msgs []browser.ConsoleMessage) []ConsoleEntry {
	entries := make([]ConsoleEntry, len(msgs))
	for i, msg := range msgs {
		entries[i] = ConsoleEntry{
			Level:     msg.Level,
			Text:      msg.Text,
			URL:       msg.URL,
			Line:      msg.Line,
			Timestamp: msg.Timestamp,
		}
	}
	return entries
}

// convertNetworkRequests converts browser network requests to capture entries.
func convertNetworkRequests(reqs []browser.NetworkRequest) []NetworkEntry {
	entries := make([]NetworkEntry, len(reqs))
	for i, req := range reqs {
		entries[i] = NetworkEntry{
			ID:           req.ID,
			URL:          req.URL,
			Method:       req.Method,
			Status:       req.Status,
			StatusText:   req.StatusText,
			ResourceType: req.ResourceType,
			StartTime:    req.StartTime,
			Duration:     req.Duration,
			Failed:       req.Failed,
			ErrorText:    req.ErrorText,
			Headers:      req.Headers,
		}
	}
	return entries
}

// HasErrors checks if there are any console errors.
func (c *FailureContext) HasErrors() bool {
	for _, entry := range c.ConsoleLogs {
		if entry.Level == "error" {
			return true
		}
	}
	return false
}

// HasFailedRequests checks if there are any failed network requests.
func (c *FailureContext) HasFailedRequests() bool {
	return len(c.FailedRequests) > 0
}

// GetErrors returns only error-level console entries.
func (c *FailureContext) GetErrors() []ConsoleEntry {
	var errors []ConsoleEntry
	for _, entry := range c.ConsoleLogs {
		if entry.Level == "error" {
			errors = append(errors, entry)
		}
	}
	return errors
}

// GetWarnings returns only warning-level console entries.
func (c *FailureContext) GetWarnings() []ConsoleEntry {
	var warnings []ConsoleEntry
	for _, entry := range c.ConsoleLogs {
		if entry.Level == "warning" {
			warnings = append(warnings, entry)
		}
	}
	return warnings
}

// Summary returns a brief summary of the failure context.
func (c *FailureContext) Summary() string {
	summary := "Failure Context:\n"
	summary += "  URL: " + c.URL + "\n"
	summary += "  Title: " + c.Title + "\n"

	if c.StepDescription != "" {
		summary += "  Step: " + c.StepDescription + "\n"
	}

	if c.ErrorMessage != "" {
		summary += "  Error: " + c.ErrorMessage + "\n"
	}

	errorCount := 0
	warningCount := 0
	for _, entry := range c.ConsoleLogs {
		if entry.Level == "error" {
			errorCount++
		} else if entry.Level == "warning" {
			warningCount++
		}
	}

	summary += "  Console: " + string(rune('0'+errorCount)) + " errors, " + string(rune('0'+warningCount)) + " warnings\n"
	summary += "  Failed Requests: " + string(rune('0'+len(c.FailedRequests))) + "\n"

	return summary
}
