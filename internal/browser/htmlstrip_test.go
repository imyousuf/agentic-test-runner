package browser

import (
	"strings"
	"testing"
)

func TestStripMarkup(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:     "removes script tags and content",
			input:    `<html><head><script>alert("xss")</script></head><body><p>Hello</p></body></html>`,
			contains: []string{"<p>Hello</p>"},
			excludes: []string{"<script>", "alert"},
		},
		{
			name:     "removes style tags and content",
			input:    `<html><head><style>body { color: red; }</style></head><body><p>Hello</p></body></html>`,
			contains: []string{"<p>Hello</p>"},
			excludes: []string{"<style>", "color: red"},
		},
		{
			name:     "removes style attributes",
			input:    `<div style="color: red; font-size: 12px"><p>Hello</p></div>`,
			contains: []string{"<div>", "<p>Hello</p>"},
			excludes: []string{"style=", "color: red"},
		},
		{
			name:     "removes on* event handlers",
			input:    `<button onclick="alert('hi')" onmouseover="foo()">Click</button>`,
			contains: []string{"<button>", "Click"},
			excludes: []string{"onclick", "onmouseover", "alert"},
		},
		{
			name:     "preserves structural markup",
			input:    `<div id="main" class="container"><h1>Title</h1><a href="/link">Link</a></div>`,
			contains: []string{`id="main"`, `class="container"`, "<h1>Title</h1>", `href="/link"`},
			excludes: []string{},
		},
		{
			name:     "preserves text content",
			input:    `<p>Some text with <strong>bold</strong> and <em>italic</em></p>`,
			contains: []string{"Some text with", "<strong>bold</strong>", "<em>italic</em>"},
			excludes: []string{},
		},
		{
			name:     "handles empty input",
			input:    "",
			contains: []string{},
			excludes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := StripMarkup(tt.input)
			if err != nil {
				t.Fatalf("StripMarkup() error = %v", err)
			}

			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("StripMarkup() result should contain %q, got %q", want, result)
				}
			}

			for _, notWant := range tt.excludes {
				if strings.Contains(result, notWant) {
					t.Errorf("StripMarkup() result should NOT contain %q, got %q", notWant, result)
				}
			}
		})
	}
}
