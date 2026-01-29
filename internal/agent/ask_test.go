package agent

import (
	"testing"
)

func TestNewAskAgent_Defaults(t *testing.T) {
	a := NewAskAgent(AskConfig{})

	if a.maxIterations != 5 {
		t.Errorf("expected maxIterations=5, got %d", a.maxIterations)
	}

	if a.timeout.Seconds() != 60 {
		t.Errorf("expected timeout=60s, got %v", a.timeout)
	}

	// Verify 4 tools registered
	tools := a.registry.All()
	if len(tools) != 4 {
		t.Errorf("expected 4 tools, got %d", len(tools))
	}

	expectedNames := map[string]bool{
		"snapshot":      false,
		"screenshot":    false,
		"raw_html_only": false,
		"full_markup":   false,
	}

	for _, tool := range tools {
		name := tool.Name()
		if _, ok := expectedNames[name]; !ok {
			t.Errorf("unexpected tool: %s", name)
		}
		expectedNames[name] = true
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestNewAskAgent_CustomConfig(t *testing.T) {
	a := NewAskAgent(AskConfig{
		MaxIterations: 10,
		Timeout:       30_000_000_000, // 30s in nanoseconds
	})

	if a.maxIterations != 10 {
		t.Errorf("expected maxIterations=10, got %d", a.maxIterations)
	}

	if a.timeout.Seconds() != 30 {
		t.Errorf("expected timeout=30s, got %v", a.timeout)
	}
}
