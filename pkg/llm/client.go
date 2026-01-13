package llm

import (
	"context"
	"fmt"
)

// Client is the interface for interacting with LLM providers.
// Implementations of this interface can be used interchangeably,
// allowing easy switching between providers (Gemini, Anthropic, OpenAI, etc.).
type Client interface {
	// Chat sends messages to the LLM and returns a response.
	// The tools parameter defines functions the LLM can call.
	Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error)

	// ChatWithHistory is like Chat but allows providing conversation history
	// and tool results from previous interactions.
	ChatWithHistory(ctx context.Context, history []Message, tools []Tool) (*Response, error)

	// Model returns the name of the model being used.
	Model() string

	// Provider returns the provider type (e.g., "gemini", "anthropic").
	Provider() Provider

	// Close releases any resources held by the client.
	Close() error
}

// Config holds configuration for creating an LLM client.
type Config struct {
	// Provider specifies which LLM provider to use.
	Provider Provider

	// Model is the model name/ID to use (e.g., "gemini-3.0-flash").
	Model string

	// APIKey is the API key for authentication (for API-based providers).
	APIKey string

	// Project is the GCP project ID (for Vertex AI).
	Project string

	// Location is the GCP region (for Vertex AI).
	Location string

	// CredentialsFile is the path to a service account JSON key file (for Vertex AI).
	CredentialsFile string

	// Temperature controls randomness in responses (0.0 to 1.0).
	Temperature float32

	// MaxTokens limits the response length.
	MaxTokens int

	// SystemPrompt is an optional system prompt to prepend to conversations.
	SystemPrompt string

	// CDPEndpoint is the browser CDP WebSocket URL for CLI backends.
	// This allows CLI tools to connect to an existing browser instance.
	CDPEndpoint string

	// Verbose enables debug logging for CLI backends.
	Verbose bool
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if c.Model == "" {
		return fmt.Errorf("model is required")
	}
	return nil
}

// ClientFactory is a function type for creating LLM clients.
// This allows registering custom providers.
type ClientFactory func(ctx context.Context, cfg Config) (Client, error)

// registry holds registered client factories.
var registry = make(map[Provider]ClientFactory)

// RegisterProvider registers a client factory for a provider.
// This should be called in init() functions of provider implementations.
func RegisterProvider(provider Provider, factory ClientFactory) {
	registry[provider] = factory
}

// NewClient creates a new LLM client based on the configuration.
// The provider must be registered using RegisterProvider.
func NewClient(ctx context.Context, cfg Config) (Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	factory, ok := registry[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s (available: %v)", cfg.Provider, availableProviders())
	}

	return factory(ctx, cfg)
}

// availableProviders returns a list of registered providers.
func availableProviders() []Provider {
	providers := make([]Provider, 0, len(registry))
	for p := range registry {
		providers = append(providers, p)
	}
	return providers
}

// IsProviderRegistered checks if a provider is registered.
func IsProviderRegistered(provider Provider) bool {
	_, ok := registry[provider]
	return ok
}
