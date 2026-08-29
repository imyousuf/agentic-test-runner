package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/history"
)

// openHistory builds the sinks for a run.
//
// A sink that will not open is reported and skipped, never fatal. The exit
// code belongs to the application under test; a historian that could turn a
// passing suite red by failing to open a database would be worse than no
// historian at all.
func openHistory(ctx context.Context, cfg *config.Config) *history.Multi {
	m := &history.Multi{
		OnError: func(err error) {
			fmt.Fprintf(os.Stderr, "Warning: history: %v\n", err)
		},
	}

	if cfg.History.Enabled {
		db, err := history.OpenSQLite(cfg.HistoryPath(), cfg.HistoryKeep())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: not recording history: %v\n", err)
		} else {
			m.Recorders = append(m.Recorders, db)
		}
	}

	// Gated on the standard OTLP variable rather than a flag of our own, so a
	// laptop with no collector emits nothing and produces no connection
	// errors, and a CI job opts in with one line.
	if cfg.Telemetry.Enabled && history.Configured() {
		tel, err := history.NewTelemetry(ctx, history.TelemetryOptions{
			ServiceName:     cfg.Telemetry.ServiceName,
			ShutdownTimeout: cfg.Telemetry.ShutdownTimeout,
			OnError:         m.OnError,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: not exporting telemetry: %v\n", err)
		} else {
			m.Recorders = append(m.Recorders, tel)
		}
	}

	return m
}

// recordOutcome copies what the compiled path learned into the run record.
func recordOutcome(rec *history.Run, outcome *agent.RunOutcome) {
	if outcome == nil {
		return
	}

	rec.Compiled = outcome.Compiled
	rec.Repaired = outcome.Repaired
	rec.CompileDuration = outcome.CompileDuration
	rec.AgentInvocations = outcome.ModelCalls

	for _, a := range outcome.Attempts {
		if a.AfterRepair {
			rec.Repairs++
		}
		rec.Attempts = append(rec.Attempts, history.Attempt{
			Number:      a.Number,
			Started:     a.Started,
			Duration:    a.Duration,
			Passed:      a.Passed,
			Kind:        string(a.Kind),
			Message:     a.Message,
			AfterRepair: a.AfterRepair,
		})
	}

	switch {
	case outcome.Passed():
		rec.Outcome = history.OutcomePassed
	case outcome.Result != nil && outcome.Result.Failure != nil:
		f := outcome.Result.Failure
		rec.FailureKind = string(f.Kind)
		rec.Message = f.Message
		if f.Kind.IsTestFailure() {
			rec.Outcome = history.OutcomeTestFailure
		} else {
			rec.Outcome = history.OutcomeInfra
		}
	default:
		rec.Outcome = history.OutcomeInfra
	}
}

// infra marks a run that never learned anything about the application.
//
// These are the ones a history has to keep in order to be worth having: a
// spec with no base URL, an unreadable file, a stale script under
// --no-compile. Each of them used to be a `continue` that produced no record
// at all, so a "true failure rate" computed from what was kept would have
// excluded exactly the category it exists to count.
func infra(rec *history.Run, format string, args ...any) bool {
	rec.Outcome = history.OutcomeInfra
	rec.Message = fmt.Sprintf(format, args...)
	return true
}

// closeHistory flushes the sinks.
func closeHistory(ctx context.Context, m *history.Multi) {
	if m == nil {
		return
	}
	_ = m.Close(ctx)
}
