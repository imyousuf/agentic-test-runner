package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

// LintMode says what to do about a compiled script that cannot fail.
type LintMode string

const (
	// LintModeError refuses to run a script with a blocking finding. The
	// default: a test that reports success unconditionally is worse than no
	// test, because it is believed.
	LintModeError LintMode = "error"
	// LintModeWarn reports and runs anyway, for a suite adopting the check
	// with scripts already committed.
	LintModeWarn LintMode = "warn"
	// LintModeOff skips the check.
	LintModeOff LintMode = "off"
)

// ErrScriptCannotFail reports a compiled script that would pass whatever the
// application did.
var ErrScriptCannotFail = errors.New("the compiled script cannot fail")

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

	// Lint says what to do about a script that cannot fail. Empty means
	// LintModeError.
	Lint LintMode

	// Log receives the script's own atr.log() output. Separate from Progress
	// because the two have different audiences: this is the test author's
	// tracing, shown on request.
	Log func(string)

	// Progress receives what the runner is doing — compiling, retrying,
	// repairing, and which iteration it is on. Shown by default: without it a
	// compile prints nothing between opening the browser and finishing, so a
	// healthy minute-long run and a wedged one look identical.
	Progress func(string)
}

// Attempt is one execution of the script within a run.
//
// A run is not one execution. It retries a transient failure, repairs a
// drifted script and runs it again, and only the last of those survived into
// RunOutcome.Result — which meant a run that failed, retried and passed
// recorded a pass and nothing else. The evidence that a spec is flaky is
// exactly the attempts that were being overwritten.
type Attempt struct {
	// Number is 1 for the first execution.
	Number int `json:"number"`
	// Started is when this execution began.
	Started time.Time `json:"started"`
	// Duration is how long the script ran.
	Duration time.Duration `json:"duration"`
	// Passed is whether this execution passed.
	Passed bool `json:"passed"`
	// Kind classifies the failure, empty when the attempt passed.
	Kind testscript.FailureKind `json:"kind,omitempty"`
	// Message is the failure's message, empty when the attempt passed.
	Message string `json:"message,omitempty"`
	// AfterRepair is true when the script was rewritten before this attempt,
	// which is what separates "the same test flaked" from "a different script
	// ran".
	AfterRepair bool `json:"after_repair,omitempty"`
}

// RunOutcome is the result of the whole compile/run/repair cycle.
type RunOutcome struct {
	// Result is the last script run. Nil if the script never ran.
	Result *testscript.Result
	// Attempts records every execution, in order. Result is the last of
	// these; the earlier ones are the only record that a passing run had to
	// try more than once.
	Attempts []Attempt
	// ScriptPath is where the compiled script lives.
	ScriptPath string
	// ValuesPath is where the test's inputs live, if any were written.
	ValuesPath string
	// Compiled is true if this run generated the script.
	Compiled bool
	// Repaired is true if the script was rewritten during this run.
	Repaired bool
	// CompileDuration is how long the compile took, zero when the script was
	// replayed. Timed because it is the most expensive thing ATR does and
	// nothing measured it.
	CompileDuration time.Duration
	// Triage is the agent's verdict, when it was consulted.
	Triage *Triage
	// ModelCalls counts how many times the agent was invoked, so the saving
	// the whole design exists for is visible rather than assumed.
	//
	// Agent invocations, not LLM requests: one compile is one increment and
	// dozens of round-trips.
	ModelCalls int
	// Lint is what the false-pass check found in the script that ran.
	Lint []testscript.Finding
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
		if req.Progress != nil {
			req.Progress(fmt.Sprintf(format, args...))
		}
	}

	// A base URL in the properties file is what lets the same test run
	// against localhost here and a staging host there.
	if req.BaseURL == "" {
		if v, err := testscript.LoadValues(req.SpecPath); err == nil {
			if base, ok, err := v.Resolve(ctx, "base_url"); err == nil && ok && base != "" {
				req.BaseURL = base
				logf("base URL from values: %s", base)
			}
		}
	}

	if a.browser == nil {
		return nil, fmt.Errorf("agent has no browser; build it with NewCompilerAgent")
	}

	outcome := &RunOutcome{}

	// A shared library beside the specs, if there is one. Loaded once: it is
	// the same file for every attempt, and re-reading it mid-run would mean a
	// retry could execute different code from the attempt it is retrying.
	library, err := testscript.LoadLibrary(req.SpecPath)
	if err != nil {
		return outcome, err
	}
	libHash := library.Hash()

	// What the committed script says it was compiled against, read before the
	// compile that may rewrite it.
	stored, _ := testscript.Load(req.SpecPath)

	source, err := a.loadOrCompile(ctx, req, outcome, library, logf)
	if err != nil {
		return outcome, err
	}

	if err := lintScript(req, source, outcome, logf); err != nil {
		return outcome, err
	}

	// Values are re-read on every attempt so a repair that added an input
	// takes effect without a second run.
	values, err := testscript.LoadValues(req.SpecPath)
	if err != nil {
		return outcome, err
	}

	repairs := 0
	attempts := 0
	afterRepair := false

	for {
		attempts++
		started := time.Now()

		result, err := testscript.Run(ctx, testscript.Options{
			Browser:      a.browser,
			Source:       source,
			Name:         testscript.ScriptPath(req.SpecPath),
			BaseURL:      req.BaseURL,
			Timeout:      req.ScriptTimeout,
			SecretFiller: req.SecretFiller,
			Values:       values,
			Log:          req.Log,
			Library:      librarySource(library),
			LibraryName:  testscript.LibraryName,
		})
		if err != nil {
			return outcome, fmt.Errorf("running compiled script: %w", err)
		}
		outcome.Result = result
		outcome.Attempts = append(outcome.Attempts, attemptOf(attempts, started, result, afterRepair))
		afterRepair = false

		// The script ran, so it is a script. Anything but a script fault means
		// the program executed — an assertion that did not hold, a missing
		// input, a page that changed — and none of those are reasons to
		// recompile it from scratch next time.
		//
		// Stamping only on a pass would be far more expensive than it looks:
		// an assertion failure means the script is right and the application
		// is broken, so every run while the app stayed broken would pay for a
		// full model compile.
		if result.Failure == nil || result.Failure.Kind != testscript.KindScript {
			// Under --no-compile the checkout is CI's, not ours: a restamp
			// there would leave the working tree dirty on a machine nobody is
			// watching. Say what would have been written instead.
			if req.NoCompile && stored != nil && stored.LibraryChanged(libHash) {
				logf("%s ran against a changed %s; re-run without --no-compile to restamp it",
					testscript.ScriptPath(req.SpecPath), testscript.LibraryName)
			} else if err := testscript.Stamp(req.SpecPath, libHash); err != nil {
				return outcome, err
			}
		}

		if result.Passed {
			return outcome, nil
		}

		failure := result.Failure
		logf("script failed: %s", failure.Error())

		// The application is wrong. Nothing to repair, nothing to retry.
		if failure.Kind.IsTestFailure() {
			return outcome, nil
		}

		// A missing or unresolvable input is already fully diagnosed: the
		// message names the key and every place it could come from. Asking a
		// model to restate that costs tokens and risks it "fixing" the
		// problem by inlining the literal back into the script.
		if failure.Kind == testscript.KindConfig {
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

			// The verdict has to reach the kind, not only the message.
			// Everything downstream switches on Kind: the printed advice, the
			// exit code, and the recorded outcome. Leaving a timeout labelled
			// as a timeout meant an agent that had just concluded "the
			// application is broken" produced a run that said "looks
			// environmental", exited 2, and was excluded from the failure
			// rate — which is the number this classification exists to
			// produce.
			//
			// A regression genuinely does present this way. The prompt teaches
			// wait-then-assert, so when a page stops reaching the state the
			// spec names, the *wait* fails first and the assertion is never
			// reached.
			result.Failure.Kind = testscript.KindAssertion
			result.Failure.Message = fmt.Sprintf("%s (triage: %s)", result.Failure.Message, triage.Reason)
			outcome.Attempts[len(outcome.Attempts)-1].Kind = testscript.KindAssertion
			outcome.Attempts[len(outcome.Attempts)-1].Message = result.Failure.Message
			return outcome, nil

		case VerdictRepaired:
			if req.NoRepair {
				logf("the agent proposed a repair but --no-repair is set — %s", triage.Reason)
				return outcome, nil
			}
			repairs++
			afterRepair = true
			outcome.Repaired = true
			source = triage.Script
			path, err := testscript.SaveDraft(req.SpecPath, req.Spec, source, libHash)
			if err != nil {
				return outcome, err
			}
			outcome.ScriptPath = path

			if triage.Properties != "" {
				added, err := testscript.MergeValues(req.SpecPath, triage.Properties)
				if err != nil {
					return outcome, err
				}
				if len(added) > 0 {
					logf("added input(s): %s", strings.Join(added, ", "))
				}
			}
			if values, err = testscript.LoadValues(req.SpecPath); err != nil {
				return outcome, err
			}
			logf("agent repaired the script — %s", triage.Reason)
			if err := lintScript(req, source, outcome, logf); err != nil {
				return outcome, err
			}
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

// librarySource is the library's text, or "" when there is none.
func librarySource(l *testscript.Library) string {
	if l == nil {
		return ""
	}
	return l.Source
}

// attemptOf summarises one execution for the run record.
func attemptOf(number int, started time.Time, result *testscript.Result, afterRepair bool) Attempt {
	a := Attempt{
		Number:      number,
		Started:     started,
		Duration:    result.Duration,
		Passed:      result.Passed,
		AfterRepair: afterRepair,
	}
	if result.Failure != nil {
		a.Kind = result.Failure.Kind
		a.Message = result.Failure.Message
	}
	return a
}

// lintScript refuses a script that would pass whatever the application did.
//
// Checked on every run rather than only on a fresh compile: a test that cannot
// fail is worthless whichever day it was generated, and replaying one silently
// is the harm. That does mean the check can turn a suite red the first time it
// ships, which is what --lint=warn is for.
//
// The findings never reach the model. Handing them to the repair loop would
// ask it to invent what the application must do, and a model asked that will
// invent something that passes — the false pass arriving by another door.
func lintScript(req RunRequest, source string, outcome *RunOutcome, logf func(string, ...any)) error {
	if req.Lint == LintModeOff {
		return nil
	}

	findings, err := testscript.Lint(source)
	if err != nil {
		// The script does not parse. The runtime already reports that as a
		// script fault and repairs it; a worse message first helps nobody.
		return nil
	}
	outcome.Lint = findings

	for _, f := range findings {
		logf("lint: %s", f)
	}

	blocking := testscript.Blocking(findings)
	if len(blocking) == 0 || req.Lint == LintModeWarn {
		return nil
	}

	reasons := make([]string, 0, len(blocking))
	for _, f := range blocking {
		reasons = append(reasons, f.String())
	}
	return fmt.Errorf("%s: %w\n  say in %s what must be true for the test to have passed, then re-run with --recompile (or --lint=warn to accept it as it is)",
		strings.Join(reasons, "\n  "), ErrScriptCannotFail, req.SpecPath)
}

// loadOrCompile returns the script to run, compiling it when the stored one
// is missing or no longer matches the spec.
func (a *Agent) loadOrCompile(ctx context.Context, req RunRequest, outcome *RunOutcome, library *testscript.Library, logf func(string, ...any)) (string, error) {
	stored, err := testscript.Load(req.SpecPath)
	if err != nil {
		return "", err
	}

	switch {
	case req.Recompile:
		logf("recompiling on request")
	case stored == nil:
		logf("no compiled script yet; compiling")
	case stored.Unverified:
		logf("the compiled script has never completed a run; recompiling")
	case !stored.Fresh(req.Spec):
		logf("the spec changed since the script was compiled; recompiling")
	default:
		// A changed library is a replay, not a recompile. The script calls
		// the library by name and loads it at run time, so editing login()
		// to follow a UI change is this feature's primary use — and paying a
		// model compile per spec for it would make the cheap fix the
		// expensive one.
		if stored.LibraryChanged(library.Hash()) {
			logf("%s changed since this script was compiled; replaying against it",
				testscript.LibraryName)
		}
		outcome.ScriptPath = stored.Path
		return stored.Source, nil
	}

	if req.NoCompile {
		if stored == nil {
			return "", fmt.Errorf("no compiled script for %s and --no-compile is set; run without it once to compile", req.SpecPath)
		}
		if stored.Unverified {
			return "", fmt.Errorf("%s has never completed a run and --no-compile is set; run without it once to prove it", stored.Path)
		}
		return "", fmt.Errorf("%s is stale (the spec changed) and --no-compile is set", stored.Path)
	}

	outcome.ModelCalls++
	compileStart := time.Now()
	source, properties, err := a.CompileBehavior(ctx, CompileRequest{
		Progress: req.Progress,
		SpecPath: req.SpecPath,
		Spec:     req.Spec,
		BaseURL:  req.BaseURL,
		Library:  librarySource(library),
	})
	outcome.CompileDuration = time.Since(compileStart)
	if err != nil {
		return "", fmt.Errorf("compiling %s: %w", req.SpecPath, err)
	}

	path, err := testscript.SaveDraft(req.SpecPath, req.Spec, source, library.Hash())
	if err != nil {
		return "", err
	}

	if properties != "" {
		valuesPath, err := testscript.SaveValues(req.SpecPath, req.BaseURL, properties)
		if err != nil {
			return "", err
		}
		outcome.ValuesPath = valuesPath
		logf("wrote inputs to %s", valuesPath)
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

	keys, _ := testscript.LoadValues(req.SpecPath)
	library, _ := testscript.LoadLibrary(req.SpecPath)
	return a.TriageFailure(ctx, TriageRequest{
		Library:   librarySource(library),
		SpecPath:  req.SpecPath,
		Spec:      req.Spec,
		Script:    source,
		BaseURL:   req.BaseURL,
		Failure:   failure,
		Attempts:  attempts,
		ValueKeys: keys.Keys(),
		Progress:  req.Progress,
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
