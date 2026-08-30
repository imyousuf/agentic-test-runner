package history

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// A local database dies with the CI container, so a CI run's history
// evaporates entirely. OpenTelemetry is how a run leaves the machine.
//
// The three signals divide by what the data *is*, not by taste:
//
//   - Metrics carry bounded dimensions only. A failure message as a label is
//     one time series per distinct message — got "Demo Shop", got "Abu Jafar
//     Saifullah" — which is the classic way to melt a metrics backend.
//   - Traces carry structure and timing. The shape answers "did this run cost
//     a model?" without reading an attribute.
//   - Logs carry the human-readable detail, correlated to the span by trace
//     and span id. A failure message belongs here, and so does a resolution
//     failure in full — useless as a label, invaluable when you are the one
//     debugging it.
//
// That split also removes a control we would otherwise have had to invent:
// whether failure messages leave the machine is decided by whether the logs
// exporter is enabled, which is a knob OTel users already have.

// Telemetry exports runs over OTLP.
type Telemetry struct {
	tracer   trace.Tracer
	tp       *sdktrace.TracerProvider
	mp       *sdkmetric.MeterProvider
	lp       *sdklog.LoggerProvider
	logger   otellog.Logger
	shutdown time.Duration

	runs      metric.Int64Counter
	duration  metric.Float64Histogram
	agentCall metric.Int64Counter
	repairs   metric.Int64Counter
	attempts  metric.Int64Counter
}

// TelemetryOptions configures the exporter.
type TelemetryOptions struct {
	ServiceName string
	// Endpoint is the OTLP collector. Empty leaves the SDK reading the
	// standard OTEL_EXPORTER_OTLP_* variables, which is what makes the
	// per-signal ones work.
	Endpoint string
	// ShutdownTimeout bounds the flush on exit. An unreachable collector must
	// delay a run's exit, never hang it.
	ShutdownTimeout time.Duration
	// OnError receives the SDK's own complaints — a refused connection, a
	// rejected batch — so they read as one warning from ATR rather than as
	// raw log lines in the middle of a test report.
	OnError func(error)
}

// Configured reports whether there is anywhere to export to.
//
// endpoint is what ATR resolved from --otel-endpoint, config, or the standard
// OTEL_EXPORTER_OTLP_ENDPOINT variable. The per-signal variables are checked
// separately because the SDK honours them on its own, and a collector reached
// only through one of those is still a collector.
//
// Nothing is exported without one, so a laptop emits nothing and produces no
// connection errors.
func Configured(endpoint string) bool {
	if endpoint != "" {
		return true
	}
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
	} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

// NewTelemetry builds the exporter. Providers are ours rather than global, so
// nothing in the process is reconfigured behind another package's back.
func NewTelemetry(ctx context.Context, opts TelemetryOptions) (*Telemetry, error) {
	if opts.ServiceName == "" {
		opts.ServiceName = "atr"
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 5 * time.Second
	}

	res, err := telemetryResource(ctx, opts.ServiceName)
	if err != nil {
		return nil, err
	}

	routeSDKErrors(opts.OnError)

	t := &Telemetry{shutdown: opts.ShutdownTimeout}

	if err := t.startTraces(ctx, res, opts.Endpoint); err != nil {
		return nil, err
	}
	if err := t.startMetrics(ctx, res, opts.Endpoint); err != nil {
		t.Close(ctx)
		return nil, err
	}
	if err := t.startLogs(ctx, res, opts.Endpoint); err != nil {
		t.Close(ctx)
		return nil, err
	}
	return t, nil
}

// newTelemetryWith assembles a Telemetry over providers the caller supplies.
//
// Exists so the exporters can be swapped for in-memory ones in a test: the
// shape of the trace and the dimensions on the metrics are the parts that go
// wrong, and neither is observable through a real OTLP connection.
func newTelemetryWith(tp *sdktrace.TracerProvider, mp *sdkmetric.MeterProvider, lp *sdklog.LoggerProvider, shutdown time.Duration) (*Telemetry, error) {
	t := &Telemetry{tp: tp, mp: mp, lp: lp, shutdown: shutdown}
	if shutdown <= 0 {
		t.shutdown = 5 * time.Second
	}
	t.tracer = tp.Tracer("github.com/imyousuf/agentic-test-runner")
	t.logger = lp.Logger("github.com/imyousuf/agentic-test-runner")
	return t, t.instruments()
}

// Record emits one run as a trace, a set of measurements, and a log record
// per failure.
func (t *Telemetry) Record(ctx context.Context, run Run) error {
	common := []attribute.KeyValue{
		attribute.String("atr.spec", run.Spec),
		attribute.String("atr.outcome", string(run.Outcome)),
		attribute.Bool("atr.compiled", run.Compiled),
	}
	if run.FailureKind != "" {
		common = append(common, attribute.String("atr.failure_kind", run.FailureKind))
	}

	ctx, span := t.tracer.Start(ctx, "behavior.run",
		trace.WithTimestamp(run.StartedAt),
		trace.WithAttributes(append(common,
			attribute.Int("atr.repairs", run.Repairs),
			attribute.Int("atr.agent_invocations", run.AgentInvocations),
		)...))

	// A compile span only when a compile happened, so the shape of the trace
	// answers "did this cost a model?" without reading an attribute.
	if run.Compiled && run.CompileDuration > 0 {
		_, compileSpan := t.tracer.Start(ctx, "compile",
			trace.WithTimestamp(run.StartedAt),
			trace.WithAttributes(attribute.Int("atr.agent_invocations", run.AgentInvocations)))
		compileSpan.End(trace.WithTimestamp(run.StartedAt.Add(run.CompileDuration)))
	}

	for _, a := range run.Attempts {
		// attemptCtx, not ctx: a log record carries the span id of whatever
		// context it is emitted with, so emitting the failure message under
		// the run's context would attach it to the run rather than to the
		// attempt that produced it — and "which attempt failed, and why" is
		// the question the correlation exists to answer.
		attemptCtx, attemptSpan := t.tracer.Start(ctx, "attempt",
			trace.WithTimestamp(a.Started),
			trace.WithAttributes(
				attribute.Int("atr.attempt", a.Number),
				attribute.Bool("atr.passed", a.Passed),
				attribute.Bool("atr.after_repair", a.AfterRepair),
			))
		if a.Kind != "" {
			attemptSpan.SetAttributes(attribute.String("atr.failure_kind", a.Kind))
		}
		// The message goes to a log record hanging off this span, never to an
		// attribute: it is unbounded content, and correlating it by span id is
		// what makes it findable anyway.
		if a.Message != "" {
			t.log(attemptCtx, a.Message, a.Kind, run.Spec)
		}
		attemptSpan.End(trace.WithTimestamp(a.Started.Add(a.Duration)))
	}

	if run.Message != "" && len(run.Attempts) == 0 {
		// A pre-run failure has no attempt to hang off, and it is exactly the
		// case a "true failure rate" needs to see.
		t.log(ctx, run.Message, run.FailureKind, run.Spec)
	}

	span.End(trace.WithTimestamp(run.FinishedAt))

	t.runs.Add(ctx, 1, metric.WithAttributes(common...))
	t.agentCall.Add(ctx, int64(run.AgentInvocations), metric.WithAttributes(common...))
	t.repairs.Add(ctx, int64(run.Repairs), metric.WithAttributes(common...))
	t.attempts.Add(ctx, int64(len(run.Attempts)), metric.WithAttributes(common...))

	// Dimensioned by compiled-versus-replayed, which the common attributes
	// already carry. A four-minute compile and a nine-second replay in one
	// bucket set destroys the percentile for both.
	t.duration.Record(ctx, run.Duration().Seconds(), metric.WithAttributes(common...))

	return nil
}

func (t *Telemetry) log(ctx context.Context, message, kind, spec string) {
	var rec otellog.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverity(otellog.SeverityError)
	rec.SetBody(attribute.StringValue(message))
	rec.AddAttributes(
		attribute.String("atr.spec", spec),
		attribute.String("atr.failure_kind", kind),
	)
	t.logger.Emit(ctx, rec)
}

// Close flushes and shuts down, bounded.
//
// The part that is easy to forget and fatal to omit: a batch span processor
// flushes on a schedule and a periodic metric reader on another, while a
// replay takes nine seconds and exits. Without this a CI run exports nothing
// and the feature looks broken.
//
// Bounded so an unreachable collector delays exit rather than hanging it.
func (t *Telemetry) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), t.shutdown)
	defer cancel()

	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if t.tp != nil {
		note(t.tp.Shutdown(ctx))
	}
	if t.mp != nil {
		note(t.mp.Shutdown(ctx))
	}
	if t.lp != nil {
		note(t.lp.Shutdown(ctx))
	}
	if firstErr != nil {
		return fmt.Errorf("flushing telemetry: %w", firstErr)
	}
	return nil
}
