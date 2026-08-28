// Package testscript runs a compiled behavior test — JavaScript generated
// from a .test.txt spec — against a browser, without an LLM in the loop.
//
// The point of compiling a spec to a script is cost: the agent works out how
// to drive the page once, and every run after that replays the result for
// free. That only pays off if a failing run can be triaged automatically,
// which is what the failure taxonomy below exists for. A run that fails has
// to answer one question before anything else: is the application wrong, is
// the environment flaky, or has the script fallen out of date with a UI that
// changed shape? Those three have opposite correct responses, and conflating
// them is how self-updating test suites end up quietly deleting the
// assertion that was catching a real regression.
package testscript

import (
	"errors"
	"fmt"
)

// FailureKind classifies why a run stopped. Each kind carries a different
// policy, which is the whole reason the distinction exists.
type FailureKind string

const (
	// KindAssertion: the application did not do what the spec requires. The
	// script is right and the app is wrong.
	//
	// This is the one kind that must NEVER be auto-repaired. "Fixing" a
	// failing assertion is indistinguishable from deleting the test, and it
	// would turn a suite that catches regressions into one that launders
	// them.
	KindAssertion FailureKind = "assertion"

	// KindNotFound: a target the script names is not on the page. Usually the
	// UI was refactored — a selector renamed, a button relabelled — while the
	// behaviour the spec describes is intact. Repair candidate.
	//
	// Not always benign: an element can be missing *because* the app broke.
	// That is why repair re-derives the target from the spec rather than
	// hunting for anything clickable, and why a repair that cannot find
	// something matching the spec's intent reports a failure instead.
	KindNotFound FailureKind = "not_found"

	// KindTimeout: something took longer than allowed. Genuinely ambiguous —
	// a slow CI box, a flaky network, or a page that now hangs forever
	// because it is broken. Retried first; escalated to the agent only if it
	// persists.
	KindTimeout FailureKind = "timeout"

	// KindEnvironment: the browser, daemon, or network failed in a way that
	// says nothing about the application or the script. Retry, then report as
	// an infrastructure error rather than a test failure, so a red suite
	// still means "the app is broken".
	KindEnvironment FailureKind = "environment"

	// KindConfig: the test asked for an input this checkout does not define.
	// Nothing is wrong with the application or the script — the machine is
	// not set up to run this test.
	//
	// Deliberately NOT repairable. The tempting "fix" for a missing value is
	// to inline the literal back into the script, which would undo the whole
	// reason inputs live outside it. The fix is to add the value, and a
	// person has to decide what it should be.
	KindConfig FailureKind = "config"

	// KindScript: the generated JavaScript is itself wrong — a syntax error,
	// a bad API call, a reference to an undefined variable. Always the
	// agent's fault and always a repair candidate.
	KindScript FailureKind = "script"
)

// Repairable reports whether the agent may rewrite the script in response to
// this kind of failure.
func (k FailureKind) Repairable() bool {
	switch k {
	case KindNotFound, KindScript:
		return true
	default:
		return false
	}
}

// Retryable reports whether re-running unchanged is worth trying before
// involving the agent at all.
func (k FailureKind) Retryable() bool {
	switch k {
	case KindTimeout, KindEnvironment:
		return true
	default:
		return false
	}
}

// IsTestFailure reports whether this kind means the application under test is
// broken, as opposed to the harness or the script being out of date. Only
// these should turn a suite red.
func (k FailureKind) IsTestFailure() bool {
	return k == KindAssertion
}

// Failure is a classified script failure.
type Failure struct {
	Kind FailureKind `json:"kind"`
	// Message is the human-readable reason.
	Message string `json:"message"`
	// Step is the step number that failed, 0 if the failure was outside any
	// step.
	Step int `json:"step,omitempty"`
	// StepDesc is the failing step's description, carried so the agent can
	// see the *intent* rather than only the mechanics.
	StepDesc string `json:"step_desc,omitempty"`
	// Target is the element the script was acting on, when relevant. This is
	// what a repair rewrites.
	Target string `json:"target,omitempty"`
	// Stack is the JavaScript stack trace, when one is available.
	Stack string `json:"stack,omitempty"`
}

func (f *Failure) Error() string {
	if f.Step > 0 {
		return fmt.Sprintf("step %d (%s): %s: %s", f.Step, f.StepDesc, f.Kind, f.Message)
	}
	return fmt.Sprintf("%s: %s", f.Kind, f.Message)
}

// classify maps an error from the browser layer onto a FailureKind.
//
// Ordering matters: a not-found is reported as such even when it surfaced
// from an operation that also has a timeout, because "the element is gone" is
// the more actionable of the two.
func classify(err error) FailureKind {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrInvalidSelector):
		// The selector does not parse, so it can never match. That is a defect
		// in the script, not the page — repairable, and above all not
		// retryable: retrying a selector the browser refuses is what let one
		// compile spend its whole iteration budget on a single bad target.
		return KindScript
	case errors.Is(err, ErrElementNotFound):
		return KindNotFound
	default:
		return KindEnvironment
	}
}

// A deadline reaching here is a lookup that ran out of time, and asNotFound has
// already translated it: "the element is gone" is the more actionable reading,
// and it is the repairable one. A wait that genuinely timed out throws
// KindTimeout itself (see jsWaitFor and jsWaitForText), so classify never has
// to produce it.
