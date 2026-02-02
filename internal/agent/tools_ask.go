package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
)

// AskSnapshotTool returns the accessibility tree as text.
type AskSnapshotTool struct {
	browser *browser.Browser
}

func (t *AskSnapshotTool) Name() string { return "snapshot" }

func (t *AskSnapshotTool) Description() string {
	return "Get the accessibility tree of the current page. Returns interactive elements with their UIDs, roles, names, and values."
}

func (t *AskSnapshotTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *AskSnapshotTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	elements, err := t.browser.Snapshot(false)
	if err != nil {
		return fmt.Sprintf("Snapshot failed: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Page: %s\n", t.browser.CurrentURL()))
	sb.WriteString(fmt.Sprintf("Title: %s\n\n", t.browser.PageTitle()))

	for _, el := range elements {
		if !el.Visible {
			continue
		}
		line := fmt.Sprintf("[%s] %s", el.UID, el.TagName)
		if el.Role != "" {
			line += fmt.Sprintf(" role=%s", el.Role)
		}
		if el.Name != "" {
			line += fmt.Sprintf(" '%s'", el.Name)
		}
		if el.Value != "" {
			line += fmt.Sprintf(" value='%s'", el.Value)
		}
		sb.WriteString(line + "\n")
	}

	return sb.String(), false
}

// AskScreenshotTool captures a screenshot and saves to a temp file.
type AskScreenshotTool struct {
	browser *browser.Browser
}

func (t *AskScreenshotTool) Name() string { return "screenshot" }

func (t *AskScreenshotTool) Description() string {
	return "Take a screenshot of the current page. Returns the file path to the saved PNG image."
}

func (t *AskScreenshotTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *AskScreenshotTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	text, _, _, isErr := t.ExecuteWithImage(ctx, args)
	return text, isErr
}

func (t *AskScreenshotTool) ExecuteWithImage(ctx context.Context, args map[string]any) (string, []byte, string, bool) {
	data, err := t.browser.Screenshot(false)
	if err != nil {
		return fmt.Sprintf("Screenshot failed: %v", err), nil, "", true
	}

	filename := fmt.Sprintf("/tmp/ask-screenshot-%d.png", os.Getpid())
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Sprintf("Failed to save screenshot: %v", err), nil, "", true
	}

	return fmt.Sprintf("Screenshot saved to %s", filename), data, "image/png", false
}

// AskRawHTMLTool returns HTML with scripts/styles/event handlers stripped.
type AskRawHTMLTool struct {
	browser *browser.Browser
}

func (t *AskRawHTMLTool) Name() string { return "raw_html_only" }

func (t *AskRawHTMLTool) Description() string {
	return "Get the page HTML with scripts, styles, and event handlers stripped. Preserves structural markup for understanding the page layout and content."
}

func (t *AskRawHTMLTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *AskRawHTMLTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	rawHTML, err := t.browser.HTML()
	if err != nil {
		return fmt.Sprintf("Failed to get HTML: %v", err), true
	}

	cleaned, err := browser.StripMarkup(rawHTML)
	if err != nil {
		return fmt.Sprintf("Failed to strip markup: %v", err), true
	}

	return cleaned, false
}

// AskFullMarkupTool returns the full HTML with a token warning.
type AskFullMarkupTool struct {
	browser *browser.Browser
}

func (t *AskFullMarkupTool) Name() string { return "full_markup" }

func (t *AskFullMarkupTool) Description() string {
	return "Get the complete raw HTML of the page including scripts and styles. WARNING: This can be very large and use many tokens. Prefer raw_html_only or snapshot unless you specifically need scripts/styles."
}

func (t *AskFullMarkupTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *AskFullMarkupTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	rawHTML, err := t.browser.HTML()
	if err != nil {
		return fmt.Sprintf("Failed to get HTML: %v", err), true
	}

	return "WARNING: Full HTML follows. This may be very large.\n\n" + rawHTML, false
}

// NewAskTools returns the 4 tools available to the ask sub-agent.
func NewAskTools(b *browser.Browser) []Tool {
	return []Tool{
		&AskSnapshotTool{browser: b},
		&AskScreenshotTool{browser: b},
		&AskRawHTMLTool{browser: b},
		&AskFullMarkupTool{browser: b},
	}
}

