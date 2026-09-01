package remote

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// "atr remote setup --check" is documented to change nothing, so the lookup it
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

// os.WriteFile only applies its mode when it creates the file, so an upgrade
// over a token file an earlier version left world-readable has to be tightened
// explicitly.
func TestWriteSecretTightensAnExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX mode bits")
	}
	path := filepath.Join(t.TempDir(), "remote.env")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeSecret(path, []byte("ATR_REMOTE_TOKEN=x\n")); err != nil {
		t.Fatalf("writeSecret: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600 after rewriting a 0644 file", perm)
	}
}

// An unreadable token file must not be reported as absent, because the advised
// remedy would mint a replacement and invalidate the URL a service is serving.
func TestLookupTokenReportsAReadFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, _, err := EnsureToken(); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	path := filepath.Join(home, ".atr", "remote.env")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if _, _, found, err := LookupToken(); err == nil || found {
		t.Fatalf("expected a read error, got found=%v err=%v", found, err)
	}
}
