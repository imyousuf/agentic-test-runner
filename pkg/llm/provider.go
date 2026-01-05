package llm

// Provider represents an LLM provider.
type Provider string

const (
	// ProviderGemini is Google's Gemini API.
	ProviderGemini Provider = "gemini"

	// ProviderVertexAI is Google's Vertex AI.
	ProviderVertexAI Provider = "vertex-ai"

	// Future providers (not yet implemented):
	// ProviderAnthropic Provider = "anthropic"
	// ProviderOpenAI    Provider = "openai"
	// ProviderGroq      Provider = "groq"
	// ProviderLocal     Provider = "local"
)

// String returns the string representation of the provider.
func (p Provider) String() string {
	return string(p)
}

// IsValid checks if the provider is a known valid provider.
func (p Provider) IsValid() bool {
	switch p {
	case ProviderGemini, ProviderVertexAI:
		return true
	default:
		return false
	}
}

// RequiresAPIKey returns true if this provider requires an API key.
func (p Provider) RequiresAPIKey() bool {
	return p == ProviderGemini
}

// RequiresGCP returns true if this provider requires GCP configuration.
func (p Provider) RequiresGCP() bool {
	return p == ProviderVertexAI
}

// DefaultModel returns the default model for this provider.
func (p Provider) DefaultModel() string {
	switch p {
	case ProviderGemini, ProviderVertexAI:
		return "gemini-3.0-flash"
	default:
		return ""
	}
}
