package testscript

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

// Hoisting a repeated sequence into a shared operation rewrites scripts that
// already pass, and the model doing the rewriting is the same one whose
// mistakes the rest of this package exists to catch. Running the rewritten
// script afterwards is necessary but not sufficient: a refactor that quietly
// weakens an assertion passes too, and passes for ever after.
//
// So the guarantee is mechanical and comes first. An extraction may move
// navigation, clicks, fills and waits. It may never move, alter, reorder or
// drop an assertion. That is checkable on the syntax tree without running
// anything, and it holds whatever the model did — which is the only kind of
// guarantee worth having about generated code.

// AssertionSignature is what a script claims about the application, in the
// order it claims it, tagged with the step each claim sits in.
//
// Two scripts with the same signature test the same thing. A refactor is
// allowed to change everything else.
func AssertionSignature(source string) ([]string, error) {
	prg, err := parser.ParseFile(nil, "script.js", source, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing the script: %w", err)
	}

	steps := stepsIn(prg)

	var sig []string
	walk(prg, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpression)
		if !ok || !isAssertion(call) {
			return true
		}
		// A matcher call contains the expect() inside it, so recording both
		// would count one assertion twice. Only the outermost is taken: an
		// inner expect() with no matcher is not an assertion anyway, which is
		// why the lint refuses it.
		if calleeName(call.Callee) == "expect" {
			return true
		}

		step := stepAt(steps, int(call.Idx0()))
		sig = append(sig, fmt.Sprintf("step %d: %s",
			step.number, normaliseSource(sourceOf(source, call))))
		return true
	})

	return sig, nil
}

// AssertionsUnchanged reports whether a rewrite left every claim intact,
// naming the first difference when it did not.
func AssertionsUnchanged(before, after string) (bool, string, error) {
	was, err := AssertionSignature(before)
	if err != nil {
		return false, "", fmt.Errorf("reading the original: %w", err)
	}
	now, err := AssertionSignature(after)
	if err != nil {
		return false, "", fmt.Errorf("reading the rewrite: %w", err)
	}

	if len(was) != len(now) {
		return false, fmt.Sprintf("asserted %d things before and %d after", len(was), len(now)), nil
	}
	for i := range was {
		if was[i] != now[i] {
			return false, fmt.Sprintf("%s\n  became %s", was[i], now[i]), nil
		}
	}
	return true, "", nil
}

// sourceOf returns the exact text a node was parsed from.
func sourceOf(source string, n ast.Node) string {
	from, to := int(n.Idx0())-1, int(n.Idx1())-1
	if from < 0 || to > len(source) || from >= to {
		return ""
	}
	return source[from:to]
}

// normaliseSource removes the formatting from an assertion, so that
// reindenting one is not mistaken for rewriting it. An extraction changes the
// shape of the code around an assertion — that is what it is for — and a check
// that rejected reflowed code would reject every real extraction.
//
// Whitespace *outside* string literals only. Collapsing it everywhere would
// make toBe("a  b") and toBe("a b") the same claim, which is a different
// assertion about a different page, and a guarantee that quietly forgives that
// is not a guarantee. Everything else is left exactly as written: changing a
// quote style or a digit is a real change and should be reported as one.
func normaliseSource(s string) string {
	var out strings.Builder
	var quote byte
	pendingSpace := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if quote != 0 {
			out.WriteByte(c)
			switch {
			case c == '\\' && i+1 < len(s):
				// An escaped character cannot close the string.
				i++
				out.WriteByte(s[i])
			case c == quote:
				quote = 0
			}
			continue
		}

		if c == '"' || c == '\'' || c == '`' {
			if pendingSpace && needsSpace(lastByte(&out)) {
				out.WriteByte(' ')
			}
			pendingSpace = false
			quote = c
			out.WriteByte(c)
			continue
		}

		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			pendingSpace = out.Len() > 0
			continue
		}

		// A space is only meaningful between two things that would otherwise
		// run together, which is why `f( x )` and `f(x)` are the same call and
		// `a b` and `ab` are not.
		if pendingSpace && needsSpace(lastByte(&out)) && needsSpace(c) {
			out.WriteByte(' ')
		}
		pendingSpace = false
		out.WriteByte(c)
	}

	return out.String()
}

// needsSpace reports whether a byte is part of a word, as opposed to
// punctuation that separates words on its own.
func needsSpace(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_', c == '$':
		return true
	default:
		return false
	}
}

func lastByte(b *strings.Builder) byte {
	s := b.String()
	if s == "" {
		return 0
	}
	return s[len(s)-1]
}

// Overlap is a run of operations that more than one script performs.
type Overlap struct {
	// Steps renders the shared sequence, one operation per entry, for a
	// report a person reads.
	Steps []string
	// Scripts names the files that perform it, by path.
	Scripts []string
}

// minOverlap is how many operations in a row make a sequence worth hoisting.
//
// Two: one repeated call is a coincidence — every script navigates — and
// naming it costs more than it saves. Three would miss the commonest real
// case, which is a two-step sign-in.
const minOverlap = 2

// FindOverlaps reports operation sequences that appear in more than one of a
// directory's compiled scripts.
//
// A cheap static pass on purpose. Extraction is on by default, so the common
// case — a compile that duplicates nothing — has to cost nothing, and asking a
// model to look for repetition that is not there would be a charge on every
// compile in the project. Only when this finds something does anything
// expensive happen.
//
// Assertions are excluded from the comparison entirely. Two scripts asserting
// the same thing are not duplicating an operation, and an assertion is the one
// thing extraction may never move.
func FindOverlaps(scripts map[string]string) ([]Overlap, error) {
	type script struct {
		path string
		runs [][]operation
	}

	var all []script
	for _, path := range sortedPaths(scripts) {
		runs, err := operationSequence(scripts[path])
		if err != nil {
			// A script that does not parse cannot be refactored, and is not a
			// reason to refuse to look at the others.
			continue
		}
		all = append(all, script{path: path, runs: runs})
	}

	var out []Overlap
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			best, ok := bestSharedRun(all[i].runs, all[j].runs)
			if !ok {
				continue
			}
			out = append(out, Overlap{
				Steps:   renderRun(best),
				Scripts: []string{all[i].path, all[j].path},
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Steps) != len(out[j].Steps) {
			return len(out[i].Steps) > len(out[j].Steps)
		}
		return strings.Join(out[i].Scripts, ",") < strings.Join(out[j].Scripts, ",")
	})

	return out, nil
}

// bestSharedRun finds the longest sequence shared by any run of one script and
// any run of the other.
func bestSharedRun(a, b [][]operation) ([]operation, bool) {
	var best []operation
	for _, ra := range a {
		for _, rb := range b {
			if shared, ok := longestSharedRun(ra, rb); ok && len(shared) > len(best) {
				best = shared
			}
		}
	}
	return best, len(best) >= minOverlap
}

// longestSharedRun finds the longest sequence of operations both scripts
// perform, in the same order.
//
// A subsequence, not a contiguous run. Two compiles of the same journey
// interleave it differently — one counts the links before clicking and the
// other after — so requiring the operations to be adjacent finds nothing on
// exactly the scripts this exists to serve. Observed on two specs that both
// open the tags page and click a tag: navigate→click→waitFor against
// navigate→eval→click, sharing everything that matters and adjacent in
// neither.
//
// What is returned is the shared spine. Whether it is worth naming is the
// agent's call, and it is told it may decline.
func longestSharedRun(a, b []operation) ([]operation, bool) {
	// Longest common subsequence, with sameOperation for equality.
	table := make([][]int, len(a)+1)
	for i := range table {
		table[i] = make([]int, len(b)+1)
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if sameOperation(a[i-1], b[j-1]) {
				table[i][j] = table[i-1][j-1] + 1
				continue
			}
			table[i][j] = max(table[i-1][j], table[i][j-1])
		}
	}

	if table[len(a)][len(b)] < minOverlap {
		return nil, false
	}

	var run []operation
	for i, j := len(a), len(b); i > 0 && j > 0; {
		switch {
		case sameOperation(a[i-1], b[j-1]):
			run = append(run, a[i-1])
			i, j = i-1, j-1
		case table[i-1][j] >= table[i][j-1]:
			i--
		default:
			j--
		}
	}
	slices.Reverse(run)

	return run, true
}

func sortedPaths(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// operation is one thing a script does to the page.
type operation struct {
	// name is the call, like "atr.click".
	name string
	// tokens are the identifiers and literals it was given, which is the
	// evidence that two calls are about the same thing.
	tokens map[string]bool
	// text is the source, for reporting.
	text string
}

// operationSequence is what a script *does*, grouped into the runs that could
// become one function.
//
// A run is broken by a step boundary and by an assertion. Hoisting replaces a
// contiguous block of statements inside one step with a call, so operations
// either side of an assertion cannot be gathered into one operation without
// moving the assertion, which is the one thing extraction may never do.
//
// Learned by proposing an overlap the agent then had to refuse: two specs
// that both opened a tag page shared a navigate and a click, with an
// expectExists between them and a step boundary as well. Detection that
// ignores the constraint the extractor works under just buys refusals, one
// model call at a time.
func operationSequence(source string) ([][]operation, error) {
	prg, err := parser.ParseFile(nil, "script.js", source, 0)
	if err != nil {
		return nil, err
	}

	steps := stepsIn(prg)
	var runs [][]operation

	for _, s := range steps {
		var current []operation

		walk(s.body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpression)
			if !ok {
				return true
			}

			if isAssertion(call) {
				// The run ends here: what follows cannot join what precedes
				// without carrying the assertion along.
				if len(current) > 0 {
					runs = append(runs, current)
					current = nil
				}
				return true
			}

			name := calleeName(call.Callee)
			if !strings.HasPrefix(name, "atr.") || nonThrowing[strings.TrimPrefix(name, "atr.")] {
				return true
			}
			if name == "atr.step" || name == "atr.setup" {
				return true
			}

			current = append(current, operation{
				name:   name,
				tokens: tokensIn(call),
				text:   normaliseSource(sourceOf(source, call)),
			})
			return true
		})

		if len(current) > 0 {
			runs = append(runs, current)
		}
	}

	return runs, nil
}

// tokensIn collects the names and literals a call was given.
func tokensIn(call *ast.CallExpression) map[string]bool {
	out := map[string]bool{}
	for _, arg := range call.ArgumentList {
		walk(arg, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.Identifier:
				out[string(v.Name)] = true
			case *ast.StringLiteral:
				if lit := strings.TrimSpace(string(v.Value)); lit != "" {
					out[lit] = true
				}
			}
			return true
		})
	}
	return out
}

// sameOperation reports whether two calls are plausibly the same step of the
// same journey.
//
// Not textual equality. Two compiles of similar specs never produce identical
// operations — one scopes a selector to the main region and the other does
// not, one waits for the list and the other asserts it — so an exact match
// finds nothing on precisely the directories this exists to serve. Observed:
// two specs that both open the tags page and click a tag shared exactly one
// call verbatim.
//
// So: the same call, given at least one thing in common. The shared token is
// what separates "both click a tag link" from "both click something".
func sameOperation(a, b operation) bool {
	if a.name != b.name {
		return false
	}
	if a.text == b.text {
		return true
	}
	for token := range a.tokens {
		if b.tokens[token] {
			return true
		}
	}
	return false
}

// renderRun describes a sequence for a person, preferring the text of the
// script it came from.
func renderRun(run []operation) []string {
	out := make([]string, 0, len(run))
	for _, op := range run {
		out = append(out, op.text)
	}
	return out
}
