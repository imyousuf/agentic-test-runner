package testscript

import (
	"strings"
	"testing"
)

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
