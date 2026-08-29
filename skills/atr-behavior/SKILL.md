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

Each run prints how it was serviced (`compiled →`, `repaired →`) and how many
model calls it cost. A replay costing more than zero is worth investigating.

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
removing unless you pass it, and says nothing at all when the script builds a
key at run time, because then no scan can be sure.

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

## Related

- **`atr-author`** — writing, recording and reviewing specs.
- **`atr-browser`** — driving the browser by hand.
- `docs/behavior-compilation.md` — the full picture.
