package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// --no-compile says it never calls the model. It used to build one anyway,
// before the spec loop, so a CI job replaying committed scripts still needed a
// configured backend and working credentials for calls it would never make —
// and found out by way of a failure that named the LLM rather than anything it
// had asked for.
func TestOnlyAReplayCanSkipTheModel(t *testing.T) {
	tests := []struct {
		name      string
		behavior  string
		noCompile bool
		interpret bool
		want      bool
	}{
		{
			name: "command analysis is nothing but a model call",
			want: true,
		},
		{
			name:     "a compile drives the application with the model",
			behavior: "tests/login.test.txt",
			want:     true,
		},
		{
			// loadOrCompile refuses instead of compiling, and triage is
			// skipped outright, so nothing can reach the model.
			name:      "a replay under --no-compile cannot",
			behavior:  "tests/login.test.txt",
			noCompile: true,
			want:      false,
		},
		{
			name:      "--interpret drives every step with the model",
			behavior:  "tests/login.test.txt",
			noCompile: true,
			interpret: true,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			behaviorFlag, noCompileFlag, interpretFlag = tt.behavior, tt.noCompile, tt.interpret
			t.Cleanup(func() { behaviorFlag, noCompileFlag, interpretFlag = "", false, false })

			if got := runNeedsModel(); got != tt.want {
				t.Errorf("runNeedsModel() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A stub rather than nil, so a path that did reach the model despite
// --no-compile says which flag forbade it instead of crashing.
func TestTheUnavailableClientNamesWhatForbadeTheCall(t *testing.T) {
	client := llm.NewUnavailable("--no-compile is set")
	defer client.Close()

	_, err := client.Chat(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("the stub answered a chat")
	}
	if !strings.Contains(err.Error(), "--no-compile") {
		t.Errorf("the error does not name the flag responsible: %v", err)
	}

	if _, err := client.ChatWithHistory(context.Background(), nil, nil); err == nil {
		t.Error("the stub answered a history chat")
	}
	if err := client.Close(); err != nil {
		t.Errorf("closing the stub failed: %v", err)
	}
}
