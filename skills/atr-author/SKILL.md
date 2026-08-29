---
name: atr-author
description: Write, author, add or record an ATR behaviour test. Convert a manual test case or a QA script into a .test.txt spec. Use when creating a new browser test, turning a bug report into a regression test, or fixing a spec that compiles badly, produces a false pass, or keeps needing repair. Companion to atr-behavior, which covers running tests that already exist.
---

# Writing ATR behaviour specs

A `.test.txt` spec is compiled once, by a model driving the real application,
into a JavaScript file that replays with no model in the loop. Running is
solved. **Writing a spec that compiles, and that does not lie when it passes,
is the hard part** — and it is what this skill is for.

The spec text reaches the compiler **verbatim**. ATR does not parse sections;
it passes the whole file through. Everything you write in it is instruction to
the model. That is the whole reason authoring guidance is a skill rather than a
manual: the spec is the one input to the compiler that does not ship with the
compiler.

Syntax lives in `references/test-file-format.md`. Start with
`references/spec-template.test.txt`. What follows is judgement.

---

## Rule 1: never write an assertion that cannot fail

A **false pass** is the worst thing ATR can produce — worse than a crash,
because nobody investigates a green test.

These are all assertions that cannot fail:

```
Expected Results:
- Steps completed successfully      ← the steps succeeded by construction
- No console errors                 ← true on most broken pages
- The page loads                    ← it loaded before you clicked anything
```

Write what would be *different* if the feature were broken:

```
Expected Results:
- The order confirmation shows the order number
- The cart badge reads 0
- The "Place order" button is gone
```

If you cannot name something that would differ, you do not yet have a test.

## Rule 2: assert on an element, not on page text

A substring match against the whole page passes for the wrong reason. One real
spec searched for `"archiv"` and matched three unrelated things on the page.

- **Bad:** "the page contains 'Archived'"
- **Good:** "the row's status badge reads 'Archived'"

Name the container. The compiler emits `atr.text(selector)` when you name one
and a whole-page match when you do not.

## Rule 3: assert absence as well as presence

"Read-only" does not mean a notice appeared. It means **zero** editors and
**zero** send buttons. A spec that only checks for the banner passes on a page
that shows the banner *and* still lets you type.

Absence is only meaningful once the page has settled — wait for the state that
removes the thing (the confirmation, the empty list, the new view) and then
assert it is gone.

## Rule 4: count before and after, never absolutely

"The last entry in the log is X" breaks the second time the test runs into the
same log. "One more entry than before, and it reads X" survives.

Tests run repeatedly against shared state. Any assertion phrased as an absolute
position or an absolute total is a time bomb.

## Rule 5: a submit is request → navigate → repaint

Not one event. Say what state ends the step:

- **Bad:** "Click Save and wait 2 seconds"
- **Good:** "Click Save and wait for the confirmation toast"

Never ask for a fixed sleep. The compiler will wait for a state if you name
one, and will guess if you do not.

## Rule 6: a destructive test rebuilds its own fixture

**A compile drives your spec more than once** — once to learn the application,
then again to verify what it wrote. A spec that deletes the only record it
depends on works once and then fails for ever.

Put the setup in the spec and say it is setup:

```
Setup:
- Create a project named "atr-fixture-{run}" if one does not exist
```

The compiler turns that into `atr.setup(...)`, which runs before the steps on
every execution and is not counted as a step. A failure there is reported as
the fixture failing, not the application misbehaving.

This is also the answer to "how do I test deletion": create the thing, delete
the thing, assert it is gone.

## Rule 7: read the DOM before naming a control

Labels hide in at least three places — `placeholder`, `aria-label`, and
`<label for>` — and two dialogs in the same application will disagree about
which. Use `atr browser snapshot` and quote what is actually there.

Rich-text editors are the recurring trap: they reject a plain fill and need an
input-event recipe. The class is universal even though the recipe is
app-specific, so put the recipe in **Notes for the compiler** (below).

## Rule 8: a missing input is nobody's bug

`config` is the one failure kind the agent will not help with: **not
repairable, not retryable, never sent to the model.** A person has to decide
what the value should be.

That is deliberate. The tempting "fix" for a missing value is to inline the
literal back into the script, which undoes the reason inputs live outside it.

Values live in `<spec>.test.properties` (committed) beneath
`<spec>.test.override.properties` (gitignored) and `ATR_VALUE_*` variables. A
committed properties file **executes** — `$(command)` and `${VAR}` expand at
read time, on every machine including CI.

## Rule 9: when sign-in cannot be scripted, seed the browser

Some applications cannot be logged into by a script — SSO, a hardware token, a
one-time code.

```bash
atr browser start          # start the daemon
# sign in by hand in the window that opens
atr run --behavior tests/  # reuses the running daemon's browser automatically
```

No flag is needed: `atr run` picks up a running daemon's CDP endpoint on its
own, and opens an isolated tab for the test.

**Name the symptom of forgetting**, because it is unrecognisable otherwise:
every step times out on a sign-in page, which looks exactly like a hang.

For a credential that *can* be scripted, use a secret reference rather than a
value — the compiler emits `atr.fillSecret`, which fetches and types inside one
call so the value never enters the script, the transcript, or a later repair
prompt.

---

## Notes for the compiler

The section nobody knows exists, and the one that decides whether a hard spec
compiles. It goes at the end of the file and holds everything the model cannot
learn by looking at one page:

- app-specific recipes (the rich-text editor; the date picker that ignores
  typing)
- which of several similar controls is the right one
- state the application carries between pages
- anything you discovered by watching a compile fail

Every note should be a trap that produced a wrong result. If you cannot say
what would go wrong without it, leave it out — the notes are prompt, and
padding costs iterations.

**Do not restate what the compiler already knows.** These are in the binary's
prompt and repeating them wastes tokens: use `atr.setup` for fixtures, wait for
state instead of sleeping, `atr.expectExists`/`atr.expectMissing` for presence,
selectors over page text, `:has-text()` for text-qualified targets.

---

## The loop

1. **Bootstrap.** `atr browser record --output tests/thing.test.txt` gives you
   the steps. It **cannot** give you the expectations — it leaves a `TODO`
   where they belong. Answer it; that is rule 1.
2. **Compile once.** `atr run --behavior tests/thing.test.txt`. It drives the
   real application, so it needs a base URL and takes minutes.
3. **Read the emitted `.test.js` before trusting it.** This is the step nobody
   does. You are checking one thing: *could this script fail?* A step with no
   assertion is a step that passes whatever happens.
4. **Commit the script.** It carries a hash of the spec, so a reviewer can see
   what changed and CI can replay it. A generated file whose behaviour depends
   on who ran it last is worse than no file.
5. **`--no-compile` in CI.** It replays or it fails loudly. Without it a spec
   edit silently turns a 9-second CI step into a multi-minute model run.
6. **`--prune-values` occasionally.** Repeated compiles accumulate aliases —
   one real spec ended up with seven keys for two inputs.
7. **Backport the compiler's corrections.** When a compile discovers your spec
   was wrong — you wrote `button[type="submit"]`, the real control is
   `button:not([type])` — put the correction back in the spec. Nobody does
   this, and spec and script diverge silently from then on.

   The reason nobody does it: editing the spec changes its hash and forces a
   recompile. So **batch backports** and do them when a recompile is due
   anyway.

---

## Before you commit

Read the generated `.test.js` against this list. It is short because it only
contains things that make a compiled test *lie*:

- [ ] Every step either asserts something or performs an action that throws
      when it fails. A step of pure `atr.log` cannot fail.
- [ ] No `toContain` with a short needle against `atr.text()` with no selector.
- [ ] No `atr.sleep` outside `atr.retry` — a fixed sleep is a race that has not
      happened yet.
- [ ] Absence is asserted where the spec says something goes away.
- [ ] The spec still describes what the script does.
- [ ] The properties file has no key the script does not read, and no secret.

`atr run` runs the mechanical half of this list for you and refuses to accept a
script that cannot fail. The judgement half is yours.

---

## Failure kinds, and what each one asks of you

| kind | means | what you do |
|---|---|---|
| `assertion` | the application is wrong | fix the app — never the assertion |
| `not_found` | the UI moved | let the agent repair it, then check the diff |
| `timeout` | slow, flaky, or hanging | retried automatically; if persistent, the spec is waiting for the wrong thing |
| `environment` | browser or network | not about your test |
| `config` | an input is missing | **you** must supply it; the agent will not |
| `script` | the generated JavaScript is wrong | the compiler's fault; it repairs itself |

Only `assertion` turns a suite red (exit 1). Everything else exits 2, so a CI
job can retry rather than escalate.

---

## Related

- **`atr-behavior`** — running specs that already exist: flags, exit codes,
  triage.
- **`atr-browser`** — driving the browser by hand while you work out what the
  spec should say.
- `references/test-file-format.md` — the syntax.
- `references/spec-template.test.txt` — start here.
