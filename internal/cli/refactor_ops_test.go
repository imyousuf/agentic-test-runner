package cli

import (
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
)

// --no-compile is what CI runs: replay the committed scripts, spend no model
// calls, write nothing. Hoisting did all three, so a replay job came back
// having called the model, rewritten two scripts and added a library — a
// modified working tree from a command whose whole promise is that it does not
// produce one.
func TestExtractionMode(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		noExtract  bool
		noCompile  bool
		want       agent.ExtractionMode
	}{
		{"default is to hoist", "", false, false, agent.ExtractAlways},
		{"unrecognised config falls back to hoisting", "sometimes", false, false, agent.ExtractAlways},
		{"configured on-demand is respected", "on-demand", false, false, agent.ExtractOnDemand},
		{"configured off is respected", "off", false, false, agent.ExtractOff},
		{"--no-extract wins over everything", "always", true, false, agent.ExtractOff},
		{"--no-compile reports but does not write", "always", false, true, agent.ExtractOnDemand},
		{"--no-compile does not switch off reporting", "on-demand", false, true, agent.ExtractOnDemand},
		{"--no-compile does not revive a disabled pass", "off", false, true, agent.ExtractOff},
		{"--no-extract still wins under --no-compile", "always", true, true, agent.ExtractOff},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractionMode(tt.configured, tt.noExtract, tt.noCompile); got != tt.want {
				t.Errorf("extractionMode(%q, noExtract=%v, noCompile=%v) = %q, want %q",
					tt.configured, tt.noExtract, tt.noCompile, got, tt.want)
			}
		})
	}
}
