package cli

import (
	"fmt"
	"strings"
	"testing"
)

func TestShellSplit(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{"simple", "navigate https://example.com", []string{"navigate", "https://example.com"}, false},
		{"double quotes", `eval "document.title"`, []string{"eval", "document.title"}, false},
		{"single quotes", `eval 'document.title'`, []string{"eval", "document.title"}, false},
		{"quoted spaces", `fill "#email" "user@example.com"`, []string{"fill", "#email", "user@example.com"}, false},
		{"flags", `wait "#modal" --timeout 5000 --visible`, []string{"wait", "#modal", "--timeout", "5000", "--visible"}, false},
		{"empty", "", nil, false},
		{"escaped quote", `eval "it's a test"`, []string{"eval", "it's a test"}, false},
		{"unclosed quote", `eval "unclosed`, nil, true},
		{"complex selector", `computed-styles "main > section:nth-child(2)"`, []string{"computed-styles", "main > section:nth-child(2)"}, false},
		{"backslash escape", `eval "line1\nline2"`, []string{"eval", "line1nline2"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := shellSplit(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("shellSplit(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got) != len(tt.want) {
				t.Errorf("shellSplit(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("shellSplit(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestInterpolateVars(t *testing.T) {
	vars := map[string]string{
		"cardCount": "6",
		"activeId":  "payments",
		"empty":     "",
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"no vars", "navigate https://example.com", "navigate https://example.com", false},
		{"single var", `eval "found [[cardCount]] cards"`, `eval "found 6 cards"`, false},
		{"multiple vars", `eval "[[activeId]] has [[cardCount]] items"`, `eval "payments has 6 items"`, false},
		{"empty var", `eval "value: [[empty]]"`, `eval "value: "`, false},
		{"undefined var", `eval "[[undefined]]"`, `eval "[[undefined]]"`, true},
		{"nested in quotes", `eval "document.querySelector('[data-id=\"[[activeId]]\"]')"`, `eval "document.querySelector('[data-id=\"payments\"]')"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := interpolateVars(tt.input, vars)
			if (err != nil) != tt.wantErr {
				t.Errorf("interpolateVars() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("interpolateVars(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractJSONPath(t *testing.T) {
	data := map[string]interface{}{
		"result": "hello",
		"styles": map[string]interface{}{
			"fontSize":   "32px",
			"fontWeight": "700",
		},
		"elements": []interface{}{
			map[string]interface{}{"text": "first"},
			map[string]interface{}{"text": "second"},
		},
		"count": float64(42),
	}

	tests := []struct {
		name string
		path string
		want interface{}
	}{
		{"root", "$", data},
		{"simple field", "$.result", "hello"},
		{"nested field", "$.styles.fontSize", "32px"},
		{"array index", "$.elements[0]", data["elements"].([]interface{})[0]},
		{"array field", "$.elements[1].text", "second"},
		{"number", "$.count", float64(42)},
		{"missing field", "$.nonexistent", nil},
		{"missing nested", "$.styles.missing", nil},
		{"bad index", "$.elements[99]", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONPath(data, tt.path)
			if got == nil && tt.want == nil {
				return
			}
			if got == nil || tt.want == nil {
				t.Errorf("extractJSONPath(%q) = %v, want %v", tt.path, got, tt.want)
				return
			}
			// Compare as strings for simplicity
			gotStr := toStr(got)
			wantStr := toStr(tt.want)
			if gotStr != wantStr {
				t.Errorf("extractJSONPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func toStr(v interface{}) string {
	return fmt.Sprintf("%v", v)
}

func TestHandleLet(t *testing.T) {
	data := map[string]interface{}{
		"result": "42",
		"styles": map[string]interface{}{
			"fontSize": "32px",
		},
	}
	vars := make(map[string]string)

	// Basic let
	err := handleLet("let count = $.result", data, vars)
	if err != nil {
		t.Errorf("handleLet basic: %v", err)
	}
	if vars["count"] != "42" {
		t.Errorf("vars[count] = %q, want 42", vars["count"])
	}

	// Nested let
	err = handleLet("let size = $.styles.fontSize", data, vars)
	if err != nil {
		t.Errorf("handleLet nested: %v", err)
	}
	if vars["size"] != "32px" {
		t.Errorf("vars[size] = %q, want 32px", vars["size"])
	}

	// Missing path warns but doesn't fatal
	err = handleLet("let missing = $.nonexistent", data, vars)
	if err == nil {
		t.Error("expected warning for missing path")
	}
	if vars["missing"] != "" {
		t.Errorf("vars[missing] = %q, want empty", vars["missing"])
	}

	// Invalid syntax
	err = handleLet("let = bad", data, vars)
	if err == nil {
		t.Error("expected error for invalid syntax")
	}
}

func TestBuildLetContext(t *testing.T) {
	// Simulate an eval command result: rawData has "result", Output has the normalized value
	r := batchResult{
		Index:   0,
		Command: `eval "document.querySelectorAll('section').length"`,
		Status:  "ok",
		Output:  float64(8), // eval output is the "result" field from API
		rawData: map[string]interface{}{
			"result": float64(8),
		},
	}

	ctx := buildLetContext(r)
	ctxMap, ok := ctx.(map[string]interface{})
	if !ok {
		t.Fatalf("buildLetContext returned %T, want map", ctx)
	}

	// $.output should be the normalized output
	if ctxMap["output"] != float64(8) {
		t.Errorf("$.output = %v, want 8", ctxMap["output"])
	}

	// $.result should still work (raw API field)
	if ctxMap["result"] != float64(8) {
		t.Errorf("$.result = %v, want 8", ctxMap["result"])
	}

	// Test let extraction with $.output
	vars := make(map[string]string)
	err := handleLet("let count = $.output", ctx, vars)
	if err != nil {
		t.Errorf("handleLet $.output: %v", err)
	}
	if vars["count"] != "8" {
		t.Errorf("vars[count] = %q, want '8'", vars["count"])
	}

	// Test let extraction with $.result (raw API path)
	err = handleLet("let count2 = $.result", ctx, vars)
	if err != nil {
		t.Errorf("handleLet $.result: %v", err)
	}
	if vars["count2"] != "8" {
		t.Errorf("vars[count2] = %q, want '8'", vars["count2"])
	}
}

func TestBuildLetContext_NestedOutput(t *testing.T) {
	// Simulate computed-styles: output is a map of styles
	r := batchResult{
		Output: map[string]interface{}{
			"fontSize":   "32px",
			"fontWeight": "700",
		},
		rawData: map[string]interface{}{
			"selector": "h1",
			"styles": map[string]interface{}{
				"fontSize":   "32px",
				"fontWeight": "700",
			},
			"count": float64(2),
		},
	}

	ctx := buildLetContext(r)
	vars := make(map[string]string)

	// $.output.fontSize should work
	err := handleLet("let size = $.output.fontSize", ctx, vars)
	if err != nil {
		t.Errorf("handleLet $.output.fontSize: %v", err)
	}
	if vars["size"] != "32px" {
		t.Errorf("vars[size] = %q, want '32px'", vars["size"])
	}

	// $.styles.fontWeight should also work (raw path)
	err = handleLet("let weight = $.styles.fontWeight", ctx, vars)
	if err != nil {
		t.Errorf("handleLet $.styles.fontWeight: %v", err)
	}
	if vars["weight"] != "700" {
		t.Errorf("vars[weight] = %q, want '700'", vars["weight"])
	}
}

func TestContainsFlag(t *testing.T) {
	args := []string{"#modal", "--timeout", "5000", "--visible"}
	if !containsFlag(args, "--visible") {
		t.Error("expected --visible to be found")
	}
	if containsFlag(args, "--full") {
		t.Error("expected --full to not be found")
	}
}

func TestGetFlagValue(t *testing.T) {
	args := []string{"#modal", "--timeout", "5000", "--visible"}

	val, ok := getFlagValue(args, "--timeout")
	if !ok || val != "5000" {
		t.Errorf("getFlagValue(--timeout) = %q, %v; want 5000, true", val, ok)
	}

	_, ok = getFlagValue(args, "--missing")
	if ok {
		t.Error("expected --missing to not be found")
	}

	// Test --flag=value form
	args2 := []string{"--timeout=3000"}
	val, ok = getFlagValue(args2, "--timeout")
	if !ok || val != "3000" {
		t.Errorf("getFlagValue(--timeout=3000) = %q, %v; want 3000, true", val, ok)
	}
}

func TestReadBatchInput(t *testing.T) {
	input := `
# This is a comment
navigate https://example.com

wait "h1" --timeout 5000
# Another comment

screenshot --file
`
	lines, err := readBatchInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("readBatchInput error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "navigate https://example.com" {
		t.Errorf("line[0] = %q", lines[0])
	}
	if lines[1] != `wait "h1" --timeout 5000` {
		t.Errorf("line[1] = %q", lines[1])
	}
	if lines[2] != "screenshot --file" {
		t.Errorf("line[2] = %q", lines[2])
	}
}

func TestDispatchBatchCommand(t *testing.T) {
	tests := []struct {
		name       string
		cmd        string
		wantMethod string
		wantErr    bool
	}{
		{"navigate", `navigate "https://example.com"`, "POST", false},
		{"eval", `eval "document.title"`, "POST", false},
		{"screenshot file", "screenshot --file", "GET", false},
		{"screenshot full", "screenshot --file --full", "GET", false},
		{"click", `click "#button"`, "POST", false},
		{"fill", `fill "#email" "test@example.com"`, "POST", false},
		{"wait", `wait "#modal" --timeout 5000`, "POST", false},
		{"wait visible", `wait "#modal" --visible`, "POST", false},
		{"scroll", `scroll --selector "#modal" --y 500`, "POST", false},
		{"computed-styles", `computed-styles "h1"`, "GET", false},
		{"url", "url", "GET", false},
		{"title", "title", "GET", false},
		{"html", "html", "GET", false},
		{"back", "back", "POST", false},
		{"unknown", "foobar", "", true},
		{"empty", "", "", true},
		{"navigate no url", "navigate", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, _, _, _, err := dispatchBatchCommand(tt.cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("dispatchBatchCommand(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
				return
			}
			if !tt.wantErr && method != tt.wantMethod {
				t.Errorf("dispatchBatchCommand(%q) method = %q, want %q", tt.cmd, method, tt.wantMethod)
			}
		})
	}
}
