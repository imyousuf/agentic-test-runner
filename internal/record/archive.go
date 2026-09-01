package record

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxImportBytes caps what one archive may expand to.
//
// A zip is a compressed format a stranger can hand you, and 4 GB of zeroes
// compresses to a few kilobytes. The cap is generous next to a real recording:
// the recorder's own default stops at 1 GB.
const maxImportBytes int64 = 4 << 30

// maxImportFiles caps how many entries an archive may hold. A recording of an
// hour at ten frames a second is thirty-six thousand.
const maxImportFiles = 200_000

/*
archiveMember reports the path an entry may be written to, and whether it is
allowed at all.

This is the security boundary of import, and it is an allowlist rather than a
sanitiser. Every path written comes from this fixed set, so no name out of the
archive ever reaches the filesystem: not "../../.ssh/authorized_keys", not an
absolute path, not a symlink, and not a Windows "..\" that a Unix-flavoured
check would miss.

One leading directory is tolerated, because an archive that has been unzipped
and zipped again usually gains one.
*/
func archiveMember(name string) (string, bool) {
	slashed := strings.ReplaceAll(name, `\`, "/")
	// Refused before cleaning, not after. "frames/../manifest.json" cleans to
	// a path inside the recording, so it escapes nothing -- but it lets one
	// archive carry two entries that land on the same file, and the second
	// wins. There is no honest reason for ".." in an archive we wrote.
	if slashed == ".." || strings.HasPrefix(slashed, "../") ||
		strings.Contains(slashed, "/../") || strings.HasSuffix(slashed, "/..") {
		return "", false
	}
	if strings.HasPrefix(slashed, "/") {
		return "", false
	}
	clean := path.Clean(slashed)
	if allowed(clean) {
		return clean, true
	}
	if _, rest, found := strings.Cut(clean, "/"); found && allowed(rest) {
		return rest, true
	}
	return "", false
}

func allowed(rel string) bool {
	switch rel {
	case manifestFile, journalFile, devtoolsFile, mp4File:
		return true
	}
	dir, file := path.Split(rel)
	return dir == framesDir+"/" && ValidFrameName(file)
}

// Export writes one recording to w as a zip.
//
// Everything that makes the recording what it is goes in: the frames, the
// manifest that carries the timeline, and the devtools journal. The MP4 is
// left out unless asked for, because it is derived from the frames and doubles
// the size for nothing.
func Export(s *Store, id string, w io.Writer, withMP4 bool) error {
	if !ValidID(id) {
		return fmt.Errorf("%q is not a recording id", id)
	}
	dir := filepath.Join(s.Root(), id)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("no recording %s", id)
	}

	zw := zip.NewWriter(w)
	add := func(rel string) error {
		src := filepath.Join(dir, filepath.FromSlash(rel))
		f, err := os.Open(src)
		if err != nil {
			if os.IsNotExist(err) {
				return nil // an optional part, such as the journal after a clean stop
			}
			return err
		}
		defer func() { _ = f.Close() }()
		out, err := zw.Create(rel)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, f)
		return err
	}

	for _, rel := range []string{manifestFile, journalFile, devtoolsFile} {
		if err := add(rel); err != nil {
			return fmt.Errorf("adding %s: %w", rel, err)
		}
	}
	if withMP4 {
		if err := add(mp4File); err != nil {
			return fmt.Errorf("adding %s: %w", mp4File, err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(dir, framesDir))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading the frames: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !ValidFrameName(e.Name()) {
			continue
		}
		if err := add(framesDir + "/" + e.Name()); err != nil {
			return fmt.Errorf("adding %s: %w", e.Name(), err)
		}
	}
	return zw.Close()
}

// ImportResult says what an import produced.
type ImportResult struct {
	ID      string
	Frames  int
	Bytes   int64
	Skipped int // entries the allowlist refused
}

/*
Import reads an archive written by Export and puts the recording back.

The id comes from the manifest inside the archive, never from a path in it, so
a tampered name cannot decide where anything lands. It is checked against the
same pattern the rest of the store uses.

The recording is built in a temporary directory and moved into place at the
end, so a failure half way through leaves no half-recording behind for the
library to list.
*/
func Import(s *Store, r io.ReaderAt, size int64, force bool) (ImportResult, error) {
	var out ImportResult

	zr, err := zip.NewReader(r, size)
	if err != nil {
		return out, fmt.Errorf("reading the archive: %w", err)
	}
	if len(zr.File) > maxImportFiles {
		return out, fmt.Errorf("the archive holds %d entries, more than the %d allowed",
			len(zr.File), maxImportFiles)
	}

	id, err := archiveID(zr)
	if err != nil {
		return out, err
	}
	out.ID = id

	dest := filepath.Join(s.Root(), id)
	if _, err := os.Stat(dest); err == nil && !force {
		return out, fmt.Errorf("%s is already here; pass --force to replace it", id)
	}

	if err := os.MkdirAll(s.Root(), 0o755); err != nil {
		return out, err
	}
	staging, err := os.MkdirTemp(s.Root(), ".import-*")
	if err != nil {
		return out, err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	if err := os.MkdirAll(filepath.Join(staging, framesDir), 0o755); err != nil {
		return out, err
	}

	for _, f := range zr.File {
		rel, ok := archiveMember(f.Name)
		if !ok {
			out.Skipped++
			continue
		}
		n, err := extract(f, filepath.Join(staging, filepath.FromSlash(rel)))
		if err != nil {
			return out, fmt.Errorf("extracting %s: %w", rel, err)
		}
		out.Bytes += n
		if out.Bytes > maxImportBytes {
			return out, fmt.Errorf("the archive expands past the %d byte limit", maxImportBytes)
		}
		if strings.HasPrefix(rel, framesDir+"/") {
			out.Frames++
		}
	}

	if _, err := os.Stat(filepath.Join(staging, manifestFile)); err != nil {
		return out, fmt.Errorf("the archive has no %s", manifestFile)
	}

	if force {
		if err := os.RemoveAll(dest); err != nil {
			return out, err
		}
	}
	if err := os.Rename(staging, dest); err != nil {
		return out, fmt.Errorf("putting the recording in place: %w", err)
	}
	return out, nil
}

// archiveID reads the manifest out of the archive and returns the id it claims.
func archiveID(zr *zip.Reader) (string, error) {
	for _, f := range zr.File {
		rel, ok := archiveMember(f.Name)
		if !ok || rel != manifestFile {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("opening %s: %w", manifestFile, err)
		}
		defer func() { _ = rc.Close() }()

		var m Manifest
		if err := json.NewDecoder(io.LimitReader(rc, 1<<28)).Decode(&m); err != nil {
			return "", fmt.Errorf("reading %s: %w", manifestFile, err)
		}
		if !ValidID(m.ID) {
			return "", fmt.Errorf("the manifest claims the id %q, which is not one", m.ID)
		}
		return m.ID, nil
	}
	return "", fmt.Errorf("the archive has no %s", manifestFile)
}

func extract(f *zip.File, dest string) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer func() { _ = rc.Close() }()

	w, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer func() { _ = w.Close() }()

	// Limited rather than trusting the header: a zip states its uncompressed
	// size, and a hostile one states it wrongly.
	n, err := io.Copy(w, io.LimitReader(rc, maxImportBytes))
	if err != nil {
		return n, err
	}
	return n, w.Close()
}
