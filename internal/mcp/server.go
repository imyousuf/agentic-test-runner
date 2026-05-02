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
	"github.com/imyousuf/agentic-test-runner/internal/ops"
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
	case "initialized", "notifications/initialized":
		// JSON-RPC notification — clients post this after init. The
		// MCP 2024-11-05 spec uses the "notifications/" prefix; the
		// older bare name is kept for compatibility with older clients.
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(ctx, req)
	default:
		// Per JSON-RPC 2.0, notifications (no id) MUST NOT receive a
		// response — including for unknown methods. Only send an error
		// for proper requests.
		if req.ID != nil {
			s.sendError(req.ID, -32601, "Method not found", req.Method)
		}
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
		var req ops.NavigateRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		mcpLog("browser_navigate: url=%s, hasPage=%v", req.URL, s.browser.HasPage())
		res, err := ops.Navigate(ctx, s.browser, req)
		if err != nil {
			mcpLog("browser_navigate: FAILED: %v", err)
			return "", err
		}
		mcpLog("browser_navigate: success url=%s", res.URL)
		return fmt.Sprintf("Navigated to %s", res.URL), nil

	case "browser_click":
		var req ops.ClickRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		mcpLog("browser_click: selector=%q, doubleClick=%v", req.Selector, req.DoubleClick)
		res, err := ops.Click(ctx, s.browser, req)
		if err != nil {
			mcpLog("browser_click: FAILED: %v", err)
			return "", err
		}
		mcpLog("browser_click: success")
		return fmt.Sprintf("Clicked on %s", res.Selector), nil

	case "browser_fill":
		var req ops.FillRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.Fill(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Filled %s with value", res.Selector), nil

	case "browser_screenshot":
		// MCP carries an extra "file" argument for the destination path.
		filename, _ := args["file"].(string)
		var req ops.ScreenshotRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.Screenshot(ctx, s.browser, req)
		if err != nil {
			return "", err
		}

		if res.IsMulti() {
			outputDir := req.OutputDir
			if outputDir == "" {
				outputDir = "/tmp"
			}
			var saved []string
			for _, r := range res.Multi {
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

		if filename == "" {
			if req.Selector != "" {
				filename = fmt.Sprintf("/tmp/element-screenshot-%d.png", os.Getpid())
			} else {
				filename = fmt.Sprintf("/tmp/screenshot-%d.png", os.Getpid())
			}
		}
		if err := os.WriteFile(filename, res.Data, 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Screenshot saved to %s", filename), nil

	case "browser_get_url":
		res, err := ops.URL(ctx, s.browser)
		if err != nil {
			return "", err
		}
		return res.URL, nil

	case "browser_get_title":
		res, err := ops.Title(ctx, s.browser)
		if err != nil {
			return "", err
		}
		return res.Title, nil

	case "browser_get_html":
		res, err := ops.HTML(ctx, s.browser)
		if err != nil {
			return "", err
		}
		return res.HTML, nil

	case "browser_snapshot":
		var req ops.SnapshotRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.Snapshot(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(res.Elements, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_console":
		var req ops.ConsoleRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.Console(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(res.Messages, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_network":
		var req ops.NetworkRequestArgs
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.Network(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(res.Requests, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_press_key":
		var req ops.PressKeyRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.PressKey(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Pressed %s", res.Key), nil

	case "browser_hover":
		var req ops.HoverRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.Hover(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Hovered over %s", res.Selector), nil

	case "browser_go_back":
		if _, err := ops.Back(ctx, s.browser); err != nil {
			return "", err
		}
		return "Navigated back", nil

	case "browser_go_forward":
		if _, err := ops.Forward(ctx, s.browser); err != nil {
			return "", err
		}
		return "Navigated forward", nil

	case "browser_reload":
		if _, err := ops.Reload(ctx, s.browser); err != nil {
			return "", err
		}
		return "Reloaded page", nil

	case "browser_ask":
		var req ops.AskRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
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
		runner := func(ctx context.Context, q string) (string, error) {
			return askAgent.Ask(ctx, q)
		}
		res, err := ops.Ask(ctx, runner, req)
		if err != nil {
			return "", err
		}
		return res.Answer, nil

	// --- Pre-v1.2.0 gap tools ---

	case "browser_eval":
		var req ops.EvalRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.Eval(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(res.Result, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_drag":
		var req ops.DragRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.Drag(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Dragged from %s to %s", res.From, res.To), nil

	case "browser_errors":
		res, err := ops.Errors(ctx, s.browser)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(res.FailedRequests, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_new_page":
		var req ops.NewPageRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		if _, err := ops.NewPage(ctx, s.browser, req); err != nil {
			return "", err
		}
		if req.URL != "" {
			return fmt.Sprintf("Opened new tab at %s", req.URL), nil
		}
		return "Opened new blank tab", nil

	case "browser_list_pages":
		res, err := ops.ListPages(ctx, s.browser)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(res.Pages, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_select_page":
		var req ops.SelectPageRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		if _, err := ops.SelectPage(ctx, s.browser, req); err != nil {
			return "", err
		}
		return fmt.Sprintf("Switched to tab %d", req.Index), nil

	case "browser_close_page":
		var req ops.ClosePageRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		if _, err := ops.ClosePage(ctx, s.browser, req); err != nil {
			return "", err
		}
		return fmt.Sprintf("Closed tab %d", req.Index), nil

	// --- v1.2.0 gap tools ---

	case "browser_wait":
		// MCP arg is named "timeout" not "timeout_ms"; map manually.
		var raw struct {
			Selector string  `json:"selector"`
			Timeout  float64 `json:"timeout"`
			Visible  bool    `json:"visible"`
		}
		if err := ops.MapToStruct(args, &raw); err != nil {
			return "", err
		}
		req := ops.WaitRequest{Selector: raw.Selector, TimeoutMs: int(raw.Timeout), Visible: raw.Visible}
		res, err := ops.Wait(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		if res.Visible {
			return fmt.Sprintf("Element %s is visible", res.Selector), nil
		}
		return fmt.Sprintf("Element %s found", res.Selector), nil

	case "browser_scroll":
		var req ops.ScrollRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.Scroll(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_computed_styles":
		// Properties is comma-separated string in the wire form.
		var raw struct {
			Selector    string `json:"selector"`
			SelectorAll string `json:"selector_all"`
			Properties  string `json:"properties"`
		}
		if err := ops.MapToStruct(args, &raw); err != nil {
			return "", err
		}
		req := ops.ComputedStylesRequest{Selector: raw.Selector, SelectorAll: raw.SelectorAll}
		if raw.Properties != "" {
			req.Properties = strings.Split(raw.Properties, ",")
		}
		res, err := ops.ComputedStyles(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		// Match historical MCP shape (raw styles map for single, array for multi).
		var payload any = res.Styles
		if res.Mode == "all" {
			payload = res.Elements
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_computed_styles_diff":
		var raw struct {
			Selector       string  `json:"selector"`
			Against        float64 `json:"against"`
			Properties     string  `json:"properties"`
			SelectorTarget string  `json:"selector_target"`
		}
		if err := ops.MapToStruct(args, &raw); err != nil {
			return "", err
		}
		if _, ok := args["against"]; !ok {
			return "", fmt.Errorf("against page index is required")
		}
		req := ops.ComputedStylesDiffRequest{
			Selector:       raw.Selector,
			Against:        int(raw.Against),
			SelectorTarget: raw.SelectorTarget,
		}
		if raw.Properties != "" {
			req.Properties = strings.Split(raw.Properties, ",")
		}
		res, err := ops.ComputedStylesDiff(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		// Preserve historical raw StyleDiffResult shape.
		payload := map[string]interface{}{
			"selector":      res.Selector,
			"matches":       res.Matches,
			"mismatches":    res.Mismatches,
			"matchCount":    res.MatchCount,
			"mismatchCount": res.MismatchCount,
			"score":         res.Score,
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_text":
		var req ops.TextRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		if req.Mode == "" {
			req.Mode = "structured"
		}
		res, err := ops.Text(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_font_check":
		var req ops.FontCheckRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.FontCheck(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_viewport":
		var req ops.ViewportRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		// GET when no preset, no width, no height.
		if req.Preset == "" && req.Width == 0 && req.Height == 0 {
			res, err := ops.GetViewport(ctx, s.browser)
			if err != nil {
				return "", err
			}
			data, err := json.MarshalIndent(map[string]interface{}{
				"width":             res.Width,
				"height":            res.Height,
				"deviceScaleFactor": res.DPR,
			}, "", "  ")
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		res, err := ops.SetViewport(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(map[string]interface{}{
			"previous": res.Previous,
			"current":  res.Current,
		}, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "browser_clean_snapshot":
		// MCP uses "json" bool to switch output format (matches existing tools.go schema).
		var req ops.CleanSnapshotRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.CleanSnapshot(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		if req.JSON && res.Tree != nil {
			data, err := json.MarshalIndent(res.Tree, "", "  ")
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		return res.HTML, nil

	case "browser_download_images":
		var req ops.DownloadImagesRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		if req.OutputDir == "" {
			req.OutputDir = "/tmp"
		}
		res, err := ops.DownloadImages(ctx, s.browser, req)
		if err != nil {
			return "", err
		}
		var saved []map[string]interface{}
		for i, img := range res.Images {
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
			path := filepath.Join(req.OutputDir, fmt.Sprintf("image-%d%s", i, ext))
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
