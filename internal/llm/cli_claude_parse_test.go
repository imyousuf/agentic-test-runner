package llm

import (
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// --output-format json alone answers with one result object.
func TestClaudeSingleObjectOutput(t *testing.T) {
	out := []byte(`{"type":"result","subtype":"success","result":"the answer","session_id":"abc"}`)
	resp, err := decodeClaudeOutput(out)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Result != "the answer" {
		t.Errorf("Result = %q", resp.Result)
	}
}

// Adding --verbose turns the same flag into stream-json: an array of events
// for the whole run. ATR passes --verbose through from its own flag, so the
// shape changes underneath it depending on how atr was invoked — which used to
// fail every model call with "cannot unmarshal array".
func TestClaudeStreamedArrayOutput(t *testing.T) {
	out := []byte(`[
		{"type":"system","subtype":"init","session_id":"abc"},
		{"type":"assistant"},
		{"type":"result","subtype":"success","result":"the answer"}
	]`)
	resp, err := decodeClaudeOutput(out)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Result != "the answer" {
		t.Errorf("Result = %q, want the result event's answer", resp.Result)
	}
}

// Earlier result events belong to earlier turns; the last one is the answer.
func TestClaudeStreamedArrayTakesTheLastResult(t *testing.T) {
	out := []byte(`[
		{"type":"result","subtype":"success","result":"first turn"},
		{"type":"assistant"},
		{"type":"result","subtype":"success","result":"final answer"}
	]`)
	resp, err := decodeClaudeOutput(out)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Result != "final answer" {
		t.Errorf("Result = %q", resp.Result)
	}
}

// An error reported inside the stream still has to surface as an error.
func TestClaudeStreamedErrorIsReported(t *testing.T) {
	c := &cliClient{provider: "claude-cli"}
	out := []byte(`[{"type":"system"},{"type":"result","subtype":"error","result":"it went wrong"}]`)
	if _, err := c.parseClaudeResponse(out); err == nil {
		t.Error("expected an error for a result event of subtype error")
	}
}

// A stream with no result event is a failure worth naming, not an empty answer.
func TestClaudeStreamWithoutAResultIsAnError(t *testing.T) {
	out := []byte(`[{"type":"system","subtype":"init"},{"type":"assistant"}]`)
	if _, err := decodeClaudeOutput(out); err == nil {
		t.Error("expected an error when the stream carries no result event")
	}
}

// The CLI signals a failed run with is_error, not only with subtype "error".
// Matching only the subtype handed the error text back as the model's answer.
func TestClaudeIsErrorIsTreatedAsAnError(t *testing.T) {
	c := &cliClient{provider: "claude-cli"}
	out := []byte(`{"type":"result","subtype":"success","is_error":true,"result":"Credit balance too low"}`)
	_, err := c.parseClaudeResponse(out)
	if err == nil {
		t.Fatal("expected an error when the CLI reports is_error")
	}
	if !strings.Contains(err.Error(), "Credit balance too low") {
		t.Errorf("err = %v, want it to carry what the CLI said", err)
	}
}

// The CLI reports cache reads and writes separately from input_tokens, so a
// caller that ignores them undercounts the prompt on any cached run.
func TestClaudeUsageIncludesCacheTokens(t *testing.T) {
	c := &cliClient{provider: "claude-cli"}
	out := []byte(`{"type":"result","subtype":"success","result":"ok",
		"usage":{"input_tokens":2,"output_tokens":4,
		"cache_creation_input_tokens":14188,"cache_read_input_tokens":24657},
		"total_cost_usd":0.0617}`)
	res, err := c.parseClaudeResponse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Usage == nil {
		t.Fatal("usage was parsed and then dropped; this backend reported no tokens at all")
	}
	if got, want := res.Usage.PromptTokens, 2+14188+24657; got != want {
		t.Errorf("PromptTokens = %d, want %d", got, want)
	}
	if res.Usage.CompletionTokens != 4 {
		t.Errorf("CompletionTokens = %d, want 4", res.Usage.CompletionTokens)
	}
}

// The allowlist has to name tools that exist. ATR's own analysis tools are not
// in the MCP server, so naming them as MCP tools produced an allowlist made
// entirely of names Claude has never heard of.
func TestAllowedToolsNamesToolsThatExist(t *testing.T) {
	c := &cliClient{provider: "claude-cli"}
	got := c.getAllowedTools([]llm.Tool{
		{Name: "execute_command"},
		{Name: "read_file"},
		{Name: "search_code"},
		{Name: "browser_click"},
	})

	want := map[string]bool{"Bash": true, "Read": true, "Grep": true, "mcp__atr-browser__browser_click": true}
	if len(got) != len(want) {
		t.Fatalf("allowed = %v, want %d entries", got, len(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("allowed names %q, which is not a tool the CLI has", name)
		}
		if strings.HasPrefix(name, "mcp__atr-browser__") && !strings.Contains(name, "browser_") {
			t.Errorf("%q is an ATR-local tool dressed up as an MCP tool", name)
		}
	}
}

// No tools means no restriction, rather than an empty allowlist that would
// forbid everything.
func TestNoToolsMeansNoAllowlist(t *testing.T) {
	c := &cliClient{provider: "claude-cli"}
	if got := c.getAllowedTools(nil); got != nil {
		t.Errorf("allowed = %v, want nil", got)
	}
}

// The configured timeout has to win over the provider's default, or a long
// browser compile is cut off at ten minutes whatever the config says.
func TestConfiguredTimeoutIsUsed(t *testing.T) {
	if got := cliTimeout(llm.Config{Timeout: 30 * time.Minute}); got != 30*time.Minute {
		t.Errorf("cliTimeout = %v, want the configured 30m", got)
	}
	if got := cliTimeout(llm.Config{}); got != defaultCLITimeout {
		t.Errorf("cliTimeout = %v, want the %v default when unset", got, defaultCLITimeout)
	}
}
