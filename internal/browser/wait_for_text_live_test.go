package browser

import (
	"context"
	"errors"
	"testing"
	"time"
)

// WaitForText asked rod for any element whose text matched, and the text of
// an element that is not rendered is its source. A page that will one day say
// "Ready now" usually contains the script that will make it say so — so the
// wait matched the <script>, returned at once, and the step after it ran
// against a page that was not ready.

// The fixture's own script contains the words it will later display.
func TestWaitForTextWaitsForTheTextToBeShown(t *testing.T) {
	start := openLateButton(t, "ms=1500")

	err := testBrowser.WaitForText(context.Background(), "Ready now", 10*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("gave up after %v: %v", elapsed.Round(time.Millisecond), err)
	}
	if elapsed < 1400*time.Millisecond {
		t.Errorf("returned in %v, before the page said it — the wait matched something that is not shown",
			elapsed.Round(time.Millisecond))
	}
	if elapsed > 2500*time.Millisecond {
		t.Errorf("returned after %v; the text was there at 1.5s", elapsed.Round(time.Millisecond))
	}
}

func TestWaitForTextDoesNotMatchWhatIsNotShown(t *testing.T) {
	// This fixture is titled "has-text", and shows the word nowhere.
	t.Run("the title", func(t *testing.T) {
		resetFixture(t)
		if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/has_text.html"); err != nil {
			t.Fatal(err)
		}

		err := testBrowser.WaitForText(context.Background(), "has-text", 700*time.Millisecond)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want the wait to time out: the word is only in the title", err)
		}
	})

	for _, tt := range []struct {
		name string
		text string
	}{
		{"the source of an inline script", "Only ever in a script"},
		{"an element that is not rendered", "Kept in reserve for later"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			openPlainTargets(t)

			err := testBrowser.WaitForText(context.Background(), tt.text, 700*time.Millisecond)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("err = %v, want the wait to time out: %q is not shown", err, tt.text)
			}
		})
	}
}

func TestWaitForTextFindsWhatIsShown(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
	}{
		{"a heading", "Orders"},
		{"part of a paragraph", "parcel has shipped"},
		{"text across an inline element", "Your parcel has shipped today"},
		{"the value in a field", "typed already"},
		{"a placeholder", "Search orders"},
		{"punctuation that would be syntax in a pattern", "Price (USD)"},
		{"quotes", `It's "fine"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			openPlainTargets(t)

			start := time.Now()
			if err := testBrowser.WaitForText(context.Background(), tt.text, 3*time.Second); err != nil {
				t.Fatalf("%q is on the page: %v", tt.text, err)
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Errorf("took %v to find text already shown", elapsed.Round(time.Millisecond))
			}
		})
	}

	// As written, not as a pattern, and with the case it was written in.
	t.Run("case matters, as it did", func(t *testing.T) {
		openPlainTargets(t)
		if err := testBrowser.WaitForText(context.Background(), "ORDER SUMMARY", 600*time.Millisecond); err == nil {
			t.Error("matched text in a different case")
		}
	})
}
