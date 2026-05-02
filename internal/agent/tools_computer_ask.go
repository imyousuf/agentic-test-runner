package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/imyousuf/agentic-test-runner/internal/computer"
)

// computerScreenshotImageTool implements ImageResultTool so the screenshot
// PNG bytes are fed back to the LLM as a multimodal message rather than
// written to disk. Used by the ComputerAskAgent.
type computerScreenshotImageTool struct {
	c *computer.Computer
}

func (t *computerScreenshotImageTool) Name() string { return "computer_screenshot" }

func (t *computerScreenshotImageTool) Description() string {
	return "Capture a screenshot of the desktop. Returns the image directly so you can see the current state. Use 'display' to pick a monitor (default: primary)."
}

func (t *computerScreenshotImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"display": map[string]any{
				"type":        "integer",
				"description": "Display index (default: primary). Call computer_displays first to see available displays.",
			},
		},
	}
}

// Execute is the non-image fallback. It still captures the screenshot but
// returns only a status string; the LLM doesn't get to see pixels. The
// agent registry prefers ExecuteWithImage when this interface is present.
func (t *computerScreenshotImageTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	res, _, _, isErr := t.ExecuteWithImage(ctx, args)
	return res, isErr
}

// ExecuteWithImage captures the screen and returns PNG bytes for the LLM.
func (t *computerScreenshotImageTool) ExecuteWithImage(ctx context.Context, args map[string]any) (string, []byte, string, bool) {
	display := -1
	if v, ok := args["display"]; ok {
		switch n := v.(type) {
		case float64:
			display = int(n)
		case int:
			display = n
		}
	}
	png, err := t.c.Screenshot(display)
	if err != nil {
		return fmt.Sprintf("screenshot failed: %v", err), nil, "", true
	}
	w, h := t.c.ScreenSize()
	displays, _ := json.Marshal(t.c.Displays())
	summary := fmt.Sprintf("Captured screenshot (%d bytes). Virtual screen: %dx%d. Displays: %s", len(png), w, h, string(displays))
	return summary, png, "image/png", false
}

// NewComputerAskTools returns the curated tool subset for the ComputerAskAgent.
// The screenshot tool returns image bytes via ImageResultTool. The remaining
// tools come from NewComputerTools (the full set), filtered to a focused
// subset chosen to keep the LLM's tool list short and purposeful.
func NewComputerAskTools(c *computer.Computer) []Tool {
	keep := map[string]struct{}{
		// Perception
		"computer_displays":      {},
		"computer_active_window": {},
		"computer_list_windows":  {},
		"computer_position":      {},
		// Actuation
		"computer_click":        {},
		"computer_type":         {},
		"computer_press_key":    {},
		"computer_key_chord":    {},
		"computer_focus_window": {},
		"computer_window_state": {},
		"computer_launch_app":   {},
	}

	out := []Tool{&computerScreenshotImageTool{c: c}}
	for _, t := range NewComputerTools(c) {
		if t.Name() == "computer_screenshot" {
			continue // replaced by the image-aware variant above
		}
		if _, ok := keep[t.Name()]; ok {
			out = append(out, t)
		}
	}
	return out
}
