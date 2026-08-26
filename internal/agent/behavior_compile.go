package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/testscript"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// A behavior spec is compiled to JavaScript once and replayed for free
// afterwards. The agent still drives the browser while compiling — it has to,
// or the selectors it writes would be guesses — but that cost is paid once
// per spec change instead of once per run.
//
// The compiled script is the "shadow" of that agent run: the same sequence of
// actions, minus the model.

// CompilerConfig configures the compile and repair agents.
type CompilerConfig struct {
	LLMClient     llm.Client
	Browser       *browser.Browser
	MaxIterations int
	Timeout       time.Duration
	Verbose       bool
}

// NewCompilerAgent builds an agent with the browser toolset, used both to
// compile a spec and to repair a script that has drifted.
func NewCompilerAgent(cfg CompilerConfig) *Agent {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 40
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Minute
	}

	registry := NewRegistry()
	for _, tool := range NewBrowserTools(cfg.Browser) {
		registry.Register(tool)
	}

	return &Agent{
		llmClient:     cfg.LLMClient,
		registry:      registry,
		maxIterations: cfg.MaxIterations,
		timeout:       cfg.Timeout,
		verbose:       cfg.Verbose,
		browser:       cfg.Browser,
	}
}

// scriptAPIReference is given to the model verbatim. Compilation is only
// worth anything if the generated script uses the API the runtime actually
// provides, and the failure taxonomy only works if the script raises the
// right kind of failure for each situation — which is a property of which
// call it reaches for.
const scriptAPIReference = `You write JavaScript against this API. Nothing else is available: there is no
DOM, no require, no fetch, no console. Use only what is listed.

Structure:
  atr.step(n, "description", () => { ... })   one numbered step per spec step
  atr.log(message)
  atr.fail(message)        the app is wrong -> TEST FAILURE
  atr.sleep(ms)
  atr.retry(times, fn)     re-runs fn while it fails transiently
  atr.base                 the base URL

Navigation:
  atr.navigate(url)        relative URLs resolve against atr.base
  atr.reload() atr.back() atr.forward()

Interaction (each throws if the target is missing):
  atr.click(target) atr.doubleClick(target)
  atr.fill(target, value)
  atr.fillSecret(target, {ref: "name"}) or {command: "..."}   never reveals the value
  atr.hover(target) atr.pressKey(key)
  atr.scroll({selector, x, y, toBottom, toTop})

Waiting:
  atr.waitFor(target, {timeout: ms, visible: true})
  atr.waitForText(text, {timeout: ms})

Reading:
  atr.exists(target) -> boolean, NEVER throws
  atr.text(selector) atr.html() atr.url() atr.title()
  atr.snapshot() atr.eval(js)
  atr.consoleErrors() atr.failedRequests()

Pages:
  atr.newPage(url) atr.listPages() atr.selectPage(i) atr.closePage(i)

Assertions — these are how you state what the application must do:
  expect(v).toBe(x) .toEqual(x) .toContain(x) .toMatch(re)
  expect(v).toBeTruthy() .toBeFalsy()
  expect(v).toBeGreaterThan(n) .toBeLessThan(n) .toHaveLength(n)

CHOOSING THE RIGHT CALL MATTERS. A failing run is triaged by which kind of
failure it raised, and the wrong call produces the wrong diagnosis:

- expect(...) and atr.fail() mean "the application is wrong". They stop the
  run and are reported as a test failure. They are never rewritten
  automatically. Use them for everything the spec actually asserts.
- A missing target from click/fill/etc. means "the page changed shape" and
  invites an automatic repair. Never use a bare click to assert that
  something exists — write expect(atr.exists("...")).toBeTruthy() instead, or
  a real page change will be quietly repaired away instead of reported.
- atr.waitFor is a timeout, treated as possibly transient and retried. Use it
  to wait for things, never to assert them.
- atr.exists() returns a boolean. Use it to branch on optional page furniture
  (cookie banners, A/B variants) so their absence is not mistaken for drift.
- atr.retry only re-runs transient failures. Wrapping an expect() in it
  achieves nothing, because a failing assertion is never retried. To let the
  page settle before asserting, wait for the state first — atr.waitForText or
  atr.waitFor — and then assert once.

Other rules:
- One atr.step per numbered step in the spec, same numbers, same wording in
  the description.
- Prefer stable targets: id, name, data-testid, aria-label, then visible text.
  Avoid selectors built from generated class names or deep child chains.
- Wait for state rather than sleeping. Use atr.sleep only as a last resort.
- Do not wrap everything in try/catch. Letting a failure propagate is what
  makes it classifiable.`

// CompileRequest asks for a spec to be compiled.
type CompileRequest struct {
	// SpecPath is the path to the .test.txt, used in messages.
	SpecPath string
	// Spec is the raw spec content.
	Spec string
	// BaseURL is the application under test.
	BaseURL string
}

// CompileBehavior drives the browser through the spec once and returns the
// JavaScript that reproduces it.
func (a *Agent) CompileBehavior(ctx context.Context, req CompileRequest) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	system := `You convert a natural-language browser test specification into a JavaScript program that reproduces it exactly.

Work in two phases.

PHASE 1 — perform the test. Use the browser tools to actually carry out every
step against the live application. Call browser_snapshot before interacting so
you use selectors that exist rather than ones you assume. This phase is how
you learn the real structure of the pages; do not skip it and do not guess.

PHASE 2 — write the script. Once you have completed the whole spec, output the
JavaScript that would have done the same thing, in a single fenced code block:

` + "```javascript" + `
...the script...
` + "```" + `

The script must reproduce what you just did, step for step, using the
selectors that actually worked. It has to run unattended with no model
involved, so anything you worked out by looking at the page must be baked in.

` + scriptAPIReference

	user := fmt.Sprintf(`Application base URL: %s

Specification (%s):
---
%s
---

Carry out this specification against the live application now, then emit the script.`,
		req.BaseURL, req.SpecPath, req.Spec)

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: system},
		{Role: llm.RoleUser, Content: user},
	}

	content, err := a.runToolLoop(ctx, messages, "compile")
	if err != nil {
		return "", err
	}

	script := extractCode(content)
	if script == "" {
		return "", fmt.Errorf("the compiler produced no script; last message: %s", truncate(content, 400))
	}
	return script, nil
}

// TriageVerdict is what the agent decided to do about a failing run.
type TriageVerdict string

const (
	// VerdictTestFailure: the application is genuinely broken. Report it.
	VerdictTestFailure TriageVerdict = "test_failure"
	// VerdictRepaired: the script was out of date and has been rewritten.
	VerdictRepaired TriageVerdict = "repaired"
	// VerdictTransient: nothing is wrong with the app or the script; the run
	// hit an environmental problem and is worth retrying.
	VerdictTransient TriageVerdict = "transient"
	// VerdictUnresolved: the agent could not work out what is wrong.
	VerdictUnresolved TriageVerdict = "unresolved"
)

// Triage is the outcome of examining a failed run.
type Triage struct {
	Verdict TriageVerdict `json:"verdict"`
	Reason  string        `json:"reason"`
	// Script is the rewritten source, set only when Verdict is repaired.
	Script string `json:"-"`
}

// TriageRequest describes a failing run.
type TriageRequest struct {
	SpecPath string
	Spec     string
	Script   string
	BaseURL  string
	Failure  *testscript.Failure
	// Attempts is how many times the run has already been tried.
	Attempts int
}

// TriageFailure examines a failed run and either repairs the script or
// explains why the failure is real.
//
// It is deliberately not called for assertion failures: the taxonomy already
// knows those mean the application is wrong, and spending a model call to
// re-derive that would undo the reason the script exists.
func (a *Agent) TriageFailure(ctx context.Context, req TriageRequest) (*Triage, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	system := `A compiled browser test has failed. Work out why, and fix it if the fix is legitimate.

The browser is still open on the page where the failure happened. Inspect it
with the browser tools before deciding anything.

There are exactly three possible answers:

1. "test_failure" — the application genuinely does not do what the
   specification requires. The script is correct. Report it; do not touch the
   script. Prefer this whenever the behaviour the spec describes is actually
   missing or wrong.

2. "repaired" — the specification is still satisfied by the application, but
   the script no longer addresses it correctly: an element was renamed,
   relabelled or moved, or the script itself has a defect. Rewrite the script
   so it tests the same thing through the new structure.

3. "transient" — neither the app nor the script is at fault. The environment
   failed: a network blip, a service still starting, a genuinely slow page.

The distinction between 1 and 2 is the one that matters, and getting it wrong
is expensive. Repairing a script until it passes would turn a suite that
catches regressions into one that hides them. Before choosing "repaired", you
must be able to say what the application still does that satisfies the spec.
If the behaviour the spec asks for is absent, that is a test_failure however
much the page has changed around it. When you cannot tell, answer
"unresolved" rather than guessing.

Never weaken, delete or narrow an assertion to make a test pass. If an
assertion is failing, that is answer 1.

Reply with a single fenced json block:

` + "```json" + `
{"verdict": "test_failure|repaired|transient|unresolved", "reason": "one or two sentences"}
` + "```" + `

When the verdict is "repaired", follow it with the complete rewritten script
in a fenced javascript block. Emit the whole script, not a fragment.

` + scriptAPIReference

	failureJSON, _ := json.MarshalIndent(req.Failure, "", "  ")
	user := fmt.Sprintf(`Application base URL: %s

Specification (%s):
---
%s
---

The compiled script that failed:
---
%s
---

The failure (already classified by the runtime; %d attempt(s) made):
%s

Inspect the page and decide.`,
		req.BaseURL, req.SpecPath, req.Spec, req.Script, req.Attempts, failureJSON)

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: system},
		{Role: llm.RoleUser, Content: user},
	}

	content, err := a.runToolLoop(ctx, messages, "triage")
	if err != nil {
		return nil, err
	}

	triage := parseTriage(content)
	if triage.Verdict == VerdictRepaired {
		script := extractCode(content)
		if script == "" {
			// A repair with no script is not a repair.
			return &Triage{
				Verdict: VerdictUnresolved,
				Reason:  "the agent said it repaired the script but produced no code",
			}, nil
		}
		triage.Script = script
	}
	return triage, nil
}

// runToolLoop drives the shared agent loop until the model answers without
// calling a tool, returning its final message.
func (a *Agent) runToolLoop(ctx context.Context, messages []llm.Message, label string) (string, error) {
	tools := a.registry.Definitions()

	for iteration := 0; iteration < a.maxIterations; iteration++ {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("%s timed out after %d iterations: %w", label, iteration, err)
		}

		resp, err := a.llmClient.Chat(ctx, messages, tools)
		if err != nil {
			return "", fmt.Errorf("%s: LLM call failed at iteration %d: %w", label, iteration, err)
		}

		if !resp.HasToolCalls() {
			return resp.Content, nil
		}

		messages = append(messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			a.verboseLog("%s tool: %s", label, tc.Name)

			out, imgData, imgMIME, _, execErr := a.registry.ExecuteWithImage(ctx, tc.Name, tc.Arguments)
			if execErr != nil {
				out = fmt.Sprintf("Error: %v", execErr)
			}
			msg := llm.Message{Role: llm.RoleTool, Content: out, ToolCallID: tc.Name}
			if len(imgData) > 0 {
				msg.ImageData = imgData
				msg.ImageMIME = imgMIME
			}
			messages = append(messages, msg)
		}

		pruneImages(messages, 2)
	}

	return "", fmt.Errorf("%s reached the iteration limit (%d)", label, a.maxIterations)
}

// pruneImages clears screenshot bytes from all but the most recent keep tool
// messages, so a long compile does not re-upload every screenshot it took.
func pruneImages(messages []llm.Message, keep int) {
	for i := len(messages) - 1; i >= 0; i-- {
		if len(messages[i].ImageData) == 0 {
			continue
		}
		if keep > 0 {
			keep--
			continue
		}
		messages[i].ImageData = nil
		messages[i].ImageMIME = ""
	}
}

var (
	codeBlockRe = regexp.MustCompile("(?s)```(?:javascript|js)\\s*\\n(.*?)```")
	jsonBlockRe = regexp.MustCompile("(?s)```(?:json)\\s*\\n(.*?)```")
)

// extractCode pulls the last javascript block out of a reply. The last one,
// not the first: a model that shows a wrong version before correcting itself
// leaves both behind.
func extractCode(content string) string {
	matches := codeBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return ""
	}
	return strings.TrimSpace(matches[len(matches)-1][1])
}

// parseTriage reads the verdict block, defaulting to unresolved so an
// unparseable answer never silently becomes a pass or a repair.
func parseTriage(content string) *Triage {
	out := &Triage{Verdict: VerdictUnresolved}

	match := jsonBlockRe.FindStringSubmatch(content)
	if match == nil {
		out.Reason = truncate(strings.TrimSpace(content), 300)
		return out
	}

	var parsed struct {
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(match[1]), &parsed); err != nil {
		out.Reason = fmt.Sprintf("could not parse the verdict: %v", err)
		return out
	}

	switch TriageVerdict(parsed.Verdict) {
	case VerdictTestFailure, VerdictRepaired, VerdictTransient:
		out.Verdict = TriageVerdict(parsed.Verdict)
	}
	out.Reason = parsed.Reason
	return out
}
