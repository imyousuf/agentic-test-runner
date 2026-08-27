package llm

import "testing"

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
