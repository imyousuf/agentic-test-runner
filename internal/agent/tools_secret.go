package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/secret"
)

// BrowserFillSecretTool fills a field with the output of a command, without
// that output ever passing through the model.
//
// This exists because the obvious alternative leaks. If the model runs
// `pass show github/password` through the shell tool and then calls
// browser_fill with the result, the plaintext becomes a tool result, joins
// the message history, and is re-sent to the model provider on every
// subsequent turn of the conversation. Here the fetch and the fill happen
// inside one tool call: the model chooses the command and the field, ATR runs
// the command and types the output, and the only thing that comes back is
// whether it worked.
type BrowserFillSecretTool struct {
	browser *browser.Browser
	vault   *secret.Vault
}

// NewBrowserFillSecretTool creates the secret-fill tool.
func NewBrowserFillSecretTool(b *browser.Browser, v *secret.Vault) *BrowserFillSecretTool {
	return &BrowserFillSecretTool{browser: b, vault: v}
}

func (t *BrowserFillSecretTool) Name() string { return "browser_fill_secret" }

func (t *BrowserFillSecretTool) Description() string {
	desc := `Fill a password or other secret into a field WITHOUT ever seeing its value.

Give the target field and either a configured "ref" or a "command" that prints
the secret on stdout. ATR runs the command itself, types the output into the
field, and tells you only whether it succeeded — the value is never shown to
you and never enters this conversation.

Use this for every password, token, API key or other credential. Do NOT run
the password manager through the shell tool and pass the result to
browser_fill: that would put the plaintext into the conversation history.

Example command: ` + secret.ExampleCommand()

	if refs := t.vault.Refs(); len(refs) > 0 {
		desc += "\n\nConfigured refs: " + strings.Join(refs, ", ")
	}
	return desc
}

func (t *BrowserFillSecretTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "Element identifier: UID, placeholder, name, label text, or CSS selector",
			},
			"ref": map[string]any{
				"type":        "string",
				"description": "Name of a secret configured in ~/.atr/config.yaml under secrets.refs. Use this when the user names an entry rather than a command.",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command that prints the secret on stdout, e.g. \"" + secret.ExampleCommand() + "\". Use when there is no configured ref.",
			},
		},
		"required": []string{"target"},
	}
}

func (t *BrowserFillSecretTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	target, _ := args["target"].(string)
	if target == "" {
		return "Missing required parameter: target", true
	}

	ref, _ := args["ref"].(string)
	command, _ := args["command"].(string)

	value, err := t.vault.Fetch(ctx, secret.Request{Ref: ref, Command: command})
	if err != nil {
		// The error names the ref or says "the supplied command"; it never
		// carries the command's stdout.
		return fmt.Sprintf("Could not obtain the secret: %v", err), true
	}

	if err := t.browser.Fill(ctx, target, value); err != nil {
		// browser.Fill echoes neither the value nor the element contents on
		// failure, but redact anyway: this string goes into the history.
		return t.vault.Redact(fmt.Sprintf("Fill failed: %v", err)), true
	}

	source := "the supplied command"
	if ref != "" {
		source = fmt.Sprintf("ref %q", ref)
	}
	return fmt.Sprintf("Filled the secret from %s into %q (%d characters). The value was not disclosed.",
		source, target, len(value)), false
}
