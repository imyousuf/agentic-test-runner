package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

// The claim this design rests on: tables are ours, views are the contract. It
// only holds if the views are recreated on open, or the first schema change
// leaves everyone querying a view that describes the old shape.
func TestViewsAreRecreatedOnEveryOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")

	s, err := OpenSQLite(path, DefaultKeep)
	if err != nil {
		t.Fatal(err)
	}
	// Stand in for a view left behind by an older version.
	if _, err := s.DB().Exec(`DROP VIEW runs; CREATE VIEW runs AS SELECT id FROM run`); err != nil {
		t.Fatal(err)
	}
	s.Close(context.Background())

	s2, err := OpenSQLite(path, DefaultKeep)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close(context.Background())

	record(t, s2, sampleRun())

	// The current view exposes far more than an id; a stale one would fail here.
	var spec, outcome string
	if err := s2.DB().QueryRow(`SELECT spec, outcome FROM runs LIMIT 1`).Scan(&spec, &outcome); err != nil {
		t.Fatalf("the stale view survived the open: %v", err)
	}
}

// WAL is what lets a second atr run write while this one is reading. Asserting
// it directly, because the concurrency test would still pass by luck on a
// single-threaded run.
func TestWALIsEnabled(t *testing.T) {
	s := openTemp(t)

	var mode string
	if err := s.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	var busy int
	if err := s.DB().QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if busy <= 0 {
		t.Errorf("busy_timeout = %d; without one a concurrent write errors instead of waiting", busy)
	}
}

// A recorder that cannot open its database must report and be skipped, never
// stop the run: the exit code belongs to the application under test.
func TestAnUnwritableDatabaseIsAnErrorNotAPanic(t *testing.T) {
	// Windows does not honour the Unix mode bits on a directory, so there is
	// no cheap way to make one unwritable — and the behaviour under test is
	// the error path, not the permission model.
	if runtime.GOOS == "windows" {
		t.Skip("a directory's Unix mode bits do not make it unwritable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can write to a read-only directory")
	}

	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	s, err := OpenSQLite(filepath.Join(dir, "sub", "history.db"), DefaultKeep)
	if err == nil {
		s.Close(context.Background())
		t.Fatal("a database in an unwritable directory opened")
	}
	if !strings.Contains(err.Error(), "history") && !strings.Contains(err.Error(), "permission") {
		t.Errorf("the error does not say what failed: %v", err)
	}
}

// A file that is not a database at all — a truncated copy, something else
// written to the same path — must fail rather than crash the run.
func TestACorruptDatabaseIsAnErrorNotAPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	if err := os.WriteFile(path, []byte("this is not a database"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := OpenSQLite(path, DefaultKeep)
	if err == nil {
		// Some drivers defer the header check; a write must still fail rather
		// than corrupt anything.
		recErr := s.Record(context.Background(), sampleRun())
		s.Close(context.Background())
		if recErr == nil {
			t.Fatal("a corrupt file accepted a run record")
		}
	}
}

// A run is written once. Recording the same id twice — a retry of the write,
// two sinks fed the same record — must not double-count it in every report.
func TestRecordingTheSameRunTwiceDoesNotDuplicateIt(t *testing.T) {
	s := openTemp(t)
	run := sampleRun()
	run.Attempts = []Attempt{{Number: 1, Started: run.StartedAt, Passed: true}}

	record(t, s, run)
	record(t, s, run)

	var runs, attempts int
	if err := s.DB().QueryRow(`SELECT count(*) FROM runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`SELECT count(*) FROM attempts`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Errorf("recorded %d runs for one id", runs)
	}
	if attempts != 1 {
		t.Errorf("recorded %d attempts for one attempt", attempts)
	}
}

// The database is meant to be queried directly — the primary consumer is an
// agent with a shell. Every view has to be readable without knowing the tables.
func TestEveryViewIsQueryable(t *testing.T) {
	s := openTemp(t)
	run := sampleRun()
	run.Attempts = []Attempt{{Number: 1, Started: run.StartedAt, Passed: true}}
	record(t, s, run)

	for _, view := range []string{"runs", "attempts", "compiles"} {
		var n int
		if err := s.DB().QueryRow(`SELECT count(*) FROM ` + view).Scan(&n); err != nil {
			t.Errorf("SELECT from %s: %v", view, err)
		}
	}
}

// Retention has to be opt-out, not accidental: passing zero means keep
// everything, which is what `atr history` does so reporting never deletes.
func TestZeroRetentionKeepsEverything(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")

	s, err := OpenSQLite(path, DefaultKeep)
	if err != nil {
		t.Fatal(err)
	}
	old := sampleRun()
	old.StartedAt = time.Now().UTC().Add(-1000 * 24 * time.Hour)
	old.FinishedAt = old.StartedAt.Add(time.Second)
	record(t, s, old)
	s.Close(context.Background())

	s2, err := OpenSQLite(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close(context.Background())

	var runs int
	if err := s2.DB().QueryRow(`SELECT count(*) FROM runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Error("opening for reporting pruned the history it was asked to report on")
	}
}

// Every window and every ordering in this package is a string comparison in
// SQL. RFC3339Nano trims trailing zeros, so "10:00:00Z" sorts after
// "10:00:00.5Z" — the '.' being below 'Z' — and two runs in the same second
// came back in the wrong order while the retention cutoff could drop a row it
// meant to keep.
func TestTimestampsSortChronologically(t *testing.T) {
	base := time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)

	moments := []time.Time{
		base,
		base.Add(500 * time.Millisecond),
		base.Add(time.Second),
		base.Add(time.Second + time.Nanosecond),
		base.Add(time.Minute),
	}

	for i := 1; i < len(moments); i++ {
		earlier, later := stamp(moments[i-1]), stamp(moments[i])
		if !(earlier < later) {
			t.Errorf("%q does not sort before %q, so a window or an ordering is wrong",
				earlier, later)
		}
	}
}

// The same thing, through the database, because the bug only bites in SQL.
func TestRunsInTheSameSecondComeBackInOrder(t *testing.T) {
	s := openTemp(t)
	base := time.Now().UTC().Truncate(time.Second)

	// Deliberately spanning a whole second and a fractional one, which is the
	// pair that inverted.
	for _, offset := range []time.Duration{0, 500 * time.Millisecond, time.Second} {
		r := run("tests/a.test.txt", base.Add(offset), OutcomePassed)
		r.FinishedAt = r.StartedAt.Add(time.Second)
		record(t, s, r)
	}

	got, err := Recent(context.Background(), s.DB(), base.Add(-time.Hour), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("returned %d runs, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].StartedAt.After(got[i-1].StartedAt) {
			t.Errorf("run %d (%s) is newer than the one before it (%s), but --runs lists newest first",
				i, got[i].StartedAt.Format(time.RFC3339Nano), got[i-1].StartedAt.Format(time.RFC3339Nano))
		}
	}
}

// The retention cutoff is the same comparison, and getting it wrong deletes
// history somebody was keeping on purpose.
func TestRetentionRespectsSubSecondBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")

	s, err := OpenSQLite(path, 0)
	if err != nil {
		t.Fatal(err)
	}

	keep := time.Hour
	cutoff := time.Now().UTC().Add(-keep)

	// Half a second the safe side of the cutoff: must survive.
	safe := run("tests/a.test.txt", cutoff.Add(500*time.Millisecond), OutcomePassed)

	// On an exact second, just the wrong side of a cutoff that has a fraction
	// — which is every cutoff, since it comes from time.Now(). This is the
	// pair that inverts under a trimmed format: "…:00Z" against
	// "…:00.123456789Z" compares as *greater*, because '.' sits below 'Z', so
	// the stale row survived its own deletion.
	stale := run("tests/a.test.txt", cutoff.Truncate(time.Second), OutcomePassed)
	record(t, s, safe)
	record(t, s, stale)
	s.Close(context.Background())

	s2, err := OpenSQLite(path, keep)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close(context.Background())

	var ids []string
	rows, err := s2.DB().Query(`SELECT id FROM runs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	if len(ids) != 1 || ids[0] != safe.ID {
		t.Errorf("kept %d rows, want only the one inside the window", len(ids))
	}
}

// stamp writes a zero time as the empty string, which sorts before every real
// timestamp — so a run with no start time is outside every window, invisible
// to `atr history`, and deleted by the first retention pass. Nothing produces
// one today, and a row that silently vanishes is a poor way to find out that
// something started to.
func TestARunWithNoStartTimeIsStillVisible(t *testing.T) {
	s := openTemp(t)

	if err := s.Record(context.Background(), Run{
		ID: "zero", Spec: "tests/a.test.txt", SpecPath: "/repo/tests/a.test.txt",
		Outcome: OutcomePassed,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var started string
	if err := s.DB().QueryRow(`SELECT started_at FROM runs WHERE id = 'zero'`).Scan(&started); err != nil {
		t.Fatal(err)
	}
	if started == "" {
		t.Fatal("a run with no start time was stored with an empty timestamp, which sorts before everything")
	}

	got, err := Summarise(context.Background(), s.DB(), time.Now().UTC().Add(-time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Error("the run is invisible to every window")
	}
}

// A second Close reported "reader is shutdown" as though an export had failed,
// which is a warning about nothing — and the warning is the only thing a user
// ever sees from this package.
func TestClosingTwiceIsQuiet(t *testing.T) {
	s := openTemp(t)
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("second close: %v", err)
	}
}
