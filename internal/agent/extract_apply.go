package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

// maxOverlapsPerPass bounds how much one refactor takes on. Overlaps arrive
// longest first, so this keeps the ones worth the most.
const maxOverlapsPerPass = 5

// RefactorRequest asks for a directory's repeated operations to be hoisted.
type RefactorRequest struct {
	// Specs are the .test.txt paths in the directory.
	Specs []string
	// Mode says whether to apply an extraction or only report one.
	Mode ExtractionMode
	// BaseURL is where the verification replays run.
	BaseURL string
	// ScriptTimeout bounds one verification replay.
	ScriptTimeout time.Duration
	// SecretFiller is passed through to the replays.
	SecretFiller testscript.SecretFiller
	// Reset returns the browser to a clean state between replays.
	Reset func(ctx context.Context) error
	// Progress reports what is happening.
	Progress func(string)
}

// RefactorOutcome is what a refactor did, or decided not to do.
type RefactorOutcome struct {
	// Overlaps are the repeated sequences found, whether or not they were
	// acted on.
	Overlaps []testscript.Overlap
	// Applied is true when files were written and kept.
	Applied bool
	// Changed lists the scripts that were rewritten.
	Changed []string
	// Reason is the agent's account of what it hoisted, or why it did not.
	Reason string
	// ModelCalls counts what this cost.
	ModelCalls int
	// RolledBack is true when a proposal was written, failed verification,
	// and every file was put back.
	RolledBack bool
}

// RefactorOperations hoists a directory's repeated operations into its shared
// library, proving the rewrites before keeping them.
func (a *Agent) RefactorOperations(ctx context.Context, req RefactorRequest) (*RefactorOutcome, error) {
	logf := func(format string, args ...any) {
		if req.Progress != nil {
			req.Progress(fmt.Sprintf(format, args...))
		}
	}

	out := &RefactorOutcome{}
	if req.Mode == ExtractOff || len(req.Specs) < 2 {
		return out, nil
	}

	scripts, err := loadScripts(req.Specs)
	if err != nil {
		return out, err
	}
	if len(scripts) < 2 {
		return out, nil
	}

	// Static first: a directory that duplicates nothing costs nothing.
	overlaps, err := testscript.FindOverlaps(scripts)
	if err != nil {
		return out, err
	}
	out.Overlaps = overlaps
	if len(overlaps) == 0 {
		return out, nil
	}

	for _, o := range overlaps {
		logf("%d operations repeated across %s", len(o.Steps), strings.Join(shortNames(o.Scripts), ", "))
	}

	if req.Mode == ExtractOnDemand {
		logf("not hoisting them: extraction is on-demand; run `atr refactor-ops` to apply")
		return out, nil
	}

	library, err := readLibrary(req.Specs[0])
	if err != nil {
		return out, err
	}

	// One pass takes on a bounded amount of it. A large directory can repeat
	// dozens of sequences, and hoisting all of them at once means a single
	// proposal rewriting most of the suite, every one of those rewrites
	// replayed before any is kept, and the whole lot discarded if one fails.
	// Smaller is likelier to be right and cheaper to prove, and nothing is
	// lost: what is left over is still repeated next run, and gets hoisted
	// then.
	take := overlaps
	if len(take) > maxOverlapsPerPass {
		take = take[:maxOverlapsPerPass]
		logf("hoisting the %d longest of them this run; the rest are found again next run",
			maxOverlapsPerPass)
	}

	logf("asking the agent to hoist them into %s", testscript.LibraryName)
	out.ModelCalls++
	ex, err := a.ProposeExtraction(ctx, ExtractRequest{
		Library:  library,
		Scripts:  scripts,
		Overlaps: take,
		Progress: req.Progress,
	})
	if err != nil {
		return out, err
	}

	if err := ex.ResolveAgainst(scripts); err != nil {
		logf("refusing the proposed extraction: %v", err)
		return out, nil
	}

	out.Reason = ex.Reason
	if ex.Empty() {
		logf("the agent found nothing worth hoisting — %s", ex.Reason)
		return out, nil
	}

	if err := ValidateExtraction(scripts, ex); err != nil {
		// Not an error for the run: the scripts on disk are untouched and
		// still correct. The extraction simply does not happen.
		logf("refusing the proposed extraction: %v", err)
		return out, nil
	}

	// From here files change, so everything is undoable.
	restore, err := writeExtraction(req.Specs[0], ex)
	if err != nil {
		// A write that failed part way is exactly the half-hoisted directory
		// this is meant to prevent: a library that exists and some of the
		// scripts calling it, with nothing to report it and the next compile
		// reasoning from it. Put back whatever was written before giving up.
		if rerr := restore(); rerr != nil {
			return out, fmt.Errorf("writing the extraction failed (%v) and could not be undone: %w", err, rerr)
		}
		return out, err
	}

	logf("verifying %d rewritten script(s) against the application", len(ex.Scripts))
	if err := a.verifyRewrites(ctx, req, ex); err != nil {
		if rerr := restore(); rerr != nil {
			// Now the directory is half-changed, which is worse than either
			// outcome, so say so loudly rather than reporting a tidy failure.
			return out, fmt.Errorf("the extraction failed verification (%v) and could not be undone: %w", err, rerr)
		}
		out.RolledBack = true
		logf("the extraction did not verify, so it was undone — %v", err)
		return out, nil
	}

	// Record what was just proved. The replays above ran every rewritten
	// script against this exact library, which is precisely what the lib hash
	// attests — leaving it off would throw away the expensive half of the work
	// and make the next run replay the whole directory to rediscover it.
	//
	// Every spec, not only the rewritten ones: a library now exists where none
	// did, and each spec in the directory loads it whether or not it calls
	// anything in it.
	if err := stampDirectory(req.Specs); err != nil {
		// The extraction itself holds — the files are written and verified. A
		// missing stamp costs a replay next run, so it is not worth undoing
		// proven work over.
		logf("hoisted, but could not record the library hash: %v", err)
	}

	out.Applied = true
	out.Changed = ex.Paths()
	logf("hoisted into %s and verified: %s", testscript.LibraryName, ex.Reason)
	return out, nil
}

// stampDirectory records the hash of the library that every script in the
// directory was just verified against.
func stampDirectory(specs []string) error {
	// From disk, not from the proposal in memory: this must be the hash the
	// next run's freshness check computes, and that one reads the file.
	lib, err := testscript.LoadLibrary(specs[0])
	if err != nil {
		return err
	}
	hash := lib.Hash()

	// Every spec that has a script, and not one abandoned because an earlier
	// one had none. A directory can hold a spec that has never compiled — one
	// just added, or one skipped as stale — and giving up at it would leave
	// the specs after it unstamped, which costs the next run a replay of the
	// whole directory to rediscover what was just proved.
	var firstErr error
	for _, spec := range specs {
		stored, err := testscript.Load(spec)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if stored == nil {
			continue
		}
		if err := testscript.Stamp(spec, hash); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// verifyRewrites replays every script the extraction touched.
//
// The operations have moved, so this is the only thing that shows they still
// drive the application the same way. What it cannot show is that the tests
// still test anything — ValidateExtraction has already settled that, before a
// byte was written.
func (a *Agent) verifyRewrites(ctx context.Context, req RefactorRequest, ex *Extraction) error {
	byScript := map[string]string{}
	for _, spec := range req.Specs {
		byScript[testscript.ScriptPath(spec)] = spec
	}

	for _, path := range ex.Paths() {
		spec, known := byScript[path]
		if !known {
			return fmt.Errorf("%s is not a script in this directory", filepath.Base(path))
		}

		if err := a.reset(ctx, RunRequest{Reset: req.Reset}); err != nil {
			return err
		}

		values, err := testscript.LoadValues(spec)
		if err != nil {
			return err
		}
		library, err := testscript.LoadLibrary(spec)
		if err != nil {
			return err
		}

		// Per spec, exactly as the run resolves it. A directory shares a
		// library, not an address: its specs can point at different hosts, and
		// the one the refactor happened to be started with is not necessarily
		// any of them. Replaying a relative navigate against an empty base
		// fails as environment, which reads as a rewrite that broke the test.
		baseURL := req.BaseURL
		if base, ok, err := values.Resolve(ctx, "base_url"); err == nil && ok && base != "" {
			baseURL = base
		}

		result, err := testscript.Run(ctx, testscript.Options{
			Browser:      a.browser,
			Source:       ex.Scripts[path],
			Name:         path,
			BaseURL:      baseURL,
			Timeout:      req.ScriptTimeout,
			SecretFiller: req.SecretFiller,
			Values:       values,
			Library:      librarySource(library),
			LibraryName:  testscript.LibraryName,
		})
		if err != nil {
			return fmt.Errorf("replaying %s: %w", filepath.Base(path), err)
		}
		if !result.Passed {
			return fmt.Errorf("%s no longer passes: %s", filepath.Base(path), result.Failure.Error())
		}
	}

	return nil
}

// writeExtraction writes the library and the rewrites, returning a function
// that puts every one of them back exactly as it was.
//
// All of it or none of it. A directory left half-hoisted — a library that
// exists and two of three scripts calling it — is worse than either outcome,
// because nothing reports it and the next compile reasons from it.
func writeExtraction(anySpec string, ex *Extraction) (restore func() error, err error) {
	type saved struct {
		path    string
		content []byte
		existed bool
	}
	var backups []saved

	remember := func(path string) error {
		content, readErr := os.ReadFile(path)
		switch {
		case readErr == nil:
			backups = append(backups, saved{path: path, content: content, existed: true})
		case os.IsNotExist(readErr):
			backups = append(backups, saved{path: path, existed: false})
		default:
			return readErr
		}
		return nil
	}

	restore = func() error {
		var firstErr error
		for _, b := range backups {
			// A file is remembered before it is written, so some of these may
			// never have changed — a write that failed at open leaves the
			// original untouched. Putting one of those back can fail for the
			// same reason the write did, and reporting that as "could not be
			// undone" would raise an alarm about a file that is already as it
			// was.
			if current, err := os.ReadFile(b.path); err == nil {
				if b.existed && bytes.Equal(current, b.content) {
					continue
				}
			} else if !b.existed && os.IsNotExist(err) {
				continue
			}

			var rerr error
			if b.existed {
				rerr = os.WriteFile(b.path, b.content, 0o644)
			} else {
				rerr = os.Remove(b.path)
				if os.IsNotExist(rerr) {
					rerr = nil
				}
			}
			if rerr != nil && firstErr == nil {
				firstErr = rerr
			}
		}
		return firstErr
	}

	libPath := testscript.LibraryPath(anySpec)
	if err := remember(libPath); err != nil {
		return restore, err
	}
	if err := os.WriteFile(libPath, []byte(ensureTrailingNewline(ex.Library)), 0o644); err != nil {
		return restore, err
	}

	for _, path := range ex.Paths() {
		if err := remember(path); err != nil {
			return restore, err
		}
		if err := os.WriteFile(path, []byte(ensureTrailingNewline(ex.Scripts[path])), 0o644); err != nil {
			return restore, err
		}
	}

	return restore, nil
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// loadScripts reads the compiled script beside each spec, skipping the specs
// that have not been compiled yet.
func loadScripts(specs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, spec := range specs {
		stored, err := testscript.Load(spec)
		if err != nil {
			return nil, err
		}
		if stored == nil {
			continue
		}
		out[stored.Path] = stored.Source
	}
	return out, nil
}

func readLibrary(anySpec string) (string, error) {
	lib, err := testscript.LoadLibrary(anySpec)
	if err != nil {
		return "", err
	}
	return librarySource(lib), nil
}

func shortNames(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	sort.Strings(out)
	return out
}
