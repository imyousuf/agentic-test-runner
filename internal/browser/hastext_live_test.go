package browser

import (
	"context"
	"errors"
	"testing"
)

// The compiler emits :has-text() unprompted, and querySelector rejects it. It
// resolves now, against a real page.
func TestHasTextResolvesAgainstThePage(t *testing.T) {
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/has_text.html"); err != nil {
		t.Fatal(err)
	}

	// Two buttons exist; only one carries this text.
	if err := testBrowser.Click(context.Background(), `button:has-text("Sign in")`, false); err != nil {
		t.Fatalf(`clicking button:has-text("Sign in"): %v`, err)
	}

	// It reads, too — atr.text goes through a different sink.
	got, err := testBrowser.GetTextContent(`span:has-text("9 items")`, "flat")
	if err != nil {
		t.Fatalf("reading through has-text: %v", err)
	}
	if len(got.Groups) == 0 || got.Groups[0].Text == "" {
		t.Fatalf("got %+v, want the matching span's text", got)
	}
	if want := "Total: 9 items"; got.Groups[0].Text != want {
		t.Errorf("text = %q, want %q — it matched the wrong row", got.Groups[0].Text, want)
	}
}

// Text that matches nothing is a missing element, not a broken selector.
func TestHasTextThatMatchesNothingIsNotFound(t *testing.T) {
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/has_text.html"); err != nil {
		t.Fatal(err)
	}

	err := testBrowser.Click(context.Background(), `button:has-text("No Such Label")`, false)
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !errors.Is(err, ErrElementNotFound) {
		t.Errorf("err = %v, want ErrElementNotFound", err)
	}
}

// A selector the browser cannot parse is a script defect: repairable, and
// above all not retryable. Retrying one is how a compile burned its whole
// iteration budget.
func TestMalformedSelectorIsReportedAsInvalid(t *testing.T) {
	resetFixture(t)

	err := testBrowser.Click(context.Background(), `#a[[[bad`, false)
	if err == nil {
		t.Fatal("expected a failure for a selector that does not parse")
	}
	if !errors.Is(err, ErrInvalidSelector) {
		t.Errorf("err = %v, want ErrInvalidSelector", err)
	}
}

// Prose containing a colon is only guessed to be CSS, so it must still reach
// the text-matching strategies rather than being called malformed.
func TestProseWithAColonStillMatchesByText(t *testing.T) {
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/has_text.html"); err != nil {
		t.Fatal(err)
	}

	if err := testBrowser.Hover(context.Background(), "Total: 5 items"); err != nil {
		t.Errorf("hovering prose containing a colon: %v", err)
	}
}
