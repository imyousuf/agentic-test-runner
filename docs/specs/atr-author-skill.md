# Tech spec: an authoring skill for behaviour specs

Status: proposed
Branch: `feat/plugin-authoring-and-history`

## Why

ATR ships four skills and every one of them is about *operating* the tool: run
a command, run a test, drive a browser, drive a desktop. Nothing covers
**writing a spec**, which is where the cost of adoption actually sits.

The evidence is a real adoption: three browser specs against a live app took
twelve commits, most of them fighting the compiler — *"Teach the browser specs
what the ATR compiler cannot infer"*, *"Get the New group dialog test running
green by walking it by hand"*. In the specs that finally worked, 55–65% of each
file is a "notes for whoever automates this" section, described by its author
as *"every note is a trap that produced a false pass"*.

Running is solved. Writing something that compiles, and that does not lie when
it passes, is not.

### The part that makes this a component rather than documentation

Compile quality has more inputs than a first look suggests: the system prompt in
the binary, the tool descriptions the model reads, `selectorHint` on a failed
click, the recorder's spec template, the values files, the wrap-up nudge near
the iteration ceiling, the triage prompt on a repair — and **the notes in the
spec**, which reach the model verbatim because ATR does not parse spec sections,
it passes the whole file through.

All but the last are ours and change with the binary. The last is written by
operators who have no idea what belongs there, and it is the only one a skill
can reach. That is what makes this a shipped component rather than a manual:
it is the input to the compiler that does not live in the compiler.

It also sets the rule for what goes in it. **If the compiler can prevent a
mistake, the fix belongs in the binary, not the skill** — a rule stated only in
a skill protects the people who load the skill, and a rule stated only in the
prompt is invisible to the person writing the spec. Where both matter, the
prompt is the enforcement and the skill explains why.

## What ships

### 1. `atr-author` — a new skill

Trigger phrases in the description should cover writing, adding, authoring or
recording a behaviour test, and converting a manual test case into one.

Content is **judgement, not syntax**. Syntax drifts and already lives in
`docs/behavior-compilation.md`, which the skill links to. The judgement is the
generalised failure taxonomy:

- **Never assert on page text when an element will do.** A substring match
  produces a *false pass*, which is the worst outcome ATR can produce — worse
  than a crash, because nobody investigates a green test. One real spec
  searched the page for `"archiv"` and matched three unrelated things.
  *This one also belongs in the binary*: the compiler can see it is emitting a
  short `waitForText` where a selector was available, and warn.
- **Assert absence as well as presence.** "Read-only" means zero editors and
  zero send buttons, not the presence of a notice.
- **Count before and after, not absolutely.** Re-running a test into the same
  log breaks any assertion phrased as "the last one is".
- **A submit is request → navigate → repaint**, not one event. Wait for the
  state you care about; never sleep a fixed amount. *Already in the prompt*
  (`behavior_compile.go:172`) — the skill's job here is the spec-side half:
  say in the spec what state ends the step, because the model cannot guess it.
- **Rich-text editors reject `fill`.** They need an input-event recipe. The
  class is universal even though the recipe is app-specific.
- **Labels hide in three places** — `placeholder`, `aria-label`, `<label for>`
  — and two dialogs in the same application will disagree. Read the DOM.
- **A destructive test rebuilds its fixture in `atr.setup`**, because a compile
  drives the spec more than once. *Already in the prompt*
  (`behavior_compile.go:167`); the skill covers the part the prompt cannot —
  deciding, while writing the spec, that the fixture is rebuildable at all.
- **A missing input is `KindConfig`: not repairable, not retryable, never sent
  to the model.** A person has to set it. It is the one failure kind where the
  agent will not help, which makes it the one the author most needs to
  understand — and the taxonomy in the current skill omits it entirely.
- **When auth cannot be scripted, seed a browser and let the run attach to it.**
  Start `atr browser start`, sign in by hand, then run: `atr run` reuses a
  running daemon's CDP endpoint automatically (`run.go:352`), no flag needed.
  Name the symptom of not doing so: every step times out on a sign-in page,
  which is indistinguishable from a hang.

Then the loop: bootstrap with `atr browser record`, compile once, **read the
emitted script before trusting it**, commit or do not (state the trade-off,
do not mandate — that is the consumer's convention to set), `--no-compile` in
CI, `--prune-values` periodically.

And the step nobody does: **when the compiler corrects the spec against the
live DOM, backport the correction.** Observed in the wild — a spec said the
submit was `button[type="submit"]`, the compiler discovered it was
`button:not([type])` and emitted the right thing, and the spec still says the
wrong thing today. Spec and script diverge silently from then on.

Say the cost out loud, because it is why nobody does it: editing the spec
changes its hash and forces a full recompile of a script that is already
correct. So batch backports, and do them when a recompile is due anyway.
(If this proves to be the reason it never happens, the fix is a `--restamp`
that accepts the current script against the edited spec — a binary change, not
a skill one.)

### 2. Fix the recorder's template

The skill's first rule forbids weak assertions. The bootstrap the skill
recommends — `atr browser record` — ends every spec it writes with:

```
Expected Results:
- Steps completed successfully
- No console errors
```

(`internal/browser/recorder.go:303`.) That is the canonical assertion that
cannot fail, emitted by ATR itself, into the file the author is least likely to
rewrite. Recommending the recorder while forbidding its output is not a
documentation problem.

Replace it with a prompt the author must answer, and which is visibly not an
assertion:

```
Expected Results:
- TODO: name what must be true at the end. A recorded click sequence
  cannot tell you what this test proves.
```

This is the discipline below, applied to itself: the skill's rules are worth
having only where the binary already obeys them.

### 3. A spec template

A skeleton with the sections that made three hard specs compile: what is being
tested, why it matters, prerequisites, numbered steps, expected results, and
**notes for the compiler**. The notes section is the point — it is the channel
the compiler reads and the one nobody knows exists.

### 4. Pre-commit checklist, inside `atr-author`

Not a separate skill and not an agent. Code review of generated JavaScript is
covered by the general-purpose reviewer that Claude Code already ships; what is
ATR-specific is a short list of things that make a compiled script lie:
assertions that cannot fail, text matches that pass for the wrong reason, fixed
sleeps masking races, missing absence-assertions, spec/script divergence.

### 5. Narrow `atr-behavior` to operating

It currently spans thirteen sections and is doing double duty, which is why
authoring guidance has nowhere clean to live. It keeps running, flags, exit
codes and failure triage, and points at `atr-author` for writing.

It also already ships a 279-line authoring reference at
`skills/atr-behavior/references/test-file-format.md`, which this spec's first
draft did not account for — a second authoring document, in the skill being
narrowed, is exactly the drift the discipline section warns about. **Move it
to `skills/atr-author/references/`** and treat it as the syntax half, with
`SKILL.md` carrying the judgement. Do not write a new format reference; audit
this one for the same drift as the rest (`:has-text`, `toMatch`, `atr.setup`,
`KindConfig`).

### 6. Refresh the shipped skills

`:has-text` and `toMatch` appear nowhere in `skills/`, though both changed this
week. `atr.setup` is mentioned in one place only because it was added by hand.
The skills drifted from the binary the moment the binary moved.

## The discipline this establishes

Skills are software, co-versioned with the CLI in the same repository. **A
change to compiler or runtime behaviour is not complete until the skill changes
in the same commit.** `:has-text` is the worked example: we taught the binary's
prompt about it and left the skill silent, so an agent reading the skill still
does not know the capability exists.

This is also the argument for keeping the skill thin on syntax and thick on
judgement: judgement does not drift.

## Non-goals

- No consumer-specific content. Nothing that names a particular application's
  selectors, URLs or auth belongs in a plugin that ships to everyone. The
  app-specific layer is the consumer's to keep, in their own repository or in
  the spec notes.
- Not a replacement for `docs/behavior-compilation.md`. The skill is
  procedural; the doc is reference.
- Not command-analysis authoring. `atr-analyze` is a different job with
  different failure modes.

## Verification

- Every rule in the skill traces to an observed failure, not to intuition.
- **The exercise is a spec that must fail.** "The first compile succeeds" is the
  wrong bar: a script that asserts nothing compiles on the first try. Write a
  spec for an unfamiliar application, compile it, then **break the application**
  and re-run — the test must go red. A green run against a broken app is the
  failure mode the whole skill exists to prevent, and it is the only one worth
  testing for.
- Take a recorder-generated spec, apply the skill, and check the result no
  longer contains an assertion that cannot fail.
- Each capability the binary gained this cycle (`:has-text`, `toMatch`,
  `atr.setup`, `KindConfig`, `--prune-values`, exit codes) is documented in the
  skills. A grep finds the string; whether the guidance is right is a review
  question, not a grep one.
- No consumer-specific string appears anywhere under `skills/`.
