# Tech spec: a shared operations library for compiled specs

Status: proposed
Branch: `feat/plugin-authoring-and-history`

## Why

Every compiled script is self-contained. The runtime evaluates exactly one
program — `goja.Compile` then `RunProgram`, nothing else — and the compile
prompt tells the model there is no `require`, no `fetch`, no DOM. So two specs
that both have to log in each contain their own idea of how to log in.

The obvious cost is duplication: change how login works and every spec is
wrong, one at a time, discovered one failing run at a time.

**The larger cost is compile time.** If logging in is eight steps the agent has
to discover from scratch, every compile pays for those eight steps in
iterations, tokens and wall-clock — and every rediscovery is a fresh chance to
get it subtly different from the last one. Giving the compiler `login()`
removes eight steps from every future compile of every spec.

That reframing decides the design: **a library is only worth having if the
compiler knows about it.** A library the model cannot see is a shelf nobody
reaches for; the agent re-derives login anyway and we have added a file.

## Design

Two halves, and the second is the one that is easy to forget.

### Runtime

A `_shared.js` beside the specs, evaluated into the same VM before the script.
Its top-level functions are simply in scope. No module system, no resolution
rules, no new syntax — the loader already builds one VM per run, so this is a
second `RunProgram` before the first.

Discovery is by convention: the file sits in the spec's directory. A spec with
no `_shared.js` beside it behaves exactly as today.

"A second RunProgram" is right about the VM and glosses three things. The
library evaluation needs the same panic containment as `execute`
(`runtime.go:173`), or a host throw during it crashes the process instead of
being reported. `Options` needs a `Library`/`LibraryName` field rather than
concatenating sources, which would destroy line numbers and stack attribution.
And the library must be **declarations-only**: a top-level `atr.navigate(...)`
produces a step-0 failure the triage model cannot diagnose. That is
enforceable cheaply — goja's parser is already in the module — by rejecting
non-declaration top-level statements at load time. Library code must not call
`atr.step` or `atr.setup` either; both would corrupt the one-step-per-spec-step
invariant differently for each calling spec.

### Compile

**The library is always injected — verbatim.** Not extracted signatures: a
signature loses what the model most needs, which is what the operation does and
what state it leaves the page in. `login()` has an empty signature; its body
says it ends on the dashboard. The library is one bounded operations-only file,
so its token cost is noise beside the snapshots a compile already carries, and
injecting the file needs no parsing machinery and covers the triage prompt for
free. Not opt-in: opt-in would mean the common case (a repo that
has a library and wants it used) requires per-spec ceremony, and the failure
mode of forgetting is silent re-derivation, which is exactly what this exists
to stop.

The consequence to accept: a stale or wrong `_shared.js` misleads every compile
in that directory. That is a real cost and the mitigation is visibility, not
opt-in — see freshness below.

## The boundary: operations, not assertions

Shared **operations** — log in, create the thing, send the message. Never
shared **assertions**.

The moment an assertion moves into the library, you can no longer read a test
and know what it checks, and the library becomes a place where the meaning of
twenty tests can be weakened in one edit. That is the same objection ATR
already makes to repairing an assertion automatically: it is indistinguishable
from deleting the test.

Stating it in the prompt aims at the wrong actor: the prompt constrains the
model, and the model does not write `_shared.js` — a person does, six months
later, and never sees the prompt.

It is enforceable, cheaply. goja exposes `CaptureCallStack` with per-frame
`SrcName()`; with the library compiled under its own name, `expect` and
`atr.fail` can refuse to run from a library frame. That catches the exact rule
on the first run of any offending library, whoever wrote it.

What it cannot catch is assertion semantics smuggled as a bare `throw` (which
is self-punishing — `KindScript` is a repair magnet) or as a `waitFor` timeout.
So: **`expect` and `atr.fail` may not execute from a library frame, enforced;
the rest is convention.** The first draft's "never shared assertions, ever"
read as a guarantee the design does not deliver.

## Freshness

A compiled script is stamped with a hash of its spec. The library is not in
that hash, so editing `_shared.js` changes the behaviour of every script beside
it while every hash stays valid: nothing recompiles, nothing reports stale, and
`--no-compile` in CI replays scripts whose meaning just changed.

**Replay, do not recompile.** An earlier draft proposed folding the library
hash into the spec stamp and accepting directory-wide recompiles. That is
backwards. Scripts *call* the library by name and load it from disk at run
time — so editing `login()` to track a UI change is the feature's primary use,
and recompiling would make a one-line fix cost N model compiles, rewrite N
committed scripts as whole-file diffs, and turn CI red across the directory
until someone re-ran every spec locally with a model.

A library change does not mean the script is wrong. It means the script is
*unproven against this library* — which is exactly `atr-unverified`. So:

- A second header line, `// atr-lib-sha256:`, recorded alongside the spec hash.
  Separate, not folded: a single combined hash cannot tell the runner whether
  the spec or the library moved, and the error message has to name which.
- On mismatch, **replay**. It costs nothing, involves no model, and is legal
  under `--no-compile`.
- On any completion that is not `KindScript`, restamp the library hash — the
  same rule already used for `atr-unverified` at `behavior_run.go:179`.
- Only a genuine signature break reaches the model, and only for the scripts
  that actually broke. The message names the library and quotes the breakage:
  *"compiled against a different `_shared.js` and no longer runs against the
  current one (TypeError: login is not a function)"*.

Two details this depends on. `Stamp` currently only strips a marker line
(`store.go:133`) and must learn to rewrite the library hash with the hash of
the library that actually ran. And `needsLiveApp` (`run_behavior.go:162`) calls
`Fresh` independently of `loadOrCompile` — if freshness grows a library
dimension in one place and not the other, a directory run decides "replay, no
base URL needed" and then recompiles without one.

The library hash must normalise like `SpecHash` does (`store.go:60`), or a
comment-only edit invalidates a directory.

Under `--no-compile` in CI, a restamp would write into the checkout. Print the
change rather than writing it.

## Failure classification

The spec's first draft designed the compile half and the loading half and left
this out; it is where a library actually touches ATR's load-bearing machinery.

**A defect in the library is `KindConfig`.** Not repairable, not retryable, and
never sent to the model — a person has to fix `_shared.js`. Classifying a
library parse error as `KindScript` instead would be actively dangerous: see
below.

**The library must reach the triage prompt, not just the compile prompt.**
`scriptAPIReference` is appended to both (`behavior_compile.go:236` and `:364`)
and opens with *"Nothing else is available… Use only what is listed"* — so a
repairing model looking at a script that calls `login()` is being told that
call is invalid. Its rational repair is to inline it. Then `SaveDraft`
overwrites the committed script, the replay passes because the library is now
unused, `Stamp` fires, and the suite has silently lost its library with every
hash valid. Injection scope is both prompts, and the "nothing else is
available" sentence must be amended wherever a library exists.

**Repair must never rewrite the library.** A model editing shared code can
weaken twenty tests in one edit.

## Values

`LoadValues` is strictly per-spec (`values.go:66`). A library operation reading
`values.get("username")` therefore requires every spec in the directory to
define it — the duplication moved one layer down rather than removed. Worse,
`--prune-values` scans only the compiled script (`references.go:67`), so a key
read solely inside the library is reported unused on every run and deleted from
the committed file, after which every test in the directory fails `KindConfig`.
The exactness safety valve does not save it: the non-literal call sits in the
library, the script's scan stays exact, and the wrong answer is delivered
confidently.

Decide one of:

- **Library operations take inputs as parameters.** Only specs read values.
  Simplest, keeps `values` a spec-level concept, and makes the prune scan
  correct by construction. Recommended.
- **A `_shared.properties` layer**, with a stated position in the existing
  precedence order. More flexible, more machinery, and the prune scan must then
  read the library too.

Either way the prune scan must account for the library, or it is destructive.

## The trade-off, stated plainly

This is Page Object Model, and it carries POM's bargain. Real reduction in
duplication and compile cost, against real loss of "the script is reviewable in
a diff" — which is one of ATR's central claims. A change to `login()` alters
twenty tests' behaviour invisibly.

Bounding it to operations rather than assertions keeps the important half of
that claim: you can still read a compiled script and see exactly what it
asserts. What you can no longer see, without opening a second file, is exactly
how it got there.

## Non-goals

- No module system, no `require`, no dependency graph. One conventional file.
- No sharing across directories in the first version. If that is wanted later,
  it is a resolution-rules discussion, and resolution rules are how simple
  ideas become complicated ones.
- No shared assertions, ever.

## Verification

- A spec directory with `_shared.js` compiles a script that calls a library
  function rather than re-deriving it, with no per-spec opt-in.
- A directory without `_shared.js` behaves exactly as before.
- Editing `_shared.js` causes affected scripts to report stale, with a message
  naming the library rather than the spec.
- `--no-compile` refuses a script whose library has changed, exiting 2.
- Measured: compile iterations for a spec whose first steps are a shared
  operation, before and after the library exists.
