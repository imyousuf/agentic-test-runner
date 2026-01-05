// Package capture handles browser state capture for failure analysis.
package capture

import (
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
)

// FailureContext contains all captured browser state at the time of failure.
type FailureContext struct {
	// Timestamp when the failure occurred.
	Timestamp time.Time `json:"timestamp"`

	// StepDescription describes what step was being executed.
	StepDescription string `json:"step_description,omitempty"`

	// ErrorMessage is the error that caused the failure.
	ErrorMessage string `json:"error_message,omitempty"`

	// URL is the current page URL at time of failure.
	URL string `json:"url"`

	// Title is the current page title at time of failure.
	Title string `json:"title"`

	// Screenshot is the viewport screenshot (PNG bytes).
	Screenshot []byte `json:"screenshot,omitempty"`

	// FullPageScreenshot is the full scrollable page screenshot (PNG bytes).
	FullPageScreenshot []byte `json:"full_page_screenshot,omitempty"`

	// ConsoleLogs contains recent console messages.
	ConsoleLogs []ConsoleEntry `json:"console_logs,omitempty"`

	// NetworkRequests contains recent network requests.
	NetworkRequests []NetworkEntry `json:"network_requests,omitempty"`

	// FailedRequests contains requests that failed or had error status.
	FailedRequests []NetworkEntry `json:"failed_requests,omitempty"`

	// DOMSnapshot is an HTML snapshot of the page.
	DOMSnapshot string `json:"dom_snapshot,omitempty"`

	// AccessibilityTree contains the page's accessibility tree.
	AccessibilityTree []browser.ElementInfo `json:"accessibility_tree,omitempty"`

	// SimilarElements contains elements similar to what was being searched for.
	SimilarElements []browser.ElementInfo `json:"similar_elements,omitempty"`
}

// ConsoleEntry represents a browser console log entry.
type ConsoleEntry struct {
	// Level is the log level (log, info, warn, error).
	Level string `json:"level"`

	// Text is the log message.
	Text string `json:"text"`

	// URL is the source URL of the log.
	URL string `json:"url,omitempty"`

	// Line is the source line number.
	Line int `json:"line,omitempty"`

	// Timestamp when the log was captured.
	Timestamp time.Time `json:"timestamp"`
}

// NetworkEntry represents a captured network request.
type NetworkEntry struct {
	// ID is the unique request identifier.
	ID string `json:"id"`

	// URL is the request URL.
	URL string `json:"url"`

	// Method is the HTTP method.
	Method string `json:"method"`

	// Status is the HTTP status code.
	Status int `json:"status"`

	// StatusText is the HTTP status text.
	StatusText string `json:"status_text"`

	// ResourceType is the type of resource (document, script, xhr, etc.).
	ResourceType string `json:"resource_type"`

	// StartTime when the request was initiated.
	StartTime time.Time `json:"start_time"`

	// Duration of the request.
	Duration time.Duration `json:"duration,omitempty"`

	// Failed indicates if the request failed.
	Failed bool `json:"failed"`

	// ErrorText contains error details if the request failed.
	ErrorText string `json:"error_text,omitempty"`

	// Headers contains request headers.
	Headers map[string]string `json:"headers,omitempty"`
}

// CaptureOptions configures what to capture.
type CaptureOptions struct {
	// Screenshots enables screenshot capture.
	Screenshots bool

	// FullPageScreenshot captures entire scrollable page.
	FullPageScreenshot bool

	// ConsoleLogs enables console log capture.
	ConsoleLogs bool

	// NetworkRequests enables network request capture.
	NetworkRequests bool

	// DOMSnapshot enables DOM snapshot capture.
	DOMSnapshot bool

	// AccessibilityTree enables accessibility tree capture.
	AccessibilityTree bool

	// MaxConsoleEntries limits console entries captured.
	MaxConsoleEntries int

	// MaxNetworkRequests limits network requests captured.
	MaxNetworkRequests int
}

// DefaultCaptureOptions returns sensible defaults for capture options.
func DefaultCaptureOptions() CaptureOptions {
	return CaptureOptions{
		Screenshots:        true,
		FullPageScreenshot: false,
		ConsoleLogs:        true,
		NetworkRequests:    true,
		DOMSnapshot:        true,
		AccessibilityTree:  true,
		MaxConsoleEntries:  100,
		MaxNetworkRequests: 50,
	}
}
