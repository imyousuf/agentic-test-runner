package history

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type harness struct {
	t       *Telemetry
	spans   *tracetest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	spans := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	lp := sdklog.NewLoggerProvider()

	tel, err := newTelemetryWith(tp, mp, lp, time.Second)
	if err != nil {
		t.Fatalf("newTelemetryWith: %v", err)
	}
	t.Cleanup(func() { tel.Close(context.Background()) })

	return &harness{t: tel, spans: spans, metrics: reader}
}

func spanNames(spans tracetest.SpanStubs) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Name)
	}
	return out
}

// The shape of the trace answers "did this run cost a model?" without reading
// an attribute, which is the point of putting the compile in the hierarchy
// rather than in a field.
func TestACompileGetsItsOwnSpanAndAReplayDoesNot(t *testing.T) {
	h := newHarness(t)
	now := time.Now()

	compiled := Run{
		ID: NewID(), Spec: "tests/a.test.txt",
		StartedAt: now, FinishedAt: now.Add(4 * time.Minute),
		Outcome: OutcomePassed, Compiled: true, CompileDuration: 3 * time.Minute,
		Attempts: []Attempt{{Number: 1, Started: now.Add(3 * time.Minute), Duration: time.Second, Passed: true}},
	}
	if err := h.t.Record(context.Background(), compiled); err != nil {
		t.Fatalf("Record: %v", err)
	}

	names := spanNames(h.spans.GetSpans())
	if !contains(names, "behavior.run") {
		t.Fatalf("no run span: %v", names)
	}
	if !contains(names, "compile") {
		t.Errorf("a compile produced no compile span: %v", names)
	}
	if !contains(names, "attempt") {
		t.Errorf("no attempt span: %v", names)
	}

	h.spans.Reset()

	replay := Run{
		ID: NewID(), Spec: "tests/a.test.txt",
		StartedAt: now, FinishedAt: now.Add(9 * time.Second),
		Outcome:  OutcomePassed,
		Attempts: []Attempt{{Number: 1, Started: now, Duration: 9 * time.Second, Passed: true}},
	}
	if err := h.t.Record(context.Background(), replay); err != nil {
		t.Fatalf("Record: %v", err)
	}

	names = spanNames(h.spans.GetSpans())
	if contains(names, "compile") {
		t.Errorf("a replay produced a compile span: %v", names)
	}
}

// Step and attempt spans are emitted always, not only on failure. Failure-only
// is cheaper and loses the baseline, and the question worth answering — which
// attempt got slower — needs the passing runs to compare against.
func TestEveryAttemptGetsASpan(t *testing.T) {
	h := newHarness(t)
	now := time.Now()

	run := Run{
		ID: NewID(), Spec: "tests/a.test.txt",
		StartedAt: now, FinishedAt: now.Add(20 * time.Second),
		Outcome: OutcomePassed,
		Attempts: []Attempt{
			{Number: 1, Started: now, Duration: 5 * time.Second, Kind: "timeout", Message: "waiting for #done"},
			{Number: 2, Started: now.Add(5 * time.Second), Duration: 6 * time.Second, Passed: true},
		},
	}
	if err := h.t.Record(context.Background(), run); err != nil {
		t.Fatal(err)
	}

	var attempts int
	for _, s := range h.spans.GetSpans() {
		if s.Name == "attempt" {
			attempts++
		}
	}
	if attempts != 2 {
		t.Errorf("emitted %d attempt spans, want 2 — the failed one is the evidence", attempts)
	}
}

// A four-minute compile and a nine-second replay in one bucket set destroys
// the percentile for both, so the dimension is not optional.
func TestDurationIsDimensionedByCompiledOrReplayed(t *testing.T) {
	h := newHarness(t)
	now := time.Now()

	for _, r := range []Run{
		{ID: NewID(), Spec: "s", StartedAt: now, FinishedAt: now.Add(4 * time.Minute), Outcome: OutcomePassed, Compiled: true},
		{ID: NewID(), Spec: "s", StartedAt: now, FinishedAt: now.Add(9 * time.Second), Outcome: OutcomePassed},
	} {
		if err := h.t.Record(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := h.metrics.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	hist := findHistogram(t, rm, "atr.run.duration")
	if len(hist.DataPoints) != 2 {
		t.Fatalf("duration has %d series, want one per compiled/replayed", len(hist.DataPoints))
	}

	seen := map[bool]float64{}
	for _, dp := range hist.DataPoints {
		v, ok := dp.Attributes.Value("atr.compiled")
		if !ok {
			t.Fatal("a duration series carries no atr.compiled dimension")
		}
		seen[v.AsBool()] = dp.Sum
	}
	if seen[true] < 200 {
		t.Errorf("the compiled series recorded %.1fs, want the four-minute compile", seen[true])
	}
	if seen[false] > 60 {
		t.Errorf("the replayed series recorded %.1fs, so a compile leaked into it", seen[false])
	}
}

// A failure message as a metric label is one time series per distinct message
// — got "Demo Shop", got "Abu Jafar Saifullah" — which is the classic way to
// melt a metrics backend. And a message can carry a resolved value.
func TestNoMetricDimensionCarriesAMessage(t *testing.T) {
	const sentinel = "Wilhelmina-Ashcombe-91731"

	h := newHarness(t)
	now := time.Now()

	run := Run{
		ID: NewID(), Spec: "tests/a.test.txt",
		StartedAt: now, FinishedAt: now.Add(time.Second),
		Outcome: OutcomeTestFailure, FailureKind: "assertion",
		Message: `expected "` + sentinel + `", got "someone else"`,
		Attempts: []Attempt{{
			Number: 1, Started: now, Kind: "assertion",
			Message: `expected "` + sentinel + `", got "someone else"`,
		}},
	}
	if err := h.t.Record(context.Background(), run); err != nil {
		t.Fatal(err)
	}

	var rm metricdata.ResourceMetrics
	if err := h.metrics.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}

	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			for _, attrs := range attributeSets(m) {
				for _, kv := range attrs {
					if strings.Contains(kv.Value.Emit(), sentinel) {
						t.Errorf("%s carries a message in dimension %s", m.Name, kv.Key)
					}
				}
			}
		}
	}

	// Span attributes are bounded too; the message belongs on a log record
	// correlated by span id.
	for _, s := range h.spans.GetSpans() {
		for _, kv := range s.Attributes {
			if strings.Contains(kv.Value.Emit(), sentinel) {
				t.Errorf("span %s carries a message in attribute %s", s.Name, kv.Key)
			}
		}
	}
}

// Zero-model replay is the property the whole compiled design exists to
// protect, so a regression in it has to be alertable.
func TestAgentInvocationsAreCounted(t *testing.T) {
	h := newHarness(t)
	now := time.Now()

	run := Run{
		ID: NewID(), Spec: "tests/a.test.txt",
		StartedAt: now, FinishedAt: now.Add(time.Minute),
		Outcome: OutcomePassed, Compiled: true, AgentInvocations: 2, Repairs: 1,
	}
	if err := h.t.Record(context.Background(), run); err != nil {
		t.Fatal(err)
	}

	var rm metricdata.ResourceMetrics
	if err := h.metrics.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}

	if got := sumCounter(t, rm, "atr.agent.invocations"); got != 2 {
		t.Errorf("agent invocations = %d, want 2", got)
	}
	if got := sumCounter(t, rm, "atr.repairs"); got != 1 {
		t.Errorf("repairs = %d, want 1", got)
	}
}

// A laptop with no collector must emit nothing and, above all, log no error:
// a warning on every local run is how a feature gets disabled by everyone.
func TestNothingIsConfiguredWithoutAnEndpoint(t *testing.T) {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
	} {
		t.Setenv(key, "")
	}
	if Configured() {
		t.Error("telemetry reports itself configured with no endpoint set")
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	if !Configured() {
		t.Error("telemetry ignores the standard endpoint variable")
	}
}

// A batch processor flushes on a schedule and a replay takes nine seconds and
// exits, so without an explicit bounded flush a CI run exports nothing. The
// bound is what keeps an unreachable collector from hanging the exit.
func TestShutdownIsBounded(t *testing.T) {
	spans := &retaining{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(spans))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	lp := sdklog.NewLoggerProvider()

	tel, err := newTelemetryWith(tp, mp, lp, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := tel.Record(context.Background(), Run{
		ID: NewID(), Spec: "tests/a.test.txt",
		StartedAt: now, FinishedAt: now.Add(9 * time.Second), Outcome: OutcomePassed,
	}); err != nil {
		t.Fatal(err)
	}

	// Nothing has left the batcher yet; the flush is what gets it out.
	start := time.Now()
	if err := tel.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("shutdown took %s, which is not bounded", elapsed)
	}
	if spans.count() == 0 {
		t.Error("shutdown did not flush, so a nine-second run exports nothing")
	}
}

// A run that is already cancelled — the user pressed Ctrl-C — still has
// telemetry worth sending, and it is often the run that matters most.
func TestShutdownFlushesEvenWhenTheRunWasCancelled(t *testing.T) {
	spans := &retaining{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(spans))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	lp := sdklog.NewLoggerProvider()

	tel, err := newTelemetryWith(tp, mp, lp, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := tel.Record(context.Background(), Run{
		ID: NewID(), Spec: "tests/a.test.txt",
		StartedAt: now, FinishedAt: now.Add(time.Second), Outcome: OutcomeTestFailure,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := tel.Close(ctx); err != nil {
		t.Fatalf("Close on a cancelled context: %v", err)
	}
	if spans.count() == 0 {
		t.Error("a cancelled run exported nothing")
	}
}

// --- helpers -----------------------------------------------------------------

// retaining keeps what was exported past its own shutdown.
//
// tracetest.InMemoryExporter clears itself when the provider shuts it down, so
// with it there is no way to observe that the flush happened — which is the
// only thing these two tests are about.
type retaining struct {
	mu       sync.Mutex
	exported int
}

func (r *retaining) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exported += len(spans)
	return nil
}

func (r *retaining) Shutdown(context.Context) error { return nil }

func (r *retaining) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.exported
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func findHistogram(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Histogram[float64] {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s is not a float histogram", name)
			}
			return h
		}
	}
	t.Fatalf("no metric named %s", name)
	return metricdata.Histogram[float64]{}
}

func sumCounter(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			s, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is not an int sum", name)
			}
			var total int64
			for _, dp := range s.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	t.Fatalf("no metric named %s", name)
	return 0
}

// attributeSets pulls the dimensions off whatever kind of metric this is.
func attributeSets(m metricdata.Metrics) [][]attributeKV {
	var out [][]attributeKV
	switch d := m.Data.(type) {
	case metricdata.Sum[int64]:
		for _, dp := range d.DataPoints {
			out = append(out, kvs(dp.Attributes))
		}
	case metricdata.Histogram[float64]:
		for _, dp := range d.DataPoints {
			out = append(out, kvs(dp.Attributes))
		}
	}
	return out
}

type attributeKV = attribute.KeyValue

func kvs(set attribute.Set) []attributeKV {
	return set.ToSlice()
}
