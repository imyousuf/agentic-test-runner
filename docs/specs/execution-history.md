# Tech spec: execution history and telemetry

Status: proposed
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

### One record, many sinks

A single run record is produced at the end of a run and handed to each sink
independently. Sinks are attached or not; disabling one is not attaching it.

This is the constraint that keeps the sinks honest. Two independent emitters
drift — a field is added to the database, nobody adds it to the traces, and six
months later the two disagree about what happened.

### Sink 1: SQLite, on by default

`~/.atr/history.db`. Cheap, private, same trust boundary as the browser the
user just drove.

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

- **Never record resolved values. Anywhere.** Not in the database, not in
  traces, not in logs. A value read through `values.get` can be a customer
  name, an account id or an internal URL. A resolution failure names the *key*
  and the layers searched — never a value that did resolve.
- **The failure message is content.** It quotes the application back at you, so
  it can contain whatever was on the page. The local database stores it in
  full; whether it leaves the machine is governed by whether the logs signal is
  exported, which is the user's decision to make with knowledge of their own
  collector's retention and audience.

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

- Not a distributed store. Local-first; a run emits a `--json` summary that a
  CI job can ship wherever it likes.
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
- No resolved value appears in the database, in any span attribute, or in any
  log record — checked by seeding a distinctive value and grepping all three.
- With both sinks disabled, nothing is written and `atr history` explains why.
