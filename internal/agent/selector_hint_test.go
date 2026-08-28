package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
)

// The agent loop hands a tool's error text straight back to the model, so the
// text is the only teaching surface there is. Without it the model retried
// :has-text() until the iteration ceiling, because nothing ever told it the
// target could not parse.
func TestSelectorHintExplainsAnUnparseableTarget(t *testing.T) {
	err := fmt.Errorf("looking for #a[[[bad: %w", browser.ErrInvalidSelector)

	hint := selectorHint(err)
	if hint == "" {
		t.Fatal("no hint for a selector the browser could not parse")
	}
	for _, want := range []string{":has-text(", "text=", "not supported"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint does not mention %q:\n%s", want, hint)
		}
	}
}

// Every other failure is about the page, not the target, and a selector
// lecture there would be noise the model has to read on each retry.
func TestSelectorHintStaysQuietForOtherFailures(t *testing.T) {
	for _, err := range []error{
		nil,
		errors.New("context deadline exceeded"),
		fmt.Errorf("looking for #gone: %w", browser.ErrElementNotFound),
	} {
		if got := selectorHint(err); got != "" {
			t.Errorf("selectorHint(%v) = %q, want empty", err, got)
		}
	}
}
