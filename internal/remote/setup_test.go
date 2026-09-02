package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// The service used to carry a token, in an EnvironmentFile on Linux and in the
// plist on macOS. Nothing authenticates any more, so neither should mention
// one -- a stale ATR_REMOTE_TOKEN in a unit file would be a credential that
// looks live and is not.
func TestTheServiceCarriesNoToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, serviceName+".service")
	result := &SetupResult{}
	if err := setupSystemd(path, "/usr/local/bin/atr",
		SetupOptions{Port: 7788, Bind: "127.0.0.1", FPS: 20}, result); err != nil {
		// systemctl is absent in a test container; the unit is written first.
		t.Logf("setupSystemd reported %v (the unit is what matters here)", err)
	}
	unit, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no unit written: %v", err)
	}
	for _, bad := range []string{"TOKEN", "EnvironmentFile"} {
		if strings.Contains(string(unit), bad) {
			t.Errorf("the unit still mentions %q:\n%s", bad, unit)
		}
	}
}
