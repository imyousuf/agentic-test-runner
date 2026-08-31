package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/record"
	"github.com/imyousuf/agentic-test-runner/internal/remote"
)

func newRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record the browser session to disk",
		Long: `Record what the browser shows and write it to disk.

A recording is a directory of JPEG frames and a manifest. That is the format,
not a step towards one, so "atr remote" plays a recording back without any
video encoder installed. Export an MP4 later with "atr record encode <id>",
which is the only part that needs ffmpeg.

The command attaches to the running browser as a second DevTools session, the
same way "atr remote" does, so you can watch and record at the same time.

Examples:
  # Record until Ctrl+C
  atr record

  # Give it a title, and keep only the last five minutes
  atr record --title "Checkout flow" --keep-last 5m

  # Record one tab and never pull it to the front
  atr record --policy pin

  # Record and export an MP4 when it stops
  atr record --encode mp4`,
		RunE: runRecord,
	}

	cmd.Flags().StringP("output", "o", "", "Recordings directory (default: ~/.atr/recordings)")
	cmd.Flags().String("title", "", "A name for this recording")
	cmd.Flags().String("attach", "", "CDP endpoint, such as cdp://127.0.0.1:9222")
	cmd.Flags().Int("quality", 60, "JPEG quality, 1 to 100")
	cmd.Flags().Int("max-width", 1600, "Largest frame width")
	cmd.Flags().Int("fps", 10, "Target frame rate")
	cmd.Flags().Duration("max-duration", 30*time.Minute, "Stop after this long; 0 means no limit")
	cmd.Flags().String("max-size", "1GB", "Stop at this size; 0 means no limit")
	cmd.Flags().Duration("keep-last", 0, "Keep only the last part of the recording")
	cmd.Flags().Duration("heartbeat", 5*time.Second, "Capture a frame after this much silence")
	cmd.Flags().String("policy", "follow", "What to do when another tab takes the foreground: follow, pin, or hold")
	cmd.Flags().String("encode", "none", "Encode when the recording stops: none or mp4")

	cmd.AddCommand(
		newRecordListCmd(),
		newRecordEncodeCmd(),
		newRecordRepairCmd(),
		newRecordRmCmd(),
		newRecordDoctorCmd(),
	)
	return cmd
}

func runRecord(cmd *cobra.Command, _ []string) error {
	f := cmd.Flags()
	output, _ := f.GetString("output")
	title, _ := f.GetString("title")
	attach, _ := f.GetString("attach")
	quality, _ := f.GetInt("quality")
	maxWidth, _ := f.GetInt("max-width")
	fps, _ := f.GetInt("fps")
	maxDuration, _ := f.GetDuration("max-duration")
	maxSizeText, _ := f.GetString("max-size")
	keepLast, _ := f.GetDuration("keep-last")
	heartbeat, _ := f.GetDuration("heartbeat")
	policy, _ := f.GetString("policy")
	encodeMode, _ := f.GetString("encode")

	switch policy {
	case "follow", "pin", "hold":
	default:
		return fmt.Errorf("--policy must be follow, pin, or hold, not %q", policy)
	}
	switch encodeMode {
	case "none", "mp4":
	default:
		return fmt.Errorf("--encode must be none or mp4, not %q", encodeMode)
	}
	maxSize, err := record.ParseSize(maxSizeText)
	if err != nil {
		return err
	}

	store, err := record.NewStore(output)
	if err != nil {
		return err
	}

	cdpURL, err := remote.Discover(attach)
	if err != nil {
		return err
	}

	streamer := remote.NewStreamer(remote.Options{
		Quality: quality, MaxWidth: maxWidth, FPS: fps, ViewOnly: true,
	})
	if err := streamer.Attach(cdpURL); err != nil {
		return err
	}
	defer streamer.Close()
	streamer.SetPolicy(policy)

	pages, _ := streamer.Pages()

	// Preflight runs before capture, never after. An error that arrives twenty
	// minutes into a recording has already wasted the twenty minutes.
	pre := record.Preflight{Root: store.Root(), Encode: encodeMode == "mp4", Pages: len(pages)}
	checks := pre.Run()
	if err := record.FirstError(checks); err != nil {
		return err
	}

	if err := streamer.Select(""); err != nil {
		return err
	}

	session := remote.NewSession(store, streamer, record.Limits{
		MaxDuration: maxDuration, MaxSize: maxSize, KeepLast: keepLast,
	}, encodeMode == "mp4")

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go streamer.Watch(ctx)
	go streamer.Heartbeat(ctx, heartbeat)

	id, err := session.Start(title)
	if err != nil {
		return err
	}
	// Publish enforces --max-duration and --max-size. Without it a recording
	// would ignore both limits when nobody is watching the live view.
	go session.Publish(ctx)

	fmt.Println("ATR recording")
	fmt.Printf("  ID:      %s\n", id)
	fmt.Printf("  Browser: %s  (attached, not owned)\n", streamer.Version())
	fmt.Printf("  Output:  %s\n", filepath.Join(store.Root(), id))
	fmt.Printf("  Policy:  %s\n", policy)
	fmt.Println("  Stop:    Ctrl+C")
	fmt.Println()

	progress := time.NewTicker(time.Second)
	defer progress.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-progress.C:
			st := session.Status()
			if !st.Recording {
				// A limit stopped it, and Publish already wrote the manifest.
				break loop
			}
			fmt.Printf("\r  %s  %d frames  %s  %d dropped   ",
				elapsed(st.ElapsedMs), st.Frames, humanBytes(st.Bytes), st.Dropped)
		}
	}
	fmt.Println()

	m, err := session.Stop()
	if err != nil {
		// Publish already stopped it when a limit was reached.
		if m, err = store.Load(id); err != nil {
			return err
		}
	}
	fmt.Printf("Recorded %d frames over %s into %s\n",
		len(m.Frames), elapsed(m.DurationMs), filepath.Join(store.Root(), m.ID))
	if m.Dropped > 0 {
		fmt.Printf("Dropped %d frames because the disk could not keep up.\n", m.Dropped)
	}

	if encodeMode == "mp4" {
		fmt.Print("Encoding the MP4 ... ")
		path, err := record.Encode(context.Background(), store, m.ID)
		if err != nil {
			fmt.Println("failed")
			return err
		}
		fmt.Println(path)
	} else {
		fmt.Printf("Play it with \"atr remote\", or export an MP4 with \"atr record encode %s\".\n", m.ID)
	}
	return nil
}

func newRecordListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the recordings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			output, _ := cmd.Flags().GetString("output")
			store, err := record.NewStore(output)
			if err != nil {
				return err
			}
			list, err := store.List()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Printf("No recording in %s.\n", store.Root())
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTARTED\tLENGTH\tFRAMES\tSIZE\tMP4\tTITLE")
			for _, r := range list {
				mp4 := ""
				if r.HasMP4 {
					mp4 = "yes"
				}
				title := r.Title
				if r.Partial {
					title = "(partial; run \"atr record repair " + r.ID + "\") " + title
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
					r.ID, r.StartedAt.Format("2006-01-02 15:04"), elapsed(r.DurationMs),
					r.Frames, humanBytes(r.Bytes), mp4, title)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringP("output", "o", "", "Recordings directory (default: ~/.atr/recordings)")
	return cmd
}

func newRecordEncodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "encode <id>",
		Short: "Export a recording as an MP4",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output, _ := cmd.Flags().GetString("output")
			store, err := record.NewStore(output)
			if err != nil {
				return err
			}
			path, err := record.Encode(cmd.Context(), store, args[0])
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "", "Recordings directory (default: ~/.atr/recordings)")
	return cmd
}

func newRecordRepairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair <id>",
		Short: "Rebuild the manifest of an interrupted recording",
		Long: `Rebuild the manifest of a recording that was interrupted.

A recording writes each frame to frames.jsonl as it goes, and writes
manifest.json only when it stops. So a recording killed part way through has
every frame on the disk but no manifest. This command builds the manifest from
the journal and drops any frame the journal lists but the disk does not have.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output, _ := cmd.Flags().GetString("output")
			store, err := record.NewStore(output)
			if err != nil {
				return err
			}
			m, err := store.Repair(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Recovered %d frames over %s.\n", len(m.Frames), elapsed(m.DurationMs))
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "", "Recordings directory (default: ~/.atr/recordings)")
	return cmd
}

func newRecordRmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm <id>...",
		Short: "Delete recordings",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output, _ := cmd.Flags().GetString("output")
			store, err := record.NewStore(output)
			if err != nil {
				return err
			}
			for _, id := range args {
				if err := store.Delete(id); err != nil {
					return err
				}
				fmt.Printf("Deleted %s\n", id)
			}
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "", "Recordings directory (default: ~/.atr/recordings)")
	return cmd
}

func newRecordDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that recording will work here",
		Long: `Check every dependency that recording needs, and say how to fix each one.

The command exits 0 when recording will work and 1 when it will not. A warning
does not fail the check.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			output, _ := cmd.Flags().GetString("output")
			attach, _ := cmd.Flags().GetString("attach")

			store, err := record.NewStore(output)
			if err != nil {
				return err
			}

			pages := -1
			cdpURL, derr := remote.Discover(attach)
			if derr == nil {
				streamer := remote.NewStreamer(remote.Options{})
				if aerr := streamer.Attach(cdpURL); aerr == nil {
					if list, perr := streamer.Pages(); perr == nil {
						pages = len(list)
					}
					streamer.Close()
				}
			}

			checks := record.Preflight{
				Root: store.Root(), Encode: true, Pages: pages,
			}.Run()

			if derr != nil {
				fmt.Printf("  ✗  %-20s %v\n", "browser", derr)
			} else {
				fmt.Printf("  ✓  %-20s %s\n", "browser", cdpURL)
			}

			failed := derr != nil
			for _, c := range checks {
				switch {
				case c.OK:
					fmt.Printf("  ✓  %-20s %s\n", c.Name, c.Detail)
				case c.Warn:
					fmt.Printf("  !  %-20s %s\n", c.Name, c.Detail)
				default:
					fmt.Printf("  ✗  %-20s %s\n", c.Name, c.Detail)
					if c.Err != nil {
						fmt.Printf("\n%s\n\n", c.Err)
					}
					failed = true
				}
			}
			if failed {
				return fmt.Errorf("recording will not work here yet")
			}
			fmt.Println("\nRecording will work here.")
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "", "Recordings directory (default: ~/.atr/recordings)")
	cmd.Flags().String("attach", "", "CDP endpoint, such as cdp://127.0.0.1:9222")
	return cmd
}

// sessionRecorder records one browser, one test at a time.
//
// "atr run --behavior --record" uses it. It attaches to the browser that the
// run already launched, exactly as "atr record" would, so a run can be
// recorded without a second browser and without changing how the run works.
type sessionRecorder struct {
	store    *record.Store
	streamer *remote.Streamer
	session  *remote.Session
	cancel   context.CancelFunc
}

func newSessionRecorder(cdpEndpoint string) (*sessionRecorder, error) {
	if cdpEndpoint == "" {
		return nil, fmt.Errorf("--record needs a CDP endpoint, and this browser exposes none")
	}
	store, err := record.NewStore("")
	if err != nil {
		return nil, err
	}

	streamer := remote.NewStreamer(remote.Options{Quality: 60, MaxWidth: 1600, FPS: 10, ViewOnly: true})
	if err := streamer.Attach(cdpEndpoint); err != nil {
		return nil, err
	}

	pages, _ := streamer.Pages()
	if err := record.FirstError((record.Preflight{Root: store.Root(), Pages: len(pages)}).Run()); err != nil {
		streamer.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	go streamer.Watch(ctx)
	go streamer.Heartbeat(ctx, 5*time.Second)

	return &sessionRecorder{
		store:    store,
		streamer: streamer,
		session:  remote.NewSession(store, streamer, record.Limits{MaxDuration: 30 * time.Minute}, false),
		cancel:   cancel,
	}, nil
}

// start begins a recording of whatever tab is in front now.
func (s *sessionRecorder) start(title string) (string, error) {
	// The test may have just opened its own tab, so pick the front one again.
	if err := s.streamer.Select(""); err != nil {
		return "", err
	}
	return s.session.Start(title)
}

func (s *sessionRecorder) stop() (*record.Manifest, error) {
	return s.session.Stop()
}

func (s *sessionRecorder) dir(id string) string {
	return filepath.Join(s.store.Root(), id)
}

func (s *sessionRecorder) Close() {
	if s.session.Active() {
		_, _ = s.session.Stop()
	}
	s.cancel()
	s.streamer.Close()
}

func elapsed(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
