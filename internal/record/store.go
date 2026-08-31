package record

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// idPattern is the only shape an id may take: a timestamp, and an optional
// slug taken from the title.
//
// The server turns an id straight into a path, so this is a security boundary
// and not a cosmetic rule. The pattern admits no dot and no separator, so
// "..", "../etc/passwd" and an absolute path all fail before any file is
// opened.
var idPattern = regexp.MustCompile(`^\d{8}-\d{6}(-[a-z0-9-]{1,40})?$`)

// framePattern is the only shape a frame file name may take.
var framePattern = regexp.MustCompile(`^\d{6}\.jpg$`)

// ValidID reports whether an id is safe to join to the recordings root.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// ValidFrameName reports whether a frame file name is safe to serve.
func ValidFrameName(name string) bool { return framePattern.MatchString(name) }

const (
	manifestFile = "manifest.json"
	journalFile  = "frames.jsonl"
	mp4File      = "recording.mp4"
	framesDir    = "frames"
)

// DefaultRoot is where recordings live when no other directory is given.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find the home directory: %w", err)
	}
	return filepath.Join(home, ".atr", "recordings"), nil
}

// Store reads and writes the recordings under one root directory.
type Store struct {
	root string
}

// NewStore opens the store at root. An empty root means the default.
func NewStore(root string) (*Store, error) {
	if root == "" {
		var err error
		if root, err = DefaultRoot(); err != nil {
			return nil, err
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", root, err)
	}
	return &Store{root: abs}, nil
}

// Root is the directory that holds every recording.
func (s *Store) Root() string { return s.root }

// Dir is the directory of one recording.
func (s *Store) Dir(id string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("%q is not a recording id", id)
	}
	return filepath.Join(s.root, id), nil
}

// NewID builds an id from the clock and an optional title.
func NewID(now time.Time, title string) string {
	id := now.Format("20060102-150405")
	if slug := Slug(title); slug != "" {
		id += "-" + slug
	}
	return id
}

// Slug reduces a title to the characters an id allows. It returns an empty
// string when nothing usable survives.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
		if b.Len() >= 40 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// Create makes the directory for a new recording and returns its path.
func (s *Store) Create(id string) (string, error) {
	dir, err := s.Dir(id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, framesDir), 0o755); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", dir, err)
	}
	return dir, nil
}

// List returns every recording, newest first.
//
// A directory with no manifest is still listed, marked partial, and described
// from its journal. A recording that was interrupted is the one a person most
// wants to find, so hiding it would be the wrong answer.
func (s *Store) List() ([]Summary, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", s.root, err)
	}

	out := make([]Summary, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		sum, err := s.summary(e.Name())
		if err != nil {
			continue
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (s *Store) summary(id string) (Summary, error) {
	dir := filepath.Join(s.root, id)
	_, mp4Err := os.Stat(filepath.Join(dir, mp4File))

	m, err := s.Load(id)
	if err == nil {
		return Summary{
			ID: m.ID, Title: m.Title, StartedAt: m.StartedAt,
			DurationMs: m.DurationMs, Frames: len(m.Frames),
			Bytes: m.Bytes, HasMP4: mp4Err == nil,
		}, nil
	}

	// No manifest. Describe it from the journal so the library can offer a
	// repair instead of an empty row.
	frames, jerr := readJournal(filepath.Join(dir, journalFile))
	if jerr != nil {
		return Summary{}, err
	}
	sum := Summary{ID: id, Frames: len(frames), HasMP4: mp4Err == nil, Partial: true}
	if t, terr := time.Parse("20060102-150405", id[:15]); terr == nil {
		sum.StartedAt = t
	}
	if n := len(frames); n > 0 {
		sum.DurationMs = frames[n-1].AtMs
	}
	sum.Bytes = dirBytes(filepath.Join(dir, framesDir))
	return sum, nil
}

// Load reads the manifest of one recording.
func (s *Store) Load(id string) (*Manifest, error) {
	dir, err := s.Dir(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read the manifest of %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("the manifest of %s is not valid JSON: %w", id, err)
	}
	return &m, nil
}

// Save writes the manifest of one recording.
func (s *Store) Save(m *Manifest) error {
	dir, err := s.Dir(m.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode the manifest: %w", err)
	}
	return writeAtomic(filepath.Join(dir, manifestFile), data)
}

// Rename changes the title of a recording. The id never changes, because the
// player and the web page hold it in a URL.
func (s *Store) Rename(id, title string) error {
	m, err := s.Load(id)
	if err != nil {
		return err
	}
	m.Title = title
	return s.Save(m)
}

// Delete removes a whole recording.
func (s *Store) Delete(id string) error {
	dir, err := s.Dir(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("no recording %s", id)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete %s: %w", id, err)
	}
	return nil
}

// Repair rebuilds a manifest from frames.jsonl, for a recording that was
// interrupted before it could write one.
func (s *Store) Repair(id string) (*Manifest, error) {
	dir, err := s.Dir(id)
	if err != nil {
		return nil, err
	}
	frames, err := readJournal(filepath.Join(dir, journalFile))
	if err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("%s has no frame to recover", id)
	}

	// Drop any tail entry whose file never reached the disk.
	kept := frames[:0]
	for _, f := range frames {
		if _, err := os.Stat(filepath.Join(dir, framesDir, f.File)); err == nil {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("%s lists frames, but none of them is on disk", id)
	}

	started := time.Time{}
	if t, terr := time.Parse("20060102-150405", id[:15]); terr == nil {
		started = t
	}
	m := &Manifest{
		Version:    Version,
		ID:         id,
		StartedAt:  started,
		StoppedAt:  started.Add(time.Duration(kept[len(kept)-1].AtMs) * time.Millisecond),
		DurationMs: kept[len(kept)-1].AtMs,
		Frames:     kept,
		Events:     []Event{},
		Bytes:      dirBytes(filepath.Join(dir, framesDir)),
	}
	if err := s.Save(m); err != nil {
		return nil, err
	}
	return m, nil
}

// readJournal reads frames.jsonl. A truncated last line is expected after a
// crash, so it is skipped instead of failing the whole read.
func readJournal(path string) ([]FrameRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var out []FrameRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var fr FrameRecord
		if err := json.Unmarshal(line, &fr); err != nil {
			continue
		}
		out = append(out, fr)
	}
	return out, nil
}

func dirBytes(dir string) int64 {
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}

// writeAtomic writes through a temporary file, so a reader never sees a half
// written manifest.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}
	return nil
}
