package browser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// A target that is not a selector — visible text, an aria-label, a
// data-testid, a name, a placeholder, a snapshot UID — is tried each of those
// ways in turn. Each way used to wait its own slice of the budget before the
// next was tried, and the slices added up to more than the budget. So the
// later ways never got a turn: inside an existence check's half second only
// the aria-label was ever looked for, and atr.exists("Sign in") said false
// with the button on the page. atr.expectExists("Welcome") waited out its ten
// seconds and reported a heading that was there throughout as an assertion
// failure. atr.expectMissing passed for the same reason, which is worse.

func openPlainTargets(t *testing.T) *rod.Page {
	t.Helper()
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/plain_targets.html"); err != nil {
		t.Fatal(err)
	}
	page, err := testBrowser.CurrentPage()
	if err != nil {
		t.Fatal(err)
	}
	return page
}

// plainTargets is one target for each way of reading one, and the element it
// names.
var plainTargets = []plainTarget{
	{"aria-label", "Close dialog", "by-aria", false},
	{"data-testid", "order-total", "by-testid", false},
	{"name", "email", "by-name", false},
	{"placeholder", "Search orders", "by-placeholder", false},
	{"exact text", "Order summary", "by-exact-text", false},
	{"label", "Quantity", "by-label", false},
	{"snapshot UID", "e0", "by-aria", false},
	{"link text, in part", "View all orders", "by-link-text", true},
	{"text, in part", "parcel has shipped", "by-partial-text", true},
	{"a field's value", "typed already", "by-value", true},
}

type plainTarget struct {
	way    string
	target string
	wantID string
	// inPart is true for the readings that match part of some longer text.
	// An action holds them back while the exact readings have a head start.
	inPart bool
}

// Every way is tried inside even the shortest wait there is: the half second
// atr.exists allows.
func TestEveryPlainTargetIsFoundInsideAnExistenceCheck(t *testing.T) {
	for _, tt := range plainTargets {
		t.Run(tt.way, func(t *testing.T) {
			openPlainTargets(t)

			start := time.Now()
			err := testBrowser.WaitForElement(context.Background(), tt.target, 500*time.Millisecond)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("%q is on the page and was not found in a 500ms wait (%v): %v",
					tt.target, elapsed.Round(time.Millisecond), err)
			}
			// It is there already, so the first look finds it, whichever way
			// it is named: a wait asks only whether the target is there, and
			// holds nothing back. (How quickly is measured against the
			// machine's own pace, in TestAPresenceCheckFindsTextInPartAtOnce.)
		})
	}
}

// And each resolves to the element it names.
func TestPlainTargetsResolveToTheElementTheyName(t *testing.T) {
	type resolution struct {
		way    string
		target string
		wantID string
	}
	var tests []resolution
	for _, pt := range plainTargets {
		tests = append(tests, resolution{pt.way, pt.target, pt.wantID})
	}

	tests = append(tests, []resolution{
		// "e2e suite" is text. It is not snapshot element 2 followed by
		// something to ignore, which is how it used to be read.
		{"text that begins like a UID", "e2e suite", "by-e-text"},

		// A word that is also the name of an HTML element is tried as a
		// selector first and as text after, in the same look.
		{"an element name with no such element on the page", "Details", "details-link"},
		{"an element name with one", "Address", "the-address"},

		// Text is read from what the page shows. The title says "Orders" too,
		// and comes first in the document; it is not what a person means by
		// the Orders on the page, and it cannot be clicked.
		{"text the title also says", "Orders", "heading"},
		// The smallest element showing the text, not the page it is on.
		{"text inside an inline child", "has shipped", "by-inline-text"},
		{"text across an inline child", "has shipped today", "by-partial-text"},

		// Punctuation that is syntax in a selector, an XPath or a pattern.
		{"an aria-label containing double quotes", `Say "hi"`, "quoted-aria"},
		{"text containing both kinds of quote", `It's "fine"`, "quoted-text"},
		{"text containing parentheses", "Price (USD)", "price"},
		// As written: this is not a pattern with an unclosed group.
		{"part of it, ending mid-parenthesis", "Price (USD", "price"},

		// A capitalised word that is also an element name is a word. With no
		// such element it is matched as text, in part too.
		{"an element name inside a button's text", "Summary", "show-summary"},

		// The order the ways are tried in is the order of precedence, and it
		// is not the order of the document.
		{"aria-label before visible text", "Save", "save-aria"},
		{"aria-label before data-testid", "Export", "export-aria"},
		{"data-testid before name", "Archive", "archive-testid"},
		{"name before placeholder", "City", "city-name"},
		{"placeholder before visible text", "Phone", "phone-placeholder"},
		{"exact text before a button containing it", "Pay now", "pay-text"},
	}...)

	for _, tt := range tests {
		t.Run(tt.way, func(t *testing.T) {
			page := openPlainTargets(t)

			el, err := testBrowser.findElement(page.Timeout(2*time.Second), tt.target)
			if err != nil {
				t.Fatalf("%q: %v", tt.target, err)
			}
			if got := idOf(t, el); got != tt.wantID {
				t.Errorf("%q resolved to #%s, want #%s", tt.target, got, tt.wantID)
			}
		})
	}
}

// Text that is in the document but not on the page is not there to be found:
// the source of an inline script, or — when only part of it is given — an
// element that is not rendered.
func TestTextThatIsNotShownIsNotFoundAsText(t *testing.T) {
	for _, tt := range []struct {
		name   string
		target string
	}{
		{"a string in an inline script", "Only ever in a script"},
		{"part of it", "ever in a script"},
		{"part of the text of an element that is not rendered", "in reserve"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			openPlainTargets(t)

			err := testBrowser.WaitForElement(context.Background(), tt.target, 600*time.Millisecond)
			if !errors.Is(err, ErrElementNotFound) {
				t.Errorf("err = %v, want ErrElementNotFound: %q is not shown anywhere", err, tt.target)
			}
		})
	}

	// The whole text of an element that is not rendered still names it, as it
	// always has: waiting for something to become visible starts by finding
	// it.
	t.Run("the whole text of an element that is not rendered", func(t *testing.T) {
		page := openPlainTargets(t)

		el, err := testBrowser.findElement(page.Timeout(2*time.Second), "Kept in reserve for later")
		if err != nil {
			t.Fatal(err)
		}
		if got := idOf(t, el); got != "not-shown" {
			t.Errorf("resolved to #%s, want #not-shown", got)
		}
	})
}

// A click on part of some text lands on the element showing it. The first
// element whose text contains anything on the page is the root, so this used
// to click the middle of the viewport.
func TestAClickOnPartOfSomeTextLandsOnIt(t *testing.T) {
	openPlainTargets(t)

	if err := testBrowser.Click(context.Background(), "parcel has shipped", false); err != nil {
		t.Fatal(err)
	}
	got, err := testBrowser.Evaluate(`document.getElementById("events").textContent`)
	if err != nil {
		t.Fatal(err)
	}
	// The click lands at the paragraph's centre, which is over the paragraph
	// or the emphasis inside it — and nowhere else.
	if got != "click:by-partial-text;" && got != "click:by-inline-text;" {
		t.Errorf("events = %q, want one click inside the paragraph", got)
	}
}

// A click on text the title shares goes to the page. It used to resolve to
// the <title> and wait thirty seconds for it to become clickable.
func TestAClickOnTextTheTitleSharesIsQuick(t *testing.T) {
	openPlainTargets(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	if err := testBrowser.Click(ctx, "Orders", false); err != nil {
		t.Fatalf("after %v: %v", time.Since(start).Round(time.Millisecond), err)
	}
	// Thirty seconds, before. Five is slack for a slow machine.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v", elapsed.Round(time.Millisecond))
	}
	got, err := testBrowser.Evaluate(`document.getElementById("events").textContent`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "click:heading;" {
		t.Errorf("events = %q, want one click on the heading", got)
	}
}

// An action on visible text used to wait out a slice of its budget for each
// of the four attributes tried before text: two seconds before a click on
// something that was there all along.
func TestAnActionOnVisibleTextDoesNotWaitForTheWaysTriedBeforeIt(t *testing.T) {
	openPlainTargets(t)

	start := time.Now()
	if err := testBrowser.Click(context.Background(), "Order summary", false); err != nil {
		t.Fatal(err)
	}
	// It took two seconds before the click even began. A second and a half
	// leaves a slow machine room for the click itself.
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Errorf("clicking text that was on the page took %v", elapsed.Round(time.Millisecond))
	}

	got, err := testBrowser.Evaluate(`document.getElementById("events").textContent`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "click:by-exact-text;" {
		t.Errorf("events = %q, want one click on the heading", got)
	}
}

// Text that has not rendered yet is waited for like any other target, and
// found when it arrives rather than when its strategy's turn comes round.
func TestPlainTextIsFoundWhenItArrives(t *testing.T) {
	start := openLateButton(t, "ms=1500")

	err := testBrowser.WaitForElement(context.Background(), "Ready now", 10*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("gave up after %v: %v", elapsed.Round(time.Millisecond), err)
	}
	if elapsed < 1400*time.Millisecond {
		t.Errorf("returned in %v, before the button could exist", elapsed.Round(time.Millisecond))
	}
	// Nearly seven seconds, before: it was found when its reading's turn
	// came round, not when it arrived.
	if elapsed > 3500*time.Millisecond {
		t.Errorf("found after %v; the button was there at 1.5s", elapsed.Round(time.Millisecond))
	}
}

// And text that never arrives is given up on at the budget — the whole of it,
// and no more than it.
func TestPlainTextThatIsNotThereIsGivenUpOnAtTheBudget(t *testing.T) {
	t.Run("an explicit wait", func(t *testing.T) {
		openPlainTargets(t)

		start := time.Now()
		err := testBrowser.WaitForElement(context.Background(), "No such words anywhere", 2*time.Second)
		elapsed := time.Since(start)

		if !errors.Is(err, ErrElementNotFound) {
			t.Errorf("err = %v, want ErrElementNotFound", err)
		}
		if elapsed < 1800*time.Millisecond || elapsed > 3*time.Second {
			t.Errorf("a 2s wait took %v", elapsed.Round(time.Millisecond))
		}
	})

	// The ways added up to longer than the budget they were sharing: an
	// action with the default three seconds took over four to fail.
	t.Run("an action with the default budget", func(t *testing.T) {
		openPlainTargets(t)

		start := time.Now()
		err := testBrowser.Click(context.Background(), "No such words anywhere", false)
		elapsed := time.Since(start)

		if !errors.Is(err, ErrElementNotFound) {
			t.Errorf("err = %v, want ErrElementNotFound", err)
		}
		if elapsed < 2800*time.Millisecond || elapsed > 3500*time.Millisecond {
			t.Errorf("an action with a 3s budget took %v to give up", elapsed.Round(time.Millisecond))
		}
	})

	// A target is text, not a pattern: what would be a pattern for the text
	// on the page does not match it.
	t.Run("a pattern for text that is there", func(t *testing.T) {
		openPlainTargets(t)

		for _, pattern := range []string{"Order.summary", "^Orders$", "Order (summary|total)"} {
			err := testBrowser.WaitForElement(context.Background(), pattern, 500*time.Millisecond)
			if !errors.Is(err, ErrElementNotFound) {
				t.Errorf("%q: err = %v, want ErrElementNotFound", pattern, err)
			}
		}
	})

	// A UID past the end of the snapshot names nothing.
	t.Run("a UID past the end", func(t *testing.T) {
		openPlainTargets(t)

		err := testBrowser.WaitForElement(context.Background(), "e9999", 500*time.Millisecond)
		if !errors.Is(err, ErrElementNotFound) {
			t.Errorf("err = %v, want ErrElementNotFound", err)
		}
	})
}

// A target written as a selector is a selector, and one that matches nothing
// yet is something to wait for. It is not looked for inside the page's text.
//
// It used to be, as soon as every reading was tried on every look: the text
// readings took the target as a regular expression, and a[href="/logout"] is
// the letter a followed by any one of a dozen characters. "parcel" has one. So
// a wait for a link that had not rendered passed at once, a click on it landed
// on some other link, and expectMissing reported it still there for ever.
func TestATargetWrittenAsASelectorIsNotMatchedAsText(t *testing.T) {
	for _, target := range []string{
		`a[href="/logout"]`,
		`div[role="dialog"]`,
		`ul li`,
		`p > em.missing`,
		// Lower case and nothing but an element name: a selector, as the docs
		// write one. There is a button that says "Open dialog" and no
		// <dialog>.
		`dialog`,
	} {
		t.Run(target, func(t *testing.T) {
			openPlainTargets(t)

			start := time.Now()
			err := testBrowser.WaitForElement(context.Background(), target, 1500*time.Millisecond)
			elapsed := time.Since(start)

			if !errors.Is(err, ErrElementNotFound) {
				t.Fatalf("err = %v after %v, want ErrElementNotFound: nothing on the page matches this selector",
					err, elapsed.Round(time.Millisecond))
			}
			if elapsed < 1300*time.Millisecond {
				t.Errorf("gave up after %v of a 1.5s wait", elapsed.Round(time.Millisecond))
			}
		})
	}

	// And it is found, as a selector, when it is there.
	t.Run("one that matches", func(t *testing.T) {
		page := openPlainTargets(t)

		el, err := testBrowser.findElement(page.Timeout(2*time.Second), `p > em`)
		if err != nil {
			t.Fatal(err)
		}
		if got := idOf(t, el); got != "by-inline-text" {
			t.Errorf("resolved to #%s, want #by-inline-text", got)
		}
	})
}

// A selector that is on its way is waited for and found, by a click as by a
// wait.
func TestAGuessedSelectorIsWaitedFor(t *testing.T) {
	start := openLateButton(t, "ms=1500")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// "button" followed by an attribute test: guessed to be CSS, not known.
	if err := testBrowser.Click(ctx, `button[id="ready"]`, false); err != nil {
		t.Fatalf("after %v: %v", time.Since(start).Round(time.Millisecond), err)
	}
	got, err := testBrowser.Evaluate(`document.getElementById("events").textContent`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "click:ready;" {
		t.Errorf("events = %q, want one click on the late button", got)
	}
}

func openStrictFirst(t *testing.T, query string) time.Time {
	t.Helper()
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/strict_first.html?"+query); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := testBrowser.Evaluate("window.arm()"); err != nil {
		t.Fatalf("arming the fixture: %v", err)
	}
	return start
}

// Trying every reading on every look must not make precedence a matter of
// what has rendered so far. The page says "Saved filters" from the start; the
// Save button is 400ms behind it. A click on "Save" is a click on the button.
func TestAnExactMatchThatIsARenderAwayBeatsAPartialOneAlreadyThere(t *testing.T) {
	openStrictFirst(t, "ms=400")

	if err := testBrowser.Click(context.Background(), "Save", false); err != nil {
		t.Fatal(err)
	}
	got, err := testBrowser.Evaluate(`document.getElementById("events").textContent`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "click:save;" {
		t.Errorf("events = %q, want the click on the Save button, not on the heading that contains the word", got)
	}
}

// The head start is a head start and no more: an action on text in part still
// finds it, once nothing better has turned up.
func TestAnActionOnTextInPartFindsItAfterTheHeadStart(t *testing.T) {
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/strict_first.html"); err != nil {
		t.Fatal(err)
	}

	// The default three-second budget: a second for the exact readings.
	start := time.Now()
	err := testBrowser.Hover(context.Background(), "Saved fil")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("text on the page was not found (%v): %v", elapsed.Round(time.Millisecond), err)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("acted on text in part after %v, with no head start for an exact match", elapsed.Round(time.Millisecond))
	}
	if elapsed > 2800*time.Millisecond {
		t.Errorf("took %v of a 3s budget to find text that was there throughout", elapsed.Round(time.Millisecond))
	}
}

// A wait or an existence check is not an action. It asks whether the target
// is on the page, not which element it is, so there is nothing for a head
// start to protect — and text shown as part of something is found on the first
// look, however short the wait.
func TestAPresenceCheckFindsTextInPartAtOnce(t *testing.T) {
	for _, timeout := range []time.Duration{500 * time.Millisecond, 10 * time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			resetFixture(t)
			if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/strict_first.html"); err != nil {
				t.Fatal(err)
			}

			// How long one look takes on this machine, from a target the
			// first look has always found: the heading by its exact text.
			start := time.Now()
			if err := testBrowser.WaitForElement(context.Background(), "Saved filters", timeout); err != nil {
				t.Fatalf("the heading was not found by its exact text: %v", err)
			}
			oneLook := time.Since(start)

			start = time.Now()
			err := testBrowser.WaitForElement(context.Background(), "Saved fil", timeout)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("text on the page was not found in a %v wait (%v): %v", timeout, elapsed.Round(time.Millisecond), err)
			}
			// Measured against that, not against the clock, so the bound
			// means the same on a slow machine: no waiting, only looking.
			if elapsed > oneLook+150*time.Millisecond {
				t.Errorf("found after %v, where one look takes %v: text in part was held back",
					elapsed.Round(time.Millisecond), oneLook.Round(time.Millisecond))
			}
		})
	}
}

// A plain target is looked for by a loop of its own, with its own way out.
// Cancelling the caller ends it, and is reported as a cancellation: "element
// not found" is the reading that sends a script for repair.
func TestACancelledLookupOfAPlainTargetIsNotAMiss(t *testing.T) {
	lookups := []struct {
		name string
		look func(ctx context.Context) error
	}{
		{"WaitForElement", func(ctx context.Context) error {
			return testBrowser.WaitForElement(ctx, "never rendered anywhere", 20*time.Second)
		}},
		{"Click", func(ctx context.Context) error {
			return testBrowser.Click(ctx, "never rendered anywhere", false)
		}},
	}

	for _, l := range lookups {
		t.Run(l.name, func(t *testing.T) {
			openPlainTargets(t)

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			time.AfterFunc(500*time.Millisecond, cancel)
			defer cancel()

			start := time.Now()
			err := l.look(ctx)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("found something that is not there")
			}
			if elapsed > 3*time.Second {
				t.Errorf("a cancelled lookup took %v to return", elapsed.Round(time.Millisecond))
			}
			if errors.Is(err, ErrElementNotFound) {
				t.Errorf("err = %v — a cancelled lookup was reported as a missing element", err)
			}
		})
	}
}

// An existence check finds what is on the page however slow the machine.
//
// It did not. Text in part was held back for half the check's half second,
// and a look was eight or ten round trips, one per reading — so on a slow
// machine the one look that included text in part began a tenth of a second
// before the deadline and did not finish. Text that was there was reported
// absent, on a macOS runner and on no machine the code was written on.
//
// Two things changed. A presence check looks every way from the first look,
// and a look is one round trip. The page is throttled to a tenth of its speed
// here. On the machine this was written on the old code survives that and
// fails at a twentieth; the new code is still finding everything at an
// eightieth. A tenth leaves a slow runner the same room.
func TestAPlainTargetIsFoundInsideAnExistenceCheckOnASlowMachine(t *testing.T) {
	for _, tt := range plainTargets {
		t.Run(tt.way, func(t *testing.T) {
			page := openPlainTargets(t)

			if err := (proto.EmulationSetCPUThrottlingRate{Rate: 10}).Call(page); err != nil {
				t.Fatalf("throttling the page: %v", err)
			}
			defer func() {
				_ = proto.EmulationSetCPUThrottlingRate{Rate: 1}.Call(page)
			}()

			// Several times: a look that only just fits passes some of the
			// time, and "some of the time" is the bug.
			for i := 0; i < 5; i++ {
				start := time.Now()
				err := testBrowser.WaitForElement(context.Background(), tt.target, 500*time.Millisecond)
				if err != nil {
					t.Fatalf("try %d: %q is on the page and was not found in a 500ms wait (%v): %v",
						i+1, tt.target, time.Since(start).Round(time.Millisecond), err)
				}
			}
		})
	}
}
