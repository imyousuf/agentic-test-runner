package testscript

import (
	"strings"
	"testing"
)

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func codes(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Code)
	}
	return out
}

func lint(t *testing.T, source string) []Finding {
	t.Helper()
	findings, err := Lint(source)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	return findings
}

func TestLintFindings(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			// The shape a recorded-then-compiled spec has when nobody
			// answered the "what must be true?" question: every step does
			// something, and the script proves nothing.
			name: "a script that asserts nothing",
			source: `
				atr.step(1, "Sign in", () => {
					atr.fill("#username", "demo");
					atr.click("#submit");
				});`,
			want: []string{CodeNoAssertions},
		},
		{
			name: "a step that only logs",
			source: `
				atr.step(1, "Look at the page", () => {
					atr.log("we are on the dashboard");
				});
				atr.step(2, "Check the heading", () => {
					expect(atr.text("#heading")).toBe("Welcome");
				});`,
			want: []string{CodeStepCannotFail},
		},
		{
			// exists() answers a question and never throws, so a step built
			// from it alone reports success whatever the page contains.
			name: "a step that only branches",
			source: `
				atr.step(1, "Maybe dismiss the banner", () => {
					if (atr.exists("#banner")) { atr.log("banner"); }
				});
				atr.step(2, "Check the heading", () => {
					expect(atr.text("#heading")).toBe("Welcome");
				});`,
			want: []string{CodeStepCannotFail},
		},
		{
			// A click throws when the target is gone, so the step can fail —
			// weakly, as drift rather than as a regression, but the lint is
			// not the place to argue about that.
			name: "a step that only clicks is not blocked",
			source: `
				atr.step(1, "Open the menu", () => {
					atr.click("#menu");
				});
				atr.step(2, "Check the heading", () => {
					expect(atr.text("#heading")).toBe("Welcome");
				});`,
			want: nil,
		},
		{
			name: "a short needle against the whole page",
			source: `
				atr.step(1, "Check it archived", () => {
					expect(atr.text()).toContain("archiv");
				});`,
			want: []string{CodeWeakTextMatch},
		},
		{
			name: "the same needle against an element",
			source: `
				atr.step(1, "Check it archived", () => {
					expect(atr.text("#status")).toContain("archiv");
				});`,
			want: nil,
		},
		{
			name: "a whole sentence against the whole page",
			source: `
				atr.step(1, "Check the confirmation", () => {
					expect(atr.text()).toContain("Your order has been placed");
				});`,
			want: nil,
		},
		{
			name: "markup matched by a short needle",
			source: `
				atr.step(1, "Check the badge", () => {
					expect(atr.html()).toContain("badge");
				});`,
			want: []string{CodeWeakTextMatch},
		},
		{
			name: "a fixed sleep",
			source: `
				atr.step(1, "Save", () => {
					atr.click("#save");
					atr.sleep(2000);
					expect(atr.text("#status")).toBe("Saved");
				});`,
			want: []string{CodeFixedSleep},
		},
		{
			// Spacing out attempts is what a retry loop is for; that is not a
			// guess about how long a page takes.
			name: "a sleep inside a retry is not a guess",
			source: `
				atr.step(1, "Save", () => {
					atr.retry(3, () => {
						atr.click("#save");
						atr.sleep(200);
						expect(atr.text("#status")).toBe("Saved");
					});
				});`,
			want: nil,
		},
		{
			name: "setup is a step too",
			source: `
				atr.setup("Prepare the fixture", () => {
					atr.log("nothing");
				});
				atr.step(1, "Check the heading", () => {
					expect(atr.text("#heading")).toBe("Welcome");
				});`,
			want: []string{CodeStepCannotFail},
		},
		{
			name: "expectExists counts as an assertion",
			source: `
				atr.step(1, "The dashboard is up", () => {
					atr.expectExists("#dashboard");
				});`,
			want: nil,
		},
		{
			name: "expectMissing counts as an assertion",
			source: `
				atr.step(1, "The banner is gone", () => {
					atr.expectMissing("#banner");
				});`,
			want: nil,
		},
		{
			name: "an assertion in a helper still counts",
			source: `
				function checkHeading() {
					expect(atr.text("#heading")).toBe("Welcome");
				}
				atr.step(1, "Check the heading", () => {
					checkHeading();
				});`,
			want: []string{CodeLocalHelper},
		},
		{
			// Wrapping is not something a compiler does deliberately, but it
			// is exactly what a model does when it tidies — and a step of
			// report() where report only logs still cannot fail.
			name: "a helper that only logs does not rescue a step",
			source: `
				function report() {
					atr.log("we are on the dashboard");
				}
				atr.step(1, "Look at the page", () => {
					report();
				});
				atr.step(2, "Check the heading", () => {
					expect(atr.text("#heading")).toBe("Welcome");
				});`,
			want: []string{CodeStepCannotFail, CodeLocalHelper},
		},
		{
			name: "an arrow helper is followed too",
			source: `
				const report = () => { atr.log("nothing"); };
				atr.step(1, "Look at the page", () => { report(); });
				atr.step(2, "Check", () => {
					expect(atr.text("#heading")).toBe("Welcome");
				});`,
			want: []string{CodeStepCannotFail, CodeLocalHelper},
		},
		{
			// A helper that acts is a step that can fail, so following the
			// call must not turn every wrapper into a *blocking* finding. It
			// is still reported as a helper — that is a warning about where
			// the operation lives, not a claim that the step is toothless.
			name: "a helper that acts keeps its step",
			source: `
				function openMenu() { atr.click("#menu"); }
				atr.step(1, "Open the menu", () => { openMenu(); });
				atr.step(2, "Check", () => {
					expect(atr.text("#heading")).toBe("Welcome");
				});`,
			want: []string{CodeLocalHelper},
		},
		{
			// Mutual recursion must not hang the lint. It is not blocked:
			// following the calls runs out of new names, and at the point the
			// check stops being able to tell what happens, "this step can
			// fail" is the safe answer for a finding that refuses a run. It
			// is also true — the recursion ends in a stack overflow.
			name: "recursive helpers terminate",
			source: `
				function a() { b(); }
				function b() { a(); }
				atr.step(1, "Go", () => { a(); });
				atr.step(2, "Check", () => {
					expect(atr.text("#heading")).toBe("Welcome");
				});`,
			want: []string{CodeLocalHelper, CodeLocalHelper},
		},
		{
			// A call to something declared outside the script — a shared
			// library — cannot be followed, so it is taken at its word rather
			// than assumed toothless.
			name: "an unknown callee is taken at its word",
			source: `
				atr.step(1, "Sign in", () => { signIn(); });
				atr.step(2, "Check", () => {
					expect(atr.text("#heading")).toBe("Welcome");
				});`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codes(lint(t, tt.source))
			if len(got) != len(tt.want) {
				t.Fatalf("findings = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("finding %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// A lint that is right in principle and fires on ordinary output is a lint
// that gets turned off. This is the shape the compiler actually emits.
func TestARealisticScriptLintsClean(t *testing.T) {
	findings := lint(t, `
		atr.setup("Make sure the cart is empty", () => {
			atr.navigate("/cart");
			if (atr.exists("#empty-cart")) {
				atr.click("#empty-cart");
				atr.waitForText("Your cart is empty");
			}
		});

		atr.step(1, "Search for the product", () => {
			atr.navigate("/products");
			atr.fill("#search", values.get("product_name"));
			atr.pressKey("Enter");
			atr.waitFor(".results");
		});

		atr.step(2, "Add it to the cart", () => {
			atr.click('button:has-text("Add to cart")');
			atr.waitForText("Added to cart");
		});

		atr.step(3, "The cart reflects it", () => {
			atr.click("#cart");
			expect(atr.text("#cart-count")).toBe("1");
			expect(atr.text("#cart-items")).toContain(values.get("product_name"));
			atr.expectMissing("#empty-cart-message");
		});
	`)

	if len(findings) != 0 {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s — %s", f.Code, f)
		}
	}
}

func TestBlockingSeparatesEnforcementFromAdvice(t *testing.T) {
	findings := lint(t, `
		atr.step(1, "Do nothing", () => {
			atr.log("hello");
		});
		atr.step(2, "Check something loosely", () => {
			atr.sleep(1000);
			expect(atr.text()).toContain("ok");
		});
	`)

	blocking := Blocking(findings)
	if len(blocking) != 1 || blocking[0].Code != CodeStepCannotFail {
		t.Fatalf("blocking = %v, want exactly the step that cannot fail", codes(blocking))
	}
	if len(findings) != 3 {
		t.Errorf("findings = %v, want the blocking one plus two warnings", codes(findings))
	}
}

// A finding has to name the step the way the spec does, or a report leaves the
// reader hunting through generated JavaScript for it.
func TestAFindingNamesItsStep(t *testing.T) {
	findings := lint(t, `
		atr.step(1, "Check the heading", () => {
			expect(atr.text("#heading")).toBe("Welcome");
		});
		atr.step(7, "Wait for the import", () => {
			atr.sleep(5000);
			expect(atr.text("#status")).toBe("Done");
		});
	`)

	if len(findings) != 1 {
		t.Fatalf("findings = %v, want one", codes(findings))
	}
	f := findings[0]
	if f.Step != 7 || f.StepDesc != "Wait for the import" {
		t.Errorf("finding attributed to step %d (%q), want step 7", f.Step, f.StepDesc)
	}
	if !strings.Contains(f.String(), "step 7 (Wait for the import)") {
		t.Errorf("the rendered finding does not name the step: %s", f)
	}
}

// A script that does not parse is already reported properly by the runtime as
// a script fault. Reporting it again here, worse, would help nobody.
func TestAnUnparseableScriptIsNotTheLintsProblem(t *testing.T) {
	findings, err := Lint(`atr.step(1, "x", () => {`)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if findings != nil {
		t.Errorf("findings = %v, want none", codes(findings))
	}
}

// `expect(x)` builds a matcher object and asserts nothing. Counting the bare
// call let a step of `expect(atr.text("#b"));` pass as a step that can fail —
// and that shape is exactly what a truncated or half-edited script looks like.
func TestAMatcherlessExpectIsNotAnAssertion(t *testing.T) {
	findings := lint(t, `
		atr.step(1, "Check the banner", () => {
			expect(atr.text("#banner"));
		});
	`)

	if len(Blocking(findings)) == 0 {
		t.Errorf("a script whose only assertion has no matcher was accepted: %v", codes(findings))
	}
}

// A compiled script has no legitimate reason to catch its own assertion:
// atr.retry exists for transient failures, and an assertion is deliberately
// the one kind never retried. A try/catch around one can only turn a red test
// green.
func TestASwallowedAssertionIsRefused(t *testing.T) {
	findings := lint(t, `
		atr.step(1, "Check the status", () => {
			atr.click("#submit");
			try {
				expect(atr.text("#status")).toBe("signed in");
			} catch (e) {
				atr.log("never mind");
			}
		});
	`)

	var found bool
	for _, f := range findings {
		if f.Code == CodeSwallowed {
			found = true
			if f.Severity != SeverityBlocking {
				t.Error("a swallowed assertion is only a warning")
			}
		}
	}
	if !found {
		t.Errorf("a swallowed assertion was accepted: %v", codes(findings))
	}

	// And it must not count towards the script having any assertions at all.
	var counted bool
	for _, f := range findings {
		if f.Code == CodeNoAssertions {
			counted = true
		}
	}
	if !counted {
		t.Error("the swallowed assertion still counted as the script's assertion")
	}
}

// A catch that rethrows, or fails the test itself, is not swallowing anything:
// the failure still reaches the runner, which is all the rule is about.
// Refusing those would block the legitimate shape of "add context, then let it
// through".
func TestACatchThatEscalatesIsNotSwallowing(t *testing.T) {
	sound := map[string]string{
		"rethrows": `atr.step(1, "Check", () => {
			try { expect(atr.text("#x")).toBe("ok"); } catch (e) { atr.log("x"); throw e; }
		});`,
		"fails the test itself": `atr.step(1, "Check", () => {
			try { atr.click("#x"); } catch (e) { atr.fail("could not click"); }
		});`,
		"cleans up, then asserts": `atr.step(1, "Check", () => {
			try { atr.click("#optional"); } catch (e) {}
			expect(atr.text("#y")).toBe("ok");
		});`,
	}

	for name, source := range sound {
		t.Run(name, func(t *testing.T) {
			for _, f := range lint(t, source) {
				if f.Code == CodeSwallowed || f.Code == CodeNoAssertions {
					t.Errorf("a sound try/catch was refused: %s", f)
				}
			}
		})
	}
}

// Three swallowed assertions in one try are one mistake, and iterating the set
// of offending nodes would order the findings differently on every run, since
// Go randomises map iteration.
func TestFindingsComeOutInAStableOrder(t *testing.T) {
	const source = `atr.step(1, "Check", () => {
		try {
			expect(atr.text("#x")).toBe("1");
			expect(atr.text("#y")).toBe("2");
			expect(atr.text("#z")).toBe("3");
		} catch (e) {}
	});`

	var first []string
	for i := range 30 {
		got := codes(lint(t, source))
		if i == 0 {
			first = got
			continue
		}
		if len(got) != len(first) {
			t.Fatalf("run %d produced %v, first run produced %v", i, got, first)
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d produced %v, first run produced %v", i, got, first)
			}
		}
	}

	// One per step, not one per assertion.
	swallowed := 0
	for _, code := range first {
		if code == CodeSwallowed {
			swallowed++
		}
	}
	if swallowed != 1 {
		t.Errorf("reported %d swallowed-assertion findings for one try, want 1", swallowed)
	}
}

// A try with no catch does not swallow anything — the assertion still
// propagates, and refusing it would block a legitimate finally.
func TestATryWithoutACatchIsFine(t *testing.T) {
	findings := lint(t, `
		atr.step(1, "Check the status", () => {
			try {
				expect(atr.text("#status")).toBe("signed in");
			} finally {
				atr.log("done");
			}
		});
	`)

	for _, f := range findings {
		if f.Code == CodeSwallowed || f.Code == CodeNoAssertions {
			t.Errorf("a try/finally was treated as swallowing: %v", codes(findings))
		}
	}
}

// A call whose callee has no name to read — `new K().m()`, `handlers[0]()` —
// used to count as harmless, which blocked a step that asserted through a
// class method. This finding refuses a run: when the check cannot tell what a
// call does, the safe answer is that the step can fail, not that it cannot.
func TestAnUnreadableCalleeDoesNotBlockAStep(t *testing.T) {
	sources := map[string]string{
		"a method on a new expression": `
			class Checks { heading() { expect(atr.text("#heading")).toBe("Welcome"); } }
			atr.step(1, "Check", () => { new Checks().heading(); });`,
		"an indexed callee": `
			const checks = [() => { expect(atr.text("#heading")).toBe("Welcome"); }];
			atr.step(1, "Check", () => { checks[0](); });`,
		"a method on a call result": `
			function checks() { return { heading() { atr.expectExists("#heading"); } }; }
			atr.step(1, "Check", () => { checks().heading(); });`,
	}

	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			for _, f := range lint(t, source) {
				if f.Code == CodeStepCannotFail {
					t.Errorf("a step whose callee could not be read was blocked: %s", f)
				}
			}
		})
	}
}

// A wait followed by an assertion about the same thing is one intent split in
// two, and the split hands the diagnosis to whichever hits the wall first —
// always the wait. So a page that stops reaching the state is reported as a
// timeout, retried, and in CI read as infrastructure rather than as the
// feature being broken.
func TestWaitThenAssertIsReported(t *testing.T) {
	findings := lint(t, `
		atr.step(1, "Check out", () => {
			atr.click("#checkout");
			atr.waitForText("Order placed", {timeout: 5000});
			expect(atr.text("#message")).toBe("Order placed");
		});
	`)

	var found bool
	for _, f := range findings {
		if f.Code == CodeWaitThenAssert {
			found = true
			if f.Severity != SeverityWarn {
				t.Error("wait-then-assert blocks a run; the script does still fail, just under the wrong name")
			}
			if !strings.Contains(f.Message, "expectText") {
				t.Errorf("the finding does not name the call that fixes it: %s", f)
			}
		}
	}
	if !found {
		t.Errorf("the split was not reported: %v", codes(findings))
	}
}

// The single call is the whole point, and reporting it would be absurd.
func TestExpectTextIsNotReported(t *testing.T) {
	for _, f := range lint(t, `
		atr.step(1, "Check out", () => {
			atr.click("#checkout");
			atr.expectText("#message", "Order placed", {timeout: 5000});
		});
	`) {
		if f.Code == CodeWaitThenAssert {
			t.Errorf("the one-call form was reported: %s", f)
		}
	}
}

// Waiting on the way to something else is not the same mistake: the wait
// reaches a state, and the assertion is about something different.
func TestWaitingOnTheWayToSomethingElseIsFine(t *testing.T) {
	for _, f := range lint(t, `
		atr.step(1, "Check out", () => {
			atr.click("#checkout");
			atr.waitForText("Loading");
			expect(atr.text("#message")).toBe("Order placed");
		});
	`) {
		if f.Code == CodeWaitThenAssert {
			t.Errorf("a wait for a different state was reported: %s", f)
		}
	}
}

// Asserting and then waiting is a different shape — odd, but not the split
// this rule warns about, and reporting it under this name would send the
// reader looking for a mistake that is not there.
func TestAssertingBeforeWaitingIsNotTheSplit(t *testing.T) {
	for _, f := range lint(t, `
		atr.step(1, "Check", () => {
			expect(atr.text("#message")).toBe("Order placed");
			atr.waitForText("Order placed");
		});
	`) {
		if f.Code == CodeWaitThenAssert {
			t.Errorf("an assertion followed by a wait was reported as the split: %s", f)
		}
	}
}

// The two calls have to be in the same step to be the same intent. A wait in
// one step and an assertion in another are a sequence, not a split.
func TestAWaitAndAnAssertionInDifferentStepsAreNotTheSplit(t *testing.T) {
	for _, f := range lint(t, `
		atr.step(1, "Get there", () => {
			atr.waitForText("Order placed");
			atr.click("#next");
		});
		atr.step(2, "Check", () => {
			expect(atr.text("#message")).toBe("Order placed");
		});
	`) {
		if f.Code == CodeWaitThenAssert {
			t.Errorf("a wait and an assertion in different steps were reported: %s", f)
		}
	}
}

// The lint refuses a script that cannot fail, and expectText is the call the
// compile prompt now prescribes — so a lint that does not recognise it as an
// assertion blocks every script written the way we asked for. A blocking
// finding on the recommended form is the worst shape a rule can have.
func TestExpectTextCountsAsAnAssertion(t *testing.T) {
	findings := lint(t, `
		atr.step(1, "Check out", () => {
			atr.click("#checkout");
			atr.expectText("#message", "Order placed", {timeout: 5000});
		});
	`)

	for _, f := range findings {
		if f.Severity == SeverityBlocking {
			t.Errorf("a script the prompt prescribes was refused: %s — %s", f.Code, f)
		}
	}
}

// The weak-match rule has to cover both phrasings, or it is bypassed by the
// one the prompt prescribes.
func TestAShortNeedleAgainstTheWholePageIsWeakEitherWay(t *testing.T) {
	forms := map[string]string{
		"through a matcher":  `atr.step(1, "Check", () => { expect(atr.text()).toContain("archiv"); });`,
		"through expectText": `atr.step(1, "Check", () => { atr.expectText("body", "archiv", {contains: true}); });`,
	}

	for name, source := range forms {
		t.Run(name, func(t *testing.T) {
			var found bool
			for _, f := range lint(t, source) {
				if f.Code == CodeWeakTextMatch {
					found = true
				}
			}
			if !found {
				t.Error("a short substring against the whole page was not reported")
			}
		})
	}

	// Naming the element is the fix, and must not be reported.
	for _, f := range lint(t, `atr.step(1, "Check", () => { atr.expectText("#status", "archiv", {contains: true}); });`) {
		if f.Code == CodeWeakTextMatch {
			t.Errorf("a substring against a named element was reported: %s", f)
		}
	}
}

// A wait for page text and an assertion about the URL share a literal and mean
// entirely different things. Reporting that pair sends the reader looking for
// a split that is not there.
func TestAWaitAndAnAssertionAboutDifferentThingsAreNotTheSplit(t *testing.T) {
	for _, f := range lint(t, `
		atr.step(1, "Go to the dashboard", () => {
			atr.waitForText("Dashboard");
			expect(atr.url()).toContain("Dashboard");
		});
	`) {
		if f.Code == CodeWaitThenAssert {
			t.Errorf("a wait and a URL assertion sharing a literal were reported as one intent: %s", f)
		}
	}
}

// The prompt forbids hardcoding inputs, so a compiled script writes the same
// values.get on both sides of the split — inline, or hoisted into a local
// first. A rule that only sees string literals misses the scripts most likely
// to have the problem.
func TestTheSplitIsFoundThroughAnInput(t *testing.T) {
	forms := map[string]string{
		"inline": `atr.step(1, "Check", () => {
			atr.waitForText(values.get("confirmation"));
			expect(atr.text("#message")).toBe(values.get("confirmation"));
		});`,
		"hoisted into a local": `atr.step(1, "Check", () => {
			const msg = values.get("confirmation");
			atr.waitForText(msg, {timeout: 5000});
			expect(atr.text("#message")).toBe(msg);
		});`,
	}

	for name, source := range forms {
		t.Run(name, func(t *testing.T) {
			var found bool
			for _, f := range lint(t, source) {
				if f.Code != CodeWaitThenAssert {
					continue
				}
				found = true
				if !strings.Contains(f.Message, "confirmation") {
					t.Errorf("the finding does not name what was waited for: %s", f)
				}
			}
			if !found {
				t.Error("the split was not reported")
			}
		})
	}
}

// The presence half of the same mistake. The wait fails first, exactly as it
// does for text, so an element that never appears is reported as a timeout
// rather than as the application being wrong.
func TestWaitForThenAssertExistsIsReported(t *testing.T) {
	var found bool
	for _, f := range lint(t, `
		atr.step(1, "The message appears", () => {
			atr.click("#send");
			atr.waitFor("#message", {timeout: 5000});
			expect(atr.exists("#message")).toBeTruthy();
		});
	`) {
		if f.Code == CodeWaitThenAssert {
			found = true
			if !strings.Contains(f.Message, "expectExists") {
				t.Errorf("the finding does not name the call that fixes it: %s", f)
			}
		}
	}
	if !found {
		t.Error("the presence form of the split was not reported")
	}
}

// Waiting for one thing and asserting another is not the split, in the
// presence form either.
func TestWaitingForADifferentTargetIsNotTheSplit(t *testing.T) {
	for _, f := range lint(t, `
		atr.step(1, "Send", () => {
			atr.waitFor("#composer");
			expect(atr.exists("#message")).toBeTruthy();
		});
	`) {
		if f.Code == CodeWaitThenAssert {
			t.Errorf("a wait for a different target was reported: %s", f)
		}
	}
}

// The case this comes from: a spec said "Open the front page using
// openFirstPost() from the shared library", no library declared that, and the
// compiler wrote a local function of that name and called it. The script
// passed. Nothing reported that the sharing the spec described did not exist.
func TestALocalHelperIsReported(t *testing.T) {
	findings, err := Lint(`function openFirstPost() {
  atr.navigate("/");
  atr.click("article h2 a");
}
atr.step(1, "Open the front page using openFirstPost() from the shared library", () => {
  openFirstPost();
});
atr.step(2, "Confirm the post opened", () => {
  atr.expectExists("article h1");
});`)
	if err != nil {
		t.Fatal(err)
	}

	var found *Finding
	for i := range findings {
		if findings[i].Code == CodeLocalHelper {
			found = &findings[i]
		}
	}
	if found == nil {
		t.Fatalf("a locally declared operation was not reported, got %v", codes(findings))
	}
	if !strings.Contains(found.Message, "openFirstPost") {
		t.Errorf("the finding does not name the function: %s", found.Message)
	}

	// A warning, not a blocker. It cannot make a broken application pass, and
	// scripts compiled before a library existed factored things for
	// themselves; blocking those would be a migration nobody asked for.
	if found.Severity != SeverityWarn {
		t.Errorf("severity = %s, want %s", found.Severity, SeverityWarn)
	}
	if len(Blocking(findings)) > 0 {
		t.Errorf("a local helper blocked the run: %v", Blocking(findings))
	}
}

// A const holding an arrow function is the same thing written differently.
func TestALocalHelperBoundToAConstIsReported(t *testing.T) {
	findings, err := Lint(`const openHome = () => { atr.navigate("/"); };
atr.step(1, "Open the home page", () => {
  openHome();
  atr.expectExists("main");
});`)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(codes(findings), CodeLocalHelper) {
		t.Errorf("an arrow function bound to a const was not reported, got %v", codes(findings))
	}
}

// The callbacks a script is made of are not helpers. If this fired on them it
// would fire on every script ever compiled, which is how a lint gets turned
// off.
func TestStepAndRetryCallbacksAreNotHelpers(t *testing.T) {
	findings, err := Lint(`const PATH = "/";
atr.setup(() => { atr.navigate(PATH); });
atr.step(1, "Search", () => {
  atr.retry(() => { atr.click("#go"); });
  atr.expectExists("#results");
});`)
	if err != nil {
		t.Fatal(err)
	}
	if contains(codes(findings), CodeLocalHelper) {
		t.Errorf("a step or retry callback was reported as a helper: %v", findings)
	}
}
