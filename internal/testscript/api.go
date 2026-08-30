package testscript

import (
	"errors"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/imyousuf/agentic-test-runner/pkg/behavior"
)

// defaultWaitTimeout is how long waitFor blocks when a script does not say.
const defaultWaitTimeout = 10 * time.Second

// install exposes the host library to the script.
//
// The surface is deliberately small and blunt. Every action either succeeds or
// throws a classified error — there are no error return values for a script to
// forget to check, because a compiled test that silently skipped a failed
// click would report a pass it did not earn.
//
// The one exception is exists(), which returns a boolean precisely so that
// scripts can branch on optional page furniture (cookie banners, A/B variants)
// without a missing element being treated as drift. It still throws when the
// browser could not answer at all, because a fault is not an answer — see
// existsOutcome.
func (r *runtime) install() error {
	atr := r.vm.NewObject()

	set := func(name string, fn any) {
		if err := atr.Set(name, fn); err != nil {
			panic(fmt.Sprintf("installing atr.%s: %v", name, err))
		}
	}

	_ = atr.Set("base", r.opts.BaseURL)

	set("step", r.jsStep)
	set("setup", r.jsSetup)
	set("log", r.jsLog)
	set("fail", r.jsFail)
	set("sleep", r.jsSleep)
	set("retry", r.jsRetry)

	// Navigation
	set("navigate", r.jsNavigate)
	set("reload", r.jsReload)
	set("back", r.jsBack)
	set("forward", r.jsForward)

	// Interaction
	set("click", r.jsClick)
	set("doubleClick", r.jsDoubleClick)
	set("fill", r.jsFill)
	set("fillSecret", r.jsFillSecret)
	set("hover", r.jsHover)
	set("pressKey", r.jsPressKey)
	set("scroll", r.jsScroll)

	// Waiting
	set("waitFor", r.jsWaitFor)
	set("waitForText", r.jsWaitForText)

	// Assertions that wait
	set("expectExists", r.jsExpectExists)
	set("expectMissing", r.jsExpectMissing)
	set("expectText", r.jsExpectText)

	// Inspection
	set("exists", r.jsExists)
	set("text", r.jsText)
	set("html", r.jsHTML)
	set("url", r.jsURL)
	set("title", r.jsTitle)
	set("snapshot", r.jsSnapshot)
	set("eval", r.jsEval)
	set("consoleErrors", r.jsConsoleErrors)
	set("failedRequests", r.jsFailedRequests)

	// Pages
	set("newPage", r.jsNewPage)
	set("listPages", r.jsListPages)
	set("selectPage", r.jsSelectPage)
	set("closePage", r.jsClosePage)

	if err := r.vm.Set("atr", atr); err != nil {
		return err
	}
	if err := r.installValues(); err != nil {
		return err
	}
	return r.installExpect()
}

// --- structure ---------------------------------------------------------------

// jsStep runs one numbered step, recording its outcome.
//
// Steps are what make a failure legible to the agent later: the number and the
// prose come straight from the .test.txt, so a triage prompt can show what the
// step was *for* rather than only which selector blew up.
func (r *runtime) jsStep(number int, description string, fn goja.Callable) {
	r.checkCtx()

	prevStep, prevDesc := r.curStep, r.curDesc
	r.curStep, r.curDesc = number, description
	r.curTarget = ""

	idx := len(r.steps)
	r.steps = append(r.steps, behavior.StepResult{
		Number:      number,
		Description: description,
		Status:      behavior.StepStatusRunning,
	})

	start := time.Now()
	// No recover here on purpose: a throw must propagate and stop the run.
	// markFailedStep fills in the outcome from the failure afterwards.
	_, err := fn(goja.Undefined())
	r.steps[idx].Duration = time.Since(start)

	if err != nil {
		r.steps[idx].Status = behavior.StepStatusFailed
		// Re-throw the original JS value so its kind survives. Panicking
		// with the Go error instead sends something goja does not recognise
		// through RunProgram, which re-panics it out of the VM entirely — a
		// deadline landing mid-step would crash the process rather than
		// being reported as a timeout.
		var ex *goja.Exception
		if errors.As(err, &ex) {
			panic(ex.Value())
		}
		panic(err)
	}

	r.steps[idx].Status = behavior.StepStatusPassed
	r.curStep, r.curDesc = prevStep, prevDesc
}

// jsSetup runs work that has to happen before the steps, every time the
// script runs.
//
// It exists for tests that consume their own precondition. A spec that
// archives a conversation and then asserts the archive is read-only spends
// that fixture the first time it runs — and a compile runs the spec more than
// once, so the verification replay immediately afterwards finds it already
// archived and fails at step 1 with a timeout that reads as a broken page
// rather than a spent fixture.
//
// Putting the rebuild here makes the test idempotent: it runs before the steps
// on the compile's own replay, on every retry, on every repair attempt, and on
// every ordinary run afterwards.
//
// Not a numbered step. Setting a fixture up is not part of what the
// specification claims about the application, so it does not belong in the
// step list — and a failure here says the fixture could not be built, which is
// a different thing from the application misbehaving.
func (r *runtime) jsSetup(description string, fn goja.Callable) {
	r.checkCtx()

	prevStep, prevDesc := r.curStep, r.curDesc
	r.curStep, r.curDesc = 0, "setup: "+description
	r.curTarget = ""

	// As in jsStep, a throw must propagate: the run cannot continue without
	// the fixture it was about to build.
	_, err := fn(goja.Undefined())
	if err != nil {
		var ex *goja.Exception
		if errors.As(err, &ex) {
			panic(ex.Value())
		}
		panic(err)
	}

	r.curStep, r.curDesc = prevStep, prevDesc
}

func (r *runtime) jsLog(msg string) {
	r.logs = append(r.logs, msg)
	if r.opts.Log != nil {
		r.opts.Log(msg)
	}
}

// jsFail is how a script reports that the application is wrong. It is an
// assertion failure, which means it will never be "repaired" away.
func (r *runtime) jsFail(msg string) {
	if r.callerIsLibrary() {
		r.refuseFromLibrary("atr.fail")
	}
	r.throw(KindAssertion, "", "%s", msg)
}

func (r *runtime) jsSleep(ms int) {
	r.checkCtx()
	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
	case <-r.ctx.Done():
		r.checkCtx()
	}
}

// jsRetry re-runs fn until it stops throwing, up to times attempts.
//
// Only transient kinds are retried. Retrying an assertion would be pointless
// and retrying drift would just fail slower.
func (r *runtime) jsRetry(times int, fn goja.Callable) goja.Value {
	if times < 1 {
		times = 1
	}

	var last goja.Value
	for attempt := 1; ; attempt++ {
		r.checkCtx()

		v, err := r.tryCall(fn)
		if err == nil {
			return v
		}
		last = err

		if attempt >= times || !r.thrownKind(err).Retryable() {
			panic(last)
		}
		r.jsLog(fmt.Sprintf("attempt %d/%d failed, retrying", attempt, times))
		r.jsSleep(500 * attempt)
	}
}

// tryCall invokes fn, converting a JS throw into a returned value.
func (r *runtime) tryCall(fn goja.Callable) (result goja.Value, thrown goja.Value) {
	defer func() {
		if rec := recover(); rec != nil {
			if v, ok := rec.(goja.Value); ok {
				thrown = v
				return
			}
			panic(rec)
		}
	}()

	v, err := fn(goja.Undefined())
	if err != nil {
		var ex *goja.Exception
		if ok := asException(err, &ex); ok {
			return nil, ex.Value()
		}
		panic(err)
	}
	return v, nil
}

// thrownKind reads the kind off a thrown value, defaulting to script.
func (r *runtime) thrownKind(v goja.Value) FailureKind {
	obj, ok := v.(*goja.Object)
	if !ok {
		return KindScript
	}
	k := obj.Get("kind")
	if k == nil || goja.IsUndefined(k) {
		return KindScript
	}
	return FailureKind(k.String())
}

// --- navigation --------------------------------------------------------------

func (r *runtime) jsNavigate(url string) {
	r.checkCtx()
	r.curTarget = url
	if err := r.browser.Navigate(r.ctx, r.resolve(url)); err != nil {
		// A navigation that fails is the environment or the app being down,
		// never a selector that moved.
		r.throw(KindEnvironment, url, "navigate to %s failed: %v", url, err)
	}
}

func (r *runtime) jsReload() {
	r.checkCtx()
	if err := r.browser.Reload(); err != nil {
		r.throw(KindEnvironment, "", "reload failed: %v", err)
	}
}

func (r *runtime) jsBack() {
	r.checkCtx()
	if err := r.browser.GoBack(); err != nil {
		r.throw(KindEnvironment, "", "back failed: %v", err)
	}
}

func (r *runtime) jsForward() {
	r.checkCtx()
	if err := r.browser.GoForward(); err != nil {
		r.throw(KindEnvironment, "", "forward failed: %v", err)
	}
}

// resolve expands a path against atr.base so scripts can be written against
// relative URLs and run at any host.
func (r *runtime) resolve(ref string) string {
	if r.opts.BaseURL == "" || strings.Contains(ref, "://") {
		return ref
	}
	// An empty path means "the base itself".
	if ref == "" {
		return r.opts.BaseURL
	}

	// Resolve the way a browser would, rather than by joining strings. A
	// base URL routinely names a document — http://host/login.html — and
	// joining "login.html" onto that produces login.html/login.html, which
	// is a 404 the script then reports as a missing element.
	base, err := neturl.Parse(r.opts.BaseURL)
	if err != nil {
		return ref
	}
	rel, err := neturl.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(rel).String()
}

// --- interaction -------------------------------------------------------------

func (r *runtime) jsClick(target string) {
	r.checkCtx()
	r.curTarget = target
	if err := r.browser.Click(r.ctx, target, false); err != nil {
		r.throwErr(err, target, fmt.Sprintf("click %q", target))
	}
}

func (r *runtime) jsDoubleClick(target string) {
	r.checkCtx()
	r.curTarget = target
	if err := r.browser.Click(r.ctx, target, true); err != nil {
		r.throwErr(err, target, fmt.Sprintf("double-click %q", target))
	}
}

func (r *runtime) jsFill(target, value string) {
	r.checkCtx()
	r.curTarget = target
	if err := r.browser.Fill(r.ctx, target, value); err != nil {
		r.throwErr(err, target, fmt.Sprintf("fill %q", target))
	}
}

// jsFillSecret types a credential without the value passing through the
// script, the transcript, or any later triage prompt.
func (r *runtime) jsFillSecret(target string, spec map[string]any) {
	r.checkCtx()
	r.curTarget = target

	if r.opts.SecretFiller == nil {
		r.throw(KindEnvironment, target,
			"fillSecret is not available: no secret backend is configured")
	}

	ref, _ := spec["ref"].(string)
	command, _ := spec["command"].(string)
	if err := r.opts.SecretFiller(r.ctx, target, ref, command); err != nil {
		// A manager that will not produce the value is an environment
		// problem; the page not having the field is drift.
		kind := classify(err)
		if kind == KindEnvironment {
			r.throw(KindEnvironment, target, "fillSecret %q: %v", target, err)
		}
		r.throw(kind, target, "fillSecret %q: %v", target, err)
	}
}

func (r *runtime) jsHover(target string) {
	r.checkCtx()
	r.curTarget = target
	if err := r.browser.Hover(r.ctx, target); err != nil {
		r.throwErr(err, target, fmt.Sprintf("hover %q", target))
	}
}

func (r *runtime) jsPressKey(key string) {
	r.checkCtx()
	if err := r.browser.PressKey(key); err != nil {
		r.throw(KindEnvironment, "", "press key %q failed: %v", key, err)
	}
}

func (r *runtime) jsScroll(opts map[string]any) {
	r.checkCtx()
	selector, _ := opts["selector"].(string)
	x := intOf(opts["x"])
	y := intOf(opts["y"])
	toBottom, _ := opts["toBottom"].(bool)
	toTop, _ := opts["toTop"].(bool)

	if _, err := r.browser.ScrollElement(selector, x, y, toBottom, toTop); err != nil {
		r.throwErr(err, selector, "scroll")
	}
}

// --- waiting -----------------------------------------------------------------

// jsWaitFor blocks until target appears.
//
// A timeout here is deliberately NOT reported as drift, even though the two
// look alike. "Not there yet" and "not there any more" need different
// responses, and only the retry-then-escalate path can tell them apart.
func (r *runtime) jsWaitFor(target string, opts map[string]any) {
	r.checkCtx()
	r.curTarget = target

	timeout := durationOf(opts["timeout"], defaultWaitTimeout)
	visible, _ := opts["visible"].(bool)

	var err error
	if visible {
		err = r.browser.WaitForElementVisible(r.ctx, target, timeout)
	} else {
		err = r.browser.WaitForElement(r.ctx, target, timeout)
	}
	if err != nil {
		r.throw(KindTimeout, target, "waiting for %q: %v", target, err)
	}
}

func (r *runtime) jsWaitForText(text string, opts map[string]any) {
	r.checkCtx()
	r.curTarget = text
	timeout := durationOf(opts["timeout"], defaultWaitTimeout)
	if err := r.browser.WaitForText(text, timeout); err != nil {
		r.throw(KindTimeout, text, "waiting for text %q: %v", text, err)
	}
}

// --- assertions about presence -----------------------------------------------

// jsExpectExists asserts that a target is on the page, waiting for it the way
// a person would.
//
// This exists because the composition it replaces had incompatible halves.
// expect(atr.exists(x)).toBeTruthy() gives the lookup exists()'s 500ms branch
// budget and then reports the miss through expect, which raises KindAssertion:
// terminal, never retried, never triaged. A page that renders in 800ms was
// therefore reported as a broken application.
func (r *runtime) jsExpectExists(target string, opts map[string]any) {
	r.checkCtx()
	r.curTarget = target

	timeout := durationOf(opts["timeout"], defaultWaitTimeout)
	err := r.browser.WaitForElement(r.ctx, target, timeout)
	present, fatal := existsOutcome(err)
	if fatal != "" {
		r.throw(fatal, target, "looking for %q: %v", target, err)
	}
	if !present {
		// The run running out of time is not the application being wrong.
		r.checkCtx()
		r.throw(KindAssertion, target,
			"expected %q to be on the page; it was not there after %s", target, timeout)
	}
}

// jsExpectMissing asserts that a target is not on the page, allowing time for
// something already there to go away.
//
// Absence cannot be proved by looking once: an element that has not rendered
// yet is indistinguishable from one that never will, and a check that passes
// on the first is a false pass. Nothing here can close that gap — the script
// has to wait for the state that implies removal first, which is why the
// compile prompt says so. What this does close is the other half: an element
// that is on its way out gets the full budget to leave.
func (r *runtime) jsExpectMissing(target string, opts map[string]any) {
	r.checkCtx()
	r.curTarget = target

	timeout := durationOf(opts["timeout"], defaultWaitTimeout)
	deadline := time.Now().Add(timeout)

	for {
		err := r.browser.WaitForElement(r.ctx, target, existsBranchTimeout)
		present, fatal := existsOutcome(err)
		if fatal != "" {
			r.throw(fatal, target, "looking for %q: %v", target, err)
		}
		if !present {
			return
		}
		if !time.Now().Before(deadline) {
			// The run running out of time is not the application being wrong.
			r.checkCtx()
			r.throw(KindAssertion, target,
				"expected %q to be gone from the page; it was still there after %s", target, timeout)
		}
		r.checkCtx()
		time.Sleep(expectMissingPoll)
	}
}

// expectTextPoll is how often expectText re-reads a target that has not
// reached the value the spec asks for.
const expectTextPoll = 200 * time.Millisecond

// jsExpectText asserts that a target reads what the spec says it should,
// waiting for it to get there.
//
// This exists because the wait and the assertion are one intent, and splitting
// them across two calls hands the classification to whichever hits the wall
// first — which is always the wait:
//
//	atr.waitForText("Order placed", {timeout: 5000});   // timeout: retryable
//	expect(atr.text("#message")).toBe("Order placed");  // assertion: terminal
//
// When the application stops reaching that state — a regression, the thing a
// test exists to catch — the wait fails, and the run reports a timeout. CI
// then reads a genuine break as infrastructure and retries it. One call cannot
// be misattributed that way: it waits like a wait and fails like an assertion.
//
// The same shape as expectExists, for the same reason.
func (r *runtime) jsExpectText(target, expected string, opts map[string]any) {
	r.checkCtx()
	r.curTarget = target

	// An empty expectation is satisfied by anything at all — trivially so
	// under contains, where strings.Contains(x, "") is always true. That is
	// an assertion that cannot fail, which is the one thing this whole
	// design exists to keep out of a suite.
	if expected == "" {
		r.throw(KindScript, target,
			"atr.expectText(%q) was given an empty expected value, which any page satisfies; "+
				"use atr.expectExists to assert the target is merely present", target)
	}

	timeout := durationOf(opts["timeout"], defaultWaitTimeout)
	contains, _ := opts["contains"].(bool)
	started := time.Now()
	deadline := started.Add(timeout)

	// Whether this call's budget reaches past the run's own. If it does, the
	// two deadlines expire within microseconds of each other, and which one
	// is observed first is a coin flip — so the same broken page was reported
	// as a timeout on one run and as the application being wrong on the next.
	//
	// Recorded up front rather than compared at the end, where the answer
	// depends on which side of the race we landed.
	capped := false
	if runDeadline, ok := r.ctx.Deadline(); ok && !deadline.Before(runDeadline) {
		capped = true
	}

	var last string
	var everFound bool

	for {
		// Presence first, with a budget this call controls. Reading the text
		// of an element that is not there carries the element lookup's own
		// floor, so going straight to the read made a 100ms budget spend
		// three seconds — and then report that it had waited 100ms.
		present, fatal := existsOutcome(
			r.browser.WaitForElement(r.ctx, target, until(deadline)))
		if fatal != "" {
			r.throw(fatal, target, "looking for %q", target)
		}

		if present {
			text, err := r.readText(target)
			if err != nil && !errors.Is(err, ErrElementNotFound) {
				// A selector that does not parse, a renderer that stopped
				// answering: faults, not answers, and not worth waiting out.
				r.throwErr(err, target, fmt.Sprintf("reading text of %q", target))
			}
			if err == nil {
				everFound, last = true, text
				if textMatches(text, expected, contains) {
					return
				}
			}
		}

		if !time.Now().Before(deadline) {
			break
		}
		r.checkCtx()
		time.Sleep(expectTextPoll)
	}

	// Running out of time is not the application being wrong. checkCtx throws
	// the run's own kind — timeout, or environment when it was cancelled — so
	// a suite that blew its budget or was interrupted is never reported as a
	// regression, which is terminal and never retried.
	r.checkCtx()

	// And when this call could never have outlived the run, giving up is not
	// evidence about the application either. Erring towards a timeout is the
	// recoverable direction: a timeout is retried and then triaged, while an
	// assertion is terminal and exits 1 on what may have been a slow box.
	if capped {
		r.throw(KindTimeout, target,
			"gave up waiting for %q at the end of the run's own budget, so nothing can be "+
				"concluded about the application; give the run more time, or this call less",
			target)
	}

	// How long it actually waited, not how long it was told to: a failure
	// that misreports its own patience is one nobody can reason about.
	waited := time.Since(started).Round(time.Millisecond)

	if !everFound {
		r.throw(KindAssertion, target,
			"expected %q to read %q; it was not on the page after %s", target, expected, waited)
	}
	verb := "read"
	if contains {
		verb = "contain"
	}
	r.throw(KindAssertion, target,
		"expected %q to %s %q, got %q after %s", target, verb, expected, last, waited)
}

// until is how much of a budget is left, floored so a lookup always gets
// enough to answer at all.
func until(deadline time.Time) time.Duration {
	remaining := time.Until(deadline)
	if remaining < existsBranchTimeout {
		return existsBranchTimeout
	}
	return remaining
}

// textMatches compares what a target reads against what the spec asked for.
func textMatches(actual, expected string, contains bool) bool {
	if contains {
		return strings.Contains(actual, expected)
	}
	return actual == expected
}

// --- inspection --------------------------------------------------------------

// existsBranchTimeout is how long a branching exists() waits. Short on
// purpose: it answers "is the cookie banner up?", and a script should not pay
// ten seconds to learn that it is not.
const existsBranchTimeout = 500 * time.Millisecond

// expectMissingPoll is how often expectMissing re-checks a target that is
// still there.
const expectMissingPoll = 200 * time.Millisecond

// existsOutcome separates "the element is not there" from "the browser could
// not tell us".
//
// exists() used to collapse both into false, so a closed page, a dead
// renderer or a selector that does not parse all read as "absent" — and
// whatever the script did with that false was reported as the application's
// behaviour. A fault is not an answer.
//
// It can only classify an error that arrives as itself. A lookup whose
// deadline expired has already been mapped to ErrElementNotFound by the
// browser layer, which is the right reading for the ordinary case and
// indistinguishable from a wedged renderer by the time it gets here.
func existsOutcome(err error) (present bool, fatal FailureKind) {
	switch {
	case err == nil:
		return true, ""
	case errors.Is(err, ErrElementNotFound):
		return false, ""
	case errors.Is(err, ErrInvalidSelector):
		// A selector that cannot parse can never match, so "absent" would be
		// a true answer to the wrong question. It is a defect in the script.
		return false, KindScript
	default:
		return false, KindEnvironment
	}
}

// jsExists reports whether a target is present, without throwing. This is the
// branch point for optional UI, and the only read that treats absence as an
// answer rather than a fault.
//
// For branching only. Asserting through it — expect(atr.exists(x)) — gives
// the lookup this short budget and reports the miss as a test failure; that is
// what atr.expectExists is for.
func (r *runtime) jsExists(target string) bool {
	r.checkCtx()
	r.curTarget = target

	err := r.browser.WaitForElement(r.ctx, target, existsBranchTimeout)
	present, fatal := existsOutcome(err)
	if fatal != "" {
		r.throw(fatal, target, "checking whether %q exists: %v", target, err)
	}
	return present
}

// jsText returns the visible text of a selector, or of the whole page.
func (r *runtime) jsText(selector string) string {
	r.checkCtx()
	if selector == "" {
		selector = "body"
	}
	r.curTarget = selector

	text, err := r.readText(selector)
	if err != nil {
		r.throwErr(err, selector, fmt.Sprintf("read text of %q", selector))
	}
	return text
}

// readText reads a selector's visible text and returns the error rather than
// throwing it, so a caller that is still waiting can tell "not there yet"
// apart from "the browser could not answer".
func (r *runtime) readText(selector string) (string, error) {
	res, err := r.browser.GetTextContent(selector, "flat")
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, g := range res.Groups {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(g.Text)
	}
	return strings.TrimSpace(sb.String()), nil
}

func (r *runtime) jsHTML() string {
	r.checkCtx()
	html, err := r.browser.HTML()
	if err != nil {
		r.throw(KindEnvironment, "", "read page HTML: %v", err)
	}
	return html
}

func (r *runtime) jsURL() string {
	r.checkCtx()
	return r.browser.CurrentURL()
}

func (r *runtime) jsTitle() string {
	r.checkCtx()
	return r.browser.PageTitle()
}

func (r *runtime) jsSnapshot() goja.Value {
	r.checkCtx()
	elements, err := r.browser.Snapshot(false)
	if err != nil {
		r.throw(KindEnvironment, "", "snapshot failed: %v", err)
	}
	return r.vm.ToValue(elements)
}

func (r *runtime) jsEval(script string) goja.Value {
	r.checkCtx()
	v, err := r.browser.Evaluate(script)
	if err != nil {
		// Evaluate failing usually means the script is wrong, not the app.
		r.throw(KindScript, "", "eval failed: %v", err)
	}
	return r.vm.ToValue(v)
}

func (r *runtime) jsConsoleErrors() goja.Value {
	r.checkCtx()
	var out []map[string]any
	for _, m := range r.browser.GetConsoleMessages(200) {
		if m.Level != "error" {
			continue
		}
		out = append(out, map[string]any{"level": m.Level, "text": m.Text})
	}
	return r.vm.ToValue(out)
}

func (r *runtime) jsFailedRequests() goja.Value {
	r.checkCtx()
	var out []map[string]any
	for _, req := range r.browser.GetFailedRequests() {
		out = append(out, map[string]any{
			"url": req.URL, "method": req.Method, "status": req.Status, "error": req.ErrorText,
		})
	}
	return r.vm.ToValue(out)
}

// --- pages -------------------------------------------------------------------

func (r *runtime) jsNewPage(url string) {
	r.checkCtx()
	if err := r.browser.NewPage(r.ctx, r.resolve(url)); err != nil {
		r.throw(KindEnvironment, url, "open new page: %v", err)
	}
}

func (r *runtime) jsListPages() goja.Value {
	r.checkCtx()
	return r.vm.ToValue(r.browser.ListPages())
}

func (r *runtime) jsSelectPage(index int) {
	r.checkCtx()
	if err := r.browser.SelectPage(index); err != nil {
		r.throw(KindEnvironment, "", "select page %d: %v", index, err)
	}
}

func (r *runtime) jsClosePage(index int) {
	r.checkCtx()
	if err := r.browser.ClosePage(index); err != nil {
		r.throw(KindEnvironment, "", "close page %d: %v", index, err)
	}
}

// --- helpers -----------------------------------------------------------------

func intOf(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

func durationOf(v any, fallback time.Duration) time.Duration {
	if ms := intOf(v); ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return fallback
}

// asException reports whether err is a goja exception, without pulling in
// errors.As at every call site.
func asException(err error, out **goja.Exception) bool {
	ex, ok := err.(*goja.Exception)
	if ok {
		*out = ex
	}
	return ok
}
