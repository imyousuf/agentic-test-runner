package remote

import (
	"testing"

	"github.com/ysmood/gson"
)

func event(fields map[string]any) gson.JSON { return gson.New(fields) }

/*
The timeline used to be fed by the live view's own input handlers, so a click
counted only if a human moved a mouse in the live view. Every agent-driven
recording showed navigations and errors and not one interaction. These pin the
translation that replaced it.
*/
func TestTimelineActionTranslatesWhatHappened(t *testing.T) {
	cases := []struct {
		name   string
		in     map[string]any
		want   Action
		wanted bool
	}{
		{
			"a click names what was clicked",
			map[string]any{"type": "click", "innerText": "Sign in", "selector": "#submit"},
			Action{Kind: "click", Detail: "Sign in"}, true,
		},
		{
			"with no text it falls back to the selector",
			map[string]any{"type": "click", "innerText": "", "selector": "#submit"},
			Action{Kind: "click", Detail: "#submit"}, true,
		},
		{
			"a double click is still a click",
			map[string]any{"type": "double_click", "innerText": "Open", "selector": "a"},
			Action{Kind: "click", Detail: "Open"}, true,
		},
		{
			"a named combination keeps its name",
			map[string]any{"type": "keypress", "value": "Ctrl+A"},
			Action{Kind: "key", Detail: "Ctrl+A"}, true,
		},
		// A scroll is continuous and would bury every other mark; a navigation
		// is already on the timeline from the page poll.
		{"a scroll is dropped", map[string]any{"type": "scroll"}, Action{}, false},
		{"a navigation is dropped", map[string]any{"type": "navigate"}, Action{}, false},
		{"an unknown kind is dropped", map[string]any{"type": "wat"}, Action{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := timelineAction(event(tc.in))
			if ok != tc.wanted {
				t.Fatalf("kept = %v, want %v", ok, tc.wanted)
			}
			if ok && got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

/*
A mark says that somebody typed. It must never say what.

The capture script masks password fields, but that is not enough to rely on: a
passphrase typed into a field nobody marked as a password is typed exactly like
a search term. So the value never leaves the script.
*/
func TestATypeMarkCarriesNoValue(t *testing.T) {
	for _, kind := range []string{"fill", "select_option"} {
		got, ok := timelineAction(event(map[string]any{
			"type":      kind,
			"value":     "hunter2",
			"innerText": "Password",
			"selector":  "#password",
		}))
		if !ok {
			t.Fatalf("%s produced no mark", kind)
		}
		if got.Kind != "type" {
			t.Errorf("%s became %q, want type", kind, got.Kind)
		}
		if got.Detail != "" {
			t.Errorf("%s leaked %q onto the timeline", kind, got.Detail)
		}
	}
}

// Two consumers can capture from one page, so the bindings must differ or
// whichever attaches second silently replaces the first.
func TestTheTimelineBindingIsItsOwn(t *testing.T) {
	if timelineBinding == "__atrRecordEvent" {
		t.Fatal("the timeline shares a binding with \"atr browser record\"")
	}
}
