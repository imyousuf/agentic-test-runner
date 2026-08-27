package browser

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A lookup used to spend a fixed three seconds regardless of what the caller
// was prepared to wait, which was wrong in both directions: a behaviour step
// with a minute of budget gave up on a slow page after three seconds, and an
// existence check that wanted an answer in half a second had no way to say so.
func TestSearchTimeoutFollowsTheCallersDeadline(t *testing.T) {
	cases := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want func(time.Duration) bool
		desc string
	}{
		{
			name: "no deadline",
			ctx:  func() (context.Context, context.CancelFunc) { return context.Background(), func() {} },
			want: func(d time.Duration) bool { return d == minElementSearchTimeout },
			desc: "the default budget",
		},
		{
			name: "impatient caller",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 500*time.Millisecond)
			},
			// An existence check asking for 500ms means it, and must not be
			// held for the floor.
			want: func(d time.Duration) bool { return d > 0 && d <= 500*time.Millisecond },
			desc: "no more than the 500ms asked for",
		},
		{
			name: "generous caller",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 10*time.Second)
			},
			want: func(d time.Duration) bool { return d > minElementSearchTimeout && d <= 10*time.Second },
			desc: "more than the floor",
		},
		{
			name: "very generous caller",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 5*time.Minute)
			},
			// One selector that will never match must not eat a whole step.
			want: func(d time.Duration) bool { return d == maxElementSearchTimeout },
			desc: "capped at the ceiling",
		},
		{
			name: "expired caller",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
				time.Sleep(time.Millisecond)
				return ctx, cancel
			},
			want: func(d time.Duration) bool { return d <= time.Millisecond },
			desc: "no budget invented past the deadline",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			defer cancel()
			if got := searchTimeout(ctx); !tc.want(got) {
				t.Errorf("searchTimeout = %v, want %s", got, tc.desc)
			}
		})
	}
}

// The regression: an element that appears later than the old fixed budget is
// found when the caller has said it is willing to wait.
func TestActionWaitsAsLongAsTheCallerAllows(t *testing.T) {
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/late_element.html?ms=4500"); err != nil {
		t.Fatal(err)
	}

	// 4.5s is past the old three-second cap and inside this deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	if err := testBrowser.Fill(ctx, "#username", "someone"); err != nil {
		t.Fatalf("fill gave up after %v: %v", time.Since(start), err)
	}
	if elapsed := time.Since(start); elapsed < 3*time.Second {
		t.Errorf("fill returned in %v, before the element could exist — the test is not exercising the wait", elapsed)
	}
}

// The other half of it: a caller that has not asked for patience must not get
// it, or every failing lookup pays the ceiling.
func TestActionWithNoDeadlineStillGivesUp(t *testing.T) {
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/late_element.html?ms=60000"); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err := testBrowser.Fill(context.Background(), "#username", "someone")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the fill to fail: the element does not appear for a minute")
	}
	if !errors.Is(err, ErrElementNotFound) {
		t.Errorf("err = %v, want ErrElementNotFound so the script runtime can classify it as repairable", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("gave up after %v; a caller with no deadline should still get the default budget", elapsed)
	}
}

// An existence check is the one lookup where absence is the answer, so it must
// stay fast even though actions on the same page now wait longer.
func TestExistenceCheckStaysImpatient(t *testing.T) {
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/late_element.html?ms=60000"); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err := testBrowser.WaitForElement(context.Background(), "#username", 500*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the element to be absent")
	}
	if elapsed > 2*time.Second {
		t.Errorf("a 500ms existence check took %v", elapsed)
	}
}

// A wait longer than the old cap must actually wait that long. Before this
// change WaitForElement's timeout nested inside a fixed three seconds, so no
// caller could wait longer than three seconds however long it asked for.
func TestExplicitWaitIsNotCappedAtThreeSeconds(t *testing.T) {
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/late_element.html?ms=4500"); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := testBrowser.WaitForElement(context.Background(), "#username", 20*time.Second); err != nil {
		t.Fatalf("waiting 20s gave up after %v: %v", time.Since(start), err)
	}
}
