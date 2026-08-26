package testscript

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/dop251/goja"
)

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
			pattern := args[0].String()
			re, err := regexp.Compile(pattern)
			if err != nil {
				// A bad pattern is a defect in the generated script, not a
				// statement about the application.
				r.throw(KindScript, "", "expect(...).toMatch: invalid pattern %q: %v", pattern, err)
			}
			return re.MatchString(actual.String()),
				fmt.Sprintf("text matching /%s/, got %s", pattern, render(actual))
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
