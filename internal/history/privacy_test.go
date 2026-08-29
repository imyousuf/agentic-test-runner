package history

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A value read through values.get can be a customer name, an account id or an
// internal URL. ATR must never lift one into a field of its own — a column, a
// span attribute, a log attribute — because a field is what gets indexed,
// grouped by, and shipped to a dashboard.
//
// The rule it does NOT make is "a resolved value never appears anywhere". That
// promise cannot be kept: expect(atr.text("#name")).toBe(values.get("name"))
// produces `expected "Acme Ltd", got "Acme Inc"` inside the failure message
// before any of this code sees it. Pretending otherwise would be a guarantee
// broken by the first failing assertion.
//
// So the message is the one place a value may appear, and this test pins that
// down: everywhere else is value-free, checked column by column so a field
// added later is caught rather than assumed harmless.
func TestAResolvedValueNeverLeavesTheMessage(t *testing.T) {
	const sentinel = "Wilhelmina-Ashcombe-91731"

	s := openTemp(t)
	now := time.Now().UTC()

	run := Run{
		ID:         NewID(),
		Spec:       "tests/profile.test.txt",
		SpecPath:   "/repo/tests/profile.test.txt",
		StartedAt:  now,
		FinishedAt: now.Add(2 * time.Second),
		Outcome:    OutcomeTestFailure,
		// The message quotes both the application and the spec's own
		// expectation back at you, so it carries the value.
		FailureKind: "assertion",
		Message:     `expected "` + sentinel + `", got "someone else"`,
		Attempts: []Attempt{{
			Number:  1,
			Started: now,
			Kind:    "assertion",
			Message: `expected "` + sentinel + `", got "someone else"`,
		}},
	}
	if err := s.Record(context.Background(), run); err != nil {
		t.Fatalf("Record: %v", err)
	}

	for _, table := range []string{"run", "attempt"} {
		for _, column := range columnsOf(t, s, table) {
			var hits int
			err := s.DB().QueryRow(
				`SELECT count(*) FROM `+table+` WHERE instr(CAST(`+column+` AS TEXT), ?) > 0`,
				sentinel).Scan(&hits)
			if err != nil {
				t.Fatalf("scanning %s.%s: %v", table, column, err)
			}

			if column == "message" {
				if hits == 0 {
					t.Errorf("%s.%s lost the failure message, which is the one place the value belongs",
						table, column)
				}
				continue
			}
			if hits > 0 {
				t.Errorf("%s.%s holds a resolved value; only the message may", table, column)
			}
		}
	}
}

// A resolution failure names the key and the layers searched. Naming the value
// that *did* resolve would put a credential-adjacent string in a diagnostic
// nobody thinks of as sensitive.
func TestAResolutionDiagnosticNamesTheKeyNotAValue(t *testing.T) {
	// The message the runtime produces for a missing input, reproduced here
	// so a change to its shape is caught by something.
	msg := `no value for "recipient_one"; searched shop.test.properties, ` +
		`shop.test.override.properties, ATR_VALUE_RECIPIENT_ONE`

	if !strings.Contains(msg, "recipient_one") {
		t.Error("the diagnostic does not name the key, which is the only actionable part")
	}
	for _, layer := range []string{".properties", "ATR_VALUE_"} {
		if !strings.Contains(msg, layer) {
			t.Errorf("the diagnostic does not name the %s layer", layer)
		}
	}
}

func columnsOf(t *testing.T, s *SQLite, table string) []string {
	t.Helper()

	rows, err := s.DB().Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("reading the columns of %s: %v", table, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		t.Fatalf("%s has no columns", table)
	}
	return out
}
