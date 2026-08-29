# Tech spec: execution history and telemetry

Status: implemented on `feat/plugin-authoring-and-history`
Where: internal/history/, internal/cli/history.go, internal/cli/run_history.go, internal/agent/behavior_run.go (attempts)
Branch: `feat/plugin-authoring-and-history`

## Why

ATR knows things about a test run that no general-purpose test reporter can:
whether a model was involved, whether the script was compiled or replayed, the
*kind* of failure, and whether a repair happened. All of it is discarded the
moment the process exits.

Two questions justify the feature on their own:

- **Repair frequency per spec.** A spec that keeps being repaired is not flaky
  — the application's DOM is churning underneath it. Nothing else in a normal
  stack can tell you that.
- **True failure rate.** Now that infrastructure failures exit `2`, pass rate
  can exclude the runs that never tested anything. Most dashboards cannot
  separate those, which is how teams learn to ignore a red check.

And one that falls out for free: because a replay is deterministic and has no
model in the loop, **its duration is dominated by the application under test**.
A suite drifting from 9s to 15s over a month is telling you the app got slower,
not that the model was chattier. Few e2e suites can offer that, because their
run time has retry and model variance baked in.

## Design

### What a record is

The first draft said "a single run record produced at the end of a run", which
does not survive contact with the code. `RunOutcome` (`behavior_run.go:72`)
carries one `Result`, and the retry loop overwrites it on every attempt
(`:168`) — so a run that failed `KindTimeout`, retried, and passed records only
the pass. The flake evidence, which is a headline reason for the feature, is
the thing that gets discarded.

**The record is a run with attempts beneath it.** One row per attempt, carrying
its own result, kind, duration and whether it was preceded by a repair; a run
row carrying the aggregate. That shape is also what the trace hierarchy needs,
so the two sinks stay consistent by construction rather than by discipline.

`RunOutcome` therefore grows an `Attempts []Attempt` alongside the existing
`Result` (kept as "the last one", which is what the printer uses).

### Where it is produced

**At the CLI loop, not inside `RunBehavior`.** The second justification —
excluding infrastructure from failure rate — requires recording the runs that
never reached a script:

- an unreadable spec file (`run.go:429`),
- a missing base URL for a spec that needs one (`run.go:454`),
- a stale script under `--no-compile` (`behavior_run.go:309`).

Each of those is a `continue` in the loop over spec files, and none produces a
`RunOutcome` at all. If recording lives inside `RunBehavior`, the database
learns nothing about them and "true failure rate" quietly excludes exactly the
category it was built to count. So the emitter wraps the per-spec iteration in
`runBehaviorTests`, and a pre-run failure is a recorded run with zero attempts
and a `config`/`infra` outcome.

`runCommandAnalysis` is out of scope for the first version; the schema should
not make it impossible to add later.

### Fields worth naming now

- **Timestamps.** `started_at` and `finished_at`, UTC. Without them "duration
  trend over a month" has no axis.
- **Spec identity.** Not the absolute path: a spec run from a laptop checkout
  and from CI is the same spec, and path identity silently splits its history
  in two. Store the path relative to the repository root when one can be
  found, with the absolute path as a separate column.
- **Compile duration.** Nothing times the compile today; `loadOrCompile`
  returns a source string. It needs a clock around it, or the most expensive
  phase is the one with no number.
- **Model calls.** `outcome.ModelCalls` counts *agent invocations*, not LLM
  requests — one compile is one increment and forty LLM round-trips. Record it
  under a name that says so (`agent_invocations`), and add a real LLM call
  count and token usage only if the provider layer already surfaces them;
  do not rename the existing field's meaning silently.
- **Failure kind and message**, from `testscript.Failure`.
- **Repairs**, count and per-attempt flag.

### Sink 1: SQLite, on by default

`~/.atr/history.db`. Cheap, private, same trust boundary as the browser the
user just drove.

Three decisions the design cannot defer:

- **Driver.** ATR cross-compiles to four targets with no CGO
  (`.github/workflows`), so `mattn/go-sqlite3` is out. `modernc.org/sqlite` is
  pure Go and works, at the cost of several MB on a binary that is currently
  dependency-light. That cost is the actual price of this sink and should be
  measured before the feature is accepted, not discovered at release.
- **Concurrency.** A directory run is sequential today, but nothing stops two
  `atr run` processes sharing `~/.atr/history.db`. WAL mode, a busy timeout,
  and one short transaction at the end of each run. **A recording failure must
  never fail the test run** — log it once and carry on; the exit code belongs
  to the application under test, not to the historian.
- **Retention.** A row per attempt per spec per run grows without bound on a
  machine that runs a suite in a loop. A default cap (age or row count),
  pruned on write, stated in the docs.

**The schema is public, via views.** An earlier draft of this design proposed
hiding it behind `atr history` to keep the schema free to change. That was
wrong: opacity throws away the reason to choose SQLite at all, and the primary
consumer here is an agent with a shell, which will reach for one `sqlite3`
query rather than learn a flag surface — and can, whether or not we bless it.

So: **views are the contract, tables are ours.** `runs`, `steps` and `compiles`
as views over whatever shape suits us underneath. `atr history` ships as a
convenience for the common questions — flake rate, repair frequency, duration
trend — not as a gate.

### Sink 2: OpenTelemetry, when configured

Exports when `OTEL_EXPORTER_OTLP_ENDPOINT` is set, silent when it is not. No
bespoke enable flag: following the standard variable means no connection errors
on a laptop with no collector, and one-line adoption in CI. This matters
because a local database dies with the CI container — today a CI run's history
evaporates entirely.

Covers **all runs**, not only compiles. Replays are the volume and therefore
the aggregate signal; compiles are rare and expensive.

**Shutdown is the part that gets forgotten.** A batch span processor flushes on
a 5s schedule and a periodic metric reader on 60s; a replay takes 9s and exits.
Without an explicit `Shutdown` with a bounded timeout on the exit path, a CI
run reliably exports nothing and the feature appears not to work. ATR's exit
path now returns errors rather than calling `os.Exit` (`cli/exit.go`), so there
is a place to put it — and it must sit outside the error branch, since a failed
run is the one whose telemetry matters most. Bound the flush (a few seconds)
so an unreachable collector delays exit rather than hanging it.

#### The three signals divide by what the data is

**Metrics — bounded dimensions only.** Run counts by result and failure kind,
duration histograms, model calls, repairs. Never a message, never a resolved
value. A failure message as a metric label is one time series per distinct
message — `got "Demo Shop"`, `got "Abu Jafar Saifullah"` — which is the classic
way to melt a metrics backend.

Duration **must** be dimensioned by compiled-versus-replayed. A four-minute
compile and a nine-second replay in the same bucket set destroys the percentile
for both. Model calls likewise deserve a dimension: zero-model is the property
the whole compiled design exists to protect, and a regression in it is worth an
alert.

**Traces — structure and timing.** One `behavior.run` span always; a `compile`
child span only when a compile happened, with iteration spans beneath it; then
step spans. The shape answers "did this run cost a model?" without reading an
attribute.

Step spans are emitted always, not only on failure. Failure-only is cheaper but
loses the baseline, and the question worth answering is "which step got
slower", which needs the passing runs to compare against. Sampling is the
collector's job.

**Logs — the human-readable detail**, correlated to the span by trace and span
id. This is where a failure message belongs, and where a resolution failure
belongs in full: *no value for `recipient_one`; searched
`shop.test.properties`, `shop.test.override.properties`,
`ATR_VALUE_RECIPIENT_ONE`* — useless as a label, invaluable when you are the
one debugging it.

The correlation is the payoff. Metrics say the failure rate moved; the trace
says which run and where the time went; the log record hanging off that span
names the key nobody set. Today those are three separate acts of detective
work.

This split also removes a bespoke control. An earlier draft proposed an
ATR-specific opt-in for sending failure messages over OTLP; with a logs signal
there is no need, because enabling or disabling the logs exporter is a control
OTel users already have.

## Privacy rules

- **ATR never extracts a resolved value into a field of its own.** Not a column,
  not a span attribute, not a log attribute. A value read through `values.get`
  can be a customer name, an account id or an internal URL.
- **Resolution diagnostics are value-free.** A resolution failure names the
  *key* and the layers searched, never a value that did resolve.

  The first draft said "never record resolved values, anywhere", which reads
  well and cannot be implemented. `expect(...).toBe(values.get("name"))`
  produces the message `expected "Acme Ltd", got "Acme Inc"`
  (`expect.go:88-92`) — the resolved value is *inside* the failure message
  before any of this code sees it. Promising it never appears would be a
  guarantee we break on the first failing assertion. The honest rule is the two
  above, plus:
- **The failure message is content, and may contain a resolved value.** It
  quotes the application — and the spec's own expectations — back at you. The
  local database stores it in full; whether it leaves the machine is governed
  by whether the logs signal is exported, which is the user's decision to make
  with knowledge of their own collector's retention and audience. Document
  that, rather than pretending the message is safe.

## Controls

- SQLite: on by default, disableable.
- OTel: off unless an endpoint is configured, disableable.
- **Both may be disabled, including simultaneously.** An earlier draft proposed
  requiring at least one; that would mean writing to disk after a user has said
  not to, which is a small betrayal a developer tool does not recover from. It
  also buys nothing, since a local tool cannot phone home regardless.
- When recording is off, `atr history` says so and explains how to enable it,
  rather than reporting an empty history as though nothing had ever run.

## Non-goals

- Not a distributed store. Local-first. (`atr run` has no `--json` today — a
  machine-readable summary is a plausible companion feature, not something this
  spec may assume exists.)
- No screenshots, no page content, no artefacts. Metadata only.
- Not a replacement for CI's own reporting. This answers questions about
  *tests over time* that CI does not keep.

## Verification

- A passing replay records a row with zero model calls; a compile records one
  with a non-zero count and a compile phase.
- Duration metrics separate compiled from replayed runs.
- With no `OTEL_EXPORTER_OTLP_ENDPOINT`, a run emits nothing and logs no error.
- With a collector configured, a failing run produces a trace whose failing
  step span carries a correlated log record naming the failure.
- No resolved value appears in a column, span attribute or log attribute of its
  own — checked by seeding a distinctive value and grepping all three. A value
  quoted inside a failure message is expected and does not fail this check.
- A run that fails, retries and then passes records both attempts, and the
  first attempt's failure kind survives.
- A spec with no base URL, and a stale script under `--no-compile`, each record
  a run — the pre-run failures the "true failure rate" number depends on.
- A run against an unreachable OTLP endpoint exits within a bounded delay and
  still exits with the test's own code.
- A corrupt or unwritable `history.db` does not change a run's exit code.
- With both sinks disabled, nothing is written and `atr history` explains why.
- Binary size before and after the SQLite driver, on all four release targets.
