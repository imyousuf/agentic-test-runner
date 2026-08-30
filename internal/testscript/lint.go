package testscript

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

// A false pass is the worst outcome this project can produce. It is worse than
// a crash, because nobody investigates a green test — a suite full of them
// reports that everything works right up until someone opens the application
// and finds it broken.
//
// Every defence against it used to be prose: the compile prompt asking the
// model nicely, a skill asking the author nicely. But the defects are visible
// in the generated JavaScript, statically, with no model and no browser. A
// step that contains nothing that can fail is not a judgement call; neither is
// a substring match against the whole page.
//
// So this is a lint over the compiled script, run before it is accepted.
//
// It deliberately does NOT call the model. The obvious next move — hand the
// findings to the repair loop and let it add the missing assertions — is the
// false pass arriving through a different door: a model asked to invent what
// the application must do will invent something that passes. Only a person
// knows what the test was for.

// Severity says what a finding costs.
type Severity string

const (
	// SeverityBlocking marks a script that cannot fail. Accepting it would
	// mean adding a test that reports success unconditionally.
	SeverityBlocking Severity = "blocking"
	// SeverityWarn marks a script that can fail, but for weaker reasons than
	// it appears to. Reported, not enforced: the judgement of whether a
	// particular match is too loose belongs to whoever wrote the spec.
	SeverityWarn Severity = "warn"
)

// Finding is one defect in a compiled script.
type Finding struct {
	// Code identifies the rule, for anyone who needs to grep or suppress.
	Code string
	// Severity is whether this blocks acceptance.
	Severity Severity
	// Step is the step the defect is in, 0 for the script as a whole.
	Step int
	// StepDesc is that step's description, so a report can name it the way
	// the spec does.
	StepDesc string
	// Message says what is wrong and what to do instead.
	Message string
}

func (f Finding) String() string {
	if f.Step > 0 {
		return fmt.Sprintf("step %d (%s): %s", f.Step, f.StepDesc, f.Message)
	}
	return f.Message
}

// Finding codes.
const (
	CodeNoAssertions   = "no-assertions"
	CodeStepCannotFail = "step-cannot-fail"
	CodeWeakTextMatch  = "weak-text-match"
	CodeSwallowed      = "swallowed-assertion"
	CodeFixedSleep     = "fixed-sleep"
)

// weakNeedle is how short a substring has to be, matched against the whole
// page, before it is more likely to match something else. Eleven characters is
// not a principle; it is short enough that "archiv" and "Success" are caught
// and long enough that a real sentence is not.
const weakNeedle = 12

// nonThrowing are the calls that answer a question rather than perform an
// action. A step built only from these cannot fail whatever the application
// does — which is the entire finding.
var nonThrowing = map[string]bool{
	"log":            true,
	"exists":         true,
	"sleep":          true,
	"url":            true,
	"title":          true,
	"listPages":      true,
	"consoleErrors":  true,
	"failedRequests": true,
}

// assertions are the calls that state what the application must do on their
// own. expect is deliberately absent: `expect(x)` builds a matcher object and
// asserts nothing, so counting the bare call would let a step of
// `expect(atr.text("#b"));` pass as one that can fail. Only the matcher call
// on the end of it counts — see matcherCall.
var assertions = map[string]bool{
	"atr.fail":          true,
	"atr.expectExists":  true,
	"atr.expectMissing": true,
}

// isAssertion reports whether a call states something about the application.
func isAssertion(call *ast.CallExpression) bool {
	if assertions[calleeName(call.Callee)] {
		return true
	}
	_, _, ok := matcherCall(call)
	return ok
}

// Lint reports the ways a compiled script could pass without testing anything.
//
// A script that does not parse produces no findings and an error: that case is
// already reported properly by the runtime as a script fault, and duplicating
// it here would only make the message worse.
func Lint(source string) ([]Finding, error) {
	prg, err := parser.ParseFile(nil, "script.js", source, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing the compiled script: %w", err)
	}

	var findings []Finding
	exempt := sleepsInsideRetry(prg)
	locals := localFunctions(prg)

	steps := stepsIn(prg)
	assertionsAnywhere := 0

	for _, s := range steps {
		asserts, throws := stepCapabilities(s.body, locals, map[string]bool{})
		assertionsAnywhere += asserts

		if asserts == 0 && !throws {
			findings = append(findings, Finding{
				Code:     CodeStepCannotFail,
				Severity: SeverityBlocking,
				Step:     s.number,
				StepDesc: s.desc,
				Message: "this step contains nothing that can fail — it neither asserts " +
					"anything nor performs an action that throws, so it reports success " +
					"whatever the application does",
			})
		}
	}

	// Counted over the whole program, not only inside steps: an assertion in a
	// helper the steps call still means the script can fail.
	if assertionsAnywhere == 0 && countAssertions(prg) == 0 {
		findings = append(findings, Finding{
			Code:     CodeNoAssertions,
			Severity: SeverityBlocking,
			Message: "this script asserts nothing at all, so it passes whatever the " +
				"application does — say in the spec's Expected Results what must be " +
				"true for the test to have passed",
		})
	}

	findings = append(findings, swallowedAssertions(prg, steps)...)
	findings = append(findings, weakMatches(prg, steps)...)
	findings = append(findings, fixedSleeps(prg, steps, exempt)...)

	sortFindings(findings)
	return findings, nil
}

// Blocking returns only the findings that must not be accepted.
func Blocking(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Severity == SeverityBlocking {
			out = append(out, f)
		}
	}
	return out
}

// --- structure ---------------------------------------------------------------

type lintStep struct {
	number int
	desc   string
	body   ast.Node
	// span is the source range of the step's callback, used to attribute a
	// finding found elsewhere in the tree to the step it sits in.
	from, to int
}

// stepsIn finds the atr.step and atr.setup callbacks.
func stepsIn(prg *ast.Program) []lintStep {
	var steps []lintStep

	walk(prg, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpression)
		if !ok {
			return true
		}
		name := calleeName(call.Callee)
		if name != "atr.step" && name != "atr.setup" {
			return true
		}
		if len(call.ArgumentList) == 0 {
			return true
		}

		body := call.ArgumentList[len(call.ArgumentList)-1]
		if !isCallable(body) {
			return true
		}

		s := lintStep{body: body, from: int(body.Idx0()), to: int(body.Idx1())}
		if name == "atr.step" {
			s.number = intLiteral(call.ArgumentList[0])
			if len(call.ArgumentList) > 1 {
				s.desc = stringLiteral(call.ArgumentList[1])
			}
		} else {
			s.desc = "setup: " + stringLiteral(call.ArgumentList[0])
		}
		steps = append(steps, s)
		return true
	})

	return steps
}

// stepCapabilities reports how many assertions a step body makes and whether
// anything in it can throw.
//
// A call to a function the script declared itself is followed into that
// function's body. Without that, `atr.step(1, "x", () => { report(); })` where
// report only logs would be read as a step that can fail, and wrapping is not
// a thing a compiler does deliberately but is exactly what a model does when
// it tidies. seen stops a recursive helper from doing the same to us.
func stepCapabilities(body ast.Node, locals map[string]ast.Node, seen map[string]bool) (asserts int, throws bool) {
	swallowed := assertionsInsideTry(body)

	walk(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ThrowStatement:
			throws = true
		case *ast.CallExpression:
			name := calleeName(v.Callee)
			if isAssertion(v) {
				// An assertion whose throw is caught and dropped states
				// nothing: the step goes green whatever the application did.
				if swallowed[n] {
					return true
				}
				asserts++
				throws = true
				return true
			}
			if method, ok := strings.CutPrefix(name, "atr."); ok {
				if !nonThrowing[method] {
					throws = true
				}
				return true
			}
			if fn, ok := locals[name]; ok && !seen[name] {
				seen[name] = true
				a, t := stepCapabilities(fn, locals, seen)
				asserts += a
				throws = throws || t
				return true
			}
			// values.get on a key this checkout does not define throws, and a
			// call to something declared elsewhere — a shared library — is not
			// visible here, so it is taken at its word.
			if _, known := locals[name]; name != "" && !known && !strings.HasPrefix(name, "console.") {
				throws = true
			}
		}
		return true
	})
	return asserts, throws
}

// localFunctions collects the functions the script declares for itself, by
// name, so a call to one can be followed rather than assumed.
//
// Top-level declarations and `const f = () => {}` both, since the compiler
// emits either. Anything it cannot name — a method, a function built at run
// time — is simply absent, and a call to it is taken at its word.
func localFunctions(prg *ast.Program) map[string]ast.Node {
	out := map[string]ast.Node{}

	walk(prg, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FunctionDeclaration:
			if v.Function != nil && v.Function.Name != nil {
				out[string(v.Function.Name.Name)] = v.Function
			}
		case *ast.Binding:
			id, ok := v.Target.(*ast.Identifier)
			if !ok {
				return true
			}
			if isCallable(v.Initializer) {
				out[string(id.Name)] = v.Initializer
			}
		}
		return true
	})

	return out
}

func countAssertions(n ast.Node) int {
	swallowed := assertionsInsideTry(n)

	count := 0
	walk(n, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpression)
		if ok && isAssertion(call) && !swallowed[node] {
			count++
		}
		return true
	})
	return count
}

// assertionsInsideTry marks the assertions whose failure is caught.
//
// A compiled script has no legitimate reason to catch its own assertion.
// atr.retry exists for transient failures, and an assertion is deliberately
// the one kind that is never retried — so a try/catch around one can only
// turn a red test green.
func assertionsInsideTry(root ast.Node) map[ast.Node]bool {
	swallowed := map[ast.Node]bool{}

	walk(root, func(n ast.Node) bool {
		try, ok := n.(*ast.TryStatement)
		if !ok || try.Catch == nil || try.Body == nil {
			return true
		}
		// A catch that rethrows, or fails the test itself, is not swallowing
		// anything — the failure still reaches the runner, which is all the
		// rule is about.
		if catchEscalates(try.Catch) {
			return true
		}
		walk(try.Body, func(inner ast.Node) bool {
			if call, ok := inner.(*ast.CallExpression); ok && isAssertion(call) {
				swallowed[inner] = true
			}
			return true
		})
		return true
	})

	return swallowed
}

// catchEscalates reports whether a catch block rethrows or fails the test.
func catchEscalates(catch *ast.CatchStatement) bool {
	escalates := false
	walk(catch, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ThrowStatement:
			escalates = true
		case *ast.CallExpression:
			if isAssertion(v) {
				escalates = true
			}
		}
		return true
	})
	return escalates
}

// swallowedAssertions reports the assertions a catch block discards.
//
// One finding per step, not per assertion: three swallowed assertions in one
// try are one mistake, and iterating the set of nodes would order the findings
// differently on every run, since Go randomises map iteration.
func swallowedAssertions(prg *ast.Program, steps []lintStep) []Finding {
	perStep := map[int]lintStep{}
	var order []int

	for node := range assertionsInsideTry(prg) {
		step := stepAt(steps, int(node.Idx0()))
		if _, seen := perStep[step.from]; seen {
			continue
		}
		perStep[step.from] = step
		order = append(order, step.from)
	}
	sort.Ints(order)

	var findings []Finding
	for _, at := range order {
		step := perStep[at]
		findings = append(findings, Finding{
			Code:     CodeSwallowed,
			Severity: SeverityBlocking,
			Step:     step.number,
			StepDesc: step.desc,
			Message: "an assertion here is inside a try whose catch discards it, so it " +
				"states nothing — the step goes green whatever the application did. " +
				"Assertions are never retried on purpose; use atr.retry around the " +
				"action if it is the action that is flaky",
		})
	}

	return findings
}

// --- weak matches ------------------------------------------------------------

// weakMatches finds substring assertions against the whole page.
//
// expect(atr.text()).toContain("ok") reads like an assertion and behaves like
// one, but atr.text() with no selector returns everything on the page, so a
// short needle matches unrelated content. One real spec searched for "archiv"
// and matched three things that had nothing to do with the feature.
func weakMatches(prg *ast.Program, steps []lintStep) []Finding {
	var findings []Finding

	walk(prg, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpression)
		if !ok {
			return true
		}
		matcher, subject, ok := matcherCall(call)
		if !ok || (matcher != "toContain" && matcher != "toMatch") {
			return true
		}
		if !isWholePageRead(subject) {
			return true
		}
		if len(call.ArgumentList) != 1 {
			return true
		}
		needle := stringLiteral(call.ArgumentList[0])
		if needle == "" || len([]rune(needle)) >= weakNeedle {
			return true
		}

		step := stepAt(steps, int(call.Idx0()))
		findings = append(findings, Finding{
			Code:     CodeWeakTextMatch,
			Severity: SeverityWarn,
			Step:     step.number,
			StepDesc: step.desc,
			Message: fmt.Sprintf(
				"%q is matched against the text of the whole page, so it can match "+
					"something unrelated and pass for the wrong reason — read the "+
					"element you mean instead", needle),
		})
		return true
	})

	return findings
}

// isWholePageRead reports whether an expression reads the entire page's text
// or markup rather than one element's.
func isWholePageRead(e ast.Expression) bool {
	call, ok := e.(*ast.CallExpression)
	if !ok {
		return false
	}
	switch calleeName(call.Callee) {
	case "atr.html":
		return true
	case "atr.text":
		// No selector, or an empty one, means the body.
		return len(call.ArgumentList) == 0 || stringLiteral(call.ArgumentList[0]) == ""
	default:
		return false
	}
}

// matcherCall recognises expect(subject).matcher(...).
func matcherCall(call *ast.CallExpression) (matcher string, subject ast.Expression, ok bool) {
	dot, ok := call.Callee.(*ast.DotExpression)
	if !ok {
		return "", nil, false
	}
	inner, ok := dot.Left.(*ast.CallExpression)
	if !ok {
		return "", nil, false
	}
	if calleeName(inner.Callee) != "expect" || len(inner.ArgumentList) == 0 {
		return "", nil, false
	}
	return string(dot.Identifier.Name), inner.ArgumentList[0], true
}

// --- fixed sleeps ------------------------------------------------------------

// fixedSleeps finds waits for a duration rather than for a state.
//
// A fixed sleep is a race that has not happened yet: it passes on the machine
// it was written on and fails on a slower one, or — worse — passes on a slower
// one by outlasting a page that is actually broken.
func fixedSleeps(prg *ast.Program, steps []lintStep, exempt map[ast.Node]bool) []Finding {
	var findings []Finding

	walk(prg, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpression)
		if !ok || calleeName(call.Callee) != "atr.sleep" || exempt[n] {
			return true
		}
		step := stepAt(steps, int(call.Idx0()))
		findings = append(findings, Finding{
			Code:     CodeFixedSleep,
			Severity: SeverityWarn,
			Step:     step.number,
			StepDesc: step.desc,
			Message: "waits for a fixed duration rather than for a state — use " +
				"atr.waitFor or atr.waitForText, which fail loudly instead of " +
				"silently continuing too early",
		})
		return true
	})

	return findings
}

// sleepsInsideRetry collects the sleeps that are part of a retry loop, where
// spacing attempts out is the point rather than a guess.
func sleepsInsideRetry(prg *ast.Program) map[ast.Node]bool {
	exempt := map[ast.Node]bool{}

	walk(prg, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpression)
		if !ok || calleeName(call.Callee) != "atr.retry" {
			return true
		}
		walk(call, func(inner ast.Node) bool {
			if c, ok := inner.(*ast.CallExpression); ok && calleeName(c.Callee) == "atr.sleep" {
				exempt[inner] = true
			}
			return true
		})
		return true
	})

	return exempt
}

// --- helpers -----------------------------------------------------------------

// stepAt returns the step whose callback encloses an offset, so a finding
// anywhere in the tree can be reported against the step a reader would look
// for it in.
func stepAt(steps []lintStep, offset int) lintStep {
	for _, s := range steps {
		if offset >= s.from && offset <= s.to {
			return s
		}
	}
	return lintStep{}
}

// calleeName renders a callee as "expect", "atr.click", "values.get". Anything
// it cannot name flatly — a call on a call, an index expression — returns "".
func calleeName(e ast.Expression) string {
	switch v := e.(type) {
	case *ast.Identifier:
		return string(v.Name)
	case *ast.DotExpression:
		left := calleeName(v.Left)
		if left == "" {
			return ""
		}
		return left + "." + string(v.Identifier.Name)
	default:
		return ""
	}
}

func stringLiteral(e ast.Expression) string {
	if s, ok := e.(*ast.StringLiteral); ok {
		return string(s.Value)
	}
	return ""
}

func intLiteral(e ast.Expression) int {
	n, ok := e.(*ast.NumberLiteral)
	if !ok {
		return 0
	}
	switch v := n.Value.(type) {
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func isCallable(e ast.Expression) bool {
	switch e.(type) {
	case *ast.FunctionLiteral, *ast.ArrowFunctionLiteral:
		return true
	default:
		return false
	}
}

func sortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		if f[i].Severity != f[j].Severity {
			return f[i].Severity == SeverityBlocking
		}
		if f[i].Step != f[j].Step {
			return f[i].Step < f[j].Step
		}
		return f[i].Code < f[j].Code
	})
}

// --- walking -----------------------------------------------------------------

var nodeInterface = reflect.TypeFor[ast.Node]()

// walk visits every node in a subtree.
//
// Reflection rather than a hand-written visitor over goja's node types: there
// are around eighty of them, the model can emit any JavaScript at all, and a
// visitor that forgets one silently stops looking inside it — which for a lint
// means quietly passing the thing it was meant to catch. A generic walk
// descends into constructs nobody thought about, including ones a future goja
// adds.
func walk(root any, visit func(ast.Node) bool) {
	seen := map[any]bool{}
	walkValue(reflect.ValueOf(root), seen, visit)
}

func walkValue(v reflect.Value, seen map[any]bool, visit func(ast.Node) bool) {
	if !v.IsValid() {
		return
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		walkValue(v.Elem(), seen, visit)

	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		// The AST shares nodes between a function's body and its declaration
		// list, so without this a variable declaration is visited twice and
		// any finding inside it is reported twice.
		key := v.Interface()
		if seen[key] {
			return
		}
		seen[key] = true

		if v.Type().Implements(nodeInterface) {
			if n, ok := key.(ast.Node); ok && !visit(n) {
				return
			}
		}
		walkValue(v.Elem(), seen, visit)

	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkValue(v.Index(i), seen, visit)
		}

	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			walkValue(v.Field(i), seen, visit)
		}
	}
}
