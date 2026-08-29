package testscript

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/pkg/behavior"
)

// ErrElementNotFound is re-exported so classify can match on it without every
// caller importing internal/browser.
var ErrElementNotFound = browser.ErrElementNotFound

// ErrInvalidSelector is re-exported for the same reason: a caller classifying
// a failure should not have to import the browser package.
var ErrInvalidSelector = browser.ErrInvalidSelector

// defaultStepTimeout bounds a single step. Generous: a step may legitimately
// wait on a slow page load.
const defaultStepTimeout = 60 * time.Second

// Options configures a run.
type Options struct {
	// Browser drives the page.
	Browser *browser.Browser
	// Source is the JavaScript to execute.
	Source string
	// Name identifies the script in stack traces.
	Name string
	// BaseURL is exposed to the script as atr.base.
	BaseURL string
	// Timeout bounds the whole run. Defaults to 5 minutes.
	Timeout time.Duration
	// SecretFiller resolves and types a secret without disclosing it. Left
	// nil, atr.fillSecret reports that secrets are not configured rather than
	// silently filling nothing.
	SecretFiller SecretFiller
	// Values supplies test inputs to the values global. Nil means the script
	// can only use defaults it supplies inline.
	Values *Values
	// Library is a shared operations file evaluated into the same VM before
	// the script, so its top-level functions are in scope.
	Library string
	// LibraryName identifies the library in stack traces, and is what the
	// assertion boundary matches on. Kept separate from Source rather than
	// concatenated, because concatenating would destroy line numbers and
	// make every stack frame point at the wrong file.
	LibraryName string
	// Log receives atr.log output.
	Log func(string)
}

// SecretFiller fills target with a secret obtained from ref or command,
// without returning the value. It mirrors the browser_fill_secret tool so a
// compiled script can log in the same way the agent does.
type SecretFiller func(ctx context.Context, target, ref, command string) error

// Result is the outcome of a run.
type Result struct {
	// Passed is true when every step completed and no assertion failed.
	Passed bool `json:"passed"`
	// Failure describes why the run stopped. Nil when Passed.
	Failure *Failure `json:"failure,omitempty"`
	// Steps records each step the script declared.
	Steps []behavior.StepResult `json:"steps,omitempty"`
	// Duration is the wall time of the run.
	Duration time.Duration `json:"duration"`
	// Logs collects atr.log output, which the agent reads when triaging.
	Logs []string `json:"logs,omitempty"`
}

// runtime holds the state of one execution.
type runtime struct {
	vm      *goja.Runtime
	ctx     context.Context
	browser *browser.Browser
	opts    Options

	values      *Values
	libraryName string
	steps       []behavior.StepResult
	logs        []string
	curStep     int
	curDesc     string
	curTarget   string
}

// Run executes a compiled behavior script.
//
// It returns an error only when the run could not be attempted at all. A
// script that fails is a successful run with Result.Passed false — the
// distinction matters because the caller's next move depends on the Failure's
// Kind, not on whether Go returned an error.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Browser == nil {
		return nil, fmt.Errorf("browser is required")
	}
	if strings.TrimSpace(opts.Source) == "" {
		return nil, fmt.Errorf("script source is empty")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.Name == "" {
		opts.Name = "test.js"
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	r := &runtime{
		vm:      goja.New(),
		ctx:     ctx,
		browser: opts.Browser,
		opts:    opts,
		values:  opts.Values,
	}
	if opts.Library != "" {
		r.libraryName = opts.LibraryName
		if r.libraryName == "" {
			r.libraryName = LibraryName
		}
	}

	// goja does not preempt: a script that loops forever, or a host call that
	// blocks, would otherwise ignore the deadline entirely. Interrupt is the
	// only way out.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			r.vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()

	if err := r.install(); err != nil {
		return nil, fmt.Errorf("installing script API: %w", err)
	}

	// A defect in the library is KindConfig: not repairable, not retryable,
	// never sent to the model. A person has to fix the shared file, and
	// classifying it as a script fault instead would point the repair loop at
	// code that twenty tests depend on.
	if opts.Library != "" {
		if err := ValidateLibrary(opts.Library, r.libraryName); err != nil {
			return &Result{Failure: &Failure{Kind: KindConfig, Message: err.Error()}}, nil
		}
		if err := r.evaluateLibrary(opts.Library, r.libraryName); err != nil {
			return &Result{Failure: &Failure{
				Kind:    KindConfig,
				Message: fmt.Sprintf("%s failed to load: %v", r.libraryName, err),
			}}, nil
		}
	}

	program, err := goja.Compile(opts.Name, opts.Source, false)
	if err != nil {
		// A script that does not even parse is the agent's fault, not the
		// application's.
		return &Result{
			Failure: &Failure{
				Kind:    KindScript,
				Message: fmt.Sprintf("the generated script does not parse: %v", err),
			},
		}, nil
	}

	start := time.Now()
	runErr := r.execute(program)
	elapsed := time.Since(start)

	result := &Result{
		Steps:    r.steps,
		Logs:     r.logs,
		Duration: elapsed,
	}

	if runErr == nil {
		result.Passed = true
		return result, nil
	}

	result.Failure = r.toFailure(runErr)
	r.markFailedStep(result.Failure)
	result.Steps = r.steps
	return result, nil
}

// execute runs the program, converting any panic that escapes the VM into an
// error.
//
// goja re-panics anything it does not recognise as a JS exception or an
// interrupt, so a stray panic in a host function — or an interrupt delivered
// at the wrong moment — would otherwise take the whole process down. A test
// runner reports failures; it does not crash on them.
func (r *runtime) execute(program *goja.Program) (err error) {
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		if recErr, ok := rec.(error); ok {
			err = recErr
			return
		}
		err = fmt.Errorf("the script panicked: %v", rec)
	}()

	_, err = r.vm.RunProgram(program)
	return err
}

// toFailure converts whatever came out of the VM into a classified Failure.
func (r *runtime) toFailure(err error) *Failure {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		kind := KindTimeout
		msg := "the script exceeded its time budget"
		if r.ctx.Err() == context.Canceled {
			kind = KindEnvironment
			msg = "the run was cancelled"
		}
		return &Failure{
			Kind:     kind,
			Message:  msg,
			Step:     r.curStep,
			StepDesc: r.curDesc,
			Target:   r.curTarget,
		}
	}

	var ex *goja.Exception
	if errors.As(err, &ex) {
		f := r.failureFromValue(ex.Value())
		if f.Stack == "" {
			f.Stack = ex.String()
		}
		return f
	}

	return &Failure{
		Kind:     KindScript,
		Message:  err.Error(),
		Step:     r.curStep,
		StepDesc: r.curDesc,
	}
}

// failureFromValue reads the kind/target the host attached to a thrown error.
// A throw the script made itself — or any plain JS error — has no kind, and
// is treated as a script defect.
func (r *runtime) failureFromValue(v goja.Value) *Failure {
	f := &Failure{
		Kind:     KindScript,
		Step:     r.curStep,
		StepDesc: r.curDesc,
		Target:   r.curTarget,
	}
	if v == nil {
		f.Message = "the script threw an empty value"
		return f
	}

	obj, ok := v.(*goja.Object)
	if !ok {
		f.Message = v.String()
		return f
	}

	if kind := obj.Get("kind"); kind != nil && !goja.IsUndefined(kind) {
		f.Kind = FailureKind(kind.String())
	}
	if msg := obj.Get("message"); msg != nil && !goja.IsUndefined(msg) {
		f.Message = msg.String()
	} else {
		f.Message = v.String()
	}
	if target := obj.Get("target"); target != nil && !goja.IsUndefined(target) {
		if s := target.String(); s != "" && s != "undefined" {
			f.Target = s
		}
	}
	if stack := obj.Get("stack"); stack != nil && !goja.IsUndefined(stack) {
		f.Stack = stack.String()
	}
	return f
}

// markFailedStep records the failure against the step that was running, so
// the report shows where the run stopped rather than only that it did.
func (r *runtime) markFailedStep(f *Failure) {
	if f.Step <= 0 {
		return
	}
	for i := range r.steps {
		if r.steps[i].Number == f.Step {
			r.steps[i].Status = behavior.StepStatusFailed
			r.steps[i].Error = f.Message
			return
		}
	}
	// The step never got as far as being recorded.
	r.steps = append(r.steps, behavior.StepResult{
		Number:      f.Step,
		Description: f.StepDesc,
		Status:      behavior.StepStatusFailed,
		Error:       f.Message,
	})
}

// throw raises a classified error inside the VM. Native functions call this
// instead of returning an error, so that a script can catch and inspect it
// with the same shape the Go side will later classify.
func (r *runtime) throw(kind FailureKind, target, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	obj := r.vm.NewGoError(errors.New(msg))
	_ = obj.Set("kind", string(kind))
	_ = obj.Set("name", kindName(kind))
	if target != "" {
		_ = obj.Set("target", target)
	}
	panic(obj)
}

// throwErr classifies a Go error and raises it.
func (r *runtime) throwErr(err error, target, context string) {
	kind := classify(err)
	if kind == "" {
		return
	}
	r.throw(kind, target, "%s: %v", context, err)
}

// kindName gives each kind a JS-conventional error name, so scripts can read
// `e.name` the way they would for a built-in.
func kindName(k FailureKind) string {
	switch k {
	case KindAssertion:
		return "AssertionError"
	case KindNotFound:
		return "NotFoundError"
	case KindTimeout:
		return "TimeoutError"
	case KindEnvironment:
		return "EnvironmentError"
	default:
		return "ScriptError"
	}
}

// checkCtx aborts a host call when the run's deadline has passed. Host calls
// are not interruptible once they enter Go, so every one of them checks on
// the way in.
func (r *runtime) checkCtx() {
	if err := r.ctx.Err(); err != nil {
		kind := KindTimeout
		if errors.Is(err, context.Canceled) {
			kind = KindEnvironment
		}
		r.throw(kind, r.curTarget, "run aborted: %v", err)
	}
}
