package testscript

import (
	"github.com/dop251/goja"
)

// installValues exposes the resolved test inputs to the script.
//
// get() throws when a key is undefined and no fallback is given. That is the
// important behaviour: returning an empty string instead would have a test
// type "" into a search box, find nothing, and depending on the assertion
// either pass vacuously or fail with a message pointing at the wrong thing.
// A test that cannot get its inputs has not failed — it has not run.
func (r *runtime) installValues() error {
	values := r.vm.NewObject()

	_ = values.Set("get", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		raw, ok := r.lookup(key)
		if ok {
			return r.vm.ToValue(raw)
		}
		if fallback := call.Argument(1); !goja.IsUndefined(fallback) {
			return fallback
		}
		r.throw(KindConfig, key, "%s", r.values.missingMessage(key))
		return goja.Undefined()
	})

	_ = values.Set("int", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		raw, ok := r.lookup(key)
		if !ok {
			if fallback := call.Argument(1); !goja.IsUndefined(fallback) {
				return fallback
			}
			r.throw(KindConfig, key, "%s", r.values.missingMessage(key))
		}
		n, err := parseIntValue(key, raw)
		if err != nil {
			// A malformed value is a configuration mistake, not a script bug.
			r.throw(KindConfig, key, "%v", err)
		}
		return r.vm.ToValue(n)
	})

	_ = values.Set("bool", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		raw, ok := r.lookup(key)
		if !ok {
			if fallback := call.Argument(1); !goja.IsUndefined(fallback) {
				return fallback
			}
			r.throw(KindConfig, key, "%s", r.values.missingMessage(key))
		}
		b, err := parseBoolValue(key, raw)
		if err != nil {
			r.throw(KindConfig, key, "%v", err)
		}
		return r.vm.ToValue(b)
	})

	_ = values.Set("has", func(key string) bool {
		_, ok := r.lookup(key)
		return ok
	})

	_ = values.Set("keys", func() goja.Value {
		return r.vm.ToValue(r.values.Keys())
	})

	return r.vm.Set("values", values)
}

// lookup reads a key, expanding any command substitution it carries.
//
// An expansion failure is thrown rather than returned: the machine cannot
// produce the input, so the test cannot run, and that is a config problem
// rather than something the script or the application got wrong.
func (r *runtime) lookup(key string) (string, bool) {
	if r.values == nil {
		return "", false
	}
	val, ok, err := r.values.Resolve(r.ctx, key)
	if err != nil {
		r.throw(KindConfig, key, "%v", err)
	}
	return val, ok
}
