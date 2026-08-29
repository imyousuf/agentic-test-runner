package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/history"
)

var (
	historySpecFlag  string
	historySinceFlag string
	historyLimitFlag int
	historyJSONFlag  bool
	historyRunsFlag  bool
)

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Report on behaviour test runs over time",
		Long: `Report on behaviour test runs recorded locally.

ATR knows things about a run that a general-purpose reporter cannot: whether a
model was involved, whether the script was compiled or replayed, the kind of
failure, and whether a repair happened.

Two numbers this makes available:

  repair frequency  A spec that keeps being repaired is not flaky — the
                    application's DOM is churning underneath it.
  true failure rate Pass rate over the runs that actually tested the
                    application, excluding the ones that never reached it.

The database is plain SQLite and the views are a stable contract, so anything
this command will not tell you is one query away:

  sqlite3 ~/.atr/history.db "SELECT spec, outcome, count(*) FROM runs GROUP BY 1,2"

Views: runs, attempts, compiles.`,
		RunE: runHistory,
	}

	cmd.Flags().StringVar(&historySpecFlag, "spec", "", "Only this spec (repository-relative path)")
	cmd.Flags().StringVar(&historySinceFlag, "since", "30d", "Window: 90m, 24h, 30d")
	cmd.Flags().IntVar(&historyLimitFlag, "limit", 20, "How many runs to list with --runs")
	cmd.Flags().BoolVar(&historyJSONFlag, "json", false, "Emit JSON")
	cmd.Flags().BoolVar(&historyRunsFlag, "runs", false, "List individual runs rather than a summary")

	return cmd
}

func runHistory(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return exitWith(ExitInfra, fmt.Errorf("loading config: %w", err))
	}

	// An empty table and a disabled recorder look identical, and the
	// difference is everything: one means nothing has run, the other means
	// nothing is being kept.
	if !cfg.History.Enabled {
		fmt.Println("History recording is off.")
		fmt.Println()
		fmt.Println("Turn it on in ~/.atr/config.yaml:")
		fmt.Println()
		fmt.Println("  history:")
		fmt.Println("    enabled: true")
		return nil
	}

	since, err := parseSince(historySinceFlag)
	if err != nil {
		return exitWith(ExitInfra, err)
	}

	path := cfg.HistoryPath()
	if _, err := os.Stat(path); err != nil {
		fmt.Printf("No history yet at %s — it is written the first time a behaviour test runs.\n", path)
		return nil
	}

	// Opening with no retention: reporting must not delete anything.
	db, err := history.OpenSQLite(path, 0)
	if err != nil {
		return exitWith(ExitInfra, fmt.Errorf("opening %s: %w", path, err))
	}
	defer db.Close(cmd.Context())

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if historyRunsFlag {
		return listRuns(ctx, db, since)
	}
	return summariseRuns(ctx, db, since, path)
}

func summariseRuns(ctx context.Context, db *history.SQLite, since time.Time, path string) error {
	summaries, err := history.Summarise(ctx, db.DB(), since, historySpecFlag)
	if err != nil {
		return exitWith(ExitInfra, err)
	}

	if historyJSONFlag {
		return emitJSON(summaries)
	}

	if len(summaries) == 0 {
		fmt.Printf("No runs since %s in %s.\n", since.Format(time.DateOnly), path)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SPEC\tRUNS\tPASS\tFAIL\tINFRA\tFLAKE\tREPAIRS\tCOMPILES\tREPLAY")
	for _, s := range summaries {
		replay := "-"
		if s.MedianReplayMS > 0 {
			replay = (time.Duration(s.MedianReplayMS) * time.Millisecond).Round(time.Millisecond).String()
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
			s.Spec, s.Runs, s.Passed, s.TestFailures, s.Infra, s.Flakes,
			s.Repairs, s.Compiles, replay)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("FAIL is the application misbehaving; INFRA never reached it, so it is")
	fmt.Println("excluded from the true failure rate. FLAKE passed only after a retry.")
	fmt.Println("REPLAY is the median duration of runs with no model in the loop, so a")
	fmt.Println("rising number means the application got slower.")
	return nil
}

func listRuns(ctx context.Context, db *history.SQLite, since time.Time) error {
	runs, err := history.Recent(ctx, db.DB(), since, historySpecFlag, historyLimitFlag)
	if err != nil {
		return exitWith(ExitInfra, err)
	}

	if historyJSONFlag {
		return emitJSON(runs)
	}

	if len(runs) == 0 {
		fmt.Printf("No runs since %s.\n", since.Format(time.DateOnly))
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "WHEN\tSPEC\tOUTCOME\tKIND\tDURATION\tATTEMPTS\tHOW")
	for _, r := range runs {
		how := "replay"
		if r.Compiled {
			how = "compiled"
		}
		if r.Repairs > 0 {
			how = fmt.Sprintf("%s +%d repair", how, r.Repairs)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			r.StartedAt.Local().Format("2006-01-02 15:04"),
			r.Spec, r.Outcome, dash(r.FailureKind),
			(time.Duration(r.DurationMS) * time.Millisecond).Round(time.Millisecond),
			r.Attempts, how)
	}
	return w.Flush()
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// parseSince accepts Go durations plus a day suffix, because "30d" is what
// anyone asking this question actually types.
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now().AddDate(0, 0, -30), nil
	}

	if days, ok := strings.CutSuffix(s, "d"); ok {
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err != nil || n < 0 {
			return time.Time{}, fmt.Errorf("--since %q: expected something like 7d, 24h or 90m", s)
		}
		return time.Now().AddDate(0, 0, -n), nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--since %q: expected something like 7d, 24h or 90m", s)
	}
	return time.Now().Add(-d), nil
}
