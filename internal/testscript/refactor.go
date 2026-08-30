package testscript

import (
	"fmt"
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
	ops := make(map[string][]string, len(scripts))
	for path, source := range scripts {
		seq, err := operationSequence(source)
		if err != nil {
			// A script that does not parse cannot be refactored, and is not a
			// reason to refuse to look at the others.
			continue
		}
		ops[path] = seq
	}

	// Every run of minOverlap or more, and which scripts perform it.
	seen := map[string]map[string]bool{}
	for path, seq := range ops {
		for size := len(seq); size >= minOverlap; size-- {
			for start := 0; start+size <= len(seq); start++ {
				key := strings.Join(seq[start:start+size], "\n")
				if seen[key] == nil {
					seen[key] = map[string]bool{}
				}
				seen[key][path] = true
			}
		}
	}

	var out []Overlap
	for key, where := range seen {
		if len(where) < 2 {
			continue
		}
		steps := strings.Split(key, "\n")
		paths := make([]string, 0, len(where))
		for p := range where {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		out = append(out, Overlap{Steps: steps, Scripts: paths})
	}

	// Longest first: a six-operation sign-in is worth naming, and the
	// two-operation prefix inside it is the same finding said smaller.
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Steps) != len(out[j].Steps) {
			return len(out[i].Steps) > len(out[j].Steps)
		}
		return strings.Join(out[i].Steps, "\n") < strings.Join(out[j].Steps, "\n")
	})

	return longestOnly(out), nil
}

// longestOnly drops a sequence that is wholly contained in a longer one
// covering the same scripts, so a six-operation overlap is reported once
// rather than as every window inside it.
func longestOnly(all []Overlap) []Overlap {
	var out []Overlap
	for _, candidate := range all {
		contained := false
		for _, kept := range out {
			if sameScripts(kept.Scripts, candidate.Scripts) &&
				strings.Contains(strings.Join(kept.Steps, "\n"), strings.Join(candidate.Steps, "\n")) {
				contained = true
				break
			}
		}
		if !contained {
			out = append(out, candidate)
		}
	}
	return out
}

func sameScripts(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// operationSequence is what a script *does*, in order, with everything it
// claims left out.
func operationSequence(source string) ([]string, error) {
	prg, err := parser.ParseFile(nil, "script.js", source, 0)
	if err != nil {
		return nil, err
	}

	steps := stepsIn(prg)
	var seq []string

	for _, s := range steps {
		walk(s.body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpression)
			if !ok || isAssertion(call) {
				return true
			}
			name := calleeName(call.Callee)
			// Only the calls that do something to the page. A helper the
			// script defines is already shared; values.get is an input.
			if !strings.HasPrefix(name, "atr.") || nonThrowing[strings.TrimPrefix(name, "atr.")] {
				return true
			}
			if name == "atr.step" || name == "atr.setup" {
				return true
			}
			seq = append(seq, normaliseSource(sourceOf(source, call)))
			return true
		})
	}

	return seq, nil
}
