package testscript

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
)

// Values may defer to the machine at run time:
//
//	password=$(cat ~/.secrets/shop.txt)
//	api_token=$(pass show shop/token)
//	home=${HOME}
//	literal=$$(not a command)
//
// This keeps a value out of the file entirely, which is the only way a
// properties file can reference something that must not be written down.
//
// It also means a properties file is executable. A committed one runs on
// every machine that runs the suite, including CI, so a value added in a pull
// request is a command someone else's laptop will execute. Review them the
// way you would review a script — and prefer putting expansions in the
// gitignored override file, where they only affect you.

// expandTimeout bounds one command. Generous: a password manager may block on
// a biometric or passphrase prompt.
const expandTimeout = 60 * time.Second

// expandWaitDelay is how long to wait, after the timeout has killed the
// shell, for inherited pipes to close. A grandchild such as gpg's pinentry
// holds stdout open and would otherwise block the read indefinitely.
const expandWaitDelay = 2 * time.Second

// expander runs and caches command substitutions.
type expander struct {
	mu     sync.Mutex
	cached map[string]string
}

// expand resolves $(...) and ${...} in raw.
//
// Results are cached per key, so a value read twice does not prompt twice.
// The expansion is lazy — done here at read time rather than at load — so a
// test that never touches a value never runs its command.
func (e *expander) expand(ctx context.Context, key, raw string) (string, error) {
	if !needsExpansion(raw) {
		return raw, nil
	}

	e.mu.Lock()
	if cached, ok := e.cached[key]; ok {
		e.mu.Unlock()
		return cached, nil
	}
	e.mu.Unlock()

	out, err := expandString(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("expanding value %q: %w", key, err)
	}

	e.mu.Lock()
	if e.cached == nil {
		e.cached = make(map[string]string)
	}
	e.cached[key] = out
	e.mu.Unlock()

	return out, nil
}

// needsExpansion is a cheap pre-check so ordinary values skip the scanner.
func needsExpansion(raw string) bool {
	return strings.Contains(raw, "$")
}

// expandString walks raw, substituting as it goes.
func expandString(ctx context.Context, raw string) (string, error) {
	var sb strings.Builder

	for i := 0; i < len(raw); {
		if raw[i] != '$' {
			sb.WriteByte(raw[i])
			i++
			continue
		}

		// "$$" is how a value spells a literal dollar sign.
		if i+1 < len(raw) && raw[i+1] == '$' {
			sb.WriteByte('$')
			i += 2
			continue
		}

		switch {
		case strings.HasPrefix(raw[i:], "$("):
			body, next, err := scanDelimited(raw, i+2, '(', ')')
			if err != nil {
				return "", err
			}
			out, err := runCommand(ctx, body)
			if err != nil {
				return "", err
			}
			sb.WriteString(out)
			i = next

		case strings.HasPrefix(raw[i:], "${"):
			body, next, err := scanDelimited(raw, i+2, '{', '}')
			if err != nil {
				return "", err
			}
			sb.WriteString(os.Getenv(strings.TrimSpace(body)))
			i = next

		default:
			// A bare $ is just a character.
			sb.WriteByte(raw[i])
			i++
		}
	}

	return sb.String(), nil
}

// scanDelimited returns the text between balanced delimiters starting at
// start, and the index just past the closer. Nesting is tracked so that a
// command containing parentheses survives.
func scanDelimited(s string, start int, open, close byte) (body string, next int, err error) {
	depth := 1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[start:i], i + 1, nil
			}
		}
	}
	return "", 0, fmt.Errorf("unterminated %c...%c", open, close)
}

// runCommand executes a substitution and returns its stdout, trimmed.
//
// stdout is never included in an error, even on failure: the whole reason a
// value is expanded from a command is usually that it must not be written
// down, and a manager can print secret material before exiting non-zero.
func runCommand(ctx context.Context, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("empty command substitution")
	}

	ctx, cancel := context.WithTimeout(ctx, expandTimeout)
	defer cancel()

	name, args := shellFor(command)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = expandWaitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if ctx.Err() != nil {
			detail = fmt.Sprintf("timed out after %s", expandTimeout)
		} else if detail == "" {
			detail = err.Error()
		}
		// The command text is deliberately omitted. A substitution usually
		// exists because the value must not be written down, and the command
		// can carry it — "$(echo hunter2)" would print the password straight
		// into a log. The key name is enough to find the offending line.
		return "", fmt.Errorf("command failed: %s", truncateText(detail, 300))
	}

	// Trailing newlines are an artefact of the command, not part of the value.
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}

func shellFor(command string) (string, []string) {
	if goruntime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "sh", []string{"-c", command}
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
