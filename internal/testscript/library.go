package testscript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

// Two specs that both have to log in each contain their own idea of how to log
// in. The obvious cost is duplication; the larger one is compile time, because
// every compile rediscovers those eight steps from scratch and gets them
// subtly different each time.
//
// A `_shared.js` beside the specs is evaluated into the same VM before the
// script, so its functions are simply in scope. No module system, no
// resolution rules — the loader already builds one VM per run.
//
// The bargain is Page Object Model's: real reduction in duplication and
// compile cost, against real loss of "the script is reviewable in a diff",
// which is one of ATR's central claims. Bounding the library to *operations*
// keeps the important half — you can still read a compiled script and see
// exactly what it asserts. What you can no longer see, without opening a
// second file, is exactly how it got there.

// LibraryName is the file a spec's directory may provide.
const LibraryName = "_shared.js"

// LibraryPath returns where a spec's shared library would live.
func LibraryPath(specPath string) string {
	return filepath.Join(filepath.Dir(specPath), LibraryName)
}

// Library is a shared operations file.
type Library struct {
	// Path is the file it came from.
	Path string
	// Source is the JavaScript.
	Source string
}

// LoadLibrary reads the library beside a spec. A missing one is not an error:
// a directory without `_shared.js` behaves exactly as it did before this
// existed.
func LoadLibrary(specPath string) (*Library, error) {
	path := LibraryPath(specPath)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return &Library{Path: path, Source: string(data)}, nil
}

// Hash is the content hash recorded in a compiled script, normalised the same
// way a spec's is so a comment-only edit does not invalidate a directory.
func (l *Library) Hash() string {
	if l == nil {
		return ""
	}
	return SpecHash(l.Source)
}

// ValidateLibrary checks that a library only declares things.
//
// A top-level `atr.navigate(...)` would run before step 1 of every spec in the
// directory and produce a step-0 failure the triage model cannot diagnose. A
// top-level `atr.step` or `atr.setup` would be worse: it would corrupt the
// one-step-per-spec-step numbering differently for each calling spec.
//
// Checked at load time with the parser that is already in the binary, so it
// costs nothing and fails on the first run of any offending library rather
// than mysteriously later.
func ValidateLibrary(source, name string) error {
	prg, err := parser.ParseFile(nil, name, source, 0)
	if err != nil {
		return fmt.Errorf("%s does not parse: %w", name, err)
	}

	// Checked before the top-level rule, because a library that declares steps
	// is a different and more confusing mistake than one that merely runs
	// code, and the message has to say which.
	var steps string
	walk(prg, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpression)
		if !ok || steps != "" {
			return true
		}
		switch calleeName(call.Callee) {
		case "atr.step", "atr.setup":
			steps = calleeName(call.Callee)
		}
		return true
	})
	if steps != "" {
		return fmt.Errorf("%s calls %s; steps belong to a spec, and a library that "+
			"declares them renumbers every spec that loads it", name, steps)
	}

	for _, stmt := range prg.Body {
		if !isDeclaration(stmt) {
			return fmt.Errorf("%s runs code at the top level; it may only declare "+
				"functions and constants, because everything in it runs before "+
				"step 1 of every spec in the directory", name)
		}
	}

	// A declaration can still hide a call: `const page = atr.url()` is a
	// variable declaration that drives the browser.
	inside := callsInsideFunctions(prg)
	var offender string
	walk(prg, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpression)
		if !ok || inside[n] || offender != "" {
			return true
		}
		if name := calleeName(call.Callee); strings.HasPrefix(name, "atr.") ||
			strings.HasPrefix(name, "values.") || name == "expect" {
			offender = name
		}
		return true
	})
	if offender != "" {
		return fmt.Errorf("%s calls %s at the top level; it may only declare "+
			"operations, which each spec then calls for itself", name, offender)
	}

	return nil
}

// isDeclaration reports whether a top-level statement only introduces a name.
func isDeclaration(stmt ast.Statement) bool {
	switch stmt.(type) {
	case *ast.FunctionDeclaration,
		*ast.ClassDeclaration,
		*ast.VariableStatement,
		*ast.LexicalDeclaration,
		*ast.EmptyStatement:
		return true
	default:
		return false
	}
}

// callsInsideFunctions marks every call that sits within some function body,
// so the ones left over are the top-level ones.
func callsInsideFunctions(prg *ast.Program) map[ast.Node]bool {
	inside := map[ast.Node]bool{}

	walk(prg, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FunctionLiteral, *ast.ArrowFunctionLiteral:
		default:
			return true
		}
		walk(n, func(child ast.Node) bool {
			if child == n {
				return true
			}
			if _, ok := child.(*ast.CallExpression); ok {
				inside[child] = true
			}
			return true
		})
		return true
	})

	return inside
}

// evaluateLibrary runs the library into the VM before the script.
//
// Panic containment matches execute's, because a host function throwing during
// library evaluation would otherwise take the process down rather than being
// reported: a test runner reports failures, it does not crash on them.
func (r *runtime) evaluateLibrary(source, name string) (err error) {
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		if recErr, ok := rec.(error); ok {
			err = recErr
			return
		}
		err = fmt.Errorf("%v", rec)
	}()

	program, compileErr := goja.Compile(name, source, false)
	if compileErr != nil {
		return compileErr
	}
	_, err = r.vm.RunProgram(program)
	return err
}

// callerIsLibrary reports whether the JavaScript that called into this host
// function came from the shared library.
//
// This is how "operations, not assertions" is enforced rather than merely
// stated. Stating it in the compile prompt aims at the wrong actor: the prompt
// constrains the model, and the model does not write `_shared.js` — a person
// does, six months later, and never sees the prompt.
//
// What it cannot catch is assertion semantics smuggled in as a bare `throw`
// (self-punishing: that is KindScript, a repair magnet) or as a waitFor
// timeout. So the guarantee is exactly this and no more.
func (r *runtime) callerIsLibrary() bool {
	if r.libraryName == "" {
		return false
	}
	for _, frame := range r.vm.CaptureCallStack(8, nil) {
		src := frame.SrcName()
		if src == "" || src == "<native>" {
			continue
		}
		return src == r.libraryName
	}
	return false
}

// refuseFromLibrary raises the boundary violation.
func (r *runtime) refuseFromLibrary(call string) {
	r.throw(KindConfig, "",
		"%s was called from %s. A library holds operations, not assertions: "+
			"once a test's assertions live in shared code you can no longer read "+
			"the test and know what it checks, and one edit can weaken every test "+
			"in the directory. Move the assertion into the spec that cares about it.",
		call, filepath.Base(r.libraryName))
}
