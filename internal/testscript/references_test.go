package testscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferencedKeys(t *testing.T) {
	script := `
		atr.step(1, "x", () => {
			atr.fill("#q", values.get("search_term"));
			expect(atr.text("#n")).toBe(values.int('expected_count'));
			if (values.bool("skip_intro")) { return; }
			if (values.has("promo")) { atr.click("#promo"); }
		});`

	keys, exact := ReferencedKeys(script)
	if !exact {
		t.Fatal("every key here is a literal; the scan should be exact")
	}
	want := []string{"expected_count", "promo", "search_term", "skip_intro"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}

// A key built at run time cannot be found by reading the source. Reporting a
// partial list would invite deleting a key the test actually reads, which
// fails on somebody else's machine as a missing input.
func TestAComputedKeyMakesTheScanInconclusive(t *testing.T) {
	for _, script := range []string{
		`values.get("prefix_" + row)`,
		"values.get(`tpl_${i}`)",
		`values.get(keyName)`,
	} {
		if _, exact := ReferencedKeys(script); exact {
			t.Errorf("scan claimed to be exact for %q", script)
		}
	}
}

func specWithValues(t *testing.T, script, properties string) string {
	t.Helper()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "sample.test.txt")
	const spec = "Test: sample\n\nSteps:\n1. Do it\n"
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(specPath, spec, script, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ValuesPath(specPath), []byte(properties), 0o644); err != nil {
		t.Fatal(err)
	}
	return specPath
}

// Repeated compiles accumulate aliases: one real spec ended up with seven keys
// for two inputs.
func TestUnreferencedKeysAreFound(t *testing.T) {
	specPath := specWithValues(t,
		`atr.fill("#q", values.get("search_term"));`,
		"# a comment\nbase_url=http://localhost:1\nsearch_term=widget\nold_alias=widget\nlegacy_sku=X1\n")

	unused, err := UnreferencedKeys(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(unused, ",") != "legacy_sku,old_alias" {
		t.Errorf("unused = %v, want [legacy_sku old_alias]", unused)
	}
}

// base_url is read by Go before the script starts, so no script mentions it.
func TestBaseURLIsNeverCalledUnused(t *testing.T) {
	specPath := specWithValues(t,
		`atr.fill("#q", values.get("search_term"));`,
		"base_url=http://localhost:1\nsearch_term=widget\n")

	unused, err := UnreferencedKeys(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(unused) != 0 {
		t.Errorf("unused = %v, want none", unused)
	}
}

// Pruning is line-wise. A rewrite would sort the file and drop every comment,
// which is not something to do silently to committed source.
func TestPruneKeepsCommentsAndOrder(t *testing.T) {
	specPath := specWithValues(t,
		`atr.fill("#q", values.get("search_term"));`,
		"# keep me\nbase_url=http://localhost:1\nsearch_term=widget\n\n# and me\nold_alias=widget\n")

	removed, err := PruneValues(specPath, []string{"old_alias"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(removed, ",") != "old_alias" {
		t.Fatalf("removed = %v", removed)
	}

	body, err := os.ReadFile(ValuesPath(specPath))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, want := range []string{"# keep me", "# and me", "search_term=widget", "base_url="} {
		if !strings.Contains(got, want) {
			t.Errorf("pruning removed %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old_alias") {
		t.Errorf("old_alias survived:\n%s", got)
	}
}

// Even asked directly, base_url is not removable: nothing in the script can
// ever reference it.
func TestPruneRefusesBaseURL(t *testing.T) {
	specPath := specWithValues(t,
		`atr.fill("#q", values.get("search_term"));`,
		"base_url=http://localhost:1\nsearch_term=widget\n")

	removed, err := PruneValues(specPath, []string{"base_url"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
}

// An inconclusive scan must report nothing rather than a partial list.
func TestNothingIsReportedWhenTheScanIsInconclusive(t *testing.T) {
	specPath := specWithValues(t,
		`atr.fill("#q", values.get("prefix_" + row));`,
		"base_url=http://localhost:1\nsearch_term=widget\nold_alias=x\n")

	unused, err := UnreferencedKeys(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(unused) != 0 {
		t.Errorf("unused = %v, want none: the script builds keys at run time", unused)
	}
}

// A value continues across lines with a trailing backslash. Dropping only the
// first line leaves the continuation behind as a line that is not key=value,
// and the whole file stops parsing on every later run — so tidying an unused
// key would break every test in the directory.
func TestPruningKeepsAContinuedValueWhole(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "sample.test.txt")

	const props = "base_url = http://localhost\n" +
		"search_term = hello \\\n" +
		"  world\n" +
		"keep = 1\n"
	if err := os.WriteFile(ValuesPath(specPath), []byte(props), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := PruneValues(specPath, []string{"search_term"})
	if err != nil {
		t.Fatalf("PruneValues: %v", err)
	}
	if len(removed) != 1 || removed[0] != "search_term" {
		t.Fatalf("removed = %v", removed)
	}

	after, err := os.ReadFile(ValuesPath(specPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "world") {
		t.Errorf("the continuation line was orphaned:\n%s", after)
	}
	parsed, err := ParseProperties(string(after))
	if err != nil {
		t.Fatalf("the pruned file no longer parses: %v\n%s", err, after)
	}
	if _, ok := parsed["keep"]; !ok {
		t.Error("pruning took a key it was not asked for")
	}
}

// ':' separates a property as well as '='. A scan for '=' alone never removed
// such a key, so it was reported unused on every run and silently left there.
func TestPruningHandlesAColonSeparator(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "sample.test.txt")

	const props = "base_url: http://localhost\nsearch_term: hello\n"
	if err := os.WriteFile(ValuesPath(specPath), []byte(props), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := PruneValues(specPath, []string{"search_term"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "search_term" {
		t.Fatalf("removed = %v, want the colon-separated key", removed)
	}

	after, err := os.ReadFile(ValuesPath(specPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "search_term") {
		t.Errorf("the key survived:\n%s", after)
	}
	// base_url is exempt and must be untouched whatever separator it uses.
	if !strings.Contains(string(after), "base_url") {
		t.Errorf("base_url was pruned:\n%s", after)
	}
}

// Comments and blank lines are why this is line-wise rather than a rewrite,
// so they have to survive a prune that happens to sit among them.
func TestPruningKeepsCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "sample.test.txt")

	const props = "# what this suite needs\n\n" +
		"base_url = http://localhost\n\n" +
		"# left over from an earlier compile\n" +
		"stale = x\n\n" +
		"keep = 1\n"
	if err := os.WriteFile(ValuesPath(specPath), []byte(props), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := PruneValues(specPath, []string{"stale"}); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(ValuesPath(specPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# what this suite needs", "# left over from an earlier compile", "keep = 1"} {
		if !strings.Contains(string(after), want) {
			t.Errorf("pruning removed %q:\n%s", want, after)
		}
	}
	if strings.Contains(string(after), "stale") {
		t.Errorf("the key survived:\n%s", after)
	}
}
