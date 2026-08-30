---
name: atr-behavior
description: Run behavior tests, run browser tests, execute .test.txt files, run e2e tests with ATR, replay compiled browser tests, natural language browser testing, or triage a failing ATR behaviour test. For writing or recording a new spec, use atr-author instead.
---

# Running ATR behaviour tests

Runs browser tests written as natural-language `.test.txt` specs. A spec is
compiled **once** into a sibling `.test.js` and afterwards replays with no
model in the loop — seconds, and no tokens. The agent comes back only to
diagnose a failure.

> **Writing a spec is a different job.** Load **`atr-author`** for that: which
> assertions are worth writing, what belongs in the notes section, and how to
> avoid a test that passes without testing anything.

## Basic usage

```bash
atr run --behavior tests/login.test.txt              # compile if needed, then replay
atr run --behavior tests/e2e/                        # every .test.txt in the directory
atr run --behavior tests/e2e/ --browser-url http://localhost:3000
```

## Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--behavior <path>` | Test file or directory | (required) |
| `--browser-url <url>` | Base URL for the application | from config or `base_url` |
| `--headless` | Run the browser headless | false |
| `--viewport <WxH>` | Viewport size | 1920x1080 |
| `--cdp-endpoint <url>` | Connect to an existing browser | - |
| `--no-compile` | Replay only; never call the model. Fails loudly if the script is missing or stale | false |
| `--recompile` | Regenerate the script even if it matches the spec | false |
| `--no-repair` | Diagnose a drifted script but do not rewrite it | false |
| `--prune-values` | Remove inputs the script no longer reads | false |
| `--lint <mode>` | What to do about a script that cannot fail: `error`, `warn`, `off` | error |
| `--otel-endpoint <url>` | OTLP collector for run telemetry, e.g. `http://localhost:4318` | from config or env |
| `--interpret` | Skip compilation; let the agent drive every step | false |
| `-v, --verbose` | Show the script's own `atr.log()` output | false |

## Cost: know which mode you are in

**Use `--no-compile` whenever cost must be predictable — always in CI.** It
replays, or it fails and says why. It never calls the model.

Without it, any of these silently turns a 9-second step into a multi-minute
model run: the spec was edited, the script is missing, the script has never
completed a run, or a failure invites a repair.

```bash
atr run --behavior tests/ --no-compile   # CI: replay or fail
atr run --behavior tests/login.test.txt  # local: compile if needed
```

**`--no-compile` needs no backend at all** — no API key, no project, no
`gcloud auth application-default login`. A CI job that only replays does not
have to hold credentials for calls it will never make. (`--interpret` is
refused alongside it, since it is nothing but model calls.)

Each run prints how it was serviced (`compiled →`, `repaired →`) and how many
model calls it cost. A replay costing more than zero is worth investigating.

## A script that cannot fail is refused

Before a script runs, ATR checks statically that it *can* fail: that the script
asserts something, and that no step is built only from reads. A step of pure
`atr.log` reports success whatever the application does, and a suite of those
reports that everything works right up until someone opens the application.

A blocking finding stops the run and exits 2 — nothing was learned about the
application, so it is not a test failure. The findings never go to the model: a
model asked to invent the missing assertions will invent ones that pass.

```bash
atr run --behavior tests/ --lint=warn   # report but run; for adopting the check
atr run --behavior tests/ --lint=off    # skip it
```

Looser problems — a short substring matched against the whole page, a fixed
`atr.sleep` outside a retry — are reported and not enforced.

## Freshness

The compiled script is **committed** and carries a hash of the spec:

```js
// atr-spec-sha256: 9f2c…
```

Edit the spec and the next run recompiles. Reflow whitespace and it does not —
the hash is over normalised text, so `sha256sum` on the raw file will never
match it. A script that has never completed a run carries
`// atr-unverified` and is recompiled rather than trusted. Deleting the hash
line marks a script hand-written and off-limits to the compiler.

A compile drives the spec **more than once** — once to learn the application,
then again to verify what it wrote — so a destructive spec needs a rebuildable
fixture. That is what `atr.setup(...)` in the compiled script is for, and what
a `Setup:` section in the spec compiles to.

## Test inputs

Beside the spec:

- `login.test.properties` — committed, written by the compiler, editable.
- `login.test.override.properties` — gitignored, wins over it.
- `ATR_VALUE_*` environment variables — win over both.

`base_url` here is what lets one spec run against localhost and staging with no
flags. Values support `$(command)` and `${VAR}`, expanded when read — so a
committed properties file **executes**, on every machine including CI.

`--prune-values` removes keys the script no longer reads. It reports without
removing unless you pass it, scans the shared library as well as the script,
and says nothing at all when either builds a key at run time, because then no
scan can be sure.

## Shared operations

A `_shared.js` beside the specs is loaded into every script in that directory.
It holds **operations** — sign in, create the thing — never assertions:
`expect` and `atr.fail` are refused from it.

Editing it does not force a recompile. Scripts carry a second header,
`// atr-lib-sha256:`, and a mismatch means the script is unproven against the
current library rather than stale — so it replays and the header is updated
when it passes. Under `--no-compile` the update is reported instead of written,
so CI does not leave a dirty tree.

A defect in the library is a `config` failure: never repaired, never retried,
never sent to the model.

## Exit codes

| code | meaning | what CI should do |
|---|---|---|
| 0 | every test passed | — |
| 1 | **the application is broken** — an assertion failed | escalate |
| 2 | nothing was learned about the application — missing input, browser or model failure, stale script under `--no-compile` | retry, or fix the environment |

1 means the app is wrong and nothing else. That separation is the point: a red
build that can also mean "the CI box was slow" is a red build people learn to
ignore.

## Failure kinds

Printed with every failure, and each asks for a different response:

| kind | means | ATR's response |
|---|---|---|
| `assertion` | the application did not do what the spec requires | stops immediately, **no model call** — never auto-repaired |
| `not_found` | a target is gone; the UI moved | agent repairs the script, budget of 1 |
| `timeout` | slow, flaky, or hanging | retried, then triaged |
| `environment` | browser, daemon or network | retried, then reported as infrastructure |
| `config` | an input this checkout does not define | reported; **never** repaired or retried — a person must supply it |
| `script` | the generated JavaScript is wrong | repaired |

An assertion failure never reaches the model. That is deliberate: asking a
model to confirm a regression spends the tokens compilation exists to save, and
risks it "fixing" the assertion that caught the regression.

**A regression often presents as a `timeout`.** A compiled script waits for the
state the spec names before asserting it, so when the application stops
reaching that state the *wait* fails first. Those are retried, then triaged —
and if the agent finds the application at fault, the failure is reclassified as
`assertion` and the run exits 1. Under `--no-compile` there is no triage, so
the same break exits 2: correct, because CI was told not to spend a model call
deciding, and 2 means "a person should look".

## Debugging a failure

```bash
atr run --behavior tests/x.test.txt --headless=false   # watch it
atr run --behavior tests/x.test.txt -v                 # show atr.log output
atr run --behavior tests/x.test.txt --no-repair        # diagnose without rewriting
```

Then read the compiled `.test.js` — it is ordinary, readable JavaScript, and
the failure names the step and the target.

## Signing in by hand

When authentication cannot be scripted (SSO, a hardware token, a one-time
code):

```bash
atr browser start          # start the daemon; a window opens
# sign in by hand
atr run --behavior tests/  # reuses the running daemon's browser automatically
```

No flag is needed — `atr run` picks up a running daemon's CDP endpoint on its
own and opens an isolated tab for the test, leaving your tabs alone.

## Configuration

`~/.atr/config.yaml`:

```yaml
behavior:
  base_url: "http://localhost:3000"
  browser:
    executable: "auto"
    headless: true
    viewport: { width: 1920, height: 1080 }
    page_timeout: "30s"
    action_timeout: "10s"
```

## What was run, over time

Every behaviour run is recorded locally in `~/.atr/history.db`, including the
runs that never reached the application — a missing input, a browser that would
not start, a stale script under `--no-compile`. Those are the ones a pass rate
has to exclude to mean anything.

```bash
atr history                          # per spec: runs, pass/fail/infra, flakes, repairs
atr history --spec tests/login.test.txt --runs
atr history --since 7d --json
```

Two numbers only ATR can give you:

- **Repair frequency.** A spec repaired again and again is not flaky — the
  application's DOM is churning underneath it.
- **True failure rate.** Pass rate over the runs that actually tested the
  application. FLAKE counts runs that passed only after a retry, which are
  green in every other report.

The median REPLAY duration is worth watching: a replay is deterministic with no
model in the loop, so a suite drifting from 9s to 15s means the *application*
got slower.

The database is plain SQLite and the views (`runs`, `attempts`, `compiles`) are
a stable contract, so anything the command will not tell you is one query away:

```bash
sqlite3 ~/.atr/history.db "SELECT spec, outcome, count(*) FROM runs GROUP BY 1,2"
```

Point ATR at an OTLP collector and the same runs are exported as traces,
metrics and logs — which is how a CI run's history survives the container being
torn down. Three ways, in precedence order:

```bash
atr run --behavior tests/ --otel-endpoint http://localhost:4318   # this run only
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318          # the standard variable
```

```yaml
telemetry:
  endpoint: "http://localhost:4318"    # ~/.atr/config.yaml
```

Give the collector's base URL; the signal path (`/v1/traces` and friends) is
appended, and a URL that already names a signal is left alone. With no endpoint
anywhere, nothing is emitted and no error is logged.

Turn either off in `~/.atr/config.yaml`; both may be off at once:

```yaml
history:
  enabled: true
  keep_days: 90
telemetry:
  enabled: true
```

Nothing ATR records lifts a resolved value into a field of its own. A value can
appear inside a failure message, because the message quotes the application and
your own expectations back at you — so whether messages leave the machine is
governed by whether you export the logs signal.

## Related

- **`atr-author`** — writing, recording and reviewing specs.
- **`atr-browser`** — driving the browser by hand.
- `docs/behavior-compilation.md` — the full picture.
