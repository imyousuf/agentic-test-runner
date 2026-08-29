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

### Compile

**The library is always injected** into the compile prompt — its available
operations and their signatures, with the instruction to use them rather than
writing equivalents. Not opt-in: opt-in would mean the common case (a repo that
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

This should be stated in the compile prompt, in the authoring skill, and in the
docs, because the pull towards putting a shared `expectSignedIn()` in there is
strong and the damage is invisible.

## Freshness — the part that needs deciding

A compiled script is stamped with a hash of its spec. **The library is not in
that hash.** So editing `_shared.js` changes the behaviour of every compiled
script beside it while every spec hash stays valid: nothing recompiles, nothing
reports stale, and `--no-compile` in CI happily replays scripts whose meaning
just changed.

That is a correctness hole, not a nicety. Two candidate mechanisms:

1. **Fold the library's hash into the stamp.** A library edit invalidates every
   script in the directory, which is honest and simple, and costs a recompile
   of the whole directory for a one-line helper change.
2. **Version the library explicitly** — the library declares a version, scripts
   record the version they were compiled against, and a mismatch is reported as
   stale. Cheaper in recompiles, but adds a number a human has to remember to
   bump, and forgetting is silent.

Option 1 is the safe default and matches the existing content-hash design; the
cost is real but recompiles are already the expensive-and-rare path while
replays are the common one. Recommend 1 unless directory-wide recompiles prove
intolerable in practice.

Either way, the failure must be *visible*: a script compiled against a
different library reports as stale with a message naming the library, not the
spec — the same lesson as `atr-unverified`, where overloading "no hash" would
have made the runner claim the spec had changed when it had not.

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
