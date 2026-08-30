package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/vertex"
	"golang.org/x/oauth2/google"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

func init() {
	llm.RegisterProvider(llm.ProviderVertexClaude, newVertexClaudeClient)
}

// Claude on Vertex AI, authenticated with Application Default Credentials.
//
// This exists as the alternative to the Claude CLI backend: the same models,
// reached over the Messages API rather than by shelling out to a subprocess
// and parsing its stdout.
//
// Prompt caching is the reason it is worth having. ATR's agent loop re-sends
// the whole conversation on every iteration, and the fixed prefix — the tool
// schemas plus the system prompt — is the largest constant part of it. The
// HUD agent alone carries 24 tool definitions. Marking that prefix once means
// every iteration after the first reads it from cache instead of paying for
// it again.
//
// A freshly written entry takes a few seconds to become readable, so on a cold
// cache the first iteration or two may rewrite it before the reads start
// landing. Every iteration after that reads.

// defaultMaxTokens bounds a reply when the caller does not say.
const defaultMaxTokens = 8192

// defaultLocation is where Claude models are served from when the config does
// not name a region. "global" lets Vertex route the request itself.
const defaultLocation = "global"

// googleCloudScope is the OAuth scope ADC is exchanged for.
const googleCloudScope = "https://www.googleapis.com/auth/cloud-platform"

type vertexClaudeClient struct {
	client       anthropic.Client
	model        string
	temperature  float32
	maxTokens    int
	systemPrompt string
	verbose      bool

	// noTemperature records that this model rejected the temperature
	// parameter. Newer Claude models have dropped it, older ones still take
	// it, and which is which changes with every release — so rather than
	// matching on model names, the first rejection is learned and the
	// parameter is left out from then on.
	noTemperature atomic.Bool
}

// vertexAuth resolves Application Default Credentials into a request option.
//
// The SDK's WithGoogleAuth panics when it cannot find credentials, so a machine
// that has never run `gcloud auth application-default login` got a Go stack
// trace instead of the one line that fixes it — and ATR already prints proper
// setup guidance for every other backend. Finding the credentials here removes
// that path and leaves an ordinary error.
//
// The recover covers what is left. WithCredentials panics too, on a transport
// it cannot build, and an empty region panics before either. A backend that
// will not start is a configuration problem, and a test runner reports those
// rather than crashing on them.
func vertexAuth(ctx context.Context, location, project string) (opt option.RequestOption, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			opt, err = nil, fmt.Errorf("configuring Claude on Vertex AI: %v", rec)
		}
	}()

	creds, credErr := google.FindDefaultCredentials(ctx, googleCloudScope)
	if credErr != nil {
		return nil, fmt.Errorf("no Google credentials for Claude on Vertex AI: %w\n"+
			"  Run: gcloud auth application-default login\n"+
			"  or set GOOGLE_APPLICATION_CREDENTIALS to a service account key file",
			credErr)
	}

	return vertex.WithCredentials(ctx, location, project, creds), nil
}

func newVertexClaudeClient(ctx context.Context, cfg llm.Config) (llm.Client, error) {
	if cfg.Project == "" {
		return nil, fmt.Errorf("project is required for Claude on Vertex AI")
	}

	location := cfg.Location
	if location == "" {
		location = defaultLocation
	}

	// Application Default Credentials: whatever gcloud auth application-default
	// login, a service account on the machine, or GOOGLE_APPLICATION_CREDENTIALS
	// provides. No API key, so nothing to leak into a config file.
	auth, err := vertexAuth(ctx, location, cfg.Project)
	if err != nil {
		return nil, err
	}
	opts := []option.RequestOption{auth}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	return &vertexClaudeClient{
		client:       anthropic.NewClient(opts...),
		model:        cfg.Model,
		temperature:  cfg.Temperature,
		maxTokens:    maxTokens,
		systemPrompt: cfg.SystemPrompt,
		// ATR_DEBUG_LLM covers the surfaces that build a client without a
		// --verbose flag to pass through: the REST daemon and the MCP server.
		verbose: cfg.Verbose || os.Getenv("ATR_DEBUG_LLM") != "",
	}, nil
}

func (c *vertexClaudeClient) Chat(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	return c.send(ctx, messages, tools)
}

func (c *vertexClaudeClient) ChatWithHistory(ctx context.Context, history []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	return c.send(ctx, history, tools)
}

func (c *vertexClaudeClient) send(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	msg, err := c.sendRaw(ctx, messages, tools)
	if err != nil {
		return nil, err
	}
	return c.toResponse(msg), nil
}

// sendRaw performs the request and returns the reply as the SDK gives it,
// usage numbers and all.
func (c *vertexClaudeClient) sendRaw(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*anthropic.Message, error) {
	params, err := c.buildParams(messages, tools)
	if err != nil {
		return nil, err
	}
	msg, err := c.client.Messages.New(ctx, params)
	if err == nil {
		return msg, nil
	}

	// Retry once without temperature if that is what the model objected to.
	if temperatureRejected(err) && !c.noTemperature.Swap(true) {
		if c.verbose {
			fmt.Printf("[DEBUG vertex-claude] %s rejects temperature; retrying without it\n", c.model)
		}
		retried, retryErr := c.buildParams(messages, tools)
		if retryErr != nil {
			return nil, retryErr
		}
		msg, err = c.client.Messages.New(ctx, retried)
		if err == nil {
			return msg, nil
		}
	}

	return nil, fmt.Errorf("claude on vertex ai: %w", err)
}

// temperatureRejected reports whether err is the API refusing the temperature
// parameter, as opposed to any other bad request.
func temperatureRejected(err error) bool {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return false
	}
	// RawJSON, not Error(): the SDK's Error() dereferences the request and
	// response it was built from, which a synthesised error need not carry.
	msg := strings.ToLower(apiErr.RawJSON())
	return strings.Contains(msg, "temperature") &&
		(strings.Contains(msg, "deprecated") ||
			strings.Contains(msg, "not supported") ||
			strings.Contains(msg, "unsupported"))
}

// buildParams assembles the request, including the cache checkpoint.
func (c *vertexClaudeClient) buildParams(messages []llm.Message, tools []llm.Tool) (anthropic.MessageNewParams, error) {
	converted, err := toAnthropicMessages(messages)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(c.maxTokens),
		Messages:  converted,
		Tools:     toAnthropicTools(tools),
	}
	if c.temperature > 0 && !c.noTemperature.Load() {
		params.Temperature = anthropic.Float(float64(c.temperature))
	}

	// One cache checkpoint, at the end of the request's fixed prefix.
	//
	// Anthropic caches everything from the start of the request up to and
	// including the marked block, and orders a request tools-then-system-then-
	// messages. So a single marker placed as late as the fixed part of the
	// request extends covers all of it.
	//
	// Where that is depends on the agent. Most of ATR's agents open with a
	// system prompt, and marking it covers every tool schema plus the prompt —
	// a prefix identical across separate atr invocations, so one run's cache
	// entry serves the next.
	//
	// The command-analysis loop has no system prompt: it puts its instructions
	// and the captured failure into the first user message. Marking the tool
	// schemas alone there would cover a few hundred tokens, below the size the
	// API will cache at all. Marking the end of that first message covers the
	// tools and the instructions and the failure context — which stay fixed
	// for the whole run, so every iteration after the first reads them back.
	//
	// Nothing later is marked. The conversation grows every turn, so a marker
	// there would write a fresh entry per iteration instead of reusing one.
	if system := systemPrompt(messages, c.systemPrompt); system != "" {
		params.System = []anthropic.TextBlockParam{{
			Text:         system,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}}
	} else if !markMessageCached(&params.Messages[0]) && len(params.Tools) > 0 {
		markLastToolCached(params.Tools)
	}

	return params, nil
}

// toResponse converts an Anthropic reply into ATR's response shape.
func (c *vertexClaudeClient) toResponse(msg *anthropic.Message) *llm.Response {
	var text strings.Builder
	var toolCalls []llm.ToolCall

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			call := llm.ToolCall{ID: block.ID, Name: block.Name}
			// Arguments arrive as raw JSON; a schema mismatch is the model's
			// problem to fix on the next turn, so an unparseable payload
			// becomes an empty argument set rather than a failed request.
			_ = call.UnmarshalArguments([]byte(block.Input))
			if call.Arguments == nil {
				call.Arguments = map[string]any{}
			}
			toolCalls = append(toolCalls, call)
		}
	}

	// Cached reads and cache writes are both prompt tokens as far as the
	// caller is concerned; counting them keeps PromptTokens comparable with
	// the other providers.
	prompt := int(msg.Usage.InputTokens + msg.Usage.CacheCreationInputTokens + msg.Usage.CacheReadInputTokens)
	completion := int(msg.Usage.OutputTokens)

	if c.verbose {
		fmt.Printf("[DEBUG vertex-claude] model=%s in=%d out=%d cache_read=%d cache_write=%d tools=%d\n",
			c.model, prompt, completion,
			msg.Usage.CacheReadInputTokens, msg.Usage.CacheCreationInputTokens, len(toolCalls))
	}

	return &llm.Response{
		Content:      text.String(),
		ToolCalls:    toolCalls,
		FinishReason: string(msg.StopReason),
		Usage: &llm.Usage{
			PromptTokens:     prompt,
			CompletionTokens: completion,
			TotalTokens:      prompt + completion,
		},
	}
}

// markMessageCached puts the cache checkpoint on a message's last block,
// reporting whether it found one that can carry it.
func markMessageCached(msg *anthropic.MessageParam) bool {
	for i := len(msg.Content) - 1; i >= 0; i-- {
		switch block := msg.Content[i]; {
		case block.OfText != nil:
			block.OfText.CacheControl = anthropic.NewCacheControlEphemeralParam()
			return true
		case block.OfImage != nil:
			block.OfImage.CacheControl = anthropic.NewCacheControlEphemeralParam()
			return true
		case block.OfToolResult != nil:
			block.OfToolResult.CacheControl = anthropic.NewCacheControlEphemeralParam()
			return true
		}
	}
	return false
}

// markLastToolCached puts the cache checkpoint on the final tool, for the case
// where there is nothing else to carry it.
func markLastToolCached(tools []anthropic.ToolUnionParam) {
	for i := len(tools) - 1; i >= 0; i-- {
		if tools[i].OfTool != nil {
			tools[i].OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
			return
		}
	}
}

func (c *vertexClaudeClient) Model() string          { return c.model }
func (c *vertexClaudeClient) Provider() llm.Provider { return llm.ProviderVertexClaude }
func (c *vertexClaudeClient) Close() error           { return nil }
