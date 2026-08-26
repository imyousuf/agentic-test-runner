package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/internal/secret"
)

const loginPage = `<!doctype html><html><body>
<form>
  <input id="username" name="username" type="text" placeholder="Username">
  <input id="password" name="password" type="password" placeholder="Password">
</form>
</body></html>`

// One browser is shared by every test in this file, and it is only launched if
// a test actually needs it.
//
// Launching one per test is what the straightforward version does, and it is
// too expensive to get away with: `go test ./...` runs packages in parallel,
// so those Chromium instances come up alongside the ones internal/browser and
// internal/api are already running. The contention is enough to stall CDP
// calls in the other packages badly enough to trip a latent deadlock there.
var (
	sharedOnce    sync.Once
	sharedBrowser *browser.Browser
	sharedServer  *httptest.Server
	sharedErr     error
)

func TestMain(m *testing.M) {
	code := m.Run()

	if sharedBrowser != nil {
		sharedBrowser.Close()
	}
	if sharedServer != nil {
		sharedServer.Close()
	}
	os.Exit(code)
}

// secretFillFixture returns the tool under test, wired to the shared browser
// on a freshly reloaded login form.
func secretFillFixture(t *testing.T, cfg secret.Config) (*BrowserFillSecretTool, *browser.Browser) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell command")
	}
	if os.Getenv("ATR_SKIP_BROWSER_TESTS") != "" {
		t.Skip("browser tests disabled")
	}

	sharedOnce.Do(func() {
		sharedServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, loginPage)
		}))

		b, err := browser.New(config.BrowserConfig{Headless: true, NoSandbox: true})
		if err != nil {
			sharedErr = fmt.Errorf("browser.New: %w", err)
			return
		}
		if err := b.Launch(context.Background()); err != nil {
			sharedErr = fmt.Errorf("launch: %w", err)
			return
		}
		if err := b.NewPage(context.Background(), sharedServer.URL); err != nil {
			sharedErr = fmt.Errorf("new page: %w", err)
			return
		}
		sharedBrowser = b
	})
	if sharedErr != nil {
		t.Fatalf("shared browser unavailable: %v", sharedErr)
	}

	// Reload so each test starts from an empty form.
	if err := sharedBrowser.Navigate(context.Background(), sharedServer.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	return NewBrowserFillSecretTool(sharedBrowser, secret.New(cfg)), sharedBrowser
}

// fieldValue reads back what actually landed in the input.
func fieldValue(t *testing.T, b *browser.Browser, selector string) string {
	t.Helper()
	res, err := b.Evaluate(`document.querySelector("` + selector + `").value`)
	if err != nil {
		t.Fatalf("reading %s: %v", selector, err)
	}
	value, _ := res.(string)
	return value
}

// The whole point of the tool: the field receives the secret, and the string
// handed back to the model does not contain it. If this regresses, every
// password the agent fills is written into the conversation history and
// re-sent to the model provider on every later turn.
func TestFillSecretFillsFieldWithoutDisclosingValue(t *testing.T) {
	const password = "correct-horse-battery-staple"

	tool, b := secretFillFixture(t, secret.Config{
		Refs: map[string]string{"github/password": "echo " + password},
	})

	result, isErr := tool.Execute(context.Background(), map[string]any{
		"target": "#password",
		"ref":    "github/password",
	})
	if isErr {
		t.Fatalf("Execute reported an error: %s", result)
	}

	if got := fieldValue(t, b, "#password"); got != password {
		t.Errorf("field holds %q, want the secret", got)
	}
	if strings.Contains(result, password) {
		t.Fatalf("SECRET LEAKED into the tool result: %q", result)
	}
	if !strings.Contains(result, "#password") {
		t.Errorf("result should name the field that was filled, got %q", result)
	}
}

func TestFillSecretAcceptsACommandDirectly(t *testing.T) {
	const token = "ghp-not-a-real-token"

	tool, b := secretFillFixture(t, secret.Config{})

	result, isErr := tool.Execute(context.Background(), map[string]any{
		"target":  "#password",
		"command": "echo " + token,
	})
	if isErr {
		t.Fatalf("Execute reported an error: %s", result)
	}

	if got := fieldValue(t, b, "#password"); got != token {
		t.Errorf("field holds %q, want the secret", got)
	}
	if strings.Contains(result, token) {
		t.Fatalf("SECRET LEAKED into the tool result: %q", result)
	}
}

// A failing manager must produce an actionable error, and still not leak
// whatever it managed to print before failing.
func TestFillSecretReportsCommandFailureWithoutLeakingStdout(t *testing.T) {
	tool, b := secretFillFixture(t, secret.Config{})

	result, isErr := tool.Execute(context.Background(), map[string]any{
		"target":  "#password",
		"command": "echo PARTIAL_SECRET; echo 'no such entry' >&2; exit 1",
	})
	if !isErr {
		t.Fatal("want an error result from a failing command")
	}
	if strings.Contains(result, "PARTIAL_SECRET") {
		t.Errorf("stdout leaked on failure: %q", result)
	}
	if !strings.Contains(result, "no such entry") {
		t.Errorf("stderr should reach the model so it can correct the entry name, got %q", result)
	}
	if got := fieldValue(t, b, "#password"); got != "" {
		t.Errorf("field should be untouched after a failed fetch, got %q", got)
	}
}

func TestFillSecretRequiresATarget(t *testing.T) {
	tool, _ := secretFillFixture(t, secret.Config{})

	result, isErr := tool.Execute(context.Background(), map[string]any{
		"command": "echo whatever",
	})
	if !isErr {
		t.Fatalf("want an error when target is missing, got %q", result)
	}
}

// The tool description is what steers the model away from the leaky path, so
// it has to actually say so.
func TestFillSecretDescriptionWarnsAgainstTheShellPath(t *testing.T) {
	tool := NewBrowserFillSecretTool(nil, secret.New(secret.Config{
		Refs: map[string]string{"github/password": "echo x"},
	}))

	desc := tool.Description()
	if !strings.Contains(desc, "browser_fill") {
		t.Error("description should warn against fetching a secret and passing it to browser_fill")
	}
	if !strings.Contains(desc, "github/password") {
		t.Error("description should list configured refs so the model knows what is available")
	}
}
