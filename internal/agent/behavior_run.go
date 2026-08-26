package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

// Default bounds on the recovery loop. Both are small on purpose: a test that
// needs three repairs to pass is telling you something that more repairs will
// only obscure.
const (
	defaultMaxRetries = 2
	defaultMaxRepairs = 1
)

// RunRequest describes one behavior test execution.
type RunRequest struct {
	// SpecPath is the path to the .test.txt.
	SpecPath string
	// Spec is its content.
	Spec string
	// BaseURL is the application under test.
	BaseURL string

	// Recompile forces a fresh compile even when the stored script matches
	// the spec.
	Recompile bool
	// NoCompile forbids using the model at all. A stale or missing script is
	// an error rather than a compile. This is the mode for CI, where an
	// unexpected model call is a cost surprise and a repair is a change
	// nobody reviewed.
	NoCompile bool

	// MaxRetries bounds re-runs after a transient failure.
	MaxRetries int
	// MaxRepairs bounds how many times the script may be rewritten. Zero
	// means the default; use NoRepair to forbid repair outright.
	MaxRepairs int
	// NoRepair keeps the agent's diagnosis but refuses to apply a rewrite.
	// Useful when a run should report drift rather than silently absorb it.
	NoRepair bool

	// Reset returns the browser to a clean starting state before a re-run.
	// Without it a second attempt would begin wherever the failed one
	// stopped, which is rarely where the spec starts.
	Reset func(ctx context.Context) error

	// SecretFiller is passed through to the script runtime for
	// atr.fillSecret. Nil means scripts cannot fill credentials.
	SecretFiller testscript.SecretFiller

	// ScriptTimeout bounds a single script run.
	ScriptTimeout time.Duration

	// Log receives progress lines.
	Log func(string)
}

// RunOutcome is the result of the whole compile/run/repair cycle.
type RunOutcome struct {
	// Result is the last script run. Nil if the script never ran.
	Result *testscript.Result
	// ScriptPath is where the compiled script lives.
	ScriptPath string
	// Compiled is true if this run generated the script.
	Compiled bool
	// Repaired is true if the script was rewritten during this run.
	Repaired bool
	// Triage is the agent's verdict, when it was consulted.
	Triage *Triage
	// ModelCalls counts how many times the agent was invoked, so the saving
	// the whole design exists for is visible rather than assumed.
	ModelCalls int
}

// Passed reports whether the test passed.
func (o *RunOutcome) Passed() bool {
	return o.Result != nil && o.Result.Passed
}

// RunBehavior compiles a spec if needed, replays it, and recovers from
// failures according to their kind.
//
// The shape of the recovery loop is the whole point:
//
//   - An assertion failure returns immediately, without a model call. The
//     runtime already knows the application is wrong, and asking a model to
//     confirm it would spend exactly the tokens this design exists to save —
//     and risks it "fixing" the assertion that caught the regression.
//   - Transient kinds are simply re-run. Most flakes need no intelligence.
//   - Drift and script defects go to the agent, which either rewrites the
//     script or tells us the application really is broken.
func (a *Agent) RunBehavior(ctx context.Context, req RunRequest) (*RunOutcome, error) {
	if req.MaxRetries <= 0 {
		req.MaxRetries = defaultMaxRetries
	}
	if req.MaxRepairs <= 0 {
		req.MaxRepairs = defaultMaxRepairs
	}
	logf := func(format string, args ...any) {
		if req.Log != nil {
			req.Log(fmt.Sprintf(format, args...))
		}
	}

	if a.browser == nil {
		return nil, fmt.Errorf("agent has no browser; build it with NewCompilerAgent")
	}

	outcome := &RunOutcome{}

	source, err := a.loadOrCompile(ctx, req, outcome, logf)
	if err != nil {
		return outcome, err
	}

	repairs := 0
	attempts := 0

	for {
		attempts++

		result, err := testscript.Run(ctx, testscript.Options{
			Browser:      a.browser,
			Source:       source,
			Name:         testscript.ScriptPath(req.SpecPath),
			BaseURL:      req.BaseURL,
			Timeout:      req.ScriptTimeout,
			SecretFiller: req.SecretFiller,
			Log:          req.Log,
		})
		if err != nil {
			return outcome, fmt.Errorf("running compiled script: %w", err)
		}
		outcome.Result = result

		if result.Passed {
			return outcome, nil
		}

		failure := result.Failure
		logf("script failed: %s", failure.Error())

		// The application is wrong. Nothing to repair, nothing to retry.
		if failure.Kind.IsTestFailure() {
			return outcome, nil
		}

		// Cheap recovery first: most transient failures just need another go.
		if failure.Kind.Retryable() && attempts <= req.MaxRetries {
			logf("retrying (attempt %d of %d)", attempts, req.MaxRetries)
			if err := a.reset(ctx, req); err != nil {
				return outcome, err
			}
			continue
		}

		// Everything else needs judgement.
		if req.NoCompile {
			logf("not triaging: --no-compile is set")
			return outcome, nil
		}
		if failure.Kind.Repairable() && repairs >= req.MaxRepairs {
			logf("repair budget exhausted (%d)", req.MaxRepairs)
			return outcome, nil
		}

		triage, err := a.triage(ctx, req, source, failure, attempts, outcome, logf)
		if err != nil {
			return outcome, err
		}
		outcome.Triage = triage

		switch triage.Verdict {
		case VerdictTestFailure:
			logf("agent verdict: the application is at fault — %s", triage.Reason)
			// Preserve the runtime's failure but record why it is real.
			result.Failure.Message = fmt.Sprintf("%s (triage: %s)", result.Failure.Message, triage.Reason)
			return outcome, nil

		case VerdictRepaired:
			if req.NoRepair {
				logf("the agent proposed a repair but --no-repair is set — %s", triage.Reason)
				return outcome, nil
			}
			repairs++
			outcome.Repaired = true
			source = triage.Script
			path, err := testscript.Save(req.SpecPath, req.Spec, source)
			if err != nil {
				return outcome, err
			}
			outcome.ScriptPath = path
			logf("agent repaired the script — %s", triage.Reason)
			if err := a.reset(ctx, req); err != nil {
				return outcome, err
			}
			continue

		case VerdictTransient:
			if attempts > req.MaxRetries+1 {
				logf("agent called it transient again after %d attempts; giving up", attempts)
				return outcome, nil
			}
			logf("agent verdict: transient — %s", triage.Reason)
			if err := a.reset(ctx, req); err != nil {
				return outcome, err
			}
			continue

		default:
			logf("agent could not determine the cause — %s", triage.Reason)
			return outcome, nil
		}
	}
}

// loadOrCompile returns the script to run, compiling it when the stored one
// is missing or no longer matches the spec.
func (a *Agent) loadOrCompile(ctx context.Context, req RunRequest, outcome *RunOutcome, logf func(string, ...any)) (string, error) {
	stored, err := testscript.Load(req.SpecPath)
	if err != nil {
		return "", err
	}

	switch {
	case req.Recompile:
		logf("recompiling on request")
	case stored == nil:
		logf("no compiled script yet; compiling")
	case !stored.Fresh(req.Spec):
		logf("the spec changed since the script was compiled; recompiling")
	default:
		outcome.ScriptPath = stored.Path
		return stored.Source, nil
	}

	if req.NoCompile {
		if stored == nil {
			return "", fmt.Errorf("no compiled script for %s and --no-compile is set; run without it once to compile", req.SpecPath)
		}
		return "", fmt.Errorf("%s is stale (the spec changed) and --no-compile is set", stored.Path)
	}

	outcome.ModelCalls++
	source, err := a.CompileBehavior(ctx, CompileRequest{
		SpecPath: req.SpecPath,
		Spec:     req.Spec,
		BaseURL:  req.BaseURL,
	})
	if err != nil {
		return "", fmt.Errorf("compiling %s: %w", req.SpecPath, err)
	}

	path, err := testscript.Save(req.SpecPath, req.Spec, source)
	if err != nil {
		return "", err
	}
	outcome.ScriptPath = path
	outcome.Compiled = true
	logf("compiled to %s", path)

	// Start the replay from a clean state: compiling drove the browser all
	// the way through the spec, so the page is sitting at the end of it.
	if err := a.reset(ctx, req); err != nil {
		return "", err
	}
	return source, nil
}

// triage asks the agent what to do about a failure.
func (a *Agent) triage(ctx context.Context, req RunRequest, source string, failure *testscript.Failure, attempts int, outcome *RunOutcome, logf func(string, ...any)) (*Triage, error) {
	logf("asking the agent to triage a %s failure", failure.Kind)
	outcome.ModelCalls++

	return a.TriageFailure(ctx, TriageRequest{
		SpecPath: req.SpecPath,
		Spec:     req.Spec,
		Script:   source,
		BaseURL:  req.BaseURL,
		Failure:  failure,
		Attempts: attempts,
	})
}

func (a *Agent) reset(ctx context.Context, req RunRequest) error {
	if req.Reset == nil {
		return nil
	}
	if err := req.Reset(ctx); err != nil {
		return fmt.Errorf("resetting the browser between attempts: %w", err)
	}
	return nil
}
