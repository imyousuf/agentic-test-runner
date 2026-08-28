package testscript

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/dop251/goja"
)

// matches implements toMatch for both shapes a caller can pass.
//
// A JavaScript regular expression is matched by JavaScript, not by Go. The
// two are not the same language: goja renders a literal /^a-z$/ as the string
// "/^a-z$/", delimiters and all, so compiling that as a Go pattern demanded
// the value contain literal slashes and no real value ever could. The reported
// symptom was an id failing "text matching //^[A-Za-z0-9_-]+$//" — the second
// pair of slashes being the tell.
//
// Handing it back to the engine also keeps the dialect honest. Go's regexp is
// RE2, which has no lookahead and no backreferences, so a perfectly ordinary
// JavaScript pattern using (?=...) would not merely mismatch — it would fail
// to compile at all. And flags come for free: /i and /m mean what they say.
//
// A plain string is still compiled as a Go pattern, which is what
// expect(x).toMatch("^abc") has always meant.
func (r *runtime) matches(want, actual goja.Value) (bool, string) {
	if re, ok := asRegExp(r.vm, want); ok {
		test, ok := goja.AssertFunction(re.Get("test"))
		if !ok {
			r.throw(KindScript, "", "expect(...).toMatch: %s has no test method", want.String())
		}
		res, err := test(re, r.vm.ToValue(actual.String()))
		if err != nil {
			r.throw(KindScript, "", "expect(...).toMatch: %v", err)
		}
		return res.ToBoolean(),
			fmt.Sprintf("text matching %s, got %s", want.String(), render(actual))
	}

	pattern := want.String()
	re, err := regexp.Compile(pattern)
	if err != nil {
		// A bad pattern is a defect in the generated script, not a
		// statement about the application.
		r.throw(KindScript, "", "expect(...).toMatch: invalid pattern %q: %v", pattern, err)
	}
	return re.MatchString(actual.String()),
		fmt.Sprintf("text matching /%s/, got %s", pattern, render(actual))
}

// asRegExp reports whether v is a JavaScript RegExp.
func asRegExp(vm *goja.Runtime, v goja.Value) (*goja.Object, bool) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil, false
	}
	obj, ok := v.(*goja.Object)
	if !ok || obj.ClassName() != "RegExp" {
		return nil, false
	}
	_ = vm
	return obj, true
}

// installExpect adds the global expect() used for assertions.
//
// Everything thrown from here is KindAssertion, and that is the point: an
// assertion is the script asserting something about the *application*. When
// one fails the script is doing its job, so repair must never touch it. Every
// other failure kind is about the harness or the script, and those are fair
// game to fix automatically.
func (r *runtime) installExpect() error {
	expect := func(call goja.FunctionCall) goja.Value {
		actual := call.Argument(0)
		obj := r.vm.NewObject()

		must := func(name string, fn func(args []goja.Value) (ok bool, detail string)) {
			_ = obj.Set(name, func(c goja.FunctionCall) goja.Value {
				ok, detail := fn(c.Arguments)
				if !ok {
					r.throw(KindAssertion, r.curTarget, "expected %s", detail)
				}
				return goja.Undefined()
			})
		}

		must("toBe", func(args []goja.Value) (bool, string) {
			want := args[0]
			return sameValue(actual, want),
				fmt.Sprintf("%s, got %s", render(want), render(actual))
		})

		must("toEqual", func(args []goja.Value) (bool, string) {
			want := args[0]
			return reflect.DeepEqual(actual.Export(), want.Export()),
				fmt.Sprintf("%s, got %s", render(want), render(actual))
		})

		must("toContain", func(args []goja.Value) (bool, string) {
			want := args[0].String()
			return strings.Contains(actual.String(), want),
				fmt.Sprintf("text containing %q, got %s", want, render(actual))
		})

		must("toMatch", func(args []goja.Value) (bool, string) {
			return r.matches(args[0], actual)
		})

		must("toBeTruthy", func([]goja.Value) (bool, string) {
			return actual.ToBoolean(), fmt.Sprintf("a truthy value, got %s", render(actual))
		})

		must("toBeFalsy", func([]goja.Value) (bool, string) {
			return !actual.ToBoolean(), fmt.Sprintf("a falsy value, got %s", render(actual))
		})

		must("toBeGreaterThan", func(args []goja.Value) (bool, string) {
			want := args[0].ToFloat()
			return actual.ToFloat() > want,
				fmt.Sprintf("a value greater than %v, got %s", want, render(actual))
		})

		must("toBeLessThan", func(args []goja.Value) (bool, string) {
			want := args[0].ToFloat()
			return actual.ToFloat() < want,
				fmt.Sprintf("a value less than %v, got %s", want, render(actual))
		})

		must("toHaveLength", func(args []goja.Value) (bool, string) {
			want := int(args[0].ToInteger())
			got := lengthOf(actual)
			return got == want, fmt.Sprintf("length %d, got %d", want, got)
		})

		return obj
	}

	return r.vm.Set("expect", expect)
}

// sameValue compares two JS values the way a test author expects: by value
// for primitives, structurally for exported objects.
func sameValue(a, b goja.Value) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.SameAs(b) {
		return true
	}
	return reflect.DeepEqual(a.Export(), b.Export())
}

// render formats a value for an assertion message, trimming anything long
// enough to bury the actual difference in noise.
func render(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) {
		return "undefined"
	}
	if goja.IsNull(v) {
		return "null"
	}
	s := v.String()
	const max = 200
	if len(s) > max {
		s = s[:max] + "…"
	}
	return fmt.Sprintf("%q", s)
}

// lengthOf returns the length of a string or array-like value.
func lengthOf(v goja.Value) int {
	if v == nil {
		return 0
	}
	if obj, ok := v.(*goja.Object); ok {
		if l := obj.Get("length"); l != nil && !goja.IsUndefined(l) {
			return int(l.ToInteger())
		}
	}
	return len([]rune(v.String()))
}
