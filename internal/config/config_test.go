package config

import (
	"strings"
	"testing"
)

// The only reason to set this key is to restrain something. An unrecognised
// value used to fall back to "always" — so somebody who wrote "none" or
// "disabled", meaning to switch hoisting off, would get the most eager setting
// and find their scripts rewritten. Refusing to guess is the point.
func TestAnUnrecognisedExtractOperationsIsRefused(t *testing.T) {
	base := func() *Config {
		return &Config{
			Backend: "vertex-ai",
			Model:   "pro",
			Vertex:  VertexConfig{Project: "p"},
		}
	}

	for _, ok := range []string{"", "always", "on-demand", "off"} {
		c := base()
		c.Behavior.ExtractOperations = ok
		if err := c.Validate(); err != nil {
			t.Errorf("extract_operations %q was refused: %v", ok, err)
		}
	}

	for _, bad := range []string{"none", "disabled", "on demand", "true", "never"} {
		c := base()
		c.Behavior.ExtractOperations = bad
		err := c.Validate()
		if err == nil {
			t.Errorf("extract_operations %q was accepted and silently means 'always'", bad)
			continue
		}
		if !strings.Contains(err.Error(), "on-demand") {
			t.Errorf("the error for %q does not say what is valid: %v", bad, err)
		}
	}
}
