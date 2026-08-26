// Package secret fetches secret values on behalf of a tool without ever
// handing them to the model.
//
// The rule this package exists to enforce: a secret is fetched and consumed
// inside a single tool call. The model supplies the command to run and the
// field to fill; the tool runs the command, writes the output into the field,
// and reports only whether that worked. The plaintext is never a tool result,
// so it never enters the message history and is never re-transmitted to the
// model provider on later turns.
//
// If the command fails, its stderr is surfaced — that is diagnostic, and the
// model needs it to correct a wrong entry name. stdout is withheld even on
// failure, because a partially-successful manager can print secret material
// before exiting non-zero.
package secret

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// defaultTimeout bounds one fetch. Password managers routinely block on a
// biometric or passphrase prompt, so this is generous.
const defaultTimeout = 60 * time.Second

// waitDelay is how long Fetch waits, after the timeout has killed the shell,
// for inherited pipes to close before abandoning them.
const waitDelay = 2 * time.Second

// minRedactLength is the shortest value Redact will scrub. Shorter values
// collide with ordinary words in command output, where scrubbing would
// corrupt more than it protects.
const minRedactLength = 6

// Config describes the named references a user has set up, and how commands
// are run.
type Config struct {
	// Refs maps a short name to the shell command that prints its value on
	// stdout, e.g. "github/password" -> "pass show github/password".
	// Optional: the model may also pass a command directly.
	Refs map[string]string `mapstructure:"refs"`
	// Timeout bounds a single fetch. Defaults to 60s.
	Timeout time.Duration `mapstructure:"timeout"`
	// KeepTrailingNewline disables the default trimming of surrounding
	// whitespace. Most managers emit a trailing newline that is not part of
	// the secret, so trimming is the default.
	KeepTrailingNewline bool `mapstructure:"keep_trailing_newline"`
}

// Request names a secret to fetch, either by configured reference or by
// giving the command outright. Exactly one of the two must be set.
type Request struct {
	// Ref is a name from Config.Refs.
	Ref string
	// Command is a shell command whose stdout is the secret.
	Command string
}

// Vault runs fetch commands and remembers the values it produced, solely so
// that Redact can scrub them back out of unrelated tool output.
type Vault struct {
	cfg Config

	mu   sync.RWMutex
	seen map[string]string // value -> label, for redaction
}

// New builds a Vault. A zero Config is valid.
func New(cfg Config) *Vault {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return &Vault{cfg: cfg, seen: make(map[string]string)}
}

// Refs lists the configured reference names, for tool descriptions and status
// output. Values and commands are never included.
func (v *Vault) Refs() []string {
	refs := make([]string, 0, len(v.cfg.Refs))
	for name := range v.cfg.Refs {
		refs = append(refs, name)
	}
	return refs
}

// ExampleCommand suggests the host platform's usual keyring lookup, used in
// tool descriptions so the model has a sensible default to reach for.
func ExampleCommand() string {
	switch runtime.GOOS {
	case "darwin":
		return `security find-generic-password -s atr -a github/password -w`
	case "windows":
		return `pass show github/password`
	default:
		return `secret-tool lookup service atr account github/password`
	}
}

// Fetch resolves a request to its plaintext value.
//
// Callers must consume the result immediately and must not return it, log it,
// or include it in an error message. The only supported consumer is a tool
// that writes it straight into its destination.
func (v *Vault) Fetch(ctx context.Context, req Request) (string, error) {
	command, label, err := v.command(req)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, v.cfg.Timeout)
	defer cancel()

	name, args := shell(command)
	cmd := exec.CommandContext(ctx, name, args...)

	// Killing the shell on timeout is not enough. A grandchild — gpg-agent's
	// pinentry, say — inherits the stdout pipe, and cmd.Wait blocks until
	// every writer closes it. Without WaitDelay a manager stuck on an
	// unanswered prompt holds the agent turn open long past Timeout.
	cmd.WaitDelay = waitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = runErr.Error()
		}
		if ctx.Err() != nil {
			detail = fmt.Sprintf("timed out after %s", v.cfg.Timeout)
		}
		// stdout is deliberately not included: on some managers a partial
		// success prints secret material before a non-zero exit.
		return "", fmt.Errorf("secret command for %s failed: %s", label, truncate(detail, 400))
	}

	value := stdout.String()
	if !v.cfg.KeepTrailingNewline {
		value = strings.TrimSpace(value)
	}
	if value == "" {
		return "", fmt.Errorf("secret command for %s produced no output", label)
	}

	if len(value) >= minRedactLength {
		v.mu.Lock()
		v.seen[value] = label
		v.mu.Unlock()
	}

	return value, nil
}

// command resolves a Request to the shell command to run and a
// non-sensitive label for it.
func (v *Vault) command(req Request) (command, label string, err error) {
	ref := strings.TrimSpace(req.Ref)
	cmd := strings.TrimSpace(req.Command)

	switch {
	case ref != "" && cmd != "":
		return "", "", fmt.Errorf("give either a ref or a command, not both")
	case ref != "":
		configured, ok := v.cfg.Refs[ref]
		if !ok {
			known := v.Refs()
			if len(known) == 0 {
				return "", "", fmt.Errorf("no secret refs are configured; add secrets.refs to ~/.atr/config.yaml or pass a command instead")
			}
			return "", "", fmt.Errorf("unknown secret ref %q; configured refs are: %s", ref, strings.Join(known, ", "))
		}
		return configured, fmt.Sprintf("ref %q", ref), nil
	case cmd != "":
		return cmd, "the supplied command", nil
	default:
		return "", "", fmt.Errorf("a secret ref or command is required")
	}
}

// Redact replaces any value this vault has produced with a marker. It is a
// backstop for output that happens to echo a secret — a page that renders the
// password back, say — and is applied to tool results before they join the
// message history.
//
// It cannot catch a secret this vault never produced, such as one the model
// fetched itself by running the manager through the shell tool.
func (v *Vault) Redact(s string) string {
	v.mu.RLock()
	defer v.mu.RUnlock()

	for value, label := range v.seen {
		s = strings.ReplaceAll(s, value, fmt.Sprintf("[redacted secret: %s]", label))
	}
	return s
}

// shell returns the argv for running a command string through the platform
// shell, so that pipes and quoting in a user's manager command behave as they
// would in a terminal.
func shell(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "sh", []string{"-c", command}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
