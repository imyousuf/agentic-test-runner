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
			Description: "Take a screenshot of the current page",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file": map[string]interface{}{
						"type":        "string",
						"description": "File path to save screenshot (default: /tmp/screenshot-<pid>.png)",
					},
					"full_page": map[string]interface{}{
						"type":        "boolean",
						"description": "Capture full scrollable page",
						"default":     false,
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
	}
}
