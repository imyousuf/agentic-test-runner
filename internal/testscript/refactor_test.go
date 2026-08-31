package testscript

import (
	"fmt"
	"strings"
	"testing"
)

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

// A hoisted function replaces a contiguous block inside one step. Operations
// either side of an assertion cannot be gathered into one without carrying the
// assertion along, which extraction may never do — so proposing them buys a
// refusal, one model call at a time.
func TestAnAssertionBreaksARun(t *testing.T) {
	scripts := map[string]string{
		"a.test.js": `atr.step(1, "x", () => {
  atr.navigate("/tags/");
  atr.expectExists("a.tag");
  atr.click("a.tag");
});`,
		"b.test.js": `atr.step(1, "x", () => {
  atr.navigate("/tags/");
  atr.expectExists("a.tag");
  atr.click("a.tag");
});`,
	}

	found, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("operations split by an assertion were offered as one hoistable run: %+v", found)
	}
}

// A step boundary breaks a run for the same reason.
func TestAStepBoundaryBreaksARun(t *testing.T) {
	scripts := map[string]string{
		"a.test.js": `atr.step(1, "x", () => { atr.navigate("/tags/"); });
atr.step(2, "y", () => { atr.click("a.tag"); atr.expectExists("#list"); });`,
		"b.test.js": `atr.step(1, "x", () => { atr.navigate("/tags/"); });
atr.step(2, "y", () => { atr.click("a.tag"); atr.expectExists("#list"); });`,
	}

	found, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("operations in different steps were offered as one hoistable run: %+v", found)
	}
}

// Two compiles of the same journey never produce identical operations — one
// scopes a selector to the main region and the other does not — and they
// interleave it differently. Requiring identical text, or adjacency, finds
// nothing on exactly the scripts this exists to serve.
func TestTheSameJourneyWrittenTwoWaysStillMatches(t *testing.T) {
	scripts := map[string]string{
		"a.test.js": `atr.step(1, "Open the tag", () => {
  atr.navigate(TAGS_PATH);
  atr.click('a[href$="/tags/' + TAG_SLUG + '"]');
  atr.waitFor(POST_LINKS, {timeout: 10000});
  atr.expectExists(POST_LINKS);
});`,
		"b.test.js": `atr.step(1, "Open the tag", () => {
  atr.navigate(TAGS_PATH);
  atr.click('div[role="main"] a[href$="/tags/' + TAG_SLUG + '"]');
  atr.waitFor('div[role="main"] ul li a', {timeout: 5000});
  atr.expectExists("#list");
});`,
	}

	found, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("the same journey written two ways was not matched: %+v", found)
	}
	// Two: the navigate and the click. Both are written differently in the two
	// scripts and both still match, because each shares something real — the
	// path constant, the tag slug.
	//
	// Not the wait. One waits for POST_LINKS and the other for
	// 'div[role="main"] ul li a', which have no token in common and are not
	// visibly the same element; matching them would be a guess. This asserted
	// three until the options bag stopped counting as something in common —
	// the two waits shared the key "timeout" and nothing else, which is the
	// same accident that had unrelated scripts naming each other.
	if len(found[0].Steps) != 2 {
		t.Errorf("matched %d operations, want the navigate and the click: %v",
			len(found[0].Steps), found[0].Steps)
	}
}

// Sharing a call name is not sharing an operation. Two scripts that both click
// something, with nothing in common about what, are not duplicating anything.
func TestTheSameCallOnDifferentThingsIsNotAnOverlap(t *testing.T) {
	scripts := map[string]string{
		"a.test.js": `atr.step(1, "x", () => {
  atr.click("#accept-cookies");
  atr.fill("#search", "widgets");
  atr.expectExists("#results");
});`,
		"b.test.js": `atr.step(1, "x", () => {
  atr.click("#open-menu");
  atr.fill("#email", "someone@example.com");
  atr.expectExists("#sent");
});`,
	}

	found, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("two unrelated journeys were reported as shared: %+v", found)
	}
}

// Matching assertions by text and step is not enough on its own. Neither of
// these changes a character of the assertion, both leave the script passing,
// and both mean it has stopped checking.
func TestARewriteMayNotPutAnAssertionOutOfReach(t *testing.T) {
	const orig = `atr.step(1, "Sign in", () => {
  atr.navigate("/login");
  atr.click("#go");
  atr.expectExists("#dashboard");
});`

	tests := []struct {
		name    string
		rewrite string
	}{
		{"guarded by a condition", `atr.step(1, "Sign in", () => {
  signIn();
  if (false) { atr.expectExists("#dashboard"); }
});`},
		{"behind an early return", `atr.step(1, "Sign in", () => {
  signIn();
  return;
  atr.expectExists("#dashboard");
});`},
		{"short-circuited away", `atr.step(1, "Sign in", () => {
  signIn();
  atr.exists("#x") && atr.expectExists("#dashboard");
});`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, why, err := AssertionsUnchanged(orig, tt.rewrite)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				t.Fatal("a rewrite that put the assertion out of reach was accepted")
			}
			if !strings.Contains(why, "control flow") {
				t.Errorf("the reason does not say what was added: %s", why)
			}
		})
	}
}

// The guard check must not refuse the hoists it exists to allow. This is the
// shape ATR actually produced against a live site: a run of operations
// replaced by one call, and nothing else touched.
func TestAnOrdinaryHoistIsNotMistakenForAGuard(t *testing.T) {
	const orig = `const TAG = values.get("tag_name");
atr.step(1, "Open the tags page and follow the tag", () => {
  atr.navigate(values.get("tags_path", "/tags/"));
  atr.waitFor('a[href$="/tags/' + TAG + '"]', { timeout: 15000, visible: true });
  atr.click('a[href$="/tags/' + TAG + '"]');
});
atr.step(2, "Confirm", () => {
  atr.expectExists('div[role="main"]');
});`
	const hoisted = `const TAG = values.get("tag_name");
atr.step(1, "Open the tags page and follow the tag", () => {
  openTagsPage(values.get("tags_path", "/tags/"), TAG);
  openTagPage(TAG);
});
atr.step(2, "Confirm", () => {
  atr.expectExists('div[role="main"]');
});`

	ok, why, err := AssertionsUnchanged(orig, hoisted)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("an ordinary hoist was refused: %s", why)
	}
}

// Carrying a branch into the library is a hoist, not a weakening: the step has
// fewer guards afterwards, and the assertion is where it was.
func TestAHoistMayTakeABranchWithIt(t *testing.T) {
	const orig = `atr.step(1, "Dismiss the banner if it is there", () => {
  if (atr.exists("#banner")) { atr.click("#close"); }
  atr.expectExists("#main");
});`
	const hoisted = `atr.step(1, "Dismiss the banner if it is there", () => {
  dismissBanner();
  atr.expectExists("#main");
});`

	ok, why, err := AssertionsUnchanged(orig, hoisted)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("a hoist that carried its branch into the library was refused: %s", why)
	}
}

// Every wait in every script carries the same options. Counting the keys of an
// options bag made any two waits share a token, so two scripts waiting for
// entirely different things were reported as performing the same operation —
// each naming a script that did not contain the operations printed under it,
// and each costing a model call to have the proposal declined.
func TestAnOptionsBagIsNotSomethingInCommon(t *testing.T) {
	scripts := map[string]string{
		"tags.test.js": `atr.step(1, "Open a tag", () => {
  atr.waitFor('a[href$="/tags/rest"]', { timeout: 15000, visible: true });
  atr.click('a[href$="/tags/rest"]');
});`,
		"post.test.js": `atr.step(1, "Open a post", () => {
  atr.waitFor(".post-link", { timeout: 15000, visible: true });
  atr.click(".post-link");
});`,
	}

	overlaps, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) != 0 {
		t.Errorf("two scripts waiting for different things were called the same operation: %v",
			overlaps[0].Steps)
	}
}

// The values inside an options bag are still worth matching on: which secret a
// fill uses is exactly the kind of thing two scripts genuinely share.
func TestAValueInsideAnOptionsBagStillCounts(t *testing.T) {
	scripts := map[string]string{
		"a.test.js": `atr.step(1, "Sign in", () => {
  atr.fill("#user", "bob");
  atr.fillSecret("#pass", { ref: "app_password" });
});`,
		"b.test.js": `atr.step(1, "Sign in again", () => {
  atr.fill("#user", "bob");
  atr.fillSecret("#password", { ref: "app_password" });
});`,
	}

	overlaps, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) == 0 {
		t.Error("two sign-ins using the same credential were not seen as shared")
	}
}

// Comparing scripts two at a time is how a shared run is found, but it is not
// how one should be reported. A journey that six specs perform comes back as
// fifteen pairs, each describing the same thing and naming two of the six — a
// wall of output for a person, and for the model the same question asked
// fifteen times.
func TestOnePairPerSequenceNotPerPairOfScripts(t *testing.T) {
	journey := `atr.step(1, "Sign in", () => {
  atr.navigate(LOGIN_PATH);
  atr.fill("#user", USERNAME);
  atr.click("#submit");
});
atr.step(2, "Check %d", () => {
  atr.expectExists("#dash%d");
});`

	scripts := map[string]string{}
	for i := range 6 {
		scripts[fmt.Sprintf("s%d.test.js", i)] = fmt.Sprintf(journey, i, i)
	}

	overlaps, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) != 1 {
		t.Fatalf("six scripts sharing one journey produced %d overlaps, want 1", len(overlaps))
	}
	if len(overlaps[0].Scripts) != 6 {
		t.Errorf("the overlap names %d scripts, want all six that perform it: %v",
			len(overlaps[0].Scripts), overlaps[0].Scripts)
	}
}

// Grouping must not merge sequences that differ.
func TestDifferentSequencesStayApart(t *testing.T) {
	scripts := map[string]string{
		"a.test.js": `atr.step(1, "Sign in", () => {
  atr.navigate(LOGIN_PATH);
  atr.click("#submit");
});`,
		"b.test.js": `atr.step(1, "Sign in", () => {
  atr.navigate(LOGIN_PATH);
  atr.click("#submit");
});`,
		"c.test.js": `atr.step(1, "Search", () => {
  atr.fill("#q", QUERY);
  atr.click("#find");
});`,
		"d.test.js": `atr.step(1, "Search", () => {
  atr.fill("#q", QUERY);
  atr.click("#find");
});`,
	}

	overlaps, err := FindOverlaps(scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) != 2 {
		t.Fatalf("two distinct journeys produced %d overlaps, want 2: %+v", len(overlaps), overlaps)
	}
	for _, o := range overlaps {
		if len(o.Scripts) != 2 {
			t.Errorf("a journey performed by two scripts names %d: %v", len(o.Scripts), o.Scripts)
		}
	}
}
