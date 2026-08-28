package testscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpec(t *testing.T, spec string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.test.txt")
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A compile used to be trusted the moment the model stopped talking: the
// script was stamped before it had ever executed, so one that could not run
// was reported fresh and replayed for ever instead of being recompiled.
func TestADraftIsNotFreshUntilItHasRun(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)

	if _, err := SaveDraft(specPath, spec, "atr.step(1, 'x', () => {});"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	stored, err := Load(specPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !stored.Unverified {
		t.Error("a draft is not marked unverified")
	}
	if stored.Fresh(spec) {
		t.Error("a draft is reported fresh, so a script that may not run would be replayed")
	}
	if stored.SpecHash == "" {
		t.Error("a draft has no spec hash, so it cannot be told apart from a hand-written script")
	}

	if err := Stamp(specPath); err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	stored, err = Load(specPath)
	if err != nil {
		t.Fatalf("Load after Stamp: %v", err)
	}
	if stored.Unverified {
		t.Error("still unverified after stamping")
	}
	if !stored.Fresh(spec) {
		t.Error("a stamped script is not fresh, so it would be recompiled every run")
	}
}

// Removing the hash line is the documented way to mark a script hand-written.
// The unverified state must not be confused with it.
func TestHandWrittenAndUnverifiedAreDifferentStates(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)

	if _, err := SaveDraft(specPath, spec, "atr.step(1, 'x', () => {});"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(ScriptPath(specPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), specHeader) {
		t.Error("a draft dropped the hash line, which is how a human marks a script hand-written")
	}
}

// Stamping twice, or stamping something already stamped, must be harmless.
func TestStampIsIdempotent(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)

	if _, err := Save(specPath, spec, "atr.step(1, 'x', () => {});"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Stamp(specPath); err != nil {
			t.Fatalf("Stamp %d: %v", i, err)
		}
	}
	stored, err := Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Fresh(spec) {
		t.Error("stamping an already-stamped script broke it")
	}
}

// The script is committed source, so a half-written file is both a broken test
// and a confusing diff.
func TestWritesAreAtomic(t *testing.T) {
	const spec = "Test: sample\n\nSteps:\n1. Do the thing\n"
	specPath := writeSpec(t, spec)

	if _, err := Save(specPath, spec, "atr.step(1, 'x', () => {});"); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Dir(specPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("a temporary file was left behind: %s", e.Name())
		}
	}
}
