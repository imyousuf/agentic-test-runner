// Package history keeps what a behaviour run knew about itself.
//
// ATR knows things about a test run that no general-purpose reporter can:
// whether a model was involved, whether the script was compiled or replayed,
// the *kind* of failure, and whether a repair happened. All of it used to be
// discarded the moment the process exited.
//
// Two questions justify keeping it:
//
//   - How often is a spec repaired? A spec that keeps being repaired is not
//     flaky — the application's DOM is churning underneath it. Nothing else in
//     a normal stack can tell you that.
//   - What is the true failure rate? Infrastructure failures exit 2, so a pass
//     rate can exclude the runs that never tested anything. Most dashboards
//     cannot separate those, which is how teams learn to ignore a red check.
//
// And one that falls out for free: a replay is deterministic with no model in
// the loop, so its duration is dominated by the application under test. A
// suite drifting from 9s to 15s over a month is telling you the application
// got slower, not that the model was chattier.
package history

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Outcome is what a run meant, in the same three categories as the exit code.
type Outcome string

const (
	// OutcomePassed: every step passed.
	OutcomePassed Outcome = "passed"
	// OutcomeTestFailure: the application is broken. Exit 1.
	OutcomeTestFailure Outcome = "test_failure"
	// OutcomeInfra: nothing was learned about the application — a missing
	// input, a browser that would not start, a stale script under
	// --no-compile. Exit 2.
	//
	// Counting these is the point. A pass rate that silently folds them in is
	// the number teams stop believing.
	OutcomeInfra Outcome = "infra"
)

// Attempt is one execution of the script inside a run.
type Attempt struct {
	Number      int
	Started     time.Time
	Duration    time.Duration
	Passed      bool
	Kind        string
	Message     string
	AfterRepair bool
}

// Run is everything worth keeping about one spec's execution.
type Run struct {
	// ID is unique per run.
	ID string
	// Spec identifies the spec across machines: its path relative to the
	// repository root when one can be found.
	//
	// Not the absolute path. A spec run from a laptop checkout and from CI is
	// the same spec, and identifying it by absolute path splits its history in
	// two without saying so.
	Spec string
	// SpecPath is the absolute path on this machine, kept separately because
	// it is what a person needs to open the file.
	SpecPath string

	StartedAt  time.Time
	FinishedAt time.Time

	Outcome Outcome
	// FailureKind is the runtime's classification, empty on a pass and on a
	// pre-run failure that never reached the script.
	FailureKind string
	// Message is the failure's message. It quotes the application, and the
	// spec's own expectations, back at you — so it can contain a resolved
	// value. That is the one place a value legitimately appears.
	Message string

	Compiled bool
	Repaired bool
	// CompileDuration is zero for a replay.
	CompileDuration time.Duration
	Repairs         int
	// AgentInvocations counts calls into the agent, not LLM requests: one
	// compile is one invocation and dozens of round-trips. Named for what it
	// counts, because "model calls" reads like the latter.
	AgentInvocations int

	Attempts []Attempt
}

// Duration is the wall time of the whole run.
func (r Run) Duration() time.Duration {
	if r.FinishedAt.IsZero() || r.StartedAt.IsZero() {
		return 0
	}
	return r.FinishedAt.Sub(r.StartedAt)
}

// Recorder is a sink for run records.
type Recorder interface {
	Record(ctx context.Context, run Run) error
	Close(ctx context.Context) error
}

// Multi fans a record out to every sink.
//
// It never returns an error from Record. A test run's exit code belongs to the
// application under test; a historian that could turn a passing suite red by
// failing to write to a database would be worse than no historian.
type Multi struct {
	Recorders []Recorder
	// OnError is called with whatever a sink reported, so a failure is
	// visible without being fatal. Nil discards.
	OnError func(error)
}

func (m *Multi) Record(ctx context.Context, run Run) error {
	for _, r := range m.Recorders {
		if err := r.Record(ctx, run); err != nil && m.OnError != nil {
			m.OnError(err)
		}
	}
	return nil
}

func (m *Multi) Close(ctx context.Context) error {
	for _, r := range m.Recorders {
		if err := r.Close(ctx); err != nil && m.OnError != nil {
			m.OnError(err)
		}
	}
	return nil
}

// Enabled reports whether anything is actually recording, so a caller can tell
// the difference between "nothing has run" and "nothing is being kept".
func (m *Multi) Enabled() bool { return len(m.Recorders) > 0 }

// NewID returns an identifier for one run.
func NewID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Only reached if the system entropy source is broken, and a run
		// record is not worth failing a test suite over.
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}

// SpecIdentity returns the stable name of a spec and its absolute path.
//
// The stable name is the path relative to the repository root, so the same
// spec run from a checkout and from CI lands on the same row.
func SpecIdentity(specPath string) (stable, absolute string) {
	abs, err := filepath.Abs(specPath)
	if err != nil {
		abs = specPath
	}
	abs = filepath.Clean(abs)

	root := repoRoot(filepath.Dir(abs))
	if root == "" {
		return filepath.Base(abs), abs
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.Base(abs), abs
	}
	return filepath.ToSlash(rel), abs
}

// repoRoot walks up looking for a .git entry, returning "" if there is none.
func repoRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == "" {
			return ""
		}
		dir = parent
	}
}

// TrimMessage bounds a failure message so one enormous page dump cannot make
// a row unreadable or a database large.
func TrimMessage(msg string) string {
	const max = 4000
	msg = strings.TrimSpace(msg)
	if len(msg) <= max {
		return msg
	}
	return msg[:max] + "… (truncated)"
}
