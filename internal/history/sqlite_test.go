package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTemp(t *testing.T) *SQLite {
	t.Helper()
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "history.db"), DefaultKeep)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { s.Close(context.Background()) })
	return s
}

func sampleRun() Run {
	start := time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)
	return Run{
		ID:               NewID(),
		Spec:             "tests/login.test.txt",
		SpecPath:         "/home/someone/repo/tests/login.test.txt",
		StartedAt:        start,
		FinishedAt:       start.Add(9 * time.Second),
		Outcome:          OutcomePassed,
		Compiled:         true,
		CompileDuration:  4 * time.Minute,
		AgentInvocations: 1,
		Attempts: []Attempt{
			{Number: 1, Started: start, Duration: 9 * time.Second, Passed: true},
		},
	}
}

func TestRoundTripThroughTheViews(t *testing.T) {
	s := openTemp(t)
	run := sampleRun()

	if err := s.Record(context.Background(), run); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var (
		spec     string
		outcome  string
		duration int64
		compile  int64
		agent    int64
	)
	err := s.DB().QueryRow(
		`SELECT spec, outcome, duration_ms, compile_ms, agent_invocations FROM runs WHERE id = ?`,
		run.ID).Scan(&spec, &outcome, &duration, &compile, &agent)
	if err != nil {
		t.Fatalf("reading back through the runs view: %v", err)
	}

	if spec != run.Spec {
		t.Errorf("spec = %q, want %q", spec, run.Spec)
	}
	if outcome != string(OutcomePassed) {
		t.Errorf("outcome = %q", outcome)
	}
	if duration != 9000 {
		t.Errorf("duration_ms = %d, want 9000", duration)
	}
	if compile != 240000 {
		t.Errorf("compile_ms = %d, want 240000 — an untimed compile hides the expensive half", compile)
	}
	if agent != 1 {
		t.Errorf("agent_invocations = %d, want 1", agent)
	}

	var attempts int
	if err := s.DB().QueryRow(`SELECT count(*) FROM attempts WHERE run_id = ?`, run.ID).Scan(&attempts); err != nil {
		t.Fatalf("reading the attempts view: %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}

	// The compiles view exists so "what did compiling cost" is one query.
	var compiles int
	if err := s.DB().QueryRow(`SELECT count(*) FROM compiles`).Scan(&compiles); err != nil {
		t.Fatalf("reading the compiles view: %v", err)
	}
	if compiles != 1 {
		t.Errorf("compiles = %d, want 1", compiles)
	}
}

// The attempts are the reason this exists: a run that failed, retried and
// passed has to leave the failure behind, or the flake is invisible.
func TestAFlakeSurvivesInTheAttempts(t *testing.T) {
	s := openTemp(t)

	start := time.Now().UTC()
	run := sampleRun()
	run.Attempts = []Attempt{
		{Number: 1, Started: start, Duration: time.Second, Kind: "timeout", Message: "waiting for #done"},
		{Number: 2, Started: start.Add(time.Second), Duration: time.Second, Passed: true},
	}

	if err := s.Record(context.Background(), run); err != nil {
		t.Fatalf("Record: %v", err)
	}

	rows, err := s.DB().Query(
		`SELECT number, passed, kind FROM attempts WHERE run_id = ? ORDER BY number`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type row struct {
		number int
		passed bool
		kind   string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.number, &r.passed, &r.kind); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("kept %d attempts, want 2", len(got))
	}
	if got[0].passed || got[0].kind != "timeout" {
		t.Errorf("the failing attempt was not kept: %+v", got[0])
	}
	if !got[1].passed {
		t.Errorf("the passing attempt was not kept: %+v", got[1])
	}
}

// Nothing stops two `atr run` processes sharing one database, and a historian
// that errors under ordinary concurrency would be noise on every parallel CI
// job.
func TestConcurrentRecordersBothSucceed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")

	first, err := OpenSQLite(path, DefaultKeep)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	defer first.Close(context.Background())

	second, err := OpenSQLite(path, DefaultKeep)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer second.Close(context.Background())

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 10 {
		for _, s := range []*SQLite{first, second} {
			wg.Add(1)
			go func(s *SQLite, i int) {
				defer wg.Done()
				run := sampleRun()
				run.Spec = "tests/spec.test.txt"
				if err := s.Record(context.Background(), run); err != nil {
					errs <- err
				}
			}(s, i)
		}
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent record failed: %v", err)
	}

	var count int
	if err := first.DB().QueryRow(`SELECT count(*) FROM runs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 20 {
		t.Errorf("recorded %d runs, want 20", count)
	}
}

// A machine running a suite in a loop would otherwise grow for ever.
func TestRetentionPrunesOldRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")

	s, err := OpenSQLite(path, DefaultKeep)
	if err != nil {
		t.Fatal(err)
	}

	old := sampleRun()
	old.StartedAt = time.Now().UTC().Add(-200 * 24 * time.Hour)
	old.FinishedAt = old.StartedAt.Add(time.Second)
	old.Attempts = []Attempt{{Number: 1, Started: old.StartedAt, Passed: true}}

	recent := sampleRun()
	recent.StartedAt = time.Now().UTC()
	recent.FinishedAt = recent.StartedAt.Add(time.Second)

	for _, r := range []Run{old, recent} {
		if err := s.Record(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	s.Close(context.Background())

	// Pruning happens on open, so the next run of the tool tidies up.
	s2, err := OpenSQLite(path, DefaultKeep)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close(context.Background())

	var runs, attempts int
	if err := s2.DB().QueryRow(`SELECT count(*) FROM runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := s2.DB().QueryRow(`SELECT count(*) FROM attempts`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Errorf("kept %d runs, want 1", runs)
	}
	if attempts != 1 {
		t.Errorf("kept %d attempts, want 1 — an orphaned attempt is a leak", attempts)
	}
}

// A test run's exit code belongs to the application under test. A historian
// that could turn a passing suite red by failing to write is worse than no
// historian.
func TestASinkFailureNeverFailsTheRun(t *testing.T) {
	var reported []error
	m := &Multi{
		Recorders: []Recorder{brokenRecorder{}},
		OnError:   func(err error) { reported = append(reported, err) },
	}

	if err := m.Record(context.Background(), sampleRun()); err != nil {
		t.Fatalf("Record returned an error to the caller: %v", err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatalf("Close returned an error to the caller: %v", err)
	}
	if len(reported) != 2 {
		t.Errorf("reported %d errors, want both to be surfaced rather than swallowed", len(reported))
	}
}

type brokenRecorder struct{}

func (brokenRecorder) Record(context.Context, Run) error { return errors.New("disk is full") }
func (brokenRecorder) Close(context.Context) error       { return errors.New("still full") }

// A laptop checkout and a CI checkout are the same spec. Identifying it by
// absolute path splits its history in two without saying so.
func TestSpecIdentityIsStableAcrossCheckouts(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, ".git"))
	mkdirAll(t, filepath.Join(root, "tests", "e2e"))

	spec := filepath.Join(root, "tests", "e2e", "login.test.txt")
	stable, absolute := SpecIdentity(spec)

	if stable != "tests/e2e/login.test.txt" {
		t.Errorf("stable name = %q, want the repository-relative path", stable)
	}
	if absolute != spec {
		t.Errorf("absolute = %q, want %q", absolute, spec)
	}
	if strings.Contains(stable, root) {
		t.Error("the stable name embeds the checkout location")
	}
}

func TestSpecIdentityOutsideARepositoryFallsBackToTheName(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.test.txt")

	stable, _ := SpecIdentity(spec)
	if stable != "login.test.txt" {
		t.Errorf("stable name = %q, want the base name", stable)
	}
}

func TestAnEnormousMessageDoesNotBloatARow(t *testing.T) {
	huge := strings.Repeat("x", 50_000)
	trimmed := TrimMessage(huge)
	if len(trimmed) > 4100 {
		t.Errorf("message kept at %d bytes", len(trimmed))
	}
	if !strings.HasSuffix(trimmed, "(truncated)") {
		t.Error("the truncation is silent")
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
