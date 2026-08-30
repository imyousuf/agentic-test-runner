package history

import (
	"context"
	"testing"
	"time"
)

func record(t *testing.T, s *SQLite, r Run) {
	t.Helper()
	if err := s.Record(context.Background(), r); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func run(spec string, at time.Time, outcome Outcome) Run {
	return Run{
		ID:         NewID(),
		Spec:       spec,
		SpecPath:   "/repo/" + spec,
		StartedAt:  at,
		FinishedAt: at.Add(10 * time.Second),
		Outcome:    outcome,
	}
}

// The number this feature exists for. A pass rate that folds infrastructure
// failures in with real ones is the number teams learn to ignore — and it is
// the reason ATR exits 1 and 2 for different things in the first place.
func TestTrueFailureRateExcludesRunsThatNeverTestedAnything(t *testing.T) {
	s := openTemp(t)
	now := time.Now().UTC()

	// Eight runs: six passed, one found the application broken, one never
	// reached it because nobody set a base URL.
	for i := range 6 {
		record(t, s, run("tests/login.test.txt", now.Add(-time.Duration(i)*time.Hour), OutcomePassed))
	}
	record(t, s, run("tests/login.test.txt", now.Add(-7*time.Hour), OutcomeTestFailure))
	record(t, s, run("tests/login.test.txt", now.Add(-8*time.Hour), OutcomeInfra))

	got, err := Summarise(context.Background(), s.DB(), now.Add(-24*time.Hour), "")
	if err != nil {
		t.Fatalf("Summarise: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("summarised %d specs, want 1", len(got))
	}

	sum := got[0]
	if sum.Runs != 8 || sum.Passed != 6 || sum.TestFailures != 1 || sum.Infra != 1 {
		t.Fatalf("counts = %+v", sum)
	}

	// One real failure in seven runs that actually tested the application —
	// not one in eight, and not two in eight.
	want := 1.0 / 7.0
	if diff := sum.TrueFailureRate() - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("true failure rate = %.4f, want %.4f", sum.TrueFailureRate(), want)
	}
}

// A timeout that passes on the second attempt is green everywhere. The
// taxonomy polices repair-laundering rigorously and says nothing about
// retry-laundering; the attempts are where that shows up.
func TestAFlakeIsVisibleEvenThoughTheRunPassed(t *testing.T) {
	s := openTemp(t)
	now := time.Now().UTC()

	flaky := run("tests/checkout.test.txt", now.Add(-time.Hour), OutcomePassed)
	flaky.Attempts = []Attempt{
		{Number: 1, Started: flaky.StartedAt, Duration: time.Second, Kind: "timeout", Message: "waiting for #done"},
		{Number: 2, Started: flaky.StartedAt.Add(time.Second), Duration: time.Second, Passed: true},
	}
	record(t, s, flaky)

	clean := run("tests/checkout.test.txt", now.Add(-2*time.Hour), OutcomePassed)
	clean.Attempts = []Attempt{{Number: 1, Started: clean.StartedAt, Passed: true}}
	record(t, s, clean)

	got, err := Summarise(context.Background(), s.DB(), now.Add(-24*time.Hour), "")
	if err != nil {
		t.Fatalf("Summarise: %v", err)
	}
	if got[0].Passed != 2 {
		t.Fatalf("both runs should be recorded as passing: %+v", got[0])
	}
	if got[0].Flakes != 1 {
		t.Errorf("flakes = %d, want 1 — a retry that rescued a run is invisible otherwise", got[0].Flakes)
	}
}

// A spec that keeps being repaired is not flaky: the application's DOM is
// churning underneath it. Nothing else in a normal stack can say that.
func TestRepairFrequencyIsPerSpec(t *testing.T) {
	s := openTemp(t)
	now := time.Now().UTC()

	churning := run("tests/dashboard.test.txt", now.Add(-time.Hour), OutcomePassed)
	churning.Repairs = 1
	record(t, s, churning)

	again := run("tests/dashboard.test.txt", now.Add(-2*time.Hour), OutcomePassed)
	again.Repairs = 1
	record(t, s, again)

	record(t, s, run("tests/stable.test.txt", now.Add(-time.Hour), OutcomePassed))

	got, err := Summarise(context.Background(), s.DB(), now.Add(-24*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}

	byname := map[string]SpecSummary{}
	for _, g := range got {
		byname[g.Spec] = g
	}
	if byname["tests/dashboard.test.txt"].Repairs != 2 {
		t.Errorf("dashboard repairs = %d, want 2", byname["tests/dashboard.test.txt"].Repairs)
	}
	if byname["tests/stable.test.txt"].Repairs != 0 {
		t.Errorf("stable spec reports repairs it never had")
	}
}

// A replay is deterministic with no model in the loop, so its duration is
// dominated by the application under test. Mixing a four-minute compile into
// that bucket destroys the number.
func TestReplayDurationExcludesCompiles(t *testing.T) {
	s := openTemp(t)
	now := time.Now().UTC()

	compile := run("tests/x.test.txt", now.Add(-3*time.Hour), OutcomePassed)
	compile.Compiled = true
	compile.AgentInvocations = 1
	compile.FinishedAt = compile.StartedAt.Add(4 * time.Minute)
	record(t, s, compile)

	for i, d := range []time.Duration{9 * time.Second, 11 * time.Second, 13 * time.Second} {
		r := run("tests/x.test.txt", now.Add(-time.Duration(i+1)*time.Hour), OutcomePassed)
		r.FinishedAt = r.StartedAt.Add(d)
		record(t, s, r)
	}

	got, err := Summarise(context.Background(), s.DB(), now.Add(-24*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].MedianReplayMS != 11000 {
		t.Errorf("median replay = %dms, want 11000 — the compile leaked into the bucket",
			got[0].MedianReplayMS)
	}
	if got[0].Compiles != 1 {
		t.Errorf("compiles = %d, want 1", got[0].Compiles)
	}
}

func TestSummariseRespectsTheWindowAndTheSpecFilter(t *testing.T) {
	s := openTemp(t)
	now := time.Now().UTC()

	record(t, s, run("tests/a.test.txt", now.Add(-time.Hour), OutcomePassed))
	record(t, s, run("tests/b.test.txt", now.Add(-time.Hour), OutcomePassed))
	record(t, s, run("tests/a.test.txt", now.Add(-100*24*time.Hour), OutcomePassed))

	got, err := Summarise(context.Background(), s.DB(), now.Add(-24*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("summarised %d specs, want 2", len(got))
	}
	for _, g := range got {
		if g.Runs != 1 {
			t.Errorf("%s has %d runs in the window, want 1", g.Spec, g.Runs)
		}
	}

	only, err := Summarise(context.Background(), s.DB(), now.Add(-24*time.Hour), "tests/a.test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].Spec != "tests/a.test.txt" {
		t.Errorf("the spec filter returned %+v", only)
	}
}

func TestRecentListsNewestFirst(t *testing.T) {
	s := openTemp(t)
	now := time.Now().UTC()

	for i := range 5 {
		r := run("tests/a.test.txt", now.Add(-time.Duration(i)*time.Hour), OutcomePassed)
		r.Attempts = []Attempt{{Number: 1, Started: r.StartedAt, Passed: true}}
		record(t, s, r)
	}

	got, err := Recent(context.Background(), s.DB(), now.Add(-24*time.Hour), "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("returned %d runs, want the 3 asked for", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].StartedAt.After(got[i-1].StartedAt) {
			t.Error("runs are not newest first")
		}
	}
	if got[0].Attempts != 1 {
		t.Errorf("attempt count = %d, want 1", got[0].Attempts)
	}
}

// A repaired run paid for a model: a triage call, a rewrite, and a second
// execution. Its duration says nothing about how fast the application is, and
// one of them in the bucket moved a 9s median to 64s — which is the number
// somebody would have read as "the app got seven times slower".
//
// The predicate is "no model in the loop", not "did not compile".
func TestARepairedRunIsNotAReplay(t *testing.T) {
	s := openTemp(t)
	now := time.Now().UTC()

	repaired := run("tests/a.test.txt", now.Add(-3*time.Hour), OutcomePassed)
	repaired.FinishedAt = repaired.StartedAt.Add(2 * time.Minute)
	repaired.Repaired = true
	repaired.Repairs = 1
	repaired.AgentInvocations = 1
	repaired.Attempts = []Attempt{
		{Number: 1, Started: repaired.StartedAt, Duration: 5 * time.Second, Kind: "not_found"},
		{Number: 2, Started: repaired.StartedAt.Add(100 * time.Second), Duration: 6 * time.Second,
			Passed: true, AfterRepair: true},
	}
	record(t, s, repaired)

	for i, d := range []time.Duration{9 * time.Second, 11 * time.Second} {
		r := run("tests/a.test.txt", now.Add(-time.Duration(i+1)*time.Hour), OutcomePassed)
		r.FinishedAt = r.StartedAt.Add(d)
		record(t, s, r)
	}

	got, err := Summarise(context.Background(), s.DB(), now.Add(-24*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}

	if got[0].MedianReplayMS != 10000 {
		t.Errorf("median replay = %dms, want 10000 — a run that paid for a model leaked into the bucket",
			got[0].MedianReplayMS)
	}
	// It is still a run, still a flake, and still a repair.
	if got[0].Runs != 3 || got[0].Repairs != 1 || got[0].Flakes != 1 {
		t.Errorf("the repaired run was dropped from the counts too: %+v", got[0])
	}
}
