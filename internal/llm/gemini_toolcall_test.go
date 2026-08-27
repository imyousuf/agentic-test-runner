package llm

import (
	"testing"

	"google.golang.org/genai"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// functionResponses pulls the function-response parts out of a converted
// history, which is where the pairing between a call and its result lives.
func functionResponses(contents []*genai.Content) []*genai.FunctionResponse {
	var out []*genai.FunctionResponse
	for _, c := range contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				out = append(out, p.FunctionResponse)
			}
		}
	}
	return out
}

// The id Gemini issues has to survive into the tool call, or a turn with two
// calls to the same tool has nothing to tell them apart by.
func TestGeminiCallIDIsKept(t *testing.T) {
	c := &geminiClient{}
	resp := c.convertResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "call-abc", Name: "read_file", Args: map[string]any{"path": "a"}}},
			{FunctionCall: &genai.FunctionCall{ID: "call-def", Name: "read_file", Args: map[string]any{"path": "b"}}},
		}}}},
	})

	if len(resp.ToolCalls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call-abc" || resp.ToolCalls[1].ID != "call-def" {
		t.Errorf("ids = %q, %q; want the ids the model issued",
			resp.ToolCalls[0].ID, resp.ToolCalls[1].ID)
	}
	if resp.ToolCalls[0].Name != "read_file" {
		t.Errorf("name = %q", resp.ToolCalls[0].Name)
	}
}

// Gemini does not always issue one. ATR still needs distinct handles, so it
// makes them up — but they must be distinct, and marked as its own.
func TestGeminiCallsWithoutIDsGetDistinctLocalOnes(t *testing.T) {
	c := &geminiClient{}
	resp := c.convertResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{Name: "read_file", Args: map[string]any{"path": "a"}}},
			{FunctionCall: &genai.FunctionCall{Name: "read_file", Args: map[string]any{"path": "b"}}},
		}}}},
	})

	if len(resp.ToolCalls) != 2 {
		t.Fatalf("got %d tool calls, want 2", len(resp.ToolCalls))
	}
	first, second := resp.ToolCalls[0].ID, resp.ToolCalls[1].ID
	if first == second {
		t.Fatalf("both calls got id %q; they are indistinguishable", first)
	}
	if !isLocalCallID(first) || !isLocalCallID(second) {
		t.Errorf("ids %q and %q are not marked as locally made up", first, second)
	}
}

// The wire format Gemini needs: the response is matched by name, and the id
// disambiguates when the model issued one.
func TestGeminiResultCarriesNameAndIssuedID(t *testing.T) {
	c := &geminiClient{}
	contents := c.convertMessages([]llm.Message{
		{Role: llm.RoleUser, Content: "read both"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "call-abc", Name: "read_file"},
			{ID: "call-def", Name: "read_file"},
		}},
		{Role: llm.RoleTool, ToolCallID: "call-abc", ToolName: "read_file", Content: "contents of a"},
		{Role: llm.RoleTool, ToolCallID: "call-def", ToolName: "read_file", Content: "contents of b"},
	})

	responses := functionResponses(contents)
	if len(responses) != 2 {
		t.Fatalf("got %d function responses, want 2", len(responses))
	}
	for i, want := range []string{"call-abc", "call-def"} {
		if responses[i].ID != want {
			t.Errorf("response %d has id %q, want %q", i, responses[i].ID, want)
		}
		if responses[i].Name != "read_file" {
			t.Errorf("response %d has name %q, want read_file — Gemini matches on the name", i, responses[i].Name)
		}
	}
}

// An id ATR invented must not be echoed back: Gemini never issued it, and
// claiming otherwise is a lie about what the model asked for.
func TestGeminiLocalIDIsNotSentBack(t *testing.T) {
	c := &geminiClient{}
	contents := c.convertMessages([]llm.Message{
		{Role: llm.RoleUser, Content: "read it"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: localCallID(0), Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: localCallID(0), ToolName: "read_file", Content: "contents"},
	})

	responses := functionResponses(contents)
	if len(responses) != 1 {
		t.Fatalf("got %d function responses, want 1", len(responses))
	}
	if responses[0].ID != "" {
		t.Errorf("sent id %q back to Gemini, which never issued it", responses[0].ID)
	}
	if responses[0].Name != "read_file" {
		t.Errorf("name = %q, want read_file", responses[0].Name)
	}
}

// Histories written before results carried a name still convert: the name
// falls back to whatever the id field holds, which is what it used to be.
func TestGeminiResultWithoutAToolNameStillConverts(t *testing.T) {
	c := &geminiClient{}
	contents := c.convertMessages([]llm.Message{
		{Role: llm.RoleUser, Content: "read it"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "read_file", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "read_file", Content: "contents"},
	})

	responses := functionResponses(contents)
	if len(responses) != 1 || responses[0].Name != "read_file" {
		t.Fatalf("responses = %+v, want one named read_file", responses)
	}
}
