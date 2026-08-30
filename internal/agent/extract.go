package agent

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// Compiling a spec re-derives whatever the application makes it re-derive. If
// signing in is five steps, every spec in the directory pays for those five
// steps at compile time and carries its own copy of them afterwards — and each
// copy is a fresh chance to have got it subtly different.
//
// A shared library fixes that, and until now somebody had to write one by
// hand before the first compile, which is exactly when nobody knows yet what
// will repeat. This closes the loop: the operations a directory keeps
// re-deriving are hoisted into _shared.js, the scripts that were duplicating
// them are rewritten to call it, and the rewrites are proved before they are
// kept.
//
// The order matters and is the whole safety story:
//
//  1. Find the overlap statically. No model unless there is one.
//  2. Ask for a proposal.
//  3. Refuse it on the syntax tree — the library must declare only
//     operations, every rewrite must still lint, and above all every rewrite
//     must claim exactly what it claimed before.
//  4. Prove it by running every script it touched.
//  5. Keep all of it or none of it.
//
// Step 3 is what makes step 4 meaningful. A refactor that weakens an
// assertion passes step 4 too, and passes for ever after.

// ExtractionMode says when a compile may rewrite a directory's scripts.
type ExtractionMode string

const (
	// ExtractAlways hoists a repeated operation as soon as one appears.
	// The default: duplication that is only reported accumulates.
	ExtractAlways ExtractionMode = "always"
	// ExtractOnDemand reports what could be hoisted and changes nothing,
	// leaving it to `atr refactor-ops`.
	ExtractOnDemand ExtractionMode = "on-demand"
	// ExtractOff does not even look.
	ExtractOff ExtractionMode = "off"
)

// Extraction is a proposed shared library and the rewrites that use it.
type Extraction struct {
	// Library is the whole new _shared.js.
	Library string
	// Scripts maps a script's path to its rewritten source. Only the scripts
	// that changed appear.
	Scripts map[string]string
	// Reason is the agent's one-line account of what it hoisted.
	Reason string
}

// Paths lists the rewritten scripts, in a stable order.
func (e *Extraction) Paths() []string {
	out := make([]string, 0, len(e.Scripts))
	for p := range e.Scripts {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ExtractRequest is everything the agent needs to propose an extraction.
type ExtractRequest struct {
	// Library is the directory's current _shared.js, empty if there is none.
	Library string
	// Scripts are the compiled scripts in the directory, by path.
	Scripts map[string]string
	// Overlaps are the sequences worth hoisting, already found statically.
	Overlaps []testscript.Overlap
	// Progress receives a line per model iteration.
	Progress func(string)
}

// ProposeExtraction asks the agent for a shared library and the rewrites.
func (a *Agent) ProposeExtraction(ctx context.Context, req ExtractRequest) (*Extraction, error) {
	if !llm.Available(a.llmClient) {
		return nil, fmt.Errorf("no model is available to propose an extraction")
	}

	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	system := `You hoist repeated operations out of compiled browser tests into a shared library.

Several scripts in one directory perform the same sequence of operations
because each was compiled on its own and re-derived it. Your job is to name
that sequence once and have each script call it.

THE RULE THAT MATTERS MOST. You may move operations — navigate, click, fill,
fillSecret, hover, pressKey, scroll, waitFor, waitForText, reload, back. You
may NOT move, reword, reorder or remove anything a script asserts: expect(...),
atr.fail, atr.expectExists, atr.expectMissing, atr.expectText. Every assertion
must survive in the same step, character for character, apart from
indentation. A rewrite that changes what a test claims is rejected
automatically, so there is nothing to gain by tidying one.

The library:
- Declares functions and constants. Nothing runs at the top level — no calls
  outside a function body, not even to build a constant.
- Contains no assertions at all. They are refused at runtime from a library
  frame, so a library that asserts fails every test in the directory.
- Declares no atr.step or atr.setup. Steps belong to a spec.
- Takes what varies as a parameter. Do not read values.get inside a library
  operation; the caller passes what it needs.
- Says what each operation leaves on screen, in a comment. The next compile
  reads this file to decide whether to call the operation, and a signature
  alone does not say where the page ends up.

The rewrites:
- Keep every step, its number and its description exactly as they are.
- Replace the hoisted operations with a call, and change nothing else.
- Keep reading their own inputs. values.get stays in the script.

Emit the library and every script you changed, each in its own block, in this
exact shape and nothing else between them:

=== FILE: _shared.js
` + "```javascript" + `
...the whole file...
` + "```" + `

=== FILE: login.test.js
` + "```javascript" + `
...the whole script, including its header comments...
` + "```" + `

Then one line beginning "REASON:" saying what you hoisted.

Emit a script only if you changed it. If the overlap is not worth naming —
the operations differ in ways that would need a parameter for every line, or
naming it would not remove real duplication — emit no files at all and say
why after REASON:.`

	user := buildExtractPrompt(req)

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: system},
		{Role: llm.RoleUser, Content: user},
	}

	resp, err := a.llmClient.ChatWithHistory(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("asking for an extraction: %w", err)
	}

	return parseExtraction(resp.Content)
}

func buildExtractPrompt(req ExtractRequest) string {
	var b strings.Builder

	b.WriteString("These scripts share operations.\n\n")
	for i, o := range req.Overlaps {
		fmt.Fprintf(&b, "Overlap %d, performed by %s:\n", i+1, strings.Join(o.Scripts, " and "))
		for _, step := range o.Steps {
			fmt.Fprintf(&b, "    %s\n", step)
		}
		b.WriteString("\n")
	}

	if strings.TrimSpace(req.Library) != "" {
		b.WriteString("The directory already has a shared library. Extend it rather than\n" +
			"replacing it, and keep every operation it already declares — other\n" +
			"scripts call them.\n\n=== _shared.js\n```javascript\n")
		b.WriteString(strings.TrimSpace(req.Library))
		b.WriteString("\n```\n\n")
	} else {
		b.WriteString("The directory has no shared library yet.\n\n")
	}

	for _, path := range sortedKeys(req.Scripts) {
		fmt.Fprintf(&b, "=== %s\n```javascript\n%s\n```\n\n", path, strings.TrimSpace(req.Scripts[path]))
	}

	return b.String()
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fileBlock matches one "=== FILE: path" heading and the fenced block under it.
var fileBlock = regexp.MustCompile("(?s)===\\s*FILE:\\s*([^\n]+?)\\s*\n+```(?:javascript|js)?\n(.*?)\n```")

// parseExtraction reads the agent's reply.
func parseExtraction(reply string) (*Extraction, error) {
	ex := &Extraction{Scripts: map[string]string{}}

	for _, m := range fileBlock.FindAllStringSubmatch(reply, -1) {
		path, body := strings.TrimSpace(m[1]), m[2]
		if path == testscript.LibraryName {
			ex.Library = body
			continue
		}
		ex.Scripts[path] = body
	}

	for _, line := range strings.Split(reply, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "REASON:"); ok {
			ex.Reason = strings.TrimSpace(rest)
			break
		}
	}

	if ex.Library == "" && len(ex.Scripts) == 0 {
		// A considered refusal is a legitimate answer, and is not an error.
		return ex, nil
	}
	if ex.Library == "" {
		return nil, fmt.Errorf("the agent rewrote scripts but proposed no library")
	}
	if len(ex.Scripts) == 0 {
		return nil, fmt.Errorf("the agent proposed a library that nothing calls")
	}
	return ex, nil
}

// Empty reports whether the agent decided there was nothing worth hoisting.
func (e *Extraction) Empty() bool {
	return e == nil || (e.Library == "" && len(e.Scripts) == 0)
}

// ValidateExtraction refuses a proposal on the syntax tree, before anything is
// written and before anything is run.
//
// This is the half that holds whatever the model did. Running the rewritten
// scripts afterwards proves the operations still work; it cannot prove the
// tests still test anything, because a weakened assertion passes.
func ValidateExtraction(before map[string]string, ex *Extraction) error {
	if ex.Empty() {
		return nil
	}

	if err := testscript.ValidateLibrary(ex.Library, testscript.LibraryName); err != nil {
		return fmt.Errorf("the proposed library is not one: %w", err)
	}

	for _, path := range ex.Paths() {
		original, known := before[path]
		if !known {
			return fmt.Errorf("the agent rewrote %s, which is not a script in this directory", path)
		}

		rewritten := ex.Scripts[path]

		ok, why, err := testscript.AssertionsUnchanged(original, rewritten)
		if err != nil {
			return fmt.Errorf("checking %s: %w", path, err)
		}
		if !ok {
			return fmt.Errorf("the rewrite of %s changes what it claims:\n  %s", path, why)
		}

		findings, err := testscript.Lint(rewritten)
		if err != nil {
			return fmt.Errorf("the rewrite of %s does not parse: %w", path, err)
		}
		if blocking := testscript.Blocking(findings); len(blocking) > 0 {
			return fmt.Errorf("the rewrite of %s cannot fail: %s", path, blocking[0])
		}
	}

	return nil
}
