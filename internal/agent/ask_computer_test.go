package agent

import (
	"context"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/computer"
)

func newTestComputerForAsk(t *testing.T) *computer.Computer {
	t.Helper()
	c, err := computer.New(computer.Config{
		CountdownMode:    computer.ModeOff,
		CountdownSeconds: 0,
	})
	if err != nil {
		t.Fatalf("computer.New: %v", err)
	}
	return c
}

func TestNewComputerAskToolsRegistersExpectedSet(t *testing.T) {
	c := newTestComputerForAsk(t)
	tools := NewComputerAskTools(c)

	want := []string{
		"computer_screenshot",
		"computer_displays",
		"computer_active_window",
		"computer_list_windows",
		"computer_position",
		"computer_click",
		"computer_type",
		"computer_press_key",
		"computer_key_chord",
		"computer_focus_window",
		"computer_window_state",
		"computer_launch_app",
	}

	got := make(map[string]bool, len(tools))
	for _, tool := range tools {
		got[tool.Name()] = true
	}

	for _, w := range want {
		if !got[w] {
			t.Errorf("missing tool %q in NewComputerAskTools", w)
		}
	}
	// Spot-check that excluded tools are NOT present (keeps the prompt focused).
	for _, excluded := range []string{"computer_drag", "computer_scroll", "computer_hover", "computer_move", "computer_quit_app"} {
		if got[excluded] {
			t.Errorf("did not expect tool %q in ask subset", excluded)
		}
	}
}

func TestComputerScreenshotImageToolImplementsImageResultTool(t *testing.T) {
	c := newTestComputerForAsk(t)
	var tool Tool = &computerScreenshotImageTool{c: c}
	if _, ok := tool.(ImageResultTool); !ok {
		t.Fatal("computerScreenshotImageTool must implement ImageResultTool")
	}
}

// TestComputerScreenshotImageToolReturnsBytes runs the actual screenshot and
// confirms the LLM would receive non-empty PNG data. Behind the smoke tag
// because it requires a display.
func TestComputerScreenshotImageToolReturnsBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("requires display")
	}
	c := newTestComputerForAsk(t)
	tool := &computerScreenshotImageTool{c: c}
	summary, png, mime, isErr := tool.ExecuteWithImage(context.Background(), nil)
	if isErr {
		t.Fatalf("ExecuteWithImage error: %s", summary)
	}
	if mime != "image/png" {
		t.Errorf("MIME = %q, want image/png", mime)
	}
	if len(png) < 100 {
		t.Errorf("expected non-trivial PNG bytes, got %d", len(png))
	}
	if summary == "" {
		t.Error("expected non-empty summary string")
	}
}

func TestNewComputerAskAgentDefaults(t *testing.T) {
	c := newTestComputerForAsk(t)
	a := NewComputerAskAgent(ComputerAskConfig{Computer: c})
	if a == nil {
		t.Fatal("NewComputerAskAgent returned nil")
	}
	if a.maxIterations != 20 {
		t.Errorf("default MaxIterations = %d, want 20", a.maxIterations)
	}
	if a.registry == nil {
		t.Fatal("registry not initialized")
	}
	if defs := a.registry.Definitions(); len(defs) < 10 {
		t.Errorf("expected ~12 tools registered, got %d", len(defs))
	}
}
