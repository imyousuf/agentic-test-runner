package config

import (
	"testing"

	"github.com/spf13/viper"
)

func claudeConfig(model string) *Config {
	return &Config{
		Backend: "vertex-claude",
		Model:   model,
		Vertex:  VertexConfig{Project: "some-project"},
		Models: ModelsConfig{
			Flash:  "gemini-3.7-flash",
			Pro:    "gemini-3.2-pro-preview",
			Sonnet: "claude-sonnet-5",
			Opus:   "claude-opus-5",
		},
	}
}

func TestClaudeTierResolvesToAVertexModel(t *testing.T) {
	cases := map[string]string{
		"sonnet": "claude-sonnet-5",
		"opus":   "claude-opus-5",
		"Opus":   "claude-opus-5",
		// The stock config ships model: flash. Switching backend should not
		// also force a model change, so the Gemini tiers map across.
		"flash": "claude-sonnet-5",
		"pro":   "claude-opus-5",
		"":      "claude-sonnet-5",
	}
	for tier, want := range cases {
		if got := claudeConfig(tier).GetModelName(); got != want {
			t.Errorf("tier %q resolved to %q, want %q", tier, got, want)
		}
	}
}

func TestClaudeBackendSelectsTheClaudeProvider(t *testing.T) {
	cfg := claudeConfig("opus")
	got := cfg.GetLLMConfig()
	if got.Provider != "vertex-claude" {
		t.Errorf("Provider = %q", got.Provider)
	}
	if got.Model != "claude-opus-5" {
		t.Errorf("Model = %q, want the resolved Vertex model id", got.Model)
	}
	if got.Project != "some-project" {
		t.Errorf("Project = %q, want it carried through for ADC", got.Project)
	}
}

func TestClaudeBackendNeedsAProject(t *testing.T) {
	cfg := claudeConfig("sonnet")
	cfg.Vertex.Project = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected a validation error when no GCP project is configured")
	}
}

func TestClaudeBackendRejectsAGeminiModelName(t *testing.T) {
	cfg := claudeConfig("haiku")
	if err := cfg.Validate(); err == nil {
		t.Error("expected 'haiku' to be rejected: it is a claude-cli tier, not a Vertex one")
	}
}

func TestClaudeBackendIsNotACLIBackend(t *testing.T) {
	if claudeConfig("sonnet").IsCLIBackend() {
		t.Error("vertex-claude is an API backend; treating it as CLI would send it the raw tier name")
	}
}

// Without defaults the tier would resolve to an empty model name and every
// request would 404.
func TestClaudeModelDefaultsAreSet(t *testing.T) {
	v := viper.New()
	setDefaults(v)
	if got := v.GetString("models.sonnet"); got != "claude-sonnet-5" {
		t.Errorf("models.sonnet default = %q", got)
	}
	if got := v.GetString("models.opus"); got != "claude-opus-5" {
		t.Errorf("models.opus default = %q", got)
	}
}
