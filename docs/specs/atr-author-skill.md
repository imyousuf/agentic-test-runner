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

ATR has exactly two channels into compile quality:

1. the system prompt baked into the binary, which we control; and
2. the notes in the spec, which reach the model verbatim — ATR does not parse
   spec sections, it passes the whole file through.

We have been improving channel one and ignoring channel two, which is written
by operators who have no idea what belongs there. A skill that teaches what to
write in a spec is not a manual for the compiler; it is the half of the
compiler's effectiveness that ships separately.

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
- **Assert absence as well as presence.** "Read-only" means zero editors and
  zero send buttons, not the presence of a notice.
- **Count before and after, not absolutely.** Re-running a test into the same
  log breaks any assertion phrased as "the last one is".
- **A submit is request → navigate → repaint**, not one event. Wait for the
  state you care about; never sleep a fixed amount.
- **Rich-text editors reject `fill`.** They need an input-event recipe. The
  class is universal even though the recipe is app-specific.
- **Labels hide in three places** — `placeholder`, `aria-label`, `<label for>`
  — and two dialogs in the same application will disagree. Read the DOM.
- **A destructive test rebuilds its fixture in `atr.setup`**, because a compile
  drives the spec more than once.
- **When auth cannot be scripted, attach to a seeded browser** via the CDP
  endpoint in `~/.atr/browser.state`. Name the symptom of not doing so: every
  step times out on a sign-in page, which is indistinguishable from a hang.

Then the loop: bootstrap with `atr browser record`, compile once, **read the
emitted script before trusting it**, commit or do not (state the trade-off,
do not mandate — that is the consumer's convention to set), `--no-compile` in
CI, `--prune-values` periodically.

And the step nobody does: **when the compiler corrects the spec against the
live DOM, backport the correction.** Observed in the wild — a spec said the
submit was `button[type="submit"]`, the compiler discovered it was
`button:not([type])` and emitted the right thing, and the spec still says the
wrong thing today. Spec and script diverge silently from then on.

### 2. A spec template

A skeleton with the sections that made three hard specs compile: what is being
tested, why it matters, prerequisites, numbered steps, expected results, and
**notes for the compiler**. The notes section is the point — it is the channel
the compiler reads and the one nobody knows exists.

### 3. Pre-commit checklist, inside `atr-author`

Not a separate skill and not an agent. Code review of generated JavaScript is
covered by the general-purpose reviewer that Claude Code already ships; what is
ATR-specific is a short list of things that make a compiled script lie:
assertions that cannot fail, text matches that pass for the wrong reason, fixed
sleeps masking races, missing absence-assertions, spec/script divergence.

### 4. Narrow `atr-behavior` to operating

It currently spans thirteen sections and is doing double duty, which is why
authoring guidance has nowhere clean to live. It keeps running, flags, exit
codes and failure triage, and points at `atr-author` for writing.

### 5. Refresh the shipped skills

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
- The skill is exercised by writing a spec for an application the author has
  not seen before, and checking that the first compile succeeds.
- `grep` the skills for each capability the binary gained this cycle
  (`:has-text`, `toMatch`, `atr.setup`, `--prune-values`, exit codes) and find
  it documented.
- No consumer-specific string appears anywhere under `skills/`.
