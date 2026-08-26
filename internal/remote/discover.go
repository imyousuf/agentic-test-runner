// Package remote serves a live view of the browser that ATR drives.
package remote

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/api"
)

// Discover finds the CDP endpoint of a running browser.
//
// Order: the explicit value, then ATR_CDP_ENDPOINT, then the state file that
// "atr browser start" writes.
func Discover(explicit string) (string, error) {
	if explicit != "" {
		return normalize(explicit)
	}

	if env := os.Getenv("ATR_CDP_ENDPOINT"); env != "" {
		return normalize(env)
	}

	state, err := api.GetRunningState()
	if err != nil {
		return "", fmt.Errorf("failed to read the browser state: %w", err)
	}
	if state != nil && state.CDPEndpoint != "" {
		return normalize(state.CDPEndpoint)
	}

	return "", fmt.Errorf("no browser found. Start one with \"atr browser start\", " +
		"or pass --attach with a CDP endpoint")
}

// normalize turns an HTTP style endpoint into the WebSocket URL that a CDP
// client can dial. go-rod dials the given string directly, so an
// "http://host:port" form has to be resolved through /json/version first.
func normalize(endpoint string) (string, error) {
	if strings.HasPrefix(endpoint, "ws://") || strings.HasPrefix(endpoint, "wss://") {
		return endpoint, nil
	}

	base := endpoint
	if strings.HasPrefix(base, "cdp://") {
		base = "http://" + strings.TrimPrefix(base, "cdp://")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	base = strings.TrimSuffix(base, "/")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(base + "/json/version")
	if err != nil {
		return "", fmt.Errorf("failed to reach %s: %w", base, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read %s/json/version: %w", base, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s/json/version returned %d", base, resp.StatusCode)
	}

	var payload struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("failed to parse %s/json/version: %w", base, err)
	}
	if payload.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("%s/json/version carries no webSocketDebuggerUrl", base)
	}

	return payload.WebSocketDebuggerURL, nil
}
