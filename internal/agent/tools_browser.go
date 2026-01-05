package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
)

// BrowserNavigateTool navigates to a URL or performs history navigation.
type BrowserNavigateTool struct {
	browser *browser.Browser
}

func NewBrowserNavigateTool(b *browser.Browser) *BrowserNavigateTool {
	return &BrowserNavigateTool{browser: b}
}

func (t *BrowserNavigateTool) Name() string { return "browser_navigate" }

func (t *BrowserNavigateTool) Description() string {
	return `Navigate the browser to a URL or perform history navigation.
Use type "url" to navigate to a specific URL.
Use type "back", "forward", or "reload" for history navigation.`
}

func (t *BrowserNavigateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to navigate to (required when type is 'url')",
			},
			"type": map[string]any{
				"type":        "string",
				"enum":        []string{"url", "back", "forward", "reload"},
				"description": "Navigation type: 'url' (default), 'back', 'forward', or 'reload'",
			},
		},
		"required": []string{},
	}
}

func (t *BrowserNavigateTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	navType, _ := args["type"].(string)
	if navType == "" {
		navType = "url"
	}

	var err error
	switch navType {
	case "url":
		url, _ := args["url"].(string)
		if url == "" {
			return "Missing required parameter: url", true
		}
		err = t.browser.Navigate(ctx, url)
	case "back":
		err = t.browser.GoBack()
	case "forward":
		err = t.browser.GoForward()
	case "reload":
		err = t.browser.Reload()
	default:
		return fmt.Sprintf("Invalid navigation type: %s", navType), true
	}

	if err != nil {
		return fmt.Sprintf("Navigation failed: %v", err), true
	}

	return fmt.Sprintf("Navigated successfully. Current URL: %s", t.browser.CurrentURL()), false
}

// BrowserNewPageTool creates a new browser tab.
type BrowserNewPageTool struct {
	browser *browser.Browser
}

func NewBrowserNewPageTool(b *browser.Browser) *BrowserNewPageTool {
	return &BrowserNewPageTool{browser: b}
}

func (t *BrowserNewPageTool) Name() string { return "browser_new_page" }

func (t *BrowserNewPageTool) Description() string {
	return "Create a new browser tab and navigate to the specified URL."
}

func (t *BrowserNewPageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to open in the new tab",
			},
		},
		"required": []string{"url"},
	}
}

func (t *BrowserNewPageTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	url, _ := args["url"].(string)
	if url == "" {
		return "Missing required parameter: url", true
	}

	if err := t.browser.NewPage(ctx, url); err != nil {
		return fmt.Sprintf("Failed to create new page: %v", err), true
	}

	pages := t.browser.ListPages()
	return fmt.Sprintf("Created new tab (index %d) at: %s", len(pages)-1, url), false
}

// BrowserListPagesTool lists all open browser tabs.
type BrowserListPagesTool struct {
	browser *browser.Browser
}

func NewBrowserListPagesTool(b *browser.Browser) *BrowserListPagesTool {
	return &BrowserListPagesTool{browser: b}
}

func (t *BrowserListPagesTool) Name() string { return "browser_list_pages" }

func (t *BrowserListPagesTool) Description() string {
	return "List all open browser tabs with their URLs and titles."
}

func (t *BrowserListPagesTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	}
}

func (t *BrowserListPagesTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	pages := t.browser.ListPages()
	if len(pages) == 0 {
		return "No pages open", false
	}

	var sb strings.Builder
	sb.WriteString("Open tabs:\n")
	for _, p := range pages {
		marker := "  "
		if p.Current {
			marker = "* "
		}
		sb.WriteString(fmt.Sprintf("%s[%d] %s - %s\n", marker, p.Index, p.Title, p.URL))
	}
	return sb.String(), false
}

// BrowserSelectPageTool switches to a specific browser tab.
type BrowserSelectPageTool struct {
	browser *browser.Browser
}

func NewBrowserSelectPageTool(b *browser.Browser) *BrowserSelectPageTool {
	return &BrowserSelectPageTool{browser: b}
}

func (t *BrowserSelectPageTool) Name() string { return "browser_select_page" }

func (t *BrowserSelectPageTool) Description() string {
	return "Switch to a specific browser tab by its index."
}

func (t *BrowserSelectPageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"page_index": map[string]any{
				"type":        "integer",
				"description": "The index of the tab to switch to (0-based)",
			},
		},
		"required": []string{"page_index"},
	}
}

func (t *BrowserSelectPageTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	index, ok := args["page_index"].(float64)
	if !ok {
		return "Missing required parameter: page_index", true
	}

	if err := t.browser.SelectPage(int(index)); err != nil {
		return fmt.Sprintf("Failed to select page: %v", err), true
	}

	return fmt.Sprintf("Switched to tab %d: %s", int(index), t.browser.CurrentURL()), false
}

// BrowserClosePageTool closes a browser tab.
type BrowserClosePageTool struct {
	browser *browser.Browser
}

func NewBrowserClosePageTool(b *browser.Browser) *BrowserClosePageTool {
	return &BrowserClosePageTool{browser: b}
}

func (t *BrowserClosePageTool) Name() string { return "browser_close_page" }

func (t *BrowserClosePageTool) Description() string {
	return "Close a browser tab by its index. Cannot close the last remaining tab."
}

func (t *BrowserClosePageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"page_index": map[string]any{
				"type":        "integer",
				"description": "The index of the tab to close (0-based)",
			},
		},
		"required": []string{"page_index"},
	}
}

func (t *BrowserClosePageTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	index, ok := args["page_index"].(float64)
	if !ok {
		return "Missing required parameter: page_index", true
	}

	if err := t.browser.ClosePage(int(index)); err != nil {
		return fmt.Sprintf("Failed to close page: %v", err), true
	}

	return fmt.Sprintf("Closed tab %d", int(index)), false
}

// BrowserWaitForTool waits for text to appear on the page.
type BrowserWaitForTool struct {
	browser *browser.Browser
}

func NewBrowserWaitForTool(b *browser.Browser) *BrowserWaitForTool {
	return &BrowserWaitForTool{browser: b}
}

func (t *BrowserWaitForTool) Name() string { return "browser_wait_for" }

func (t *BrowserWaitForTool) Description() string {
	return "Wait for specific text to appear on the page."
}

func (t *BrowserWaitForTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "The text to wait for",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds (default: 30)",
			},
		},
		"required": []string{"text"},
	}
}

func (t *BrowserWaitForTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	text, _ := args["text"].(string)
	if text == "" {
		return "Missing required parameter: text", true
	}

	timeout := 30 * time.Second
	if timeoutSec, ok := args["timeout"].(float64); ok {
		timeout = time.Duration(timeoutSec) * time.Second
	}

	if err := t.browser.WaitForText(text, timeout); err != nil {
		return fmt.Sprintf("Text not found within timeout: %v", err), true
	}

	return fmt.Sprintf("Found text: %s", text), false
}

// BrowserClickTool clicks on an element.
type BrowserClickTool struct {
	browser *browser.Browser
}

func NewBrowserClickTool(b *browser.Browser) *BrowserClickTool {
	return &BrowserClickTool{browser: b}
}

func (t *BrowserClickTool) Name() string { return "browser_click" }

func (t *BrowserClickTool) Description() string {
	return `Click on an element. The target can be:
- Element UID from browser_snapshot (e.g., "e0", "e1")
- Text content (e.g., "Sign In", "Submit")
- CSS selector (e.g., "#submit-btn", ".login-button")
- aria-label value
- data-testid value`
}

func (t *BrowserClickTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "Element identifier: UID, text, aria-label, data-testid, or CSS selector",
			},
			"double_click": map[string]any{
				"type":        "boolean",
				"description": "Perform double-click instead of single click",
			},
		},
		"required": []string{"target"},
	}
}

func (t *BrowserClickTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	target, _ := args["target"].(string)
	if target == "" {
		return "Missing required parameter: target", true
	}

	doubleClick, _ := args["double_click"].(bool)

	if err := t.browser.Click(ctx, target, doubleClick); err != nil {
		return fmt.Sprintf("Click failed: %v", err), true
	}

	action := "Clicked"
	if doubleClick {
		action = "Double-clicked"
	}
	return fmt.Sprintf("%s: %s", action, target), false
}

// BrowserFillTool types text into an input element.
type BrowserFillTool struct {
	browser *browser.Browser
}

func NewBrowserFillTool(b *browser.Browser) *BrowserFillTool {
	return &BrowserFillTool{browser: b}
}

func (t *BrowserFillTool) Name() string { return "browser_fill" }

func (t *BrowserFillTool) Description() string {
	return `Fill text into an input, textarea, or select element.
For select elements, the value should match the option text.
The target can be element UID, placeholder text, name attribute, label text, or CSS selector.`
}

func (t *BrowserFillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "Element identifier: UID, placeholder, name, label text, or CSS selector",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Text to enter or option to select",
			},
		},
		"required": []string{"target", "value"},
	}
}

func (t *BrowserFillTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	target, _ := args["target"].(string)
	if target == "" {
		return "Missing required parameter: target", true
	}

	value, _ := args["value"].(string)

	if err := t.browser.Fill(ctx, target, value); err != nil {
		return fmt.Sprintf("Fill failed: %v", err), true
	}

	return fmt.Sprintf("Filled '%s' into: %s", value, target), false
}

// BrowserHoverTool hovers over an element.
type BrowserHoverTool struct {
	browser *browser.Browser
}

func NewBrowserHoverTool(b *browser.Browser) *BrowserHoverTool {
	return &BrowserHoverTool{browser: b}
}

func (t *BrowserHoverTool) Name() string { return "browser_hover" }

func (t *BrowserHoverTool) Description() string {
	return "Hover over an element to trigger hover states or tooltips."
}

func (t *BrowserHoverTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "Element identifier: UID, text, or CSS selector",
			},
		},
		"required": []string{"target"},
	}
}

func (t *BrowserHoverTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	target, _ := args["target"].(string)
	if target == "" {
		return "Missing required parameter: target", true
	}

	if err := t.browser.Hover(ctx, target); err != nil {
		return fmt.Sprintf("Hover failed: %v", err), true
	}

	return fmt.Sprintf("Hovering over: %s", target), false
}

// BrowserPressKeyTool presses a key or key combination.
type BrowserPressKeyTool struct {
	browser *browser.Browser
}

func NewBrowserPressKeyTool(b *browser.Browser) *BrowserPressKeyTool {
	return &BrowserPressKeyTool{browser: b}
}

func (t *BrowserPressKeyTool) Name() string { return "browser_press_key" }

func (t *BrowserPressKeyTool) Description() string {
	return `Press a key or key combination.
Examples: "Enter", "Tab", "Escape", "Control+A", "Control+Shift+V", "ArrowDown"`
}

func (t *BrowserPressKeyTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "Key or key combination (e.g., 'Enter', 'Control+A')",
			},
		},
		"required": []string{"key"},
	}
}

func (t *BrowserPressKeyTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	key, _ := args["key"].(string)
	if key == "" {
		return "Missing required parameter: key", true
	}

	if err := t.browser.PressKey(key); err != nil {
		return fmt.Sprintf("Key press failed: %v", err), true
	}

	return fmt.Sprintf("Pressed: %s", key), false
}

// BrowserDragTool drags an element to another.
type BrowserDragTool struct {
	browser *browser.Browser
}

func NewBrowserDragTool(b *browser.Browser) *BrowserDragTool {
	return &BrowserDragTool{browser: b}
}

func (t *BrowserDragTool) Name() string { return "browser_drag" }

func (t *BrowserDragTool) Description() string {
	return "Drag an element and drop it on another element."
}

func (t *BrowserDragTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"from_target": map[string]any{
				"type":        "string",
				"description": "Source element identifier",
			},
			"to_target": map[string]any{
				"type":        "string",
				"description": "Destination element identifier",
			},
		},
		"required": []string{"from_target", "to_target"},
	}
}

func (t *BrowserDragTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	from, _ := args["from_target"].(string)
	to, _ := args["to_target"].(string)

	if from == "" || to == "" {
		return "Missing required parameters: from_target and to_target", true
	}

	if err := t.browser.Drag(ctx, from, to); err != nil {
		return fmt.Sprintf("Drag failed: %v", err), true
	}

	return fmt.Sprintf("Dragged from '%s' to '%s'", from, to), false
}

// BrowserUploadFileTool uploads a file to a file input.
type BrowserUploadFileTool struct {
	browser *browser.Browser
}

func NewBrowserUploadFileTool(b *browser.Browser) *BrowserUploadFileTool {
	return &BrowserUploadFileTool{browser: b}
}

func (t *BrowserUploadFileTool) Name() string { return "browser_upload_file" }

func (t *BrowserUploadFileTool) Description() string {
	return "Upload a file to a file input element."
}

func (t *BrowserUploadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "File input element identifier",
			},
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to upload",
			},
		},
		"required": []string{"target", "file_path"},
	}
}

func (t *BrowserUploadFileTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	// File upload requires special handling with rod
	return "File upload not yet implemented", true
}

// BrowserHandleDialogTool handles browser dialogs.
type BrowserHandleDialogTool struct {
	browser *browser.Browser
}

func NewBrowserHandleDialogTool(b *browser.Browser) *BrowserHandleDialogTool {
	return &BrowserHandleDialogTool{browser: b}
}

func (t *BrowserHandleDialogTool) Name() string { return "browser_handle_dialog" }

func (t *BrowserHandleDialogTool) Description() string {
	return "Handle browser dialogs (alert, confirm, prompt). Accept or dismiss the dialog."
}

func (t *BrowserHandleDialogTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"accept", "dismiss"},
				"description": "Action to take: 'accept' or 'dismiss'",
			},
			"prompt_text": map[string]any{
				"type":        "string",
				"description": "Text to enter for prompt dialogs",
			},
		},
		"required": []string{"action"},
	}
}

func (t *BrowserHandleDialogTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	// Dialog handling requires special setup with rod
	return "Dialog handling not yet implemented", true
}

// BrowserSnapshotTool returns the accessibility tree.
type BrowserSnapshotTool struct {
	browser *browser.Browser
}

func NewBrowserSnapshotTool(b *browser.Browser) *BrowserSnapshotTool {
	return &BrowserSnapshotTool{browser: b}
}

func (t *BrowserSnapshotTool) Name() string { return "browser_snapshot" }

func (t *BrowserSnapshotTool) Description() string {
	return `Get a snapshot of interactive elements on the page.
Returns elements with unique IDs (UIDs) that can be used with other browser tools.
Always call this before interacting with elements to get current UIDs.`
}

func (t *BrowserSnapshotTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verbose": map[string]any{
				"type":        "boolean",
				"description": "Include detailed attributes and bounds",
			},
		},
		"required": []string{},
	}
}

func (t *BrowserSnapshotTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	verbose, _ := args["verbose"].(bool)

	elements, err := t.browser.Snapshot(verbose)
	if err != nil {
		return fmt.Sprintf("Snapshot failed: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Page: %s\n", t.browser.CurrentURL()))
	sb.WriteString(fmt.Sprintf("Title: %s\n\n", t.browser.PageTitle()))
	sb.WriteString("Interactive Elements:\n")

	for _, el := range elements {
		if !el.Visible {
			continue
		}

		line := fmt.Sprintf("[%s] %s", el.UID, el.TagName)
		if el.Role != "" {
			line += fmt.Sprintf(" role=%s", el.Role)
		}
		if el.Name != "" {
			line += fmt.Sprintf(" '%s'", el.Name)
		}
		if el.Value != "" {
			line += fmt.Sprintf(" value='%s'", el.Value)
		}

		if verbose && len(el.Attributes) > 0 {
			attrs := make([]string, 0)
			for k, v := range el.Attributes {
				attrs = append(attrs, fmt.Sprintf("%s=%s", k, v))
			}
			line += fmt.Sprintf(" [%s]", strings.Join(attrs, ", "))
		}

		sb.WriteString(line + "\n")
	}

	return sb.String(), false
}

// BrowserScreenshotTool captures a screenshot.
type BrowserScreenshotTool struct {
	browser *browser.Browser
}

func NewBrowserScreenshotTool(b *browser.Browser) *BrowserScreenshotTool {
	return &BrowserScreenshotTool{browser: b}
}

func (t *BrowserScreenshotTool) Name() string { return "browser_screenshot" }

func (t *BrowserScreenshotTool) Description() string {
	return `Capture a screenshot of the current page or a specific element.
Returns the screenshot as base64-encoded PNG.`
}

func (t *BrowserScreenshotTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "Element to screenshot (omit for full page)",
			},
			"full_page": map[string]any{
				"type":        "boolean",
				"description": "Capture entire scrollable page",
			},
		},
		"required": []string{},
	}
}

func (t *BrowserScreenshotTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	target, _ := args["target"].(string)
	fullPage, _ := args["full_page"].(bool)

	var data []byte
	var err error

	if target != "" {
		data, err = t.browser.GetElementScreenshot(target)
	} else {
		data, err = t.browser.Screenshot(fullPage)
	}

	if err != nil {
		return fmt.Sprintf("Screenshot failed: %v", err), true
	}

	return fmt.Sprintf("Screenshot captured (%d bytes)", len(data)), false
}

// BrowserEvaluateTool executes JavaScript.
type BrowserEvaluateTool struct {
	browser *browser.Browser
}

func NewBrowserEvaluateTool(b *browser.Browser) *BrowserEvaluateTool {
	return &BrowserEvaluateTool{browser: b}
}

func (t *BrowserEvaluateTool) Name() string { return "browser_evaluate" }

func (t *BrowserEvaluateTool) Description() string {
	return `Execute JavaScript in the browser page context.
Returns the result of the script execution.`
}

func (t *BrowserEvaluateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"script": map[string]any{
				"type":        "string",
				"description": "JavaScript code to execute",
			},
		},
		"required": []string{"script"},
	}
}

func (t *BrowserEvaluateTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	script, _ := args["script"].(string)
	if script == "" {
		return "Missing required parameter: script", true
	}

	result, err := t.browser.Evaluate(script)
	if err != nil {
		return fmt.Sprintf("Script failed: %v", err), true
	}

	// Try to JSON encode the result
	if resultJSON, err := json.Marshal(result); err == nil {
		return string(resultJSON), false
	}

	return fmt.Sprintf("%v", result), false
}

// BrowserListConsoleTool lists console messages.
type BrowserListConsoleTool struct {
	browser *browser.Browser
}

func NewBrowserListConsoleTool(b *browser.Browser) *BrowserListConsoleTool {
	return &BrowserListConsoleTool{browser: b}
}

func (t *BrowserListConsoleTool) Name() string { return "browser_list_console" }

func (t *BrowserListConsoleTool) Description() string {
	return "List console messages from the current page."
}

func (t *BrowserListConsoleTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of messages to return",
			},
		},
		"required": []string{},
	}
}

func (t *BrowserListConsoleTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	limit := 50
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	messages := t.browser.GetConsoleMessages(limit)
	if len(messages) == 0 {
		return "No console messages", false
	}

	var sb strings.Builder
	sb.WriteString("Console Messages:\n")
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", msg.Level, msg.Text))
	}

	return sb.String(), false
}

// BrowserListNetworkTool lists network requests.
type BrowserListNetworkTool struct {
	browser *browser.Browser
}

func NewBrowserListNetworkTool(b *browser.Browser) *BrowserListNetworkTool {
	return &BrowserListNetworkTool{browser: b}
}

func (t *BrowserListNetworkTool) Name() string { return "browser_list_network" }

func (t *BrowserListNetworkTool) Description() string {
	return "List network requests made by the current page."
}

func (t *BrowserListNetworkTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of requests to return",
			},
		},
		"required": []string{},
	}
}

func (t *BrowserListNetworkTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	limit := 50
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	requests := t.browser.GetNetworkRequests(limit)
	if len(requests) == 0 {
		return "No network requests", false
	}

	var sb strings.Builder
	sb.WriteString("Network Requests:\n")
	for _, req := range requests {
		status := fmt.Sprintf("%d", req.Status)
		if req.Failed {
			status = "FAILED: " + req.ErrorText
		}
		sb.WriteString(fmt.Sprintf("%s %s -> %s\n", req.Method, req.URL, status))
	}

	return sb.String(), false
}

// BrowserGetNetworkTool gets details of a specific network request.
type BrowserGetNetworkTool struct {
	browser *browser.Browser
}

func NewBrowserGetNetworkTool(b *browser.Browser) *BrowserGetNetworkTool {
	return &BrowserGetNetworkTool{browser: b}
}

func (t *BrowserGetNetworkTool) Name() string { return "browser_get_network" }

func (t *BrowserGetNetworkTool) Description() string {
	return "Get details of failed network requests."
}

func (t *BrowserGetNetworkTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	}
}

func (t *BrowserGetNetworkTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	failed := t.browser.GetFailedRequests()
	if len(failed) == 0 {
		return "No failed requests", false
	}

	var sb strings.Builder
	sb.WriteString("Failed Requests:\n")
	for _, req := range failed {
		sb.WriteString(fmt.Sprintf("%s %s\n", req.Method, req.URL))
		if req.Status > 0 {
			sb.WriteString(fmt.Sprintf("  Status: %d %s\n", req.Status, req.StatusText))
		}
		if req.ErrorText != "" {
			sb.WriteString(fmt.Sprintf("  Error: %s\n", req.ErrorText))
		}
	}

	return sb.String(), false
}

// BrowserResizeTool resizes the viewport.
type BrowserResizeTool struct {
	browser *browser.Browser
}

func NewBrowserResizeTool(b *browser.Browser) *BrowserResizeTool {
	return &BrowserResizeTool{browser: b}
}

func (t *BrowserResizeTool) Name() string { return "browser_resize" }

func (t *BrowserResizeTool) Description() string {
	return "Resize the browser viewport to specific dimensions."
}

func (t *BrowserResizeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"width": map[string]any{
				"type":        "integer",
				"description": "Viewport width in pixels",
			},
			"height": map[string]any{
				"type":        "integer",
				"description": "Viewport height in pixels",
			},
		},
		"required": []string{"width", "height"},
	}
}

func (t *BrowserResizeTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	width, ok1 := args["width"].(float64)
	height, ok2 := args["height"].(float64)

	if !ok1 || !ok2 {
		return "Missing required parameters: width and height", true
	}

	if err := t.browser.SetViewport(int(width), int(height)); err != nil {
		return fmt.Sprintf("Resize failed: %v", err), true
	}

	return fmt.Sprintf("Viewport resized to %dx%d", int(width), int(height)), false
}

// BrowserEmulateTool sets network/CPU throttling.
type BrowserEmulateTool struct {
	browser *browser.Browser
}

func NewBrowserEmulateTool(b *browser.Browser) *BrowserEmulateTool {
	return &BrowserEmulateTool{browser: b}
}

func (t *BrowserEmulateTool) Name() string { return "browser_emulate" }

func (t *BrowserEmulateTool) Description() string {
	return "Emulate network conditions or device features (not yet implemented)."
}

func (t *BrowserEmulateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"network": map[string]any{
				"type":        "string",
				"enum":        []string{"3G", "4G", "offline"},
				"description": "Network condition to emulate",
			},
		},
		"required": []string{},
	}
}

func (t *BrowserEmulateTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	return "Network emulation not yet implemented", true
}

// NewBrowserTools creates all browser tools for the given browser instance.
func NewBrowserTools(b *browser.Browser) []Tool {
	return []Tool{
		// Navigation
		NewBrowserNavigateTool(b),
		NewBrowserNewPageTool(b),
		NewBrowserListPagesTool(b),
		NewBrowserSelectPageTool(b),
		NewBrowserClosePageTool(b),
		NewBrowserWaitForTool(b),
		// Input
		NewBrowserClickTool(b),
		NewBrowserFillTool(b),
		NewBrowserHoverTool(b),
		NewBrowserPressKeyTool(b),
		NewBrowserDragTool(b),
		NewBrowserUploadFileTool(b),
		NewBrowserHandleDialogTool(b),
		// Debugging
		NewBrowserSnapshotTool(b),
		NewBrowserScreenshotTool(b),
		NewBrowserEvaluateTool(b),
		NewBrowserListConsoleTool(b),
		// Network
		NewBrowserListNetworkTool(b),
		NewBrowserGetNetworkTool(b),
		// Emulation
		NewBrowserResizeTool(b),
		NewBrowserEmulateTool(b),
	}
}
