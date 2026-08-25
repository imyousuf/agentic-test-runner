package rdp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// "atr rdp setup --check" is documented to change nothing, so the lookup it
// uses must not create a directory or mint a token.
func TestLookupTokenWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	token, path, found, err := LookupToken()
	if err != nil {
		t.Fatalf("LookupToken: %v", err)
	}
	if found || token != "" {
		t.Fatalf("expected no token, got %q", token)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("%s must not be created by a lookup", filepath.Dir(path))
	}
}

func TestLookupTokenReadsWhatEnsureTokenWrote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	want, path, err := EnsureToken()
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	// The file holds a secret and must stay owner-only. Windows has no POSIX
	// mode bits -- Perm() reports 0666 for any writable file -- so the
	// assertion only means something on Unix.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("token file mode = %o, want 600", perm)
		}
	}

	got, _, found, err := LookupToken()
	if err != nil || !found {
		t.Fatalf("LookupToken after EnsureToken: found=%v err=%v", found, err)
	}
	if got != want {
		t.Fatalf("token = %q, want %q", got, want)
	}
}

// A home directory containing XML metacharacters must not produce a plist that
// launchctl cannot parse.
func TestXMLEscapeProtectsThePlist(t *testing.T) {
	got := xmlEscape(`/Users/a&b/<tools>/atr`)
	for _, bad := range []string{"&b", "<tools>"} {
		if strings.Contains(got, bad) {
			t.Fatalf("xmlEscape left %q unescaped: %s", bad, got)
		}
	}
	if !strings.Contains(got, "&amp;") || !strings.Contains(got, "&lt;") {
		t.Fatalf("xmlEscape produced %s", got)
	}
}
