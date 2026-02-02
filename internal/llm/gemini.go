// Package llm provides LLM client implementations.
package llm

import (
	"context"
	"fmt"
	"os"

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

	resp, err := c.client.Models.GenerateContent(ctx, c.model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("generate content failed: %w", err)
	}

	return c.convertResponse(resp), nil
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

		// Handle tool responses
		if msg.Role == llm.RoleTool && msg.ToolCallID != "" {
			contents = append(contents, &genai.Content{
				Role: role,
				Parts: []*genai.Part{
					genai.NewPartFromFunctionResponse(msg.ToolCallID, map[string]any{
						"result": msg.Content,
					}),
				},
			})
			if len(msg.ImageData) > 0 {
				pendingImages = append(pendingImages, pendingImage{data: msg.ImageData, mime: msg.ImageMIME})
			}
			// Flush pending images when next message is not a tool response
			if len(pendingImages) > 0 && (i+1 >= len(messages) || messages[i+1].Role != llm.RoleTool) {
				var parts []*genai.Part
				parts = append(parts, genai.NewPartFromText("Here are the screenshot images from the tool results above:"))
				for _, img := range pendingImages {
					parts = append(parts, genai.NewPartFromBytes(img.data, img.mime))
				}
				contents = append(contents, &genai.Content{
					Role:  "user",
					Parts: parts,
				})
				pendingImages = nil
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
					ID:        part.FunctionCall.Name, // Use name as ID for Gemini
					Name:      part.FunctionCall.Name,
					Arguments: part.FunctionCall.Args,
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
