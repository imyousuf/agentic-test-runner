package browser

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The schedule itself, without a browser: when a lookup that keeps failing
// takes each of its looks.
func TestLookupSleeperSchedule(t *testing.T) {
	const budget = 3 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	sleep := lookupSleeper()
	start := time.Now()

	// The first look happens before any sleep.
	looks := []time.Duration{0}
	var endErr error
	for {
		if err := sleep(ctx); err != nil {
			endErr = err
			break
		}
		looks = append(looks, time.Since(start))
		if len(looks) > 100 {
			t.Fatal("the sleeper never ended at the deadline")
		}
	}
	ended := time.Since(start)

	if !errors.Is(endErr, context.DeadlineExceeded) {
		t.Errorf("ended with %v, want the context's deadline", endErr)
	}
	if ended < budget-50*time.Millisecond || ended > budget+500*time.Millisecond {
		t.Errorf("ended after %v, want the %v budget", ended.Round(time.Millisecond), budget)
	}

	// Quick at first: something that renders straight away is not kept
	// waiting.
	if len(looks) < 3 || looks[1] > 250*time.Millisecond || looks[2] > 600*time.Millisecond {
		t.Errorf("early looks at %v, want the first two retries within about half a second", looks)
	}

	// Never blind for long. This is the property the old schedule lacked: it
	// went from 1.4s to 3s without looking.
	for i := 1; i < len(looks); i++ {
		if gap := looks[i] - looks[i-1]; gap > 700*time.Millisecond {
			t.Errorf("no look between %v and %v", looks[i-1].Round(time.Millisecond), looks[i].Round(time.Millisecond))
		}
	}

	// And one look timed to land just before giving up, so the end of the
	// budget is not wasted. The steady pace alone would leave the last look
	// at about 2.7s here, 300ms short; this is the look added for the purpose.
	last := looks[len(looks)-1]
	if short := budget - last; short > lookupLastLook+80*time.Millisecond || short < lookupLastLook-80*time.Millisecond {
		t.Errorf("the last look was at %v of a %v budget, want it %v before the end",
			last.Round(time.Millisecond), budget, lookupLastLook)
	}

	// Looking more often is not looking constantly.
	if len(looks) > 15 {
		t.Errorf("%d looks in %v", len(looks), budget)
	}
}

// With no deadline to aim at, the sleeper just keeps its steady pace, and
// stops when it is cancelled.
func TestLookupSleeperWithoutADeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sleep := lookupSleeper()

	start := time.Now()
	for i := 0; i < 4; i++ {
		if err := sleep(ctx); err != nil {
			t.Fatalf("sleep %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("four sleeps took %v", elapsed.Round(time.Millisecond))
	}

	cancel()
	if err := sleep(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v after cancellation, want context.Canceled", err)
	}
}

// The final look is its own piece of logic, so it is shown on a budget where
// nothing else would produce it: one second, in which the steady pace looks at
// 0.1s, 0.3s and 0.7s and would next look at 1.2s.
func TestLookupSleeperTakesALookJustBeforeTheDeadline(t *testing.T) {
	const budget = time.Second

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	sleep := lookupSleeper()
	start := time.Now()

	var looks []time.Duration
	for sleep(ctx) == nil {
		looks = append(looks, time.Since(start))
	}

	if len(looks) != 4 {
		t.Fatalf("looks at %v, want the three steady ones and one before the deadline", looks)
	}
	last := looks[len(looks)-1]
	if last < 820*time.Millisecond || last > 980*time.Millisecond {
		t.Errorf("the last look was at %v, want it about %v before the %v deadline",
			last.Round(time.Millisecond), lookupLastLook, budget)
	}
}

// A budget shorter than the first interval still gets a second look, rather
// than sleeping straight through to the deadline.
func TestLookupSleeperInsideATinyBudget(t *testing.T) {
	// Shorter than lookupPollStart: the first sleep alone would outlast it.
	const budget = 60 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	sleep := lookupSleeper()
	start := time.Now()

	looks := 0
	for sleep(ctx) == nil {
		looks++
	}
	if looks == 0 {
		t.Errorf("slept through a %v budget without a second look", budget)
	}
	if looks > 3 {
		t.Errorf("%d looks inside %v: the sleeper is spinning", looks, budget)
	}
	if ended := time.Since(start); ended > time.Second {
		t.Errorf("took %v to end a %v budget", ended.Round(time.Millisecond), budget)
	}
}
