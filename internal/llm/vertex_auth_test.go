package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// A machine that has never run `gcloud auth application-default login` used to
// get a Go stack trace out of the SDK — WithGoogleAuth panics rather than
// returning an error. ATR prints proper setup guidance for every other
// backend, and a panic is both unreadable and unrecoverable by the caller that
// wanted to fall back or report.
func TestMissingCredentialsAreAnErrorNotAPanic(t *testing.T) {
	withoutCredentials(t)

	client, err := newVertexClaudeClient(context.Background(), llm.Config{
		Provider: llm.ProviderVertexClaude,
		Model:    "claude-opus-5",
		Project:  "some-project",
	})

	if err == nil {
		client.Close()
		t.Skip("this machine has ambient Google credentials, so the failure path cannot be reached")
	}
	if client != nil {
		t.Error("a client was returned alongside the error")
	}

	// The message has to name the command that fixes it. "failed to find
	// default credentials" alone leaves the reader to guess.
	if !strings.Contains(err.Error(), "gcloud auth application-default login") {
		t.Errorf("the error does not say how to fix it: %v", err)
	}
	if !strings.Contains(err.Error(), "GOOGLE_APPLICATION_CREDENTIALS") {
		t.Errorf("the error does not mention the service-account route: %v", err)
	}
}

// The SDK panics on an empty region too. Nothing in ATR passes one — location
// is defaulted — but the recover has to be doing its job for that to stay
// merely theoretical.
func TestAnEmptyRegionIsAnErrorNotAPanic(t *testing.T) {
	withoutCredentials(t)

	_, err := vertexAuth(context.Background(), "", "some-project")
	if err == nil {
		t.Skip("this machine has ambient Google credentials")
	}
	if strings.Contains(err.Error(), "panic") {
		t.Errorf("the panic leaked into the message verbatim: %v", err)
	}
}

// withoutCredentials points every Application Default Credentials source at
// nothing, so the lookup fails the way it does on an unconfigured machine.
func withoutCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent/atr-test-credentials.json")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
	// Stops the lookup from asking the GCE metadata server, which on a
	// non-GCE machine is a slow way to reach the same answer.
	t.Setenv("GCE_METADATA_HOST", "127.0.0.1:1")
}
