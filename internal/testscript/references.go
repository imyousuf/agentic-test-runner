package testscript

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// valueCall matches a literal key passed to one of the values accessors.
//
// The trailing [,)] is what makes this safe: it requires the literal to be the
// whole argument. Without it, values.get("prefix_" + row) would match its
// first fragment and the key would be reported as "prefix_" — and then as
// unreferenced, and then deleted.
var valueCall = regexp.MustCompile(`values\.(?:get|int|bool|has)\(\s*(?:"([^"]*)"|'([^']*)')\s*[,)]`)

// valueCallAny matches any call to a values accessor, literal or not.
var valueCallAny = regexp.MustCompile(`values\.(?:get|int|bool|has)\(`)

// alwaysUsed are keys nothing in the script reads, because Go reads them.
//
// base_url is resolved before the script runs, to point the browser. A scan
// that did not know that would call it unused on every test there is.
var alwaysUsed = map[string]bool{"base_url": true}

// ReferencedKeys returns the value keys a compiled script reads.
//
// The second result is false when the script calls a values accessor with
// something other than a string literal. The answer is then not knowable by
// reading the source, and a caller must not draw conclusions from a partial
// list — deleting a key a test actually reads turns into a missing-input
// failure on somebody else's machine.
func ReferencedKeys(script string) (keys []string, exact bool) {
	literal := valueCall.FindAllStringSubmatch(script, -1)
	if len(literal) != len(valueCallAny.FindAllString(script, -1)) {
		return nil, false
	}

	seen := map[string]bool{}
	for _, m := range literal {
		key := m[1]
		if key == "" {
			key = m[2]
		}
		if key == "" {
			return nil, false
		}
		seen[key] = true
	}

	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, true
}

// UnreferencedKeys returns the keys defined in a spec's committed properties
// file that neither its compiled script nor the shared library beside it
// reads.
//
// Deliberately reads that one file rather than the merged view: Values.Keys()
// folds in the gitignored override and every ATR_VALUE_* variable, so a
// committed default would look unused merely because CI set the same key in
// the environment.
//
// The library has to be scanned too, and this is not a nicety. A key read only
// inside _shared.js would otherwise be reported unused on every run and then
// deleted from the committed file, after which every test in the directory
// fails with a missing input. The exactness valve does not save it either: the
// non-literal call sits in the library, the script's own scan stays exact, and
// the wrong answer is delivered confidently.
func UnreferencedKeys(specPath string) ([]string, error) {
	stored, err := Load(specPath)
	if err != nil || stored == nil {
		return nil, err
	}

	referenced, exact := ReferencedKeys(stored.Source)
	if !exact {
		// The script builds a key at run time; nothing can be said safely.
		return nil, nil
	}

	library, err := LoadLibrary(specPath)
	if err != nil {
		return nil, err
	}
	if library != nil {
		fromLibrary, libExact := ReferencedKeys(library.Source)
		if !libExact {
			return nil, nil
		}
		referenced = append(referenced, fromLibrary...)
	}

	defined, err := committedValues(specPath)
	if err != nil || defined == nil {
		return nil, err
	}

	used := map[string]bool{}
	for _, k := range referenced {
		used[k] = true
	}

	var unused []string
	for key := range defined {
		if used[key] || alwaysUsed[key] {
			continue
		}
		unused = append(unused, key)
	}
	sort.Strings(unused)
	return unused, nil
}

// PruneValues removes the named keys from a spec's committed properties file,
// returning the ones it removed.
//
// Line-wise, leaving every other byte alone. Rewriting through
// FormatProperties would sort the file and discard every comment in it, which
// is not an acceptable thing to do silently to committed source.
func PruneValues(specPath string, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	path := ValuesPath(specPath)

	data, err := readIfExists(path)
	if err != nil || data == "" {
		return nil, err
	}

	drop := map[string]bool{}
	for _, k := range keys {
		if !alwaysUsed[k] {
			drop[k] = true
		}
	}

	var kept []string
	var removed []string
	for _, line := range strings.Split(data, "\n") {
		if key, _, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			if name := strings.TrimSpace(key); drop[name] {
				removed = append(removed, name)
				continue
			}
		}
		kept = append(kept, line)
	}
	if len(removed) == 0 {
		return nil, nil
	}

	if err := writeFileAtomic(path, strings.Join(kept, "\n")); err != nil {
		return nil, err
	}
	sort.Strings(removed)
	return removed, nil
}

// committedValues parses only the committed properties file for a spec.
func committedValues(specPath string) (map[string]string, error) {
	data, err := readIfExists(ValuesPath(specPath))
	if err != nil || data == "" {
		return nil, err
	}
	return ParseProperties(data)
}

// readIfExists returns a file's contents, or "" when it is not there.
func readIfExists(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), nil
}
