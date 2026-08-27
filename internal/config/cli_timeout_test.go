package config

import (
	"testing"
	"time"
)

// cli.timeout was reported by `atr config show` and then ignored: the CLI
// clients hardcoded ten minutes, so a compile died at exactly 10m however high
// the setting was. The value has to reach the client, not just the display.
func TestCLITimeoutReachesTheClient(t *testing.T) {
	cfg := &Config{
		Backend: "claude-cli",
		Model:   "sonnet",
		CLI:     CLIConfig{Timeout: 30 * time.Minute},
	}

	if got := cfg.GetCLITimeout(); got != 30*time.Minute {
		t.Fatalf("GetCLITimeout = %v, want 30m", got)
	}
	if got := cfg.GetLLMConfig().Timeout; got != 30*time.Minute {
		t.Errorf("the LLM config carries Timeout = %v; the client would fall back to its own default", got)
	}
}

// An unset timeout must leave the provider's default in play rather than
// passing zero through as "no time at all".
func TestUnsetCLITimeoutFallsBackToTheDefault(t *testing.T) {
	cfg := &Config{Backend: "claude-cli", Model: "sonnet"}
	if got := cfg.GetLLMConfig().Timeout; got <= 0 {
		t.Errorf("Timeout = %v, want the default GetCLITimeout supplies", got)
	}
}
