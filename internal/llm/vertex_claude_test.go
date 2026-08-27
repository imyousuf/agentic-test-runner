package llm

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

func testClient() *vertexClaudeClient {
	return &vertexClaudeClient{model: "claude-sonnet-5", maxTokens: 1024, temperature: 0.3}
}

func probeTools() []llm.Tool {
	return []llm.Tool{
		{Name: "shell", Description: "Run a command", Parameters: map[string]any{"type": "object"}},
		{Name: "read_file", Description: "Read a file", Parameters: map[string]any{"type": "object"}},
	}
}

// checkpoints reports where cache_control markers ended up on the wire, which
// is the only place the answer is unambiguous: the SDK's marker type carries a
// constant field that is always populated, so inspecting the structs cannot
// tell a set marker from an unset one.
type checkpoints struct {
	system   bool
	messages []int // indexes of the messages carrying a marker
	tools    bool
	total    int
}

func markers(t *testing.T, p anthropic.MessageNewParams) checkpoints {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var body struct {
		System   []map[string]any `json:"system"`
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode request: %v", err)
	}

	var found checkpoints
	for _, b := range body.System {
		if _, ok := b["cache_control"]; ok {
			found.system = true
			found.total++
		}
	}
	for i, m := range body.Messages {
		for _, b := range m.Content {
			if _, ok := b["cache_control"]; ok {
				found.messages = append(found.messages, i)
				found.total++
			}
		}
	}
	for _, tl := range body.Tools {
		if _, ok := tl["cache_control"]; ok {
			found.tools = true
			found.total++
		}
	}
	return found
}

// With a system prompt the checkpoint belongs there: tools are ordered before
// system in the request, so one marker covers both, and the resulting prefix is
// identical across separate atr invocations.
func TestCheckpointGoesOnTheSystemPrompt(t *testing.T) {
	c := testClient()
	params, err := c.buildParams([]llm.Message{
		{Role: llm.RoleSystem, Content: "you are a browser agent"},
		{Role: llm.RoleUser, Content: "click the button"},
	}, probeTools())
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	found := markers(t, params)
	if !found.system {
		t.Error("the system prompt is not marked; nothing would be cached")
	}
	if found.total != 1 {
		t.Errorf("%d checkpoints on the request, want exactly 1 (%+v)", found.total, found)
	}
}

// The command-analysis loop has no system prompt — its instructions and the
// captured failure are the first user message, and they stay fixed for the
// whole run. Marking the tools alone there would cover too little to cache.
func TestCheckpointFallsBackToTheFirstMessage(t *testing.T) {
	c := testClient()
	params, err := c.buildParams([]llm.Message{
		{Role: llm.RoleUser, Content: "a long analysis prompt with the captured failure"},
		{Role: llm.RoleAssistant, Content: "looking"},
		{Role: llm.RoleUser, Content: "and?"},
	}, probeTools())
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	found := markers(t, params)
	if found.total != 1 {
		t.Fatalf("%d checkpoints on the request, want exactly 1 (%+v)", found.total, found)
	}
	// The marker has to be on the first message: the history after it grows
	// every turn, so a marker there would write a new entry per iteration.
	if len(found.messages) != 1 || found.messages[0] != 0 {
		t.Errorf("checkpoint landed on messages %v, want the first message only", found.messages)
	}
}

// An image-only first message still has to carry the checkpoint somewhere.
func TestCheckpointLandsOnANonTextBlock(t *testing.T) {
	c := testClient()
	params, err := c.buildParams([]llm.Message{
		{Role: llm.RoleUser, ImageData: []byte{0x89, 0x50}, ImageMIME: "image/png"},
	}, probeTools())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if found := markers(t, params); len(found.messages) == 0 {
		t.Errorf("no checkpoint was placed on an image-only message (%+v)", found)
	}
}

func TestTemperatureIsSentUntilTheModelRefusesIt(t *testing.T) {
	c := testClient()
	msgs := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}

	if !sendsTemperature(t, c, msgs) {
		t.Error("temperature was configured but not sent")
	}

	// Once a model has rejected it, it must not be sent again — otherwise
	// every request in the process pays for a failed round trip.
	c.noTemperature.Store(true)
	if sendsTemperature(t, c, msgs) {
		t.Error("temperature is still being sent after the model rejected it")
	}
}

// sendsTemperature reports whether the parameter reaches the wire.
func sendsTemperature(t *testing.T, c *vertexClaudeClient, msgs []llm.Message) bool {
	t.Helper()
	params, err := c.buildParams(msgs, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_, ok := body["temperature"]
	return ok
}

func TestTemperatureRejectionIsDistinguishedFromOtherBadRequests(t *testing.T) {
	// A 400 that says nothing about temperature must not trigger the retry,
	// or a genuinely malformed request gets sent twice.
	if temperatureRejected(&anthropic.Error{StatusCode: http.StatusBadRequest}) {
		t.Error("an empty 400 must not be read as a temperature rejection")
	}
	if temperatureRejected(errors.New("connection reset")) {
		t.Error("a transport error must not be read as a temperature rejection")
	}
	if temperatureRejected(&anthropic.Error{StatusCode: http.StatusTooManyRequests}) {
		t.Error("a 429 must not be read as a temperature rejection")
	}

	// The real thing, as the API sends it.
	rejection := &anthropic.Error{StatusCode: http.StatusBadRequest}
	if err := rejection.UnmarshalJSON([]byte(
		`{"type":"error","error":{"type":"invalid_request_error","message":"` +
			"`temperature`" + ` is deprecated for this model."}}`)); err != nil {
		t.Fatalf("build error: %v", err)
	}
	if !temperatureRejected(rejection) {
		t.Error("the API's own temperature rejection was not recognised; every request would retry-and-fail")
	}
}
