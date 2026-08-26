package testscript

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/pkg/behavior"
)

const fixtureHTML = `<!doctype html><html><head><title>Fixture</title></head><body>
<h1 id="heading">Welcome back</h1>
<form>
  <input id="username" name="username" type="text" placeholder="Username">
  <input id="password" name="password" type="password" placeholder="Password">
  <button id="submit" type="button">Sign in</button>
</form>
<div id="status">idle</div>
<script>
  document.getElementById('submit').addEventListener('click', function () {
    document.getElementById('status').textContent = 'signed in';
  });
</script>
</body></html>`

// One browser for the whole package. Launching one per test is expensive
// enough to destabilise other packages when `go test ./...` runs them in
// parallel.
var (
	testBrowser *browser.Browser
	fixtureURL  string
)

func TestMain(m *testing.M) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, fixtureHTML)
	}))
	fixtureURL = ts.URL

	b, err := browser.New(config.BrowserConfig{Headless: true, NoSandbox: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "browser.New: %v\n", err)
		os.Exit(1)
	}
	if err := b.Launch(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "launch: %v\n", err)
		os.Exit(1)
	}
	if err := b.NewPage(context.Background(), fixtureURL); err != nil {
		fmt.Fprintf(os.Stderr, "new page: %v\n", err)
		os.Exit(1)
	}
	testBrowser = b

	code := m.Run()

	b.Close()
	ts.Close()
	os.Exit(code)
}

// run executes a script against the fixture, freshly loaded.
func run(t *testing.T, source string) *Result {
	t.Helper()

	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatalf("navigate to fixture: %v", err)
	}

	res, err := Run(context.Background(), Options{
		Browser: testBrowser,
		Source:  source,
		Name:    t.Name() + ".js",
		BaseURL: fixtureURL,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run returned an error (it should report failures in Result): %v", err)
	}
	return res
}

func TestPassingScript(t *testing.T) {
	res := run(t, `
		atr.step(1, "Sign in", () => {
			atr.fill("#username", "testuser");
			atr.click("#submit");
		});
		atr.step(2, "Verify the status updates", () => {
			expect(atr.text("#status")).toBe("signed in");
		});
	`)

	if !res.Passed {
		t.Fatalf("expected pass, got failure: %v", res.Failure)
	}
	if len(res.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(res.Steps))
	}
	for _, s := range res.Steps {
		if s.Status != behavior.StepStatusPassed {
			t.Errorf("step %d status = %s, want passed", s.Number, s.Status)
		}
	}
}

// The three classifications the whole design rests on. Getting any of these
// wrong means the agent takes the opposite of the correct action — most
// destructively, "repairing" a genuine regression away.
func TestFailureClassification(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		wantKind    FailureKind
		wantRepair  bool
		wantRetry   bool
		wantTestFai bool
	}{
		{
			name: "assertion failure means the app is wrong",
			source: `atr.step(1, "Check the heading", () => {
				expect(atr.text("#heading")).toBe("Something else");
			});`,
			wantKind:    KindAssertion,
			wantRepair:  false,
			wantRetry:   false,
			wantTestFai: true,
		},
		{
			name: "missing element means the UI moved",
			source: `atr.step(1, "Click a button that is gone", () => {
				atr.click("#no-such-button-anywhere");
			});`,
			wantKind:   KindNotFound,
			wantRepair: true,
			wantRetry:  false,
		},
		{
			name: "waitFor timeout is transient until proven otherwise",
			source: `atr.step(1, "Wait for something that never appears", () => {
				atr.waitFor("#never", {timeout: 300});
			});`,
			wantKind:   KindTimeout,
			wantRepair: false,
			wantRetry:  true,
		},
		{
			name: "a bad API call is the generated script's fault",
			source: `atr.step(1, "Call something that does not exist", () => {
				atr.thisMethodDoesNotExist();
			});`,
			wantKind:   KindScript,
			wantRepair: true,
			wantRetry:  false,
		},
		{
			name:       "a syntax error is the generated script's fault",
			source:     `atr.step(1, "Broken", () => { this is not javascript });`,
			wantKind:   KindScript,
			wantRepair: true,
			wantRetry:  false,
		},
		{
			name: "atr.fail is an explicit assertion failure",
			source: `atr.step(1, "Give up", () => {
				atr.fail("the cart total did not match the line items");
			});`,
			wantKind:    KindAssertion,
			wantRepair:  false,
			wantTestFai: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, tt.source)

			if res.Passed {
				t.Fatal("expected the script to fail")
			}
			if res.Failure == nil {
				t.Fatal("failure not populated")
			}
			if res.Failure.Kind != tt.wantKind {
				t.Errorf("kind = %q, want %q (message: %s)",
					res.Failure.Kind, tt.wantKind, res.Failure.Message)
			}
			if got := res.Failure.Kind.Repairable(); got != tt.wantRepair {
				t.Errorf("Repairable() = %v, want %v", got, tt.wantRepair)
			}
			if got := res.Failure.Kind.Retryable(); got != tt.wantRetry {
				t.Errorf("Retryable() = %v, want %v", got, tt.wantRetry)
			}
			if got := res.Failure.Kind.IsTestFailure(); got != tt.wantTestFai {
				t.Errorf("IsTestFailure() = %v, want %v", got, tt.wantTestFai)
			}
		})
	}
}

// A repair needs to know which element to re-derive, so the failing target
// has to survive into the Failure.
func TestFailureCarriesStepAndTarget(t *testing.T) {
	res := run(t, `
		atr.step(1, "Open the page", () => { atr.navigate(atr.base); });
		atr.step(7, "Click the checkout button", () => { atr.click("#checkout-gone"); });
	`)

	if res.Failure == nil {
		t.Fatal("expected a failure")
	}
	if res.Failure.Step != 7 {
		t.Errorf("Step = %d, want 7", res.Failure.Step)
	}
	if !strings.Contains(res.Failure.StepDesc, "checkout") {
		t.Errorf("StepDesc = %q, should carry the step's intent", res.Failure.StepDesc)
	}
	if res.Failure.Target != "#checkout-gone" {
		t.Errorf("Target = %q, want the selector that could not be found", res.Failure.Target)
	}
}

// exists() is the one read where absence is an answer rather than a fault,
// so that scripts can branch on optional page furniture.
func TestExistsDoesNotThrow(t *testing.T) {
	res := run(t, `
		atr.step(1, "Branch on optional UI", () => {
			if (atr.exists("#cookie-banner")) { atr.click("#accept"); }
			expect(atr.exists("#heading")).toBeTruthy();
			expect(atr.exists("#definitely-not-here")).toBeFalsy();
		});
	`)

	if !res.Passed {
		t.Fatalf("expected pass, got: %v", res.Failure)
	}
}

// retry() must retry transient kinds and refuse to retry an assertion —
// re-running a failing assertion just fails slower and hides nothing.
func TestRetryOnlyRepeatsTransientFailures(t *testing.T) {
	res := run(t, `
		atr.step(1, "Retry an assertion", () => {
			atr.retry(3, () => { expect(1).toBe(2); });
		});
	`)
	if res.Failure == nil || res.Failure.Kind != KindAssertion {
		t.Fatalf("expected an assertion failure, got %v", res.Failure)
	}
	for _, line := range res.Logs {
		if strings.Contains(line, "retrying") {
			t.Error("an assertion failure must not be retried")
		}
	}

	res = run(t, `
		atr.step(1, "Retry a timeout", () => {
			atr.retry(2, () => { atr.waitFor("#never", {timeout: 200}); });
		});
	`)
	if res.Failure == nil || res.Failure.Kind != KindTimeout {
		t.Fatalf("expected a timeout failure, got %v", res.Failure)
	}
	var retried bool
	for _, line := range res.Logs {
		if strings.Contains(line, "retrying") {
			retried = true
		}
	}
	if !retried {
		t.Error("a timeout should have been retried")
	}
}

// goja does not preempt, so without an interrupt a runaway script would
// ignore the deadline and hang the whole suite.
func TestRunawayScriptIsInterrupted(t *testing.T) {
	start := time.Now()
	res, err := Run(context.Background(), Options{
		Browser: testBrowser,
		Source:  `atr.step(1, "Spin forever", () => { while (true) {} });`,
		Name:    "runaway.js",
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("interrupt did not fire, took %s", elapsed)
	}
	if res.Passed {
		t.Fatal("a runaway script must not pass")
	}
	if res.Failure.Kind != KindTimeout {
		t.Errorf("kind = %q, want %q", res.Failure.Kind, KindTimeout)
	}
}

// A script with no secret backend must say so rather than fill nothing and
// report a pass it did not earn.
func TestFillSecretWithoutBackendFails(t *testing.T) {
	res := run(t, `
		atr.step(1, "Fill a password", () => {
			atr.fillSecret("#password", {ref: "app/test"});
		});
	`)
	if res.Passed {
		t.Fatal("expected a failure when no secret backend is configured")
	}
	if res.Failure.Kind != KindEnvironment {
		t.Errorf("kind = %q, want %q", res.Failure.Kind, KindEnvironment)
	}
}

func TestSecretFillerIsUsed(t *testing.T) {
	var gotTarget, gotRef string
	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Options{
		Browser: testBrowser,
		Source:  `atr.step(1, "Fill a password", () => { atr.fillSecret("#password", {ref: "app/test"}); });`,
		BaseURL: fixtureURL,
		Timeout: 30 * time.Second,
		SecretFiller: func(ctx context.Context, target, ref, command string) error {
			gotTarget, gotRef = target, ref
			return testBrowser.Fill(ctx, target, "from-the-vault")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Fatalf("expected pass, got %v", res.Failure)
	}
	if gotTarget != "#password" || gotRef != "app/test" {
		t.Errorf("filler got target=%q ref=%q", gotTarget, gotRef)
	}
}
