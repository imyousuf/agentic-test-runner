package history

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Pure Go. mattn/go-sqlite3 needs CGO, and ATR cross-compiles to four
	// targets from one machine with CGO off; a driver that breaks the release
	// build is not a driver.
	_ "modernc.org/sqlite"
)

// DefaultKeep is how long a run stays in the database.
//
// A machine that runs a suite in a loop would otherwise grow a row per attempt
// per spec for ever. Ninety days is long enough for "has this got slower over
// a quarter" and short enough that nobody has to think about it.
const DefaultKeep = 90 * 24 * time.Hour

// SQLite records runs in a local database.
//
// The schema is deliberately public — through views. An earlier design hid it
// behind an `atr history` command to keep it free to change, which throws away
// the reason to choose SQLite at all: the primary consumer is an agent with a
// shell, which will reach for one query rather than learn a flag surface, and
// can, whether or not we bless it.
//
// So: views are the contract, tables are ours. `runs`, `attempts` and
// `compiles` are recreated on every open, over whatever shape suits underneath.
type SQLite struct {
	db   *sql.DB
	path string
}

// OpenSQLite opens or creates the history database and prunes what has aged
// out.
func OpenSQLite(path string, keep time.Duration) (*SQLite, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating the history directory: %w", err)
	}

	// WAL so a second `atr run` on the same machine does not block this one,
	// and a busy timeout so it waits rather than failing outright. Nothing
	// stops two runs sharing one database, and a historian that errors under
	// ordinary concurrency would be noise on every parallel CI job.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	s := &SQLite{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if keep > 0 {
		if err := s.prune(keep); err != nil {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}

// Path is where the database lives.
func (s *SQLite) Path() string { return s.path }

// DB exposes the handle for queries. Reading is not the recorder's job, but
// `atr history` should not open a second connection to the same file.
func (s *SQLite) DB() *sql.DB { return s.db }

const schema = `
CREATE TABLE IF NOT EXISTS run (
	id                TEXT PRIMARY KEY,
	spec              TEXT NOT NULL,
	spec_path         TEXT NOT NULL,
	started_at        TEXT NOT NULL,
	finished_at       TEXT NOT NULL,
	duration_ms       INTEGER NOT NULL,
	outcome           TEXT NOT NULL,
	failure_kind      TEXT NOT NULL DEFAULT '',
	message           TEXT NOT NULL DEFAULT '',
	compiled          INTEGER NOT NULL DEFAULT 0,
	repaired          INTEGER NOT NULL DEFAULT 0,
	compile_ms        INTEGER NOT NULL DEFAULT 0,
	repairs           INTEGER NOT NULL DEFAULT 0,
	agent_invocations INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS run_spec_started ON run (spec, started_at);
CREATE INDEX IF NOT EXISTS run_started ON run (started_at);

CREATE TABLE IF NOT EXISTS attempt (
	run_id       TEXT NOT NULL,
	number       INTEGER NOT NULL,
	started_at   TEXT NOT NULL,
	duration_ms  INTEGER NOT NULL,
	passed       INTEGER NOT NULL,
	kind         TEXT NOT NULL DEFAULT '',
	message      TEXT NOT NULL DEFAULT '',
	after_repair INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (run_id, number)
);
`

// views are dropped and recreated on every open. That is what lets the tables
// change shape without breaking anyone's query.
const views = `
DROP VIEW IF EXISTS runs;
CREATE VIEW runs AS
	SELECT id, spec, spec_path, started_at, finished_at, duration_ms, outcome,
	       failure_kind, message, compiled, repaired, compile_ms, repairs,
	       agent_invocations
	FROM run;

DROP VIEW IF EXISTS attempts;
CREATE VIEW attempts AS
	SELECT a.run_id, r.spec, a.number, a.started_at, a.duration_ms, a.passed,
	       a.kind, a.message, a.after_repair
	FROM attempt a JOIN run r ON r.id = a.run_id;

DROP VIEW IF EXISTS compiles;
CREATE VIEW compiles AS
	SELECT id, spec, started_at, compile_ms, agent_invocations, outcome
	FROM run WHERE compiled = 1;
`

func (s *SQLite) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("creating the history schema: %w", err)
	}
	if _, err := s.db.Exec(views); err != nil {
		return fmt.Errorf("creating the history views: %w", err)
	}
	return nil
}

func (s *SQLite) prune(keep time.Duration) error {
	// stamp, not RFC3339Nano: the cutoff is compared against stored strings,
	// so it has to be written the same fixed-width way they are.
	cutoff := stamp(time.Now().UTC().Add(-keep))

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM attempt WHERE run_id IN (SELECT id FROM run WHERE started_at < ?)`,
		cutoff); err != nil {
		return fmt.Errorf("pruning attempts: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM run WHERE started_at < ?`, cutoff); err != nil {
		return fmt.Errorf("pruning runs: %w", err)
	}
	return tx.Commit()
}

// Record writes one run and its attempts in a single transaction.
func (s *SQLite) Record(ctx context.Context, run Run) error {
	run = withStartTime(run)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("recording %s: %w", run.Spec, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO run (
			id, spec, spec_path, started_at, finished_at, duration_ms, outcome,
			failure_kind, message, compiled, repaired, compile_ms, repairs,
			agent_invocations)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID,
		run.Spec,
		run.SpecPath,
		stamp(run.StartedAt),
		stamp(run.FinishedAt),
		run.Duration().Milliseconds(),
		string(run.Outcome),
		run.FailureKind,
		TrimMessage(run.Message),
		run.Compiled,
		run.Repaired,
		run.CompileDuration.Milliseconds(),
		run.Repairs,
		run.AgentInvocations,
	); err != nil {
		return fmt.Errorf("recording %s: %w", run.Spec, err)
	}

	for _, a := range run.Attempts {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO attempt (
				run_id, number, started_at, duration_ms, passed, kind, message,
				after_repair)
			VALUES (?,?,?,?,?,?,?,?)`,
			run.ID,
			a.Number,
			stamp(a.Started),
			a.Duration.Milliseconds(),
			a.Passed,
			a.Kind,
			TrimMessage(a.Message),
			a.AfterRepair,
		); err != nil {
			return fmt.Errorf("recording attempt %d of %s: %w", a.Number, run.Spec, err)
		}
	}

	return tx.Commit()
}

func (s *SQLite) Close(context.Context) error { return s.db.Close() }

// timeFormat is RFC 3339 with a fixed-width fraction.
//
// Fixed-width because every window and every ordering in this package is a
// string comparison in SQL, and RFC3339Nano trims trailing zeros — so
// "10:00:00Z" sorts *after* "10:00:00.5Z", the '.' being below 'Z'. Two runs
// in the same second came back in the wrong order, and the retention cutoff
// could drop a row it meant to keep. Nine digits and no trimming makes
// lexicographic order and chronological order the same thing.
const timeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// withStartTime makes sure a run has one.
//
// stamp writes a zero time as the empty string, which sorts before every real
// timestamp — so a run with no start time is outside every window, invisible
// to `atr history`, and deleted by the first retention pass. Nothing produces
// one today, and a row that silently vanishes is a poor way to find out that
// something started to.
func withStartTime(run Run) Run {
	if !run.StartedAt.IsZero() {
		return run
	}
	if !run.FinishedAt.IsZero() {
		run.StartedAt = run.FinishedAt
		return run
	}
	run.StartedAt = time.Now().UTC()
	if run.FinishedAt.IsZero() {
		run.FinishedAt = run.StartedAt
	}
	return run
}

// stamp renders a time in UTC, so rows written in two timezones sort.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeFormat)
}
