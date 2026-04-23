package mcp

// GetBrowserTools returns the list of available browser tools for MCP.
func GetBrowserTools() []Tool {
	return []Tool{
		{
			Name:        "browser_navigate",
			Description: "Navigate to a URL in the browser",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to navigate to",
					},
				},
				"required": []string{"url"},
			},
		},
		{
			Name:        "browser_click",
			Description: "Click on an element. Use CSS selectors, XPath, aria-label, data-testid, or visible text",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "Element selector (CSS, XPath, aria-label, data-testid, or text)",
					},
					"double": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to double-click",
						"default":     false,
					},
				},
				"required": []string{"selector"},
			},
		},
		{
			Name:        "browser_fill",
			Description: "Fill a form field with text. Works with input, textarea, and select elements",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "Element selector",
					},
					"value": map[string]interface{}{
						"type":        "string",
						"description": "Value to fill",
					},
				},
				"required": []string{"selector", "value"},
			},
		},
		{
			Name:        "browser_screenshot",
			Description: "Take a screenshot. Supports full page, single element (by CSS selector), full-height element, and multiple elements.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file": map[string]interface{}{
						"type":        "string",
						"description": "File path to save screenshot (default: /tmp/screenshot-<pid>.png)",
					},
					"full_page": map[string]interface{}{
						"type":        "boolean",
						"description": "Capture full scrollable page, or full-height element when combined with selector",
						"default":     false,
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of element to screenshot. Combine with full_page for full-height element capture.",
					},
					"selector_all": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector matching multiple elements to screenshot individually",
					},
					"output_dir": map[string]interface{}{
						"type":        "string",
						"description": "Directory to save screenshots (used with selector_all, default: /tmp/)",
					},
				},
			},
		},
		{
			Name:        "browser_get_url",
			Description: "Get the current page URL",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "browser_get_title",
			Description: "Get the current page title",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "browser_get_html",
			Description: "Get the HTML content of the current page",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "browser_snapshot",
			Description: "Get a snapshot of interactive elements (accessibility tree)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"verbose": map[string]interface{}{
						"type":        "boolean",
						"description": "Include detailed attributes and bounds",
						"default":     false,
					},
				},
			},
		},
		{
			Name:        "browser_console",
			Description: "Get browser console messages",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of messages to return",
						"default":     50,
					},
				},
			},
		},
		{
			Name:        "browser_network",
			Description: "Get network requests",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of requests to return",
						"default":     50,
					},
				},
			},
		},
		{
			Name:        "browser_press_key",
			Description: "Press a key or key combination (e.g., 'Enter', 'Ctrl+A', 'Escape')",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{
						"type":        "string",
						"description": "Key or key combination to press",
					},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "browser_hover",
			Description: "Hover over an element",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "Element selector",
					},
				},
				"required": []string{"selector"},
			},
		},
		{
			Name:        "browser_go_back",
			Description: "Navigate back in browser history",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "browser_go_forward",
			Description: "Navigate forward in browser history",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "browser_reload",
			Description: "Reload the current page",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "browser_ask",
			Description: "Ask a natural language question about the current browser page. A sub-agent inspects the page and returns a concise answer without polluting your context with raw page content.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"question": map[string]interface{}{
						"type":        "string",
						"description": "The question to ask about the current page",
					},
				},
				"required": []string{"question"},
			},
		},
		// --- Pre-v1.2.0 gap tools ---
		{
			Name:        "browser_eval",
			Description: "Execute JavaScript in the page context and return the result",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"script": map[string]interface{}{
						"type":        "string",
						"description": "JavaScript code to execute",
					},
				},
				"required": []string{"script"},
			},
		},
		{
			Name:        "browser_drag",
			Description: "Drag one element to another",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"from": map[string]interface{}{
						"type":        "string",
						"description": "Selector of element to drag from",
					},
					"to": map[string]interface{}{
						"type":        "string",
						"description": "Selector of element to drag to",
					},
				},
				"required": []string{"from", "to"},
			},
		},
		{
			Name:        "browser_errors",
			Description: "Get failed network requests (4xx, 5xx, or network errors)",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "browser_new_page",
			Description: "Open a new browser tab, optionally navigating to a URL",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL to navigate to (optional, opens blank tab if omitted)",
					},
				},
			},
		},
		{
			Name:        "browser_list_pages",
			Description: "List all open browser tabs with their URLs and titles",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "browser_select_page",
			Description: "Switch to a browser tab by index (0-based)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"index": map[string]interface{}{
						"type":        "number",
						"description": "Tab index (0-based)",
					},
				},
				"required": []string{"index"},
			},
		},
		{
			Name:        "browser_close_page",
			Description: "Close a browser tab by index (0-based)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"index": map[string]interface{}{
						"type":        "number",
						"description": "Tab index (0-based)",
					},
				},
				"required": []string{"index"},
			},
		},
		// --- v1.2.0 gap tools ---
		{
			Name:        "browser_wait",
			Description: "Wait for an element to appear in the DOM, optionally requiring visibility",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector to wait for",
					},
					"timeout": map[string]interface{}{
						"type":        "number",
						"description": "Timeout in milliseconds",
						"default":     5000,
					},
					"visible": map[string]interface{}{
						"type":        "boolean",
						"description": "Wait for element to be visible (not display:none or opacity:0)",
						"default":     false,
					},
				},
				"required": []string{"selector"},
			},
		},
		{
			Name:        "browser_scroll",
			Description: "Scroll within an element's scroll container (modals, sidebars, etc.)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of scrollable element",
					},
					"x": map[string]interface{}{
						"type":        "number",
						"description": "Horizontal scroll position in pixels",
						"default":     0,
					},
					"y": map[string]interface{}{
						"type":        "number",
						"description": "Vertical scroll position in pixels",
						"default":     0,
					},
					"to_bottom": map[string]interface{}{
						"type":        "boolean",
						"description": "Scroll to bottom of element",
						"default":     false,
					},
					"to_top": map[string]interface{}{
						"type":        "boolean",
						"description": "Scroll to top of element",
						"default":     false,
					},
				},
				"required": []string{"selector"},
			},
		},
		{
			Name:        "browser_computed_styles",
			Description: "Get computed CSS styles for an element. Returns a map of CSS property names to their computed values.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of the element",
					},
					"properties": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated CSS properties to return (e.g., 'fontSize,color,fontWeight'). Omit for default set.",
					},
				},
				"required": []string{"selector"},
			},
		},
		{
			Name:        "browser_computed_styles_diff",
			Description: "Compare computed CSS styles of an element across two open pages. Returns matches, mismatches, and a similarity score.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector on the current page",
					},
					"against": map[string]interface{}{
						"type":        "number",
						"description": "Page index to compare against (0-based)",
					},
					"properties": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated CSS properties to compare (omit for default set)",
					},
					"selector_target": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector on the target page (defaults to source selector)",
					},
				},
				"required": []string{"selector", "against"},
			},
		},
		{
			Name:        "browser_text",
			Description: "Extract text content from an element. Supports structured, flat, links-only, and headings-only modes.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of the element",
					},
					"mode": map[string]interface{}{
						"type":        "string",
						"description": "Extraction mode: 'structured' (default), 'flat', 'links', or 'headings'",
						"default":     "structured",
					},
				},
				"required": []string{"selector"},
			},
		},
		{
			Name:        "browser_font_check",
			Description: "Check if a font family is actually loaded and rendering in the browser. Uses the CSS Font Loading API.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"family": map[string]interface{}{
						"type":        "string",
						"description": "Font family name to check (e.g., 'Roboto', 'Inter')",
					},
				},
				"required": []string{"family"},
			},
		},
		{
			Name:        "browser_viewport",
			Description: "Get or set the browser viewport dimensions. Without width/height, returns the current size. Supports named presets: mobile, tablet, desktop, wide.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"width": map[string]interface{}{
						"type":        "number",
						"description": "Viewport width in pixels",
					},
					"height": map[string]interface{}{
						"type":        "number",
						"description": "Viewport height in pixels",
					},
					"preset": map[string]interface{}{
						"type":        "string",
						"description": "Named preset: 'mobile' (375x812), 'tablet' (768x1024), 'desktop' (1440x900), 'wide' (1920x1080)",
					},
					"dpr": map[string]interface{}{
						"type":        "number",
						"description": "Device pixel ratio (default: 1)",
					},
				},
			},
		},
		{
			Name:        "browser_clean_snapshot",
			Description: "Get a cleaned, indented DOM subtree for an element. Removes noise (data-*/aria-* attrs, scripts, hidden elements), collapses SVGs, truncates text.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of the element",
					},
					"depth": map[string]interface{}{
						"type":        "number",
						"description": "Maximum tree depth (0 = unlimited)",
						"default":     0,
					},
					"max_length": map[string]interface{}{
						"type":        "number",
						"description": "Maximum output characters",
						"default":     5000,
					},
					"svg_full": map[string]interface{}{
						"type":        "boolean",
						"description": "Include full SVG path data (collapsed by default)",
						"default":     false,
					},
					"json": map[string]interface{}{
						"type":        "boolean",
						"description": "Return JSON tree instead of HTML",
						"default":     false,
					},
				},
				"required": []string{"selector"},
			},
		},
		{
			Name:        "browser_download_images",
			Description: "Download images found within elements matching a CSS selector. Returns file paths of saved images.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector for elements containing images",
					},
					"output_dir": map[string]interface{}{
						"type":        "string",
						"description": "Directory to save images (default: /tmp/)",
					},
					"fallback_screenshot": map[string]interface{}{
						"type":        "boolean",
						"description": "Screenshot elements when no <img> tags found",
						"default":     false,
					},
				},
				"required": []string{"selector"},
			},
		},
	}
}
