package history

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// telemetryResource identifies ATR to the collector.
func telemetryResource(ctx context.Context, service string) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName(service)),
	)
	if err != nil {
		return nil, fmt.Errorf("describing this process to the collector: %w", err)
	}
	return res, nil
}

func (t *Telemetry) startTraces(ctx context.Context, res *resource.Resource) error {
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return fmt.Errorf("starting the trace exporter: %w", err)
	}
	t.tp = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	t.tracer = t.tp.Tracer("github.com/imyousuf/agentic-test-runner")
	return nil
}

func (t *Telemetry) startMetrics(ctx context.Context, res *resource.Resource) error {
	exp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return fmt.Errorf("starting the metric exporter: %w", err)
	}
	t.mp = sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
		sdkmetric.WithResource(res),
	)
	return t.instruments()
}

// instruments creates the measurements. Every one of them is dimensioned only
// by things with a small fixed set of values — never a message, never a
// resolved value.
func (t *Telemetry) instruments() error {
	m := t.mp.Meter("github.com/imyousuf/agentic-test-runner")

	var err error
	if t.runs, err = m.Int64Counter("atr.runs",
		otelDesc("Behaviour test runs, by outcome and failure kind")); err != nil {
		return err
	}
	if t.duration, err = m.Float64Histogram("atr.run.duration",
		otelDesc("Run duration in seconds, separated by compiled and replayed"),
		otelUnit("s")); err != nil {
		return err
	}
	if t.agentCall, err = m.Int64Counter("atr.agent.invocations",
		otelDesc("Calls into the agent. Zero for a replay, which is the property the compiled design exists to protect")); err != nil {
		return err
	}
	if t.repairs, err = m.Int64Counter("atr.repairs",
		otelDesc("Scripts rewritten. A spec repaired repeatedly means the application's DOM is churning")); err != nil {
		return err
	}
	if t.attempts, err = m.Int64Counter("atr.attempts",
		otelDesc("Script executions. More than one per run means a retry or a repair rescued it")); err != nil {
		return err
	}
	return nil
}

func (t *Telemetry) startLogs(ctx context.Context, res *resource.Resource) error {
	exp, err := otlploghttp.New(ctx)
	if err != nil {
		return fmt.Errorf("starting the log exporter: %w", err)
	}
	t.lp = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
		sdklog.WithResource(res),
	)
	t.logger = t.lp.Logger("github.com/imyousuf/agentic-test-runner")
	return nil
}

func otelDesc(s string) metric.InstrumentOption { return metric.WithDescription(s) }
func otelUnit(s string) metric.InstrumentOption { return metric.WithUnit(s) }
