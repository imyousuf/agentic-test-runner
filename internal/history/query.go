package history

import (
	"context"
	"database/sql"
	"sort"
	"time"
)

// SpecSummary is one spec's record over a window.
//
// The three counts are kept apart on purpose. A pass rate that folds
// infrastructure failures in with real ones is the number teams learn to
// ignore, and separating them is the whole reason ATR exits 1 and 2 for
// different things.
type SpecSummary struct {
	Spec string
	// Runs is every recorded run, including the ones that never reached the
	// application.
	Runs int
	// Passed, TestFailures and Infra add up to Runs.
	Passed       int
	TestFailures int
	Infra        int
	// Repairs is how many times a script was rewritten. A spec that keeps
	// being repaired is not flaky — the application's DOM is churning
	// underneath it, and nothing else in a normal stack can tell you that.
	Repairs int
	// Flakes counts runs that failed at least once and then passed. These are
	// green in every report but the application, or the test, is unstable.
	Flakes int
	// Compiles is how many runs paid for a model.
	Compiles int
	// MedianReplayMS is the median duration of the runs that replayed with no
	// model in the loop. Those are deterministic, so their duration is
	// dominated by the application under test — which makes this a measure of
	// the application getting slower, not of the model getting chattier.
	MedianReplayMS int64
}

// TrueFailureRate is the share of runs that found the application broken,
// counting only the runs that actually tested it.
func (s SpecSummary) TrueFailureRate() float64 {
	tested := s.Passed + s.TestFailures
	if tested == 0 {
		return 0
	}
	return float64(s.TestFailures) / float64(tested)
}

// Summarise aggregates per spec over runs started at or after since.
func Summarise(ctx context.Context, db *sql.DB, since time.Time, spec string) ([]SpecSummary, error) {
	where := "started_at >= ?"
	args := []any{stamp(since)}
	if spec != "" {
		where += " AND spec = ?"
		args = append(args, spec)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT spec, outcome, compiled, repairs, duration_ms, id
		FROM runs WHERE `+where+` ORDER BY spec, started_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type acc struct {
		SpecSummary
		replays []int64
		runIDs  []string
	}
	byspec := map[string]*acc{}
	var order []string

	for rows.Next() {
		var (
			spec     string
			outcome  string
			compiled bool
			repairs  int
			duration int64
			id       string
		)
		if err := rows.Scan(&spec, &outcome, &compiled, &repairs, &duration, &id); err != nil {
			return nil, err
		}

		a := byspec[spec]
		if a == nil {
			a = &acc{SpecSummary: SpecSummary{Spec: spec}}
			byspec[spec] = a
			order = append(order, spec)
		}

		a.Runs++
		a.Repairs += repairs
		a.runIDs = append(a.runIDs, id)
		switch Outcome(outcome) {
		case OutcomePassed:
			a.Passed++
		case OutcomeTestFailure:
			a.TestFailures++
		default:
			a.Infra++
		}
		if compiled {
			a.Compiles++
		} else if Outcome(outcome) != OutcomeInfra {
			a.replays = append(a.replays, duration)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// A flake is a run whose first attempt failed and whose last one passed.
	// Counted from the attempts, because the run record itself says "passed"
	// — which is exactly why the attempts are kept.
	flakes, err := flakyRuns(ctx, db, since, spec)
	if err != nil {
		return nil, err
	}

	out := make([]SpecSummary, 0, len(order))
	for _, name := range order {
		a := byspec[name]
		a.MedianReplayMS = median(a.replays)
		for _, id := range a.runIDs {
			if flakes[id] {
				a.Flakes++
			}
		}
		out = append(out, a.SpecSummary)
	}
	return out, nil
}

// flakyRuns returns the runs that failed an attempt and then passed.
func flakyRuns(ctx context.Context, db *sql.DB, since time.Time, spec string) (map[string]bool, error) {
	where := "r.started_at >= ?"
	args := []any{stamp(since)}
	if spec != "" {
		where += " AND r.spec = ?"
		args = append(args, spec)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT a.run_id
		FROM attempt a JOIN run r ON r.id = a.run_id
		WHERE `+where+`
		GROUP BY a.run_id
		HAVING sum(CASE WHEN a.passed = 0 THEN 1 ELSE 0 END) > 0
		   AND sum(CASE WHEN a.passed = 1 THEN 1 ELSE 0 END) > 0`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	flakes := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		flakes[id] = true
	}
	return flakes, rows.Err()
}

// RecentRun is one run, for the per-spec listing.
type RecentRun struct {
	ID          string
	Spec        string
	StartedAt   time.Time
	DurationMS  int64
	Outcome     Outcome
	FailureKind string
	Message     string
	Compiled    bool
	Repairs     int
	Attempts    int
}

// Recent lists runs newest first.
func Recent(ctx context.Context, db *sql.DB, since time.Time, spec string, limit int) ([]RecentRun, error) {
	where := "started_at >= ?"
	args := []any{stamp(since)}
	if spec != "" {
		where += " AND spec = ?"
		args = append(args, spec)
	}
	if limit <= 0 {
		limit = 20
	}
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, `
		SELECT id, spec, started_at, duration_ms, outcome, failure_kind, message,
		       compiled, repairs,
		       (SELECT count(*) FROM attempt WHERE attempt.run_id = runs.id)
		FROM runs WHERE `+where+` ORDER BY started_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RecentRun
	for rows.Next() {
		var (
			r       RecentRun
			started string
			outcome string
		)
		if err := rows.Scan(&r.ID, &r.Spec, &started, &r.DurationMS, &outcome,
			&r.FailureKind, &r.Message, &r.Compiled, &r.Repairs, &r.Attempts); err != nil {
			return nil, err
		}
		r.Outcome = Outcome(outcome)
		// RFC3339Nano parses the fixed-width form too, and still reads a row
		// written before the format was pinned.
		r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		out = append(out, r)
	}
	return out, rows.Err()
}

func median(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}
