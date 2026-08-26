package testscript

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}
}

func TestExpandCommandSubstitution(t *testing.T) {
	skipOnWindows(t)

	v := NewValues(map[string]string{"password": "$(printf hunter2)"})
	got, ok, err := v.Resolve(context.Background(), "password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ok {
		t.Fatal("key not found")
	}
	if got != "hunter2" {
		t.Errorf("got %q, want %q", got, "hunter2")
	}
}

// The motivating case: read a credential from a file at run time so it is
// never written into the properties file.
func TestExpandReadsAFileAtRunTime(t *testing.T) {
	skipOnWindows(t)

	dir := t.TempDir()
	secretFile := filepath.Join(dir, "passwd.txt")
	if err := os.WriteFile(secretFile, []byte("s3cret-from-disk\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	v := NewValues(map[string]string{"password": "$(cat " + secretFile + ")"})
	got, _, err := v.Resolve(context.Background(), "password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// The trailing newline is an artefact of the file, not the password.
	if got != "s3cret-from-disk" {
		t.Errorf("got %q, want %q", got, "s3cret-from-disk")
	}
}

func TestExpandEnvironmentVariable(t *testing.T) {
	t.Setenv("ATR_TEST_HOST", "staging.example.com")

	v := NewValues(map[string]string{"base_url": "https://${ATR_TEST_HOST}/shop"})
	got, _, err := v.Resolve(context.Background(), "base_url")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "https://staging.example.com/shop" {
		t.Errorf("got %q", got)
	}
}

func TestExpandMixedWithLiteralText(t *testing.T) {
	skipOnWindows(t)

	v := NewValues(map[string]string{"greeting": "hello $(printf world), goodbye"})
	got, _, err := v.Resolve(context.Background(), "greeting")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "hello world, goodbye" {
		t.Errorf("got %q", got)
	}
}

// A value has to be able to contain a literal dollar sign.
func TestDoubleDollarIsALiteral(t *testing.T) {
	v := NewValues(map[string]string{"price": "$$(19.99)", "plain": "US$$5"})

	got, _, err := v.Resolve(context.Background(), "price")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "$(19.99)" {
		t.Errorf("price = %q, want %q", got, "$(19.99)")
	}

	got, _, err = v.Resolve(context.Background(), "plain")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "US$5" {
		t.Errorf("plain = %q, want %q", got, "US$5")
	}
}

func TestExpandHandlesNestedParentheses(t *testing.T) {
	skipOnWindows(t)

	v := NewValues(map[string]string{"x": "$(echo $(printf inner))"})
	got, _, err := v.Resolve(context.Background(), "x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "inner" {
		t.Errorf("got %q, want inner", got)
	}
}

// A failing command must be reported, and must not leak whatever it printed
// before failing.
func TestExpandFailureReportsStderrNotStdout(t *testing.T) {
	skipOnWindows(t)

	v := NewValues(map[string]string{
		"password": "$(printf hunter2; echo 'no such entry' >&2; exit 1)",
	})
	_, _, err := v.Resolve(context.Background(), "password")
	if err == nil {
		t.Fatal("expected an error from a failing command")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("stdout must not reach the error: %v", err)
	}
	// Nor the command itself, which often carries the secret or its location.
	if strings.Contains(err.Error(), "printf") {
		t.Errorf("the command text must not reach the error: %v", err)
	}
	if !strings.Contains(err.Error(), "no such entry") {
		t.Errorf("stderr should be surfaced: %v", err)
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("the error should name the key: %v", err)
	}
}

// A password manager must not be asked twice for the same value in one run.
func TestExpansionIsCachedPerKey(t *testing.T) {
	skipOnWindows(t)

	dir := t.TempDir()
	counter := filepath.Join(dir, "count")

	v := NewValues(map[string]string{
		"token": "$(printf x >> " + counter + "; printf value)",
	})

	for i := 0; i < 3; i++ {
		got, _, err := v.Resolve(context.Background(), "token")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got != "value" {
			t.Errorf("got %q", got)
		}
	}

	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("the command never ran: %v", err)
	}
	if len(data) != 1 {
		t.Errorf("command ran %d times, want 1 (expansion should be cached)", len(data))
	}
}

// Values with no substitution must not pay for a shell.
func TestPlainValuesAreNotExpanded(t *testing.T) {
	v := NewValues(map[string]string{"term": "widget"})
	got, _, err := v.Resolve(context.Background(), "term")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "widget" {
		t.Errorf("got %q", got)
	}
}

func TestUnterminatedSubstitutionIsAnError(t *testing.T) {
	v := NewValues(map[string]string{"x": "$(echo unterminated"})
	if _, _, err := v.Resolve(context.Background(), "x"); err == nil {
		t.Error("expected an error for an unterminated substitution")
	}
}

// Get must stay raw: listing keys should never execute anything.
func TestGetDoesNotExpand(t *testing.T) {
	v := NewValues(map[string]string{"password": "$(printf hunter2)"})
	raw, ok := v.Get("password")
	if !ok {
		t.Fatal("key not found")
	}
	if raw != "$(printf hunter2)" {
		t.Errorf("Get returned %q; it must not expand", raw)
	}
}
