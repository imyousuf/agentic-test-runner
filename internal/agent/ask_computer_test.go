package agent

import (
	"context"
	"runtime"
	"testing"

	"github.com/vcaesar/screenshot"

	"github.com/imyousuf/agentic-test-runner/internal/computer"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

func newTestComputerForAsk(t *testing.T) *computer.Computer {
	t.Helper()
	if runtime.GOOS == "windows" {
		// vcaesar/screenshot's NumActiveDisplays() trips a checkptr
		// pointer-arithmetic violation under `go test -race` on the
		// Windows GitHub runner. Computer is documented as Linux-X11
		// only in v1, so skip rather than chase the upstream bug.
		t.Skip("internal/agent computer-using tests skipped on Windows: vcaesar/screenshot checkptr violation under -race")
	}
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
// confirms the LLM would receive non-empty PNG data. Auto-skipped when no
// displays are attached (e.g., headless CI runners) so the suite stays
// green on `go test ./...` without `-short`.
func TestComputerScreenshotImageToolReturnsBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("requires display")
	}
	if runtime.GOOS == "windows" {
		// screenshot.NumActiveDisplays() itself trips checkptr on
		// Windows under -race; skip before the call.
		t.Skip("internal/agent screenshot test skipped on Windows: vcaesar/screenshot checkptr violation under -race")
	}
	if screenshot.NumActiveDisplays() == 0 {
		t.Skip("no displays available (headless environment)")
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

func TestTrimImageHistoryKeepsRecentN(t *testing.T) {
	mkTool := func(content string, withImg bool) llm.Message {
		m := llm.Message{Role: llm.RoleTool, Content: content}
		if withImg {
			m.ImageData = []byte("png-bytes-" + content)
			m.ImageMIME = "image/png"
		}
		return m
	}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "go"},
		{Role: llm.RoleAssistant, Content: ""},
		mkTool("step1", true),
		{Role: llm.RoleAssistant, Content: ""},
		mkTool("step2", true),
		{Role: llm.RoleAssistant, Content: ""},
		mkTool("step3", true),
		{Role: llm.RoleAssistant, Content: ""},
		mkTool("step4", true),
	}
	out := trimImageHistory(messages, 2)

	// Most recent two tool messages must KEEP image data
	if len(out[len(out)-1].ImageData) == 0 {
		t.Error("most recent tool message lost ImageData")
	}
	if len(out[len(out)-3].ImageData) == 0 {
		t.Error("second-most recent tool message lost ImageData")
	}
	// Older tool messages must have empty ImageData but keep Content
	if len(out[3].ImageData) != 0 {
		t.Errorf("step1 should have ImageData stripped, got %d bytes", len(out[3].ImageData))
	}
	if out[3].Content != "step1" {
		t.Errorf("step1 Content lost: %q", out[3].Content)
	}
	if len(out[5].ImageData) != 0 {
		t.Error("step2 should have ImageData stripped")
	}
}

func TestTrimImageHistoryNoOpUnderThreshold(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleTool, Content: "a", ImageData: []byte("x"), ImageMIME: "image/png"},
		{Role: llm.RoleTool, Content: "b", ImageData: []byte("y"), ImageMIME: "image/png"},
	}
	out := trimImageHistory(messages, 5)
	for i, m := range out {
		if len(m.ImageData) == 0 {
			t.Errorf("message %d unexpectedly stripped: %+v", i, m)
		}
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
