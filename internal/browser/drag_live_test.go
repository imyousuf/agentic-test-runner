package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Drag found both elements and then handed them to the page as JSON. A
// rod.Element marshals to nothing a page can use, so the script received two
// plain objects and failed on the first thing it asked of them:
//
//	TypeError: from.getBoundingClientRect is not a function
//
// Every drag failed that way, whatever was dragged and however it was named.
// Nothing tested it against a page.

func openDragFixture(t *testing.T) {
	t.Helper()
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/drag.html"); err != nil {
		t.Fatal(err)
	}
}

func dragEvents(t *testing.T) string {
	t.Helper()
	got, err := testBrowser.Evaluate(`document.getElementById("events").textContent`)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := got.(string)
	return s
}

func TestDragMovesTheElement(t *testing.T) {
	openDragFixture(t)

	if err := testBrowser.Drag(context.Background(), "#card", "#done"); err != nil {
		t.Fatalf("drag: %v", err)
	}

	// The whole gesture, in order, on the right elements: the page learns
	// what is being dragged at dragstart and is told where at drop.
	const want = "dragstart:card;dragover:done;drop:card->done;dragend:card;"
	if got := dragEvents(t); got != want {
		t.Errorf("events = %q, want %q", got, want)
	}

	// And the page did what a drop makes it do.
	moved, err := testBrowser.Evaluate(`document.getElementById("card").parentElement.id`)
	if err != nil {
		t.Fatal(err)
	}
	if moved != "done" {
		t.Errorf("the card is in #%v, want it moved to #done", moved)
	}
	stayed, err := testBrowser.Evaluate(`document.getElementById("other-card").parentElement.id`)
	if err != nil {
		t.Fatal(err)
	}
	if stayed != "todo" {
		t.Errorf("the other card is in #%v, want it left in #todo", stayed)
	}
}

// Both ends take every spelling of a target, like any other action.
func TestDragTakesEveryKindOfTarget(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
	}{
		{"css", `#card`, `#done`},
		{"xpath", `//div[@id="card"]`, `//div[@id="done"]`},
		{"has-text", `.card:has-text("Write the report")`, `.zone:has-text("Done")`},
		{"visible text and aria-label", `Write the report`, `Done column`},
		{"mixed", `//div[@id="card"]`, `Done column`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openDragFixture(t)

			if err := testBrowser.Drag(context.Background(), tt.from, tt.to); err != nil {
				t.Fatalf("drag %q to %q: %v", tt.from, tt.to, err)
			}
			if got := dragEvents(t); !strings.Contains(got, "drop:card->done;") {
				t.Errorf("events = %q, want the card dropped on the done column", got)
			}
		})
	}
}

// A drag back again: the element found is the element as it is now, not a
// stale handle on where it was.
func TestDragThereAndBack(t *testing.T) {
	openDragFixture(t)

	if err := testBrowser.Drag(context.Background(), "#card", "#done"); err != nil {
		t.Fatalf("there: %v", err)
	}
	if err := testBrowser.Drag(context.Background(), "#card", "#todo"); err != nil {
		t.Fatalf("back: %v", err)
	}

	where, err := testBrowser.Evaluate(`document.getElementById("card").parentElement.id`)
	if err != nil {
		t.Fatal(err)
	}
	if where != "todo" {
		t.Errorf("the card is in #%v, want it back in #todo", where)
	}
}

// An end that is not there is a missing element, and the error says which
// end.
func TestDragReportsWhichEndIsMissing(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		wantText string
	}{
		{"the source", "#no-such-card", "#done", "source element not found"},
		{"the destination", "#card", "#no-such-zone", "target element not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openDragFixture(t)

			ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
			defer cancel()

			err := testBrowser.Drag(ctx, tt.from, tt.to)
			if !errors.Is(err, ErrElementNotFound) {
				t.Fatalf("err = %v, want ErrElementNotFound", err)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("err = %q, want it to say %q", err, tt.wantText)
			}
			if got := dragEvents(t); got != "" {
				t.Errorf("events = %q, want nothing dispatched", got)
			}
		})
	}
}
