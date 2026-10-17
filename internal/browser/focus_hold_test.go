package browser

import (
	"context"
	"testing"
	"time"

	"github.com/go-rod/rod"
)

// testHold is how long the shared browser's holds last in these tests: long
// against a CDP round trip on a loaded machine, short against a test run.
const testHold = time.Second

// setFocusHoldFor shortens the shared browser's holds for one test, and ends
// any it leaves, so the page goes back to the next test unheld.
func setFocusHoldFor(t *testing.T, b *Browser, d time.Duration) {
	t.Helper()
	b.focusHoldMu.Lock()
	b.focusHoldFor = d
	b.focusHoldMu.Unlock()
	t.Cleanup(func() {
		b.focusHoldMu.Lock()
		var pages []*rod.Page
		for target, h := range b.focusHolds {
			h.timer.Stop()
			delete(b.focusHolds, target)
			pages = append(pages, h.page)
		}
		b.focusHoldFor = 0
		b.focusHoldMu.Unlock()
		for _, page := range pages {
			b.applyFocusHold(page)
		}
	})
}

func currentTestPage(t *testing.T) *rod.Page {
	t.Helper()
	resetFixture(t)
	page, err := testBrowser.CurrentPage()
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestAHoldLapsesOnceThePageIsLeftAlone(t *testing.T) {
	page := currentTestPage(t)
	setFocusHoldFor(t, testBrowser, testHold)

	testBrowser.holdFocus(page)
	if !testBrowser.focusHeld(page) {
		t.Fatal("not held after holdFocus")
	}
	time.Sleep(testHold * 5 / 2)
	if testBrowser.focusHeld(page) {
		t.Error("still held after being left alone for longer than the hold")
	}
}

// The hold is for as long as ATR is using the page, not for a fixed time from
// the last give-back: a step that only fills, waits or reads must not have the
// page blur under it.
func TestUsingThePagePutsTheHoldOff(t *testing.T) {
	page := currentTestPage(t)
	setFocusHoldFor(t, testBrowser, testHold)

	testBrowser.holdFocus(page)
	for start := time.Now(); time.Since(start) < testHold*5/2; {
		time.Sleep(testHold / 5)
		if _, err := testBrowser.CurrentPage(); err != nil {
			t.Fatal(err)
		}
	}
	if !testBrowser.focusHeld(page) {
		t.Fatal("the hold lapsed while the page was being used")
	}
	time.Sleep(testHold * 5 / 2)
	if testBrowser.focusHeld(page) {
		t.Error("still held after the page was left alone")
	}
}

// A health check is not ATR using the page. Polled from outside, it would
// otherwise keep the page held for as long as it kept polling.
func TestAHealthCheckDoesNotPutTheHoldOff(t *testing.T) {
	page := currentTestPage(t)
	setFocusHoldFor(t, testBrowser, testHold)

	testBrowser.holdFocus(page)
	for start := time.Now(); time.Since(start) < testHold*5/2; {
		time.Sleep(testHold / 5)
		if url, _ := testBrowser.Glance(); url == "" {
			t.Fatal("Glance returned no URL")
		}
	}
	if testBrowser.focusHeld(page) {
		t.Error("health checks kept the page held")
	}
}

// A wait can run for longer than the hold. The page must not blur under it.
func TestAHoldDoesNotLapseDuringAWait(t *testing.T) {
	page := currentTestPage(t)
	setFocusHoldFor(t, testBrowser, testHold)

	testBrowser.holdFocus(page)
	wait := testHold * 5 / 2
	start := time.Now()
	err := testBrowser.WaitForText(context.Background(), "text that is never shown", wait)
	if err == nil {
		t.Fatal("the wait found text that is not on the page")
	}
	if elapsed := time.Since(start); elapsed < wait-testHold/5 {
		t.Fatalf("the wait ended after %v, too soon for this test to mean anything", elapsed)
	}
	if !testBrowser.focusHeld(page) {
		t.Fatal("the hold lapsed during the wait")
	}
	time.Sleep(testHold * 5 / 2)
	if testBrowser.focusHeld(page) {
		t.Error("still held long after the wait ended")
	}
}

// The hold is made just before the focus is given back. A page that cannot
// answer — its main thread busy, a confirm() open — must not keep the user's
// window waiting for it.
func TestHoldingAPageThatCannotAnswerDoesNotWaitForIt(t *testing.T) {
	page := currentTestPage(t)
	setFocusHoldFor(t, testBrowser, time.Minute)

	if _, err := page.Eval(`() => { setTimeout(() => { const end = Date.now() + 4000; while (Date.now() < end) {} }, 0); }`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	testBrowser.holdFocus(page)
	if elapsed := time.Since(start); elapsed > focusHoldCallTimeout+time.Second {
		t.Errorf("holding a page that cannot answer took %v", elapsed)
	}
	// Let the page recover before the next test uses it.
	time.Sleep(4 * time.Second)
}

func TestOpensFileChooser(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()
	if err := testBrowser.Navigate(ctx, testFixtureURL+"/file_input.html"); err != nil {
		t.Fatal(err)
	}
	page, err := testBrowser.CurrentPage()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		selector string
		want     bool
	}{
		{"#upload", true},
		{"#upload-label", true},
		{"#styled", true},
		{"#shadowed", true},
		{"#plain", false},
		{"#pick", false},
		{"#off", false},
		{"#off-label", false},
	} {
		el, err := page.Timeout(5 * time.Second).Element(tc.selector)
		if err != nil {
			t.Fatalf("%s: %v", tc.selector, err)
		}
		shape, err := el.Shape()
		if err != nil {
			t.Fatalf("%s: %v", tc.selector, err)
		}
		pt := shape.OnePointInside()
		if pt == nil {
			t.Fatalf("%s: no point inside", tc.selector)
		}
		if got := opensFileChooser(el, *pt); got != tc.want {
			t.Errorf("opensFileChooser(%s) = %v, want %v", tc.selector, got, tc.want)
		}
	}
}
