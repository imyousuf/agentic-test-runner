package cli

import (
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/history"
	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

// The classification a run is recorded under has to be the same one the exit
// code is derived from. If they can disagree, the database says a suite was
// healthy on the days CI said it was broken.
func TestOutcomeMatchesTheExitCodeMeaning(t *testing.T) {
	tests := []struct {
		name    string
		outcome *agent.RunOutcome
		want    history.Outcome
	}{
		{
			name: "a pass",
			outcome: &agent.RunOutcome{
				Result: &testscript.Result{Passed: true},
			},
			want: history.OutcomePassed,
		},
		{
			// The only thing that means the application is broken.
			name: "an assertion failure",
			outcome: &agent.RunOutcome{
				Result: &testscript.Result{Failure: &testscript.Failure{
					Kind: testscript.KindAssertion, Message: "expected 1, got 0",
				}},
			},
			want: history.OutcomeTestFailure,
		},
		{
			// A missing input says nothing about the application, and folding
			// it into a failure rate is how a red build stops being believed.
			name: "a missing input",
			outcome: &agent.RunOutcome{
				Result: &testscript.Result{Failure: &testscript.Failure{
					Kind: testscript.KindConfig, Message: "no value for username",
				}},
			},
			want: history.OutcomeInfra,
		},
		{
			name: "drift the agent could not repair",
			outcome: &agent.RunOutcome{
				Result: &testscript.Result{Failure: &testscript.Failure{
					Kind: testscript.KindNotFound, Message: "#submit is gone",
				}},
			},
			want: history.OutcomeInfra,
		},
		{
			name: "a browser that fell over",
			outcome: &agent.RunOutcome{
				Result: &testscript.Result{Failure: &testscript.Failure{
					Kind: testscript.KindEnvironment, Message: "websocket closed",
				}},
			},
			want: history.OutcomeInfra,
		},
		{
			// A run that never produced a result at all.
			name:    "nothing ran",
			outcome: &agent.RunOutcome{},
			want:    history.OutcomeInfra,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rec history.Run
			recordOutcome(&rec, tt.outcome)
			if rec.Outcome != tt.want {
				t.Errorf("outcome = %q, want %q", rec.Outcome, tt.want)
			}
		})
	}
}

// The attempts are the flake evidence, and the run record is the only place
// they survive the process exiting.
func TestAttemptsAreCarriedIntoTheRecord(t *testing.T) {
	start := time.Now()
	outcome := &agent.RunOutcome{
		Result:          &testscript.Result{Passed: true},
		Compiled:        true,
		Repaired:        true,
		CompileDuration: 3 * time.Minute,
		ModelCalls:      2,
		Attempts: []agent.Attempt{
			{Number: 1, Started: start, Duration: time.Second, Kind: testscript.KindScript, Message: "boom"},
			{Number: 2, Started: start.Add(time.Second), Duration: 2 * time.Second, Passed: true, AfterRepair: true},
		},
	}

	var rec history.Run
	recordOutcome(&rec, outcome)

	if len(rec.Attempts) != 2 {
		t.Fatalf("recorded %d attempts, want 2", len(rec.Attempts))
	}
	if rec.Attempts[0].Kind != string(testscript.KindScript) || rec.Attempts[0].Passed {
		t.Errorf("the failing attempt was lost: %+v", rec.Attempts[0])
	}
	if !rec.Attempts[1].AfterRepair {
		t.Error("the repair is not visible on the attempt that followed it")
	}
	if rec.Repairs != 1 {
		t.Errorf("repairs = %d, want 1 — repair frequency is the headline number", rec.Repairs)
	}
	if rec.CompileDuration != 3*time.Minute {
		t.Errorf("compile duration = %s", rec.CompileDuration)
	}
	if rec.AgentInvocations != 2 {
		t.Errorf("agent invocations = %d, want 2", rec.AgentInvocations)
	}
	// The run passed in the end, and that is what it is recorded as — the
	// attempts are where the trouble shows.
	if rec.Outcome != history.OutcomePassed {
		t.Errorf("outcome = %q, want passed", rec.Outcome)
	}
}

// The pre-run failures — an unreadable spec, no base URL, a stale script under
// --no-compile — never produced a RunOutcome at all, so a history built from
// outcomes alone would exclude exactly the category that "true failure rate"
// exists to separate out.
func TestAPreRunFailureIsStillARecordedRun(t *testing.T) {
	var rec history.Run
	failed := infra(&rec, "no base URL: set base_url in %s", "login.test.properties")

	if !failed {
		t.Error("a pre-run failure did not count as a failure")
	}
	if rec.Outcome != history.OutcomeInfra {
		t.Errorf("outcome = %q, want infra", rec.Outcome)
	}
	if rec.Message == "" {
		t.Error("the record does not say what went wrong")
	}
	if len(rec.Attempts) != 0 {
		t.Error("a run that never reached the script has attempts")
	}
}

func TestParseSince(t *testing.T) {
	now := time.Now()

	tests := []struct {
		in      string
		wantErr bool
		within  time.Duration
	}{
		{in: "7d", within: 7 * 24 * time.Hour},
		{in: "24h", within: 24 * time.Hour},
		{in: "90m", within: 90 * time.Minute},
		{in: "", within: 30 * 24 * time.Hour},
		{in: "yesterday", wantErr: true},
		{in: "7days", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSince(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSince(%q) accepted an unparseable window", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSince(%q): %v", tt.in, err)
			}
			delta := now.Sub(got)
			if delta < tt.within-time.Minute || delta > tt.within+time.Minute {
				t.Errorf("parseSince(%q) went back %s, want about %s", tt.in, delta, tt.within)
			}
		})
	}
}
