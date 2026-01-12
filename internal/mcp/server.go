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

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// Server implements the MCP server for browser automation.
type Server struct {
	browser     *browser.Browser
	config      config.BrowserConfig
	cdpEndpoint string
	scanner     *bufio.Scanner
	writer      io.Writer
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithCDPEndpoint sets the CDP endpoint to connect to an existing browser.
func WithCDPEndpoint(endpoint string) ServerOption {
	return func(s *Server) {
		s.cdpEndpoint = endpoint
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
	result := ToolsListResult{
		Tools: GetBrowserTools(),
	}
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
		s.sendResult(req.ID, ToolCallResult{
			Content: []ContentItem{{Type: "text", Text: err.Error()}},
			IsError: true,
		})
		return
	}

	s.sendResult(req.ID, ToolCallResult{
		Content: []ContentItem{{Type: "text", Text: result}},
	})
}

// executeTool executes a browser tool.
func (s *Server) executeTool(ctx context.Context, name string, args map[string]any) (string, error) {
	// Lazy browser initialization
	if s.browser == nil {
		b, err := browser.New(s.config)
		if err != nil {
			return "", fmt.Errorf("failed to create browser: %w", err)
		}

		if err := b.LaunchOrConnect(ctx, s.cdpEndpoint); err != nil {
			return "", fmt.Errorf("failed to start browser: %w", err)
		}
		s.browser = b
	}

	switch name {
	case "browser_navigate":
		url, _ := args["url"].(string)
		if url == "" {
			return "", fmt.Errorf("url is required")
		}
		// Use NewPage if no page exists, otherwise Navigate
		if !s.browser.HasPage() {
			if err := s.browser.NewPage(ctx, url); err != nil {
				return "", err
			}
		} else {
			if err := s.browser.Navigate(ctx, url); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("Navigated to %s", url), nil

	case "browser_click":
		selector, _ := args["selector"].(string)
		if selector == "" {
			return "", fmt.Errorf("selector is required")
		}
		doubleClick, _ := args["double"].(bool)
		if err := s.browser.Click(ctx, selector, doubleClick); err != nil {
			return "", err
		}
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
		data, err := s.browser.Screenshot(fullPage)
		if err != nil {
			return "", err
		}
		// Save to temp file and return path
		filename, _ := args["file"].(string)
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
	data, _ := json.Marshal(resp)
	fmt.Fprintln(s.writer, string(data))
}

// Close closes the server and browser.
func (s *Server) Close() error {
	if s.browser != nil {
		return s.browser.Close()
	}
	return nil
}
