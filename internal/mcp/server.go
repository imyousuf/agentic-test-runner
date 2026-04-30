// Package mcp implements a Model Context Protocol (MCP) server for ATR browser tools.
// This allows CLI tools (claude, gemini) to use ATR's browser automation via MCP.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/computer"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// Server implements the MCP server for browser automation.
type Server struct {
	browser     *browser.Browser
	computer    *computer.Computer
	config      config.BrowserConfig
	cdpEndpoint string
	scanner     *bufio.Scanner
	writer      io.Writer
	appConfig   *config.Config
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithCDPEndpoint sets the CDP endpoint to connect to an existing browser.
func WithCDPEndpoint(endpoint string) ServerOption {
	return func(s *Server) {
		s.cdpEndpoint = endpoint
	}
}

// WithAppConfig sets the full application config for LLM-powered features.
func WithAppConfig(cfg *config.Config) ServerOption {
	return func(s *Server) {
		s.appConfig = cfg
	}
}

// NewServer creates a new MCP server.
func NewServer(cfg config.BrowserConfig, opts ...ServerOption) *Server {
	s := &Server{
		config:  cfg,
		scanner: bufio.NewScanner(os.Stdin),
		writer:  os.Stdout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ServerInfo contains server metadata.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the result of the initialize method.
type InitializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ServerInfo      ServerInfo `json:"serverInfo"`
	Capabilities    struct {
		Tools struct{} `json:"tools"`
	} `json:"capabilities"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolsListResult is the result of tools/list method.
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// ToolCallParams are the parameters for tools/call.
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolCallResult is the result of tools/call method.
type ToolCallResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem represents a content item in tool results.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Run starts the MCP server and processes requests.
func (s *Server) Run(ctx context.Context) error {
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if line == "" {
			continue
		}

		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.sendError(nil, -32700, "Parse error", err.Error())
			continue
		}

		s.handleRequest(ctx, &req)
	}

	return s.scanner.Err()
}

// handleRequest dispatches a request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req *Request) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized":
		// Notification, no response needed
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(ctx, req)
	default:
		s.sendError(req.ID, -32601, "Method not found", req.Method)
	}
}

// handleInitialize handles the initialize request.
func (s *Server) handleInitialize(req *Request) {
	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo: ServerInfo{
			Name:    "atr-browser",
			Version: "1.0.0",
		},
	}
	s.sendResult(req.ID, result)
}

// handleToolsList returns the list of available tools.
func (s *Server) handleToolsList(req *Request) {
	tools := GetBrowserTools()
	tools = append(tools, GetComputerTools()...)
	result := ToolsListResult{Tools: tools}
	s.sendResult(req.ID, result)
}

// handleToolsCall executes a tool call.
func (s *Server) handleToolsCall(ctx context.Context, req *Request) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	result, err := s.executeTool(ctx, params.Name, params.Arguments)
	if err != nil {
		mcpLog("Tool %s returned error: %v", params.Name, err)
		s.sendResult(req.ID, ToolCallResult{
			Content: []ContentItem{{Type: "text", Text: err.Error()}},
			IsError: true,
		})
		return
	}

	mcpLog("Tool %s completed successfully, result length: %d", params.Name, len(result))
	s.sendResult(req.ID, ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: result}},
	})
}

// mcpLog writes to the MCP debug log file
func mcpLog(format string, args ...interface{}) {
	homeDir, _ := os.UserHomeDir()
	logPath := homeDir + "/.atr/mcp-debug.log"
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05.000"), msg)
}

// executeTool executes a browser or computer tool.
func (s *Server) executeTool(ctx context.Context, name string, args map[string]any) (string, error) {
	// Computer tools route to a separate dispatcher.
	if isComputerTool(name) {
		return s.executeComputerTool(ctx, name, args)
	}

	// Lazy browser initialization
	if s.browser == nil {
		mcpLog("Initializing browser, cdpEndpoint=%q", s.cdpEndpoint)
		b, err := browser.New(s.config)
		if err != nil {
			mcpLog("Failed to create browser: %v", err)
			return "", fmt.Errorf("failed to create browser: %w", err)
		}

		mcpLog("Calling LaunchOrConnect...")
		if err := b.LaunchOrConnect(ctx, s.cdpEndpoint); err != nil {
			mcpLog("LaunchOrConnect failed: %v", err)
			return "", fmt.Errorf("failed to start browser: %w", err)
		}
		mcpLog("Browser connected successfully")
		s.browser = b
	}

	mcpLog("Executing tool: %s with args: %v", name, args)

	switch name {
	case "browser_navigate":
		url, _ := args["url"].(string)
		if url == "" {
			return "", fmt.Errorf("url is required")
		}
		mcpLog("browser_navigate: url=%s, hasPage=%v", url, s.browser.HasPage())
		// Use NewPage if no page exists, otherwise Navigate
		if !s.browser.HasPage() {
			mcpLog("browser_navigate: calling NewPage...")
			if err := s.browser.NewPage(ctx, url); err != nil {
				mcpLog("browser_navigate: NewPage failed: %v", err)
				return "", err
			}
			mcpLog("browser_navigate: NewPage completed")
		} else {
			mcpLog("browser_navigate: calling Navigate...")
			if err := s.browser.Navigate(ctx, url); err != nil {
				mcpLog("browser_navigate: Navigate failed: %v", err)
				return "", err
			}
			mcpLog("browser_navigate: Navigate completed")
		}
		mcpLog("browser_navigate: success")
		return fmt.Sprintf("Navigated to %s", url), nil

	case "browser_click":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		doubleClick, _ := args["double"].(bool)
		mcpLog("browser_click: selector=%q, doubleClick=%v", selector, doubleClick)
		if err := s.browser.Click(ctx, selector, doubleClick); err != nil {
			mcpLog("browser_click: FAILED: %v", err)
			return "", fmt.Errorf("click failed on %q: %w", selector, err)
		}
		mcpLog("browser_click: success")
		return fmt.Sprintf("Clicked on %s", selector), nil

	case "browser_fill":
		selector, _ := args["selector"].(string)
		value, _ := args["value"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		if err := s.browser.Fill(ctx, selector, value); err != nil {
			return "", err
		}
		return fmt.Sprintf("Filled %s with value", selector), nil

	case "browser_screenshot":
		fullPage, _ := args["full_page"].(bool)
		selector, _ := args["selector"].(string)
		selectorAll, _ := args["selector_all"].(string)
		filename, _ := args["file"].(string)

		// Multiple element screenshots
		if selectorAll != "" {
			outputDir, _ := args["output_dir"].(string)
			if outputDir == "" {
				outputDir = "/tmp"
			}
			results, err := s.browser.GetMultipleElementScreenshots(selectorAll)
			if err != nil {
				return "", fmt.Errorf("multi-screenshot failed: %w", err)
			}
			var saved []string
			for _, r := range results {
				if r.Error != "" {
					continue
				}
				path := filepath.Join(outputDir, fmt.Sprintf("element-%d.png", r.Index))
				if err := os.WriteFile(path, r.Data, 0644); err != nil {
					continue
				}
				saved = append(saved, path)
			}
			data, _ := json.MarshalIndent(map[string]interface{}{
				"count": len(saved),
				"files": saved,
			}, "", "  ")
			return string(data), nil
		}

		// Single element screenshot
		if selector != "" {
			var data []byte
			var err error
			if fullPage {
				data, err = s.browser.GetElementFullHeightScreenshot(selector)
			} else {
				data, err = s.browser.GetElementScreenshotByCSS(selector)
			}
			if err != nil {
				return "", err
			}
			if filename == "" {
				filename = fmt.Sprintf("/tmp/element-screenshot-%d.png", os.Getpid())
			}
			if err := os.WriteFile(filename, data, 0644); err != nil {
				return "", err
			}
			return fmt.Sprintf("Screenshot saved to %s", filename), nil
		}

		// Full page or viewport screenshot
		data, err := s.browser.Screenshot(fullPage)
		if err != nil {
			return "", err
		}
		if filename == "" {
			filename = fmt.Sprintf("/tmp/screenshot-%d.png", os.Getpid())
		}
		if err := os.WriteFile(filename, data, 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Screenshot saved to %s", filename), nil

	case "browser_get_url":
		url := s.browser.CurrentURL()
		return url, nil

	case "browser_get_title":
		title := s.browser.PageTitle()
		return title, nil

	case "browser_get_html":
		html, err := s.browser.HTML()
		if err != nil {
			return "", err
		}
		return html, nil

	case "browser_snapshot":
		verbose, _ := args["verbose"].(bool)
		infos, err := s.browser.Snapshot(verbose)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(infos, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_console":
		limit := 50
		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}
		messages := s.browser.GetConsoleMessages(limit)
		data, err := json.MarshalIndent(messages, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_network":
		limit := 50
		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}
		requests := s.browser.GetNetworkRequests(limit)
		data, err := json.MarshalIndent(requests, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_press_key":
		key, _ := args["key"].(string)
		if key == "" {
			return "", fmt.Errorf("key is required")
		}
		if err := s.browser.PressKey(key); err != nil {
			return "", err
		}
		return fmt.Sprintf("Pressed %s", key), nil

	case "browser_hover":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		if err := s.browser.Hover(ctx, selector); err != nil {
			return "", err
		}
		return fmt.Sprintf("Hovered over %s", selector), nil

	case "browser_go_back":
		if err := s.browser.GoBack(); err != nil {
			return "", err
		}
		return "Navigated back", nil

	case "browser_go_forward":
		if err := s.browser.GoForward(); err != nil {
			return "", err
		}
		return "Navigated forward", nil

	case "browser_reload":
		if err := s.browser.Reload(); err != nil {
			return "", err
		}
		return "Reloaded page", nil

	case "browser_ask":
		question, _ := args["question"].(string)
		if question == "" {
			return "", fmt.Errorf("question is required")
		}
		if s.appConfig == nil {
			return "", fmt.Errorf("LLM not configured: app config not provided to MCP server")
		}
		if err := s.appConfig.ValidateForLLM(); err != nil {
			return "", fmt.Errorf("LLM configuration error: %w", err)
		}
		llmCfg := s.appConfig.GetLLMConfig()
		llmClient, err := llm.NewClient(ctx, llmCfg)
		if err != nil {
			return "", fmt.Errorf("failed to create LLM client: %w", err)
		}
		defer llmClient.Close()

		askAgent := agent.NewAskAgent(agent.AskConfig{
			LLMClient: llmClient,
			Browser:   s.browser,
		})
		answer, err := askAgent.Ask(ctx, question)
		if err != nil {
			return "", fmt.Errorf("ask failed: %w", err)
		}
		return answer, nil

	// --- Pre-v1.2.0 gap tools ---

	case "browser_eval":
		script, _ := args["script"].(string)
		if script == "" {
			return "", fmt.Errorf("script is required")
		}
		result, err := s.browser.Evaluate(script)
		if err != nil {
			return "", fmt.Errorf("eval failed: %w", err)
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_drag":
		from, _ := args["from"].(string)
		to, _ := args["to"].(string)
		if from == "" || to == "" {
			return "", fmt.Errorf("from and to are required")
		}
		if err := s.browser.Drag(ctx, from, to); err != nil {
			return "", fmt.Errorf("drag failed: %w", err)
		}
		return fmt.Sprintf("Dragged from %s to %s", from, to), nil

	case "browser_errors":
		requests := s.browser.GetFailedRequests()
		data, err := json.MarshalIndent(requests, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_new_page":
		url, _ := args["url"].(string)
		if err := s.browser.NewPage(ctx, url); err != nil {
			return "", fmt.Errorf("new page failed: %w", err)
		}
		if url != "" {
			return fmt.Sprintf("Opened new tab at %s", url), nil
		}
		return "Opened new blank tab", nil

	case "browser_list_pages":
		pages := s.browser.ListPages()
		data, err := json.MarshalIndent(pages, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_select_page":
		index, ok := args["index"].(float64)
		if !ok {
			return "", fmt.Errorf("index is required")
		}
		if err := s.browser.SelectPage(int(index)); err != nil {
			return "", fmt.Errorf("select page failed: %w", err)
		}
		return fmt.Sprintf("Switched to tab %d", int(index)), nil

	case "browser_close_page":
		index, ok := args["index"].(float64)
		if !ok {
			return "", fmt.Errorf("index is required")
		}
		if err := s.browser.ClosePage(int(index)); err != nil {
			return "", fmt.Errorf("close page failed: %w", err)
		}
		return fmt.Sprintf("Closed tab %d", int(index)), nil

	// --- v1.2.0 gap tools ---

	case "browser_wait":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		timeout := 5000 * time.Millisecond
		if t, ok := args["timeout"].(float64); ok && t > 0 {
			timeout = time.Duration(t) * time.Millisecond
		}
		visible, _ := args["visible"].(bool)
		if visible {
			if err := s.browser.WaitForElementVisible(ctx, selector, timeout); err != nil {
				return "", fmt.Errorf("wait visible failed: %w", err)
			}
			return fmt.Sprintf("Element %s is visible", selector), nil
		}
		if err := s.browser.WaitForElement(ctx, selector, timeout); err != nil {
			return "", fmt.Errorf("wait failed: %w", err)
		}
		return fmt.Sprintf("Element %s found", selector), nil

	case "browser_scroll":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		var x, y int
		if v, ok := args["x"].(float64); ok {
			x = int(v)
		}
		if v, ok := args["y"].(float64); ok {
			y = int(v)
		}
		toBottom, _ := args["to_bottom"].(bool)
		toTop, _ := args["to_top"].(bool)
		result, err := s.browser.ScrollElement(selector, x, y, toBottom, toTop)
		if err != nil {
			return "", fmt.Errorf("scroll failed: %w", err)
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_computed_styles":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		var properties []string
		if p, ok := args["properties"].(string); ok && p != "" {
			properties = strings.Split(p, ",")
		}
		styles, err := s.browser.GetComputedStyles(selector, properties)
		if err != nil {
			return "", fmt.Errorf("computed styles failed: %w", err)
		}
		data, err := json.MarshalIndent(styles, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_computed_styles_diff":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		against, ok := args["against"].(float64)
		if !ok {
			return "", fmt.Errorf("against page index is required")
		}
		var properties []string
		if p, ok := args["properties"].(string); ok && p != "" {
			properties = strings.Split(p, ",")
		}
		selectorTarget, _ := args["selector_target"].(string)
		result, err := s.browser.GetComputedStylesDiff(selector, int(against), properties, selectorTarget)
		if err != nil {
			return "", fmt.Errorf("style diff failed: %w", err)
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_text":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		mode, _ := args["mode"].(string)
		if mode == "" {
			mode = "structured"
		}
		result, err := s.browser.GetTextContent(selector, mode)
		if err != nil {
			return "", fmt.Errorf("text extraction failed: %w", err)
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_font_check":
		family, _ := args["family"].(string)
		if family == "" {
			return "", fmt.Errorf("family is required")
		}
		result, err := s.browser.CheckFont(family)
		if err != nil {
			return "", fmt.Errorf("font check failed: %w", err)
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_viewport":
		preset, _ := args["preset"].(string)
		width, hasWidth := args["width"].(float64)
		height, hasHeight := args["height"].(float64)

		// GET: no args → return current viewport
		if preset == "" && !hasWidth && !hasHeight {
			vp, err := s.browser.GetViewport()
			if err != nil {
				return "", err
			}
			data, err := json.MarshalIndent(vp, "", "  ")
			if err != nil {
				return "", err
			}
			return string(data), nil
		}

		// Resolve preset
		if preset != "" {
			switch preset {
			case "mobile":
				width, height = 375, 812
			case "tablet":
				width, height = 768, 1024
			case "desktop":
				width, height = 1440, 900
			case "wide":
				width, height = 1920, 1080
			default:
				return "", fmt.Errorf("unknown preset: %s (use mobile, tablet, desktop, or wide)", preset)
			}
		}

		if width == 0 || height == 0 {
			return "", fmt.Errorf("width and height are required")
		}

		var dprArgs []float64
		if d, ok := args["dpr"].(float64); ok && d > 0 {
			dprArgs = []float64{d}
		}
		prev, current, err := s.browser.SetViewport(int(width), int(height), dprArgs...)
		if err != nil {
			return "", fmt.Errorf("viewport resize failed: %w", err)
		}
		data, err := json.MarshalIndent(map[string]interface{}{
			"previous": prev,
			"current":  current,
		}, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_clean_snapshot":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		opts := browser.CleanSnapshotOptions{}
		if d, ok := args["depth"].(float64); ok {
			opts.Depth = int(d)
		}
		if m, ok := args["max_length"].(float64); ok {
			opts.MaxLength = int(m)
		}
		if sv, ok := args["svg_full"].(bool); ok {
			opts.SVGFull = sv
		}
		jsonOutput, _ := args["json"].(bool)
		htmlStr, jsonNode, err := s.browser.GetCleanSnapshot(selector, opts)
		if err != nil {
			return "", fmt.Errorf("clean snapshot failed: %w", err)
		}
		if jsonOutput && jsonNode != nil {
			data, err := json.MarshalIndent(jsonNode, "", "  ")
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		return htmlStr, nil

	case "browser_download_images":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		fallbackScreenshot, _ := args["fallback_screenshot"].(bool)
		outputDir, _ := args["output_dir"].(string)
		if outputDir == "" {
			outputDir = "/tmp"
		}
		images, err := s.browser.DownloadImages(selector, fallbackScreenshot)
		if err != nil {
			return "", fmt.Errorf("download images failed: %w", err)
		}
		var saved []map[string]interface{}
		for i, img := range images {
			if img.Error != "" {
				saved = append(saved, map[string]interface{}{
					"index": i,
					"error": img.Error,
				})
				continue
			}
			ext := ".png"
			if img.Method == "download" && img.Source != "" {
				if strings.HasSuffix(strings.ToLower(img.Source), ".jpg") || strings.HasSuffix(strings.ToLower(img.Source), ".jpeg") {
					ext = ".jpg"
				}
			}
			path := filepath.Join(outputDir, fmt.Sprintf("image-%d%s", i, ext))
			if err := os.WriteFile(path, img.Data, 0644); err != nil {
				saved = append(saved, map[string]interface{}{
					"index": i,
					"error": err.Error(),
				})
				continue
			}
			saved = append(saved, map[string]interface{}{
				"index":  i,
				"path":   path,
				"method": img.Method,
				"source": img.Source,
			})
		}
		data, err := json.MarshalIndent(map[string]interface{}{
			"count": len(saved),
			"files": saved,
		}, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// sendResult sends a successful response.
func (s *Server) sendResult(id interface{}, result interface{}) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.send(resp)
}

// sendError sends an error response.
func (s *Server) sendError(id interface{}, code int, message, data string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	s.send(resp)
}

// send writes a response to stdout.
func (s *Server) send(resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		// Log error to stderr (not stdout which is for protocol)
		fmt.Fprintf(os.Stderr, "MCP: failed to marshal response: %v\n", err)
		return
	}
	fmt.Fprintln(s.writer, string(data))
}

// Close closes the server and browser.
func (s *Server) Close() error {
	if s.browser != nil {
		return s.browser.Close()
	}
	return nil
}
