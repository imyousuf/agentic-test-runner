package llm

// Provider represents an LLM provider.
type Provider string

const (
	// ProviderGemini is Google's Gemini API.
	ProviderGemini Provider = "gemini"

	// ProviderVertexAI is Google's Vertex AI.
	ProviderVertexAI Provider = "vertex-ai"

	// ProviderVertexClaude is Claude (Sonnet/Opus) served by Vertex AI and
	// authenticated with Application Default Credentials. This is the
	// non-subprocess alternative to the Claude CLI backend.
	ProviderVertexClaude Provider = "vertex-claude"

	// ProviderClaudeCLI is Claude CLI backend.
	ProviderClaudeCLI Provider = "claude-cli"

	// ProviderGeminiCLI is Gemini CLI backend.
	ProviderGeminiCLI Provider = "gemini-cli"

	// Future providers (not yet implemented):
	// ProviderAnthropic Provider = "anthropic"
	// ProviderOpenAI    Provider = "openai"
	// ProviderGroq      Provider = "groq"
	// ProviderLocal     Provider = "local"
)

// CLIPriority is the order of preference for CLI backends.
var CLIPriority = []Provider{
	ProviderClaudeCLI,
	ProviderGeminiCLI,
}

// String returns the string representation of the provider.
func (p Provider) String() string {
	return string(p)
}

// IsValid checks if the provider is a known valid provider.
func (p Provider) IsValid() bool {
	switch p {
	case ProviderGemini, ProviderVertexAI, ProviderVertexClaude, ProviderClaudeCLI, ProviderGeminiCLI:
		return true
	default:
		return false
	}
}

// IsCLI returns true if this provider is a CLI-based backend.
func (p Provider) IsCLI() bool {
	switch p {
	case ProviderClaudeCLI, ProviderGeminiCLI:
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

// RequiresCLI returns true if this provider requires a CLI tool to be installed.
func (p Provider) RequiresCLI() bool {
	return p.IsCLI()
}

// CLIExecutable returns the CLI executable name for CLI-based providers.
func (p Provider) CLIExecutable() string {
	switch p {
	case ProviderClaudeCLI:
		return "claude"
	case ProviderGeminiCLI:
		return "gemini"
	default:
		return ""
	}
}

// DefaultModel returns the default model for this provider.
func (p Provider) DefaultModel() string {
	switch p {
	case ProviderGemini, ProviderVertexAI:
		return "gemini-3.7-flash"
	case ProviderClaudeCLI:
		return "" // CLI uses its own default model
	case ProviderGeminiCLI:
		return "" // CLI uses its own default model
	default:
		return ""
	}
}
