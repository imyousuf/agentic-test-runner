package rdp

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// Close must win over a stream() that is still on its way in, and it must do so
// before anything touches the page -- passing nil here is what pins that
// ordering. The shutdown return also has to leave switching down: a flag stuck
// true would tell the watchdog a tab switch was in flight forever, and no later
// recovery would ever run.
func TestStreamRefusesOnceTheViewIsClosed(t *testing.T) {
	st := NewStreamer(NewHub(), Options{})
	st.Close()

	if err := st.stream(nil); err == nil {
		t.Fatal("stream() must refuse once the live view is closed")
	}

	st.mu.Lock()
	switching := st.switching
	st.mu.Unlock()
	if switching {
		t.Fatal("the shutdown path must not leave switching raised")
	}
}

// The watchdog reads "no page" as a view that has died and starts a recovery of
// its own. switching is what tells it the page is missing only because a tab
// switch is in flight, which is why stream() raises it before it drops the old
// page rather than after. This pins the consuming half of that contract: the
// state is identical in both halves of the test and only the flag differs, so
// the watchdog standing down and then acting can be nothing else.
func TestWatchLeavesASwitchInFlightAlone(t *testing.T) {
	st := NewStreamer(NewHub(), Options{})

	// This is what stream() looks like partway through: the old page is gone,
	// the last published state still says live, and no browser is attached, so
	// the only trace of a recovery attempt is the status the watchdog publishes
	// on its way to Select("").
	st.mu.Lock()
	st.page = nil
	st.live = true
	st.switching = true
	st.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go st.Watch(ctx)

	// The watchdog ticks once a second, so this gives it a tick to act on.
	time.Sleep(1500 * time.Millisecond)
	if !st.Live() {
		t.Fatal("the watchdog must leave a switch that is in flight alone")
	}

	st.mu.Lock()
	st.switching = false
	st.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for st.Live() {
		if time.Now().After(deadline) {
			t.Fatal("the watchdog must act once the switch is over")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// liveStreamer brings up a headless browser with two tabs and a Streamer
// attached to it.
//
// It skips rather than fails when no browser is installed: the window the
// ordering test below measures is a CDP round trip, so with nothing to talk to
// there is nothing to measure. That also keeps the package buildable and
// testable on the release targets that have no Chromium build.
func liveStreamer(t *testing.T) (*Streamer, []PageInfo) {
	t.Helper()

	bin, found := launcher.LookPath()
	if !found {
		t.Skip("no browser is installed, and the window under test is a CDP round trip")
	}
	l := launcher.New().Bin(bin).Headless(true).Set("no-sandbox")
	cdpURL, err := l.Launch()
	if err != nil {
		t.Skipf("could not launch a browser: %v", err)
	}
	t.Cleanup(func() {
		l.Kill()
		l.Cleanup()
	})

	st := NewStreamer(NewHub(), Options{})
	if err := st.Attach(cdpURL); err != nil {
		t.Fatalf("failed to attach: %v", err)
	}
	t.Cleanup(st.Close)

	// Two tabs, so there is something to switch between.
	for i := 0; i < 2; i++ {
		if _, err := st.browser.Page(proto.TargetCreateTarget{URL: "about:blank"}); err != nil {
			t.Fatalf("failed to open a tab: %v", err)
		}
	}
	pages, err := st.Pages()
	if err != nil {
		t.Fatalf("failed to list the pages: %v", err)
	}
	if len(pages) < 2 {
		t.Fatalf("expected at least 2 tabs, got %d", len(pages))
	}
	return st, pages
}

// sampleWhile runs fn while watching the pair the watchdog reads, and reports
// how often the streamer looked like a dead view and how many samples it took
// to find out. Anything this sampler can see, a tick could have seen.
func sampleWhile(st *Streamer, fn func()) (bad, samples int) {
	stop := make(chan struct{})
	result := make(chan [2]int)

	go func() {
		var bad, samples int
		for {
			select {
			case <-stop:
				result <- [2]int{bad, samples}
				return
			default:
			}
			st.mu.Lock()
			if st.page == nil && !st.switching {
				bad++
			}
			st.mu.Unlock()
			samples++
			runtime.Gosched()
		}
	}()

	fn()
	close(stop)
	r := <-result
	return r[0], r[1]
}

// A tab switch must never look like a view that has died, because the watchdog
// acts on exactly that: it sees no page with no switch in flight, concludes an
// earlier attempt failed, and calls Select("") -- which tears down the stream
// the viewer just asked for and can land on a different tab entirely.
//
// This is why switching has to be raised before stop() rather than after it.
// stop() clears s.page under the mutex and only then makes the
// PageStopScreencast round trip, so raising the flag afterwards leaves the
// state readable as "dead" for the whole of that CDP call -- milliseconds, not
// instructions, and a tick lands there sooner or later.
func TestATabSwitchNeverLooksLikeADeadView(t *testing.T) {
	st, pages := liveStreamer(t)

	if err := st.Select(pages[0].ID); err != nil {
		t.Fatalf("failed to select the first tab: %v", err)
	}

	bad, samples := sampleWhile(st, func() {
		if err := st.Select(pages[1].ID); err != nil {
			t.Errorf("failed to switch to the second tab: %v", err)
		}
	})
	if bad != 0 {
		t.Fatalf("the switch looked like a dead view in %d of %d samples; a watchdog tick landing there would tear the new stream down",
			bad, samples)
	}

	// The control matters as much as the assertion: it proves the sampler can
	// see the state it just failed to find. stop() on its own leaves precisely
	// that state behind -- no page, no switch in flight -- and it is the one the
	// watchdog is supposed to recover from.
	if control, controlSamples := sampleWhile(st, st.stop); control == 0 {
		t.Fatalf("the sampler saw no dead view in %d samples even after stop(), so the check above proved nothing",
			controlSamples)
	}
}
