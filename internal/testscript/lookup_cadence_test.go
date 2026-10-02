package testscript

import (
	"testing"
)

// A read takes no timeout of its own and gets the lookup's default three
// seconds. With rod's backoff the last look inside those three seconds was at
// about 1.4s, so a script that read something which rendered two seconds after
// the click that caused it failed as not_found — drift, a repair, a model
// call — on a page that was merely unhurried.
func TestCallsFindATargetThatArrivesLateInTheirBudget(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"text, css", `expect(atr.text("#ready")).toBe("Ready now");`},
		{"text, xpath", `expect(atr.text('//button[@id="ready"]')).toBe("Ready now");`},
		{"text, has-text", `expect(atr.text('button:has-text("Ready now")')).toBe("Ready now");`},
		{"scroll", `atr.scroll({selector: "#ready", y: 0});`},
		{"waitForText inside three seconds", `atr.waitForText("Ready now", {timeout: 3000});`},
		{"waitFor inside three seconds", `atr.waitFor("#ready", {timeout: 3000});`},
		{"expectExists inside three seconds", `atr.expectExists('button:has-text("Ready now")', {timeout: 3000});`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, `
				atr.step(1, "Use a button that renders 2s from now", () => {
					`+buttonLater("ready", "Ready now", 2000)+`
					`+tt.body+`
				});
			`)
			if !res.Passed {
				t.Fatalf("a button that rendered inside the budget was missed: %v", res.Failure)
			}
		})
	}
}
