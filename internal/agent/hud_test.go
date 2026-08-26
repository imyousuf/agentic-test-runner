package agent

import (
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

func newTestSession(maxHistory int) *HudSession {
	return &HudSession{
		maxHistory: maxHistory,
		messages:   []llm.Message{{Role: llm.RoleSystem, Content: "system"}},
	}
}

// The panel shows the target of a secret fill, but never the ref or the
// command: a command can embed an entry path the user would rather not put on
// a screen someone else may be looking at.
func TestToolDetailHidesSecretSource(t *testing.T) {
	detail := toolDetail(llm.ToolCall{
		Name: "browser_fill_secret",
		Arguments: map[string]any{
			"target":  "#password",
			"command": "pass show personal/bank/login",
			"ref":     "bank/login",
		},
	})

	if strings.Contains(detail, "pass show") || strings.Contains(detail, "bank/login") {
		t.Errorf("secret source leaked into the panel: %q", detail)
	}
	if !strings.Contains(detail, "#password") {
		t.Errorf("detail should name the field being filled, got %q", detail)
	}
}

func TestToolDetailPrefersIdentifyingArgument(t *testing.T) {
	tests := []struct {
		name string
		call llm.ToolCall
		want string
	}{
		{
			name: "navigate shows the url",
			call: llm.ToolCall{Name: "browser_navigate", Arguments: map[string]any{"url": "https://example.com"}},
			want: "https://example.com",
		},
		{
			name: "shell shows the command",
			call: llm.ToolCall{Name: "execute_command", Arguments: map[string]any{"command": "ls -la"}},
			want: "ls -la",
		},
		{
			name: "no arguments yields nothing",
			call: llm.ToolCall{Name: "browser_snapshot", Arguments: map[string]any{}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolDetail(tt.call); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Newlines would break the panel's single-line tool rows.
func TestToolDetailIsSingleLine(t *testing.T) {
	detail := toolDetail(llm.ToolCall{
		Name:      "execute_command",
		Arguments: map[string]any{"command": "echo one\necho two"},
	})
	if strings.Contains(detail, "\n") {
		t.Errorf("detail must be a single line, got %q", detail)
	}
}

func TestTrimKeepsSystemPrompt(t *testing.T) {
	s := newTestSession(4)
	for i := 0; i < 20; i++ {
		s.messages = append(s.messages, llm.Message{Role: llm.RoleUser, Content: "hello"})
	}

	s.trim()

	if len(s.messages) > 5 {
		t.Errorf("history not trimmed: %d messages", len(s.messages))
	}
	if s.messages[0].Role != llm.RoleSystem {
		t.Errorf("system prompt must survive trimming, got role %q", s.messages[0].Role)
	}
}

// Providers reject a tool result whose originating assistant turn was
// trimmed away, so the retained window must never begin with one.
func TestTrimDoesNotStrandToolResults(t *testing.T) {
	s := newTestSession(3)
	s.messages = append(s.messages,
		llm.Message{Role: llm.RoleUser, Content: "do it"},
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{Name: "browser_snapshot"}}},
		llm.Message{Role: llm.RoleTool, Content: "result one", ToolCallID: "browser_snapshot"},
		llm.Message{Role: llm.RoleTool, Content: "result two", ToolCallID: "browser_snapshot"},
		llm.Message{Role: llm.RoleAssistant, Content: "done"},
	)

	s.trim()

	if len(s.messages) < 2 {
		t.Fatalf("trim dropped everything: %d messages", len(s.messages))
	}
	if s.messages[1].Role == llm.RoleTool {
		t.Errorf("retained window starts with an orphaned tool result: %+v", s.messages[1])
	}
}

func TestTrimIsANoOpBelowTheLimit(t *testing.T) {
	s := newTestSession(10)
	s.messages = append(s.messages, llm.Message{Role: llm.RoleUser, Content: "hello"})

	s.trim()

	if len(s.messages) != 2 {
		t.Errorf("got %d messages, want 2", len(s.messages))
	}
}

// Screenshots are megabytes each. Without pruning, every one taken in a
// long HUD conversation would be re-uploaded on every later model call.
func TestPruneImagesKeepsOnlyRecentScreenshots(t *testing.T) {
	s := newTestSession(100)
	for i := 0; i < 5; i++ {
		s.messages = append(s.messages, llm.Message{
			Role:      llm.RoleTool,
			Content:   "screenshot taken",
			ImageData: []byte{0x89, 0x50, 0x4e, 0x47},
			ImageMIME: "image/png",
		})
	}

	s.pruneImages()

	withImages := 0
	for _, m := range s.messages {
		if len(m.ImageData) > 0 {
			withImages++
			if m.ImageMIME == "" {
				t.Error("a retained image lost its MIME type")
			}
		}
	}
	if withImages != hudRecentImageWindow {
		t.Errorf("got %d messages with images, want %d", withImages, hudRecentImageWindow)
	}

	// The most recent ones are the ones that must survive.
	last := s.messages[len(s.messages)-1]
	if len(last.ImageData) == 0 {
		t.Error("the newest screenshot was pruned")
	}
}

func TestPruneImagesPreservesText(t *testing.T) {
	s := newTestSession(100)
	for i := 0; i < 4; i++ {
		s.messages = append(s.messages, llm.Message{
			Role:      llm.RoleTool,
			Content:   "the textual result",
			ImageData: []byte{0x89},
			ImageMIME: "image/png",
		})
	}

	s.pruneImages()

	for i, m := range s.messages[1:] {
		if m.Content != "the textual result" {
			t.Errorf("message %d lost its text content: %q", i, m.Content)
		}
	}
}
