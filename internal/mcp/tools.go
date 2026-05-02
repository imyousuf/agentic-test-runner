package mcp

import "github.com/imyousuf/agentic-test-runner/internal/ops"

// GetBrowserTools returns the list of available browser tools for MCP.
//
// Input schemas are reflected from the canonical Request structs in
// internal/ops, so any field rename / required-flag change there flows
// through to MCP automatically.
func GetBrowserTools() []Tool {
	return []Tool{
		{
			Name:        "browser_navigate",
			Description: "Navigate to a URL in the browser",
			InputSchema: schemaFor(&ops.NavigateRequest{}),
		},
		{
			Name:        "browser_click",
			Description: "Click on an element. Use CSS selectors, XPath, aria-label, data-testid, or visible text",
			InputSchema: schemaFor(&ops.ClickRequest{}),
		},
		{
			Name:        "browser_fill",
			Description: "Fill a form field with text. Works with input, textarea, and select elements",
			InputSchema: schemaFor(&ops.FillRequest{}),
		},
		{
			Name:        "browser_screenshot",
			Description: "Take a screenshot. Supports full page, single element (by CSS selector), full-height element, and multiple elements.",
			InputSchema: schemaFor(&browserScreenshotSchemaArgs{}),
		},
		{
			Name:        "browser_get_url",
			Description: "Get the current page URL",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "browser_get_title",
			Description: "Get the current page title",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "browser_get_html",
			Description: "Get the HTML content of the current page",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "browser_snapshot",
			Description: "Get a snapshot of interactive elements (accessibility tree)",
			InputSchema: schemaFor(&ops.SnapshotRequest{}),
		},
		{
			Name:        "browser_console",
			Description: "Get browser console messages",
			InputSchema: schemaFor(&ops.ConsoleRequest{}),
		},
		{
			Name:        "browser_network",
			Description: "Get network requests",
			InputSchema: schemaFor(&ops.NetworkRequestArgs{}),
		},
		{
			Name:        "browser_press_key",
			Description: "Press a key or key combination (e.g., 'Enter', 'Ctrl+A', 'Escape')",
			InputSchema: schemaFor(&ops.PressKeyRequest{}),
		},
		{
			Name:        "browser_hover",
			Description: "Hover over an element",
			InputSchema: schemaFor(&ops.HoverRequest{}),
		},
		{
			Name:        "browser_go_back",
			Description: "Navigate back in browser history",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "browser_go_forward",
			Description: "Navigate forward in browser history",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "browser_reload",
			Description: "Reload the current page",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "browser_ask",
			Description: "Ask a natural language question about the current browser page. A sub-agent inspects the page and returns a concise answer without polluting your context with raw page content.",
			InputSchema: schemaFor(&ops.AskRequest{}),
		},
		// --- Pre-v1.2.0 gap tools ---
		{
			Name:        "browser_eval",
			Description: "Execute JavaScript in the page context and return the result",
			InputSchema: schemaFor(&ops.EvalRequest{}),
		},
		{
			Name:        "browser_drag",
			Description: "Drag one element to another",
			InputSchema: schemaFor(&ops.DragRequest{}),
		},
		{
			Name:        "browser_errors",
			Description: "Get failed network requests (4xx, 5xx, or network errors)",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "browser_new_page",
			Description: "Open a new browser tab, optionally navigating to a URL",
			InputSchema: schemaFor(&ops.NewPageRequest{}),
		},
		{
			Name:        "browser_list_pages",
			Description: "List all open browser tabs with their URLs and titles",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "browser_select_page",
			Description: "Switch to a browser tab by index (0-based)",
			InputSchema: schemaFor(&ops.SelectPageRequest{}),
		},
		{
			Name:        "browser_close_page",
			Description: "Close a browser tab by index (0-based)",
			InputSchema: schemaFor(&ops.ClosePageRequest{}),
		},
		// --- v1.2.0 gap tools ---
		{
			Name:        "browser_wait",
			Description: "Wait for an element to appear in the DOM, optionally requiring visibility",
			InputSchema: schemaFor(&ops.WaitRequest{}),
		},
		{
			Name:        "browser_scroll",
			Description: "Scroll within an element's scroll container (modals, sidebars, etc.)",
			InputSchema: schemaFor(&ops.ScrollRequest{}),
		},
		{
			Name:        "browser_computed_styles",
			Description: "Get computed CSS styles for an element. Returns a map of CSS property names to their computed values.",
			InputSchema: schemaFor(&browserComputedStylesSchemaArgs{}),
		},
		{
			Name:        "browser_computed_styles_diff",
			Description: "Compare computed CSS styles of an element across two open pages. Returns matches, mismatches, and a similarity score.",
			InputSchema: schemaFor(&browserComputedStylesDiffSchemaArgs{}),
		},
		{
			Name:        "browser_text",
			Description: "Extract text content from an element. Supports structured, flat, links-only, and headings-only modes.",
			InputSchema: schemaFor(&ops.TextRequest{}),
		},
		{
			Name:        "browser_font_check",
			Description: "Check if a font family is actually loaded and rendering in the browser. Uses the CSS Font Loading API.",
			InputSchema: schemaFor(&ops.FontCheckRequest{}),
		},
		{
			Name:        "browser_viewport",
			Description: "Get or set the browser viewport dimensions. Without width/height, returns the current size. Supports named presets: mobile, tablet, desktop, wide.",
			InputSchema: schemaFor(&ops.ViewportRequest{}),
		},
		{
			Name:        "browser_clean_snapshot",
			Description: "Get a cleaned, indented DOM subtree for an element. Removes noise (data-*/aria-* attrs, scripts, hidden elements), collapses SVGs, truncates text.",
			InputSchema: schemaFor(&ops.CleanSnapshotRequest{}),
		},
		{
			Name:        "browser_download_images",
			Description: "Download images found within elements matching a CSS selector. Returns file paths of saved images.",
			InputSchema: schemaFor(&ops.DownloadImagesRequest{}),
		},
	}
}

// --- MCP-specific schema alias structs ---
//
// A handful of MCP tools accept arguments that don't map 1:1 onto the
// canonical ops Request structs (typically because the MCP wire format
// differs slightly — e.g. a comma-separated string instead of []string —
// or because the dispatcher reads an extra side-channel field like
// "file" for browser_screenshot). For these we declare alias structs
// here whose tags drive the reflected schema; the dispatcher does the
// shape conversion before calling into ops.

// browserScreenshotSchemaArgs documents the MCP arguments for
// browser_screenshot. The MCP dispatcher additionally reads "file" out of
// the raw args map to choose a destination path.
type browserScreenshotSchemaArgs struct {
	File        string `json:"file"         jsonschema_description:"File path to save screenshot (default: /tmp/screenshot-<pid>.png)"`
	FullPage    bool   `json:"full_page"    jsonschema_description:"Capture full scrollable page, or full-height element when combined with selector"`
	Selector    string `json:"selector"     jsonschema_description:"CSS selector of element to screenshot. Combine with full_page for full-height element capture."`
	SelectorAll string `json:"selector_all" jsonschema_description:"CSS selector matching multiple elements to screenshot individually"`
	OutputDir   string `json:"output_dir"   jsonschema_description:"Directory to save screenshots (used with selector_all, default: /tmp/)"`
}

// browserComputedStylesSchemaArgs documents the MCP-specific arguments for
// browser_computed_styles. The MCP wire form takes "properties" as a
// comma-separated string while the canonical ops type uses []string.
//
// Either selector OR selector_all is required, but not both — the dispatcher
// branches on which one is set. Marking neither as required in the schema is
// the honest answer; the description spells out the constraint for callers.
type browserComputedStylesSchemaArgs struct {
	Selector    string `json:"selector"     jsonschema_description:"CSS selector of a single element. Provide this OR selector_all."`
	Properties  string `json:"properties"   jsonschema_description:"Comma-separated CSS properties to return (e.g., 'fontSize,color,fontWeight'). Omit for default set."`
	SelectorAll string `json:"selector_all" jsonschema_description:"CSS selector matching multiple elements (one entry per match). Provide this OR selector."`
}

// browserComputedStylesDiffSchemaArgs documents the MCP-specific arguments
// for browser_computed_styles_diff (comma-separated "properties" again).
type browserComputedStylesDiffSchemaArgs struct {
	Selector       string `json:"selector"        jsonschema:"required" jsonschema_description:"CSS selector on the current page"`
	Against        int    `json:"against"         jsonschema:"required" jsonschema_description:"Page index to compare against (0-based)"`
	Properties     string `json:"properties"                             jsonschema_description:"Comma-separated CSS properties to compare (omit for default set)"`
	SelectorTarget string `json:"selector_target"                        jsonschema_description:"CSS selector on the target page (defaults to source selector)"`
}
