package testscript

import "testing"

const beforeRefactor = `
atr.step(1, "Sign in", () => {
  atr.navigate("/login");
  atr.fill("#username", values.get("username"));
  atr.click("#submit");
  atr.expectExists("#dashboard");
});
atr.step(2, "Check the greeting", () => {
  expect(atr.text("#greeting")).toBe("Welcome back");
});
`

// The refactor this feature exists to perform: the sign-in steps move into a
// shared operation, the assertions stay exactly where they were.
func TestHoistingAnOperationLeavesTheClaimsIntact(t *testing.T) {
	after := `
atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  atr.expectExists("#dashboard");
});
atr.step(2, "Check the greeting", () => {
  expect(atr.text("#greeting")).toBe("Welcome back");
});
`
	ok, why, err := AssertionsUnchanged(beforeRefactor, after)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("a sound extraction was rejected: %s", why)
	}
}

// Everything below is a way a refactor can quietly weaken a suite. Running
// the rewritten script catches none of them: each one still passes.
func TestARefactorMayNotTouchTheClaims(t *testing.T) {
	tests := map[string]string{
		"an assertion is dropped": `
atr.step(1, "Sign in", () => { signIn(); atr.expectExists("#dashboard"); });
atr.step(2, "Check the greeting", () => { atr.log("fine"); });`,

		"an assertion is loosened": `
atr.step(1, "Sign in", () => { signIn(); atr.expectExists("#dashboard"); });
atr.step(2, "Check the greeting", () => {
  expect(atr.text("#greeting")).toContain("Welcome");
});`,

		"an expected value changes": `
atr.step(1, "Sign in", () => { signIn(); atr.expectExists("#dashboard"); });
atr.step(2, "Check the greeting", () => {
  expect(atr.text("#greeting")).toBe("Welcome");
});`,

		"an assertion moves into the library's step": `
atr.step(1, "Sign in", () => {
  signIn();
  atr.expectExists("#dashboard");
  expect(atr.text("#greeting")).toBe("Welcome back");
});
atr.step(2, "Check the greeting", () => { atr.log("moved"); });`,

		"an assertion is added": `
atr.step(1, "Sign in", () => { signIn(); atr.expectExists("#dashboard"); });
atr.step(2, "Check the greeting", () => {
  expect(atr.text("#greeting")).toBe("Welcome back");
  atr.expectExists("#logout");
});`,

		"the target of an assertion changes": `
atr.step(1, "Sign in", () => { signIn(); atr.expectExists("#header"); });
atr.step(2, "Check the greeting", () => {
  expect(atr.text("#greeting")).toBe("Welcome back");
});`,
	}

	for name, after := range tests {
		t.Run(name, func(t *testing.T) {
			ok, why, err := AssertionsUnchanged(beforeRefactor, after)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				t.Error("a refactor that changed what the test claims was accepted")
			}
			if why == "" {
				t.Error("the rejection does not say what changed")
			}
		})
	}
}

// Reindenting is not rewriting. An extraction changes the shape of the code
// around an assertion, and rejecting that would reject every real extraction.
func TestReformattingIsNotAChange(t *testing.T) {
	after := `
atr.step(1, "Sign in", () => {
        signIn(values.get("username"));
        atr.expectExists(
                "#dashboard"
        );
});
atr.step(2, "Check the greeting", () => {
        expect(atr.text("#greeting"))
                .toBe("Welcome back");
});
`
	ok, why, err := AssertionsUnchanged(beforeRefactor, after)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("reindenting was treated as a change: %s", why)
	}
}

// A rewrite that does not parse is not a rewrite worth checking further.
func TestAnUnparseableRewriteIsRejected(t *testing.T) {
	if _, _, err := AssertionsUnchanged(beforeRefactor, `atr.step(1, "x", () => {`); err == nil {
		t.Error("a rewrite that does not parse was accepted")
	}
}

// The signature has to name the step, or moving a claim from one step to
// another reads as no change at all.
func TestTheSignatureNamesTheStep(t *testing.T) {
	sig, err := AssertionSignature(beforeRefactor)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 2 {
		t.Fatalf("signature = %v, want one entry per assertion", sig)
	}
	for i, want := range []string{"step 1:", "step 2:"} {
		if len(sig[i]) < len(want) || sig[i][:len(want)] != want {
			t.Errorf("entry %d = %q, want it to name its step", i, sig[i])
		}
	}
}

// The whole reason formatting is stripped outside string literals and nowhere
// else: "a  b" and "a b" are claims about different pages, and a check that
// forgives the difference is not a guarantee.
func TestWhitespaceInsideAStringIsPartOfTheClaim(t *testing.T) {
	before := `atr.step(1, "x", () => { expect(atr.text("#m")).toBe("Order  placed"); });`
	after := `atr.step(1, "x", () => { expect(atr.text("#m")).toBe("Order placed"); });`

	ok, why, err := AssertionsUnchanged(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("collapsing a double space inside an expected value was accepted as no change")
	}
	if why == "" {
		t.Error("the rejection does not say what changed")
	}

	// And the same text reflowed around is still the same claim.
	same := "atr.step(1, \"x\", () => {\n  expect( atr.text(\"#m\") )\n    .toBe(\"Order  placed\");\n});"
	ok, why, err = AssertionsUnchanged(before, same)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("reflowing the same claim was rejected: %s", why)
	}
}

// Escapes must not end the literal early, or everything after a quoted quote
// is compared as if it were code.
func TestAnEscapedQuoteDoesNotEndTheLiteral(t *testing.T) {
	before := `atr.step(1, "x", () => { expect(atr.text("#m")).toBe("say \"hi\"  now"); });`
	after := `atr.step(1, "x", () => { expect(atr.text("#m")).toBe("say \"hi\" now"); });`

	ok, _, err := AssertionsUnchanged(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("a change inside an escaped string was accepted as no change")
	}
}

const signInSteps = `
  atr.navigate("/login");
  atr.fill("#username", values.get("username"));
  atr.fillSecret("#password", {ref: "app_password"});
  atr.click("#submit");
  atr.waitFor("#dashboard");
`

// The case the feature exists for: two specs that both sign in, each having
// re-derived the same five operations at compile time.
func TestOverlappingOperationsAreFound(t *testing.T) {
	scripts := map[string]string{
		"login.test.js": `atr.step(1, "Sign in", () => {` + signInSteps + `
  atr.expectExists("#dashboard");
});`,
		"checkout.test.js": `atr.step(1, "Sign in", () => {` + signInSteps + `});
atr.step(2, "Check out", () => {
  atr.click("#checkout");
  expect(atr.text("#status")).toBe("Order placed");
});`,
	}

	found, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d overlaps, want the one sign-in sequence: %+v", len(found), found)
	}

	o := found[0]
	if len(o.Steps) != 5 {
		t.Errorf("the overlap is %d operations, want all five of the sign-in: %v", len(o.Steps), o.Steps)
	}
	if len(o.Scripts) != 2 {
		t.Errorf("scripts = %v, want both", o.Scripts)
	}
	if o.Steps[0] != `atr.navigate("/login")` {
		t.Errorf("first operation = %q", o.Steps[0])
	}
}

// A six-operation overlap must be reported once, not as every window inside
// it — otherwise one duplication reads as fifteen findings.
func TestAnOverlapIsReportedAtItsFullLength(t *testing.T) {
	scripts := map[string]string{
		"a.test.js": `atr.step(1, "x", () => {` + signInSteps + `atr.expectExists("#d"); });`,
		"b.test.js": `atr.step(1, "x", () => {` + signInSteps + `atr.expectExists("#d"); });`,
	}

	found, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Errorf("one duplication reported as %d findings", len(found))
	}
}

// Two scripts that merely navigate are not duplicating an operation worth
// naming, and reporting them would make the feature noise on every project.
func TestATrivialSimilarityIsNotAnOverlap(t *testing.T) {
	scripts := map[string]string{
		"a.test.js": `atr.step(1, "x", () => { atr.navigate("/"); atr.expectExists("#a"); });`,
		"b.test.js": `atr.step(1, "x", () => { atr.navigate("/"); atr.expectExists("#b"); });`,
	}

	found, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("a single shared navigate was reported as an overlap: %+v", found)
	}
}

// Asserting the same thing is not duplicating an operation, and an assertion
// is the one thing extraction may never move — so it must not even be
// considered part of the shared sequence.
func TestSharedAssertionsAreNotAnOverlap(t *testing.T) {
	scripts := map[string]string{
		"a.test.js": `atr.step(1, "x", () => {
  atr.click("#a");
  expect(atr.text("#m")).toBe("ok");
  atr.expectExists("#done");
});`,
		"b.test.js": `atr.step(1, "x", () => {
  atr.click("#b");
  expect(atr.text("#m")).toBe("ok");
  atr.expectExists("#done");
});`,
	}

	found, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("two scripts sharing assertions were reported as sharing an operation: %+v", found)
	}
}

// A script that does not parse is not a reason to stop looking at the others.
func TestAnUnparseableSiblingIsSkipped(t *testing.T) {
	scripts := map[string]string{
		"broken.test.js": `atr.step(1, "x", () => {`,
		"a.test.js":      `atr.step(1, "x", () => {` + signInSteps + `atr.expectExists("#d"); });`,
		"b.test.js":      `atr.step(1, "x", () => {` + signInSteps + `atr.expectExists("#d"); });`,
	}

	found, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatalf("one unparseable script stopped the whole search: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("found %d overlaps, want the one between the two sound scripts", len(found))
	}
}

// Nothing to extract must cost nothing: a single script cannot overlap.
func TestOneScriptHasNothingToShare(t *testing.T) {
	found, err := FindOverlaps(map[string]string{
		"a.test.js": `atr.step(1, "x", () => {` + signInSteps + `atr.expectExists("#d"); });`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("a lone script overlapped with itself: %+v", found)
	}
}
