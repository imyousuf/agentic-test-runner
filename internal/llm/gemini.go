// Package llm provides LLM client implementations.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"google.golang.org/genai"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

func init() {
	// Register both Gemini and Vertex AI providers
	llm.RegisterProvider(llm.ProviderGemini, newGeminiClient)
	llm.RegisterProvider(llm.ProviderVertexAI, newGeminiClient)
}

// geminiClient implements llm.Client using Google's GenAI SDK.
type geminiClient struct {
	client   *genai.Client
	model    string
	provider llm.Provider
	temp     float32
}

// newGeminiClient creates a new Gemini client.
func newGeminiClient(ctx context.Context, cfg llm.Config) (llm.Client, error) {
	var clientCfg *genai.ClientConfig

	switch cfg.Provider {
	case llm.ProviderGemini:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("API key required for Gemini provider")
		}
		clientCfg = &genai.ClientConfig{
			APIKey:  cfg.APIKey,
			Backend: genai.BackendGeminiAPI,
		}
	case llm.ProviderVertexAI:
		if cfg.Project == "" {
			return nil, fmt.Errorf("project required for Vertex AI provider")
		}
		location := cfg.Location
		if location == "" {
			location = "us-central1"
		}
		// Set credentials file if provided
		if cfg.CredentialsFile != "" {
			if err := os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", cfg.CredentialsFile); err != nil {
				return nil, fmt.Errorf("failed to set credentials: %w", err)
			}
		}
		clientCfg = &genai.ClientConfig{
			Project:  cfg.Project,
			Location: location,
			Backend:  genai.BackendVertexAI,
		}
	default:
		return nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}

	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	temp := cfg.Temperature
	if temp == 0 {
		temp = 0.2 // Default temperature
	}

	return &geminiClient{
		client:   client,
		model:    cfg.Model,
		provider: cfg.Provider,
		temp:     temp,
	}, nil
}

// Chat sends messages to the LLM and returns a response.
func (c *geminiClient) Chat(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	contents := c.convertMessages(messages)
	genaiTools := c.convertTools(tools)

	config := &genai.GenerateContentConfig{
		Temperature: genai.Ptr(c.temp),
	}

	// Only add tools if we have any
	if len(genaiTools) > 0 {
		config.Tools = genaiTools
		config.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAuto,
			},
		}
	}

	// Debug: log the contents being sent
	for ci, content := range contents {
		log.Printf("[DEBUG gemini] Content[%d] role=%s parts=%d", ci, content.Role, len(content.Parts))
		for pi, part := range content.Parts {
			if part.Text != "" {
				truncated := part.Text
				if len(truncated) > 200 {
					truncated = truncated[:200] + "..."
				}
				log.Printf("[DEBUG gemini]   Part[%d] text=%q", pi, truncated)
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				log.Printf("[DEBUG gemini]   Part[%d] functionCall=%s args=%s", pi, part.FunctionCall.Name, string(argsJSON))
			}
			if part.FunctionResponse != nil {
				log.Printf("[DEBUG gemini]   Part[%d] functionResponse=%s", pi, part.FunctionResponse.Name)
			}
			if part.InlineData != nil {
				log.Printf("[DEBUG gemini]   Part[%d] inlineData mime=%s len=%d", pi, part.InlineData.MIMEType, len(part.InlineData.Data))
			}
		}
	}

	resp, err := c.client.Models.GenerateContent(ctx, c.model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("generate content failed: %w", err)
	}

	llmResp := c.convertResponse(resp)
	log.Printf("[DEBUG gemini] Response: content=%d chars, toolCalls=%d, finish=%s", len(llmResp.Content), len(llmResp.ToolCalls), llmResp.FinishReason)
	for i, tc := range llmResp.ToolCalls {
		log.Printf("[DEBUG gemini]   ToolCall[%d] name=%s id=%s", i, tc.Name, tc.ID)
	}

	return llmResp, nil
}

// ChatWithHistory is like Chat but allows providing conversation history.
func (c *geminiClient) ChatWithHistory(ctx context.Context, history []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	return c.Chat(ctx, history, tools)
}

// Model returns the model name being used.
func (c *geminiClient) Model() string {
	return c.model
}

// Provider returns the provider type.
func (c *geminiClient) Provider() llm.Provider {
	return c.provider
}

// Close releases resources.
func (c *geminiClient) Close() error {
	// The genai client doesn't have a Close method currently
	return nil
}

// convertMessages converts llm.Message to genai.Content.
func (c *geminiClient) convertMessages(messages []llm.Message) []*genai.Content {
	var contents []*genai.Content
	// Collect image data from tool responses; emit as a separate user message
	// after the entire batch of tool responses so we don't break the Gemini
	// constraint that function response part count must match function call count.
	var pendingImages []pendingImage
	var funcParts []*genai.Part

	for i, msg := range messages {
		var role string
		switch msg.Role {
		case llm.RoleUser:
			role = "user"
		case llm.RoleAssistant:
			role = "model"
		case llm.RoleSystem:
			// Gemini handles system prompts differently, prepend to first user message
			role = "user"
		case llm.RoleTool:
			// Tool responses need special handling
			role = "user"
		default:
			role = "user"
		}

		// Handle tool responses — batch consecutive tool messages into one Content
		// so the function response part count matches the function call part count.
		if msg.Role == llm.RoleTool && (msg.ToolCallID != "" || msg.ToolName != "") {
			// Name is what Gemini matches on and is required; the id is what
			// disambiguates two calls to the same tool, and is sent only when
			// it is one Gemini issued.
			name := msg.ToolName
			if name == "" {
				name = msg.ToolCallID
			}
			resp := &genai.FunctionResponse{
				Name:     name,
				Response: map[string]any{"result": msg.Content},
			}
			if !isLocalCallID(msg.ToolCallID) {
				resp.ID = msg.ToolCallID
			}
			funcParts = append(funcParts, &genai.Part{FunctionResponse: resp})
			if len(msg.ImageData) > 0 {
				pendingImages = append(pendingImages, pendingImage{data: msg.ImageData, mime: msg.ImageMIME})
			}
			// Flush when next message is not a tool response
			if i+1 >= len(messages) || messages[i+1].Role != llm.RoleTool {
				contents = append(contents, &genai.Content{
					Role:  role,
					Parts: funcParts,
				})
				funcParts = nil
				if len(pendingImages) > 0 {
					var imgParts []*genai.Part
					imgParts = append(imgParts, genai.NewPartFromText("Here are the screenshot images from the tool results above:"))
					for _, img := range pendingImages {
						imgParts = append(imgParts, genai.NewPartFromBytes(img.data, img.mime))
					}
					contents = append(contents, &genai.Content{
						Role:  "user",
						Parts: imgParts,
					})
					pendingImages = nil
				}
			}
			continue
		}

		// Handle tool calls from assistant
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
			var parts []*genai.Part
			for _, tc := range msg.ToolCalls {
				part := genai.NewPartFromFunctionCall(tc.Name, tc.Arguments)
				// Include thought signature if present (Gemini 3+)
				if tc.ThoughtSignature != "" {
					part.ThoughtSignature = []byte(tc.ThoughtSignature)
				}
				parts = append(parts, part)
			}
			contents = append(contents, &genai.Content{
				Role:  "model",
				Parts: parts,
			})
			continue
		}

		// Regular text message
		parts := []*genai.Part{
			genai.NewPartFromText(msg.Content),
		}
		if len(msg.ImageData) > 0 {
			parts = append(parts, genai.NewPartFromBytes(msg.ImageData, msg.ImageMIME))
		}
		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: parts,
		})
	}

	return contents
}

type pendingImage struct {
	data []byte
	mime string
}

// convertTools converts llm.Tool to genai.Tool.
func (c *geminiClient) convertTools(tools []llm.Tool) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}

	var declarations []*genai.FunctionDeclaration

	for _, tool := range tools {
		declarations = append(declarations, &genai.FunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  c.convertSchema(tool.Parameters),
		})
	}

	return []*genai.Tool{
		{FunctionDeclarations: declarations},
	}
}

// convertSchema converts a JSON Schema map to genai.Schema.
func (c *geminiClient) convertSchema(params map[string]any) *genai.Schema {
	if params == nil {
		return nil
	}

	schema := &genai.Schema{}

	if t, ok := params["type"].(string); ok {
		switch t {
		case "object":
			schema.Type = genai.TypeObject
		case "string":
			schema.Type = genai.TypeString
		case "number":
			schema.Type = genai.TypeNumber
		case "integer":
			schema.Type = genai.TypeInteger
		case "boolean":
			schema.Type = genai.TypeBoolean
		case "array":
			schema.Type = genai.TypeArray
		}
	}

	if desc, ok := params["description"].(string); ok {
		schema.Description = desc
	}

	if props, ok := params["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]*genai.Schema)
		for name, prop := range props {
			if propMap, ok := prop.(map[string]any); ok {
				schema.Properties[name] = c.convertSchema(propMap)
			}
		}
	}

	if required, ok := params["required"].([]any); ok {
		for _, r := range required {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	if required, ok := params["required"].([]string); ok {
		schema.Required = required
	}

	if items, ok := params["items"].(map[string]any); ok {
		schema.Items = c.convertSchema(items)
	}

	return schema
}

// convertResponse converts genai response to llm.Response.
func (c *geminiClient) convertResponse(resp *genai.GenerateContentResponse) *llm.Response {
	response := &llm.Response{}

	if resp == nil || len(resp.Candidates) == 0 {
		return response
	}

	candidate := resp.Candidates[0]

	// Extract text content and function calls
	if candidate.Content != nil {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				response.Content += part.Text
			}
			if part.FunctionCall != nil {
				tc := llm.ToolCall{
					ID:        part.FunctionCall.ID,
					Name:      part.FunctionCall.Name,
					Arguments: part.FunctionCall.Args,
				}
				// Gemini does not always issue an id. One is synthesised so
				// that ATR can still tell two calls to the same tool apart in
				// its own bookkeeping; localCallID marks it as ours so it is
				// not echoed back as though the model had issued it.
				if tc.ID == "" {
					tc.ID = localCallID(len(response.ToolCalls))
				}
				// Capture thought signature if present (Gemini 3+)
				if len(part.ThoughtSignature) > 0 {
					tc.ThoughtSignature = string(part.ThoughtSignature)
				}
				response.ToolCalls = append(response.ToolCalls, tc)
			}
		}
	}

	// Extract finish reason
	if candidate.FinishReason != "" {
		response.FinishReason = string(candidate.FinishReason)
	}

	// Extract usage
	if resp.UsageMetadata != nil {
		response.Usage = &llm.Usage{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
		}
	}

	return response
}

// Gemini may return a function call with no id of its own. ATR needs some
// stable handle to pair a result with the call it answers — otherwise two
// calls to the same tool in one turn are indistinguishable — so it makes one
// up. The prefix keeps ours separable from an id the model issued, which
// matters on the way back: echoing an invented id to Gemini would be claiming
// it asked for something it did not.
const localCallIDPrefix = "atr-call-"

func localCallID(index int) string {
	return fmt.Sprintf("%s%d", localCallIDPrefix, index)
}

func isLocalCallID(id string) bool {
	return strings.HasPrefix(id, localCallIDPrefix)
}
