package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/config"
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

// A replay uses a backend if one is configured and stays silent if not: it
// needs no model, but a failure it cannot classify is reported under whatever
// kind the runtime guessed, and a regression that presented as a timeout reads
// as infrastructure.
func TestAReplayUsesABackendWhenThereIsOne(t *testing.T) {
	behaviorFlag, noCompileFlag = "tests/login.test.txt", true
	t.Cleanup(func() { behaviorFlag, noCompileFlag, noTriageFlag = "", false, false })

	// Nothing configured: no lookup, no warning, no model.
	unconfigured := &config.Config{Backend: "gemini-api"}
	client := openModel(context.Background(), unconfigured, llm.Config{})
	if llm.Available(client) {
		t.Error("a replay found a model where none is configured")
	}

	// Explicitly refused, whatever is configured.
	noTriageFlag = true
	client = openModel(context.Background(), unconfigured, llm.Config{})
	if llm.Available(client) {
		t.Error("--no-triage did not refuse")
	}
	if _, err := client.Chat(context.Background(), nil, nil); err == nil ||
		!strings.Contains(err.Error(), "--no-triage") {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
}

// A run that cannot proceed without a model must fail loudly rather than
// quietly carrying on with a stub.
func TestARunThatNeedsAModelSaysSoWhenItCannotHaveOne(t *testing.T) {
	behaviorFlag = ""
	t.Cleanup(func() { behaviorFlag = "" })

	client := openModel(context.Background(),
		&config.Config{Backend: "gemini-api"}, llm.Config{Provider: llm.Provider("nonexistent")})

	unusable, ok := client.(*llm.Unavailable)
	if !ok {
		t.Fatalf("an unreachable backend produced a usable client: %T", client)
	}
	if !unusable.Fatal {
		t.Error("a run that needs a model treated its absence as survivable")
	}
	if unusable.Err == nil {
		t.Error("nothing says what went wrong")
	}
}

// A stub rather than nil, so a path that did reach the model despite
// --no-triage says which flag forbade it instead of crashing.
func TestTheUnavailableClientNamesWhatForbadeTheCall(t *testing.T) {
	client := llm.NewUnavailable("--no-triage is set")
	defer client.Close()

	_, err := client.Chat(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("the stub answered a chat")
	}
	if !strings.Contains(err.Error(), "--no-triage") {
		t.Errorf("the error does not name the flag responsible: %v", err)
	}

	if _, err := client.ChatWithHistory(context.Background(), nil, nil); err == nil {
		t.Error("the stub answered a history chat")
	}
	if err := client.Close(); err != nil {
		t.Errorf("closing the stub failed: %v", err)
	}
}
