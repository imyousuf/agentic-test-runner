package remote

import (
	"strings"

	"github.com/go-rod/rod"
	"github.com/ysmood/gson"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
)

/*
timelineBinding is the JS→Go function the capture script calls.

It is deliberately not the name "atr browser record" uses. Both can be
capturing from the same page -- writing a spec and recording a session are
things people do together -- and one global would mean whichever attached
second silently replaced the first.
*/
const timelineBinding = "__atrTimelineEvent"

/*
startActions puts what a person or an agent did onto the recording's timeline.

Until this existed, the marks came from the live view's own input handlers, so
a click only counted if a human moved a mouse in the live view. Everything an
agent did -- every compiled test, every "atr browser click", every MCP call --
produced a recording whose timeline showed navigations and errors and not one
interaction. That is the wrong way round: the recording exists to explain an
agent's run.

The script is the one "atr browser record" uses, because it is the only thing
that reads a DOM event properly: selectors, shadow DOM, click de-duplication
and password masking are hard enough to get right once.

What reaches the timeline is only the shape of the action. A "type" mark says
somebody typed; it never says what. The script does mask password fields, but
the mark carries no value at all, because a search box and a passphrase are
typed the same way.
*/
func (s *Streamer) startActions(bound *rod.Page, targetID string) {
	stop, err := bound.Expose(timelineBinding, func(payload gson.JSON) (any, error) {
		if act, ok := timelineAction(payload); ok {
			s.act(act)
		}
		return nil, nil
	})
	if err != nil {
		// A page that will not take the binding still streams and still logs.
		// Losing the marks is worth less than losing the picture.
		return
	}

	script := browser.CaptureScript(timelineBinding, false)
	// On every document, so a navigation does not silently end the capture,
	// and once now, because the current document has already loaded.
	remove, _ := bound.EvalOnNewDocument(script)
	_, _ = bound.Eval(`() => { ` + script + ` }`)

	s.mu.Lock()
	s.actionStops = append(s.actionStops, func() {
		_ = stop()
		if remove != nil {
			_ = remove()
		}
	})
	s.mu.Unlock()
	_ = targetID
}

// stopActions detaches the capture from the page it was on. Called when the
// stream moves, so the bindings do not pile up one per tab switch.
func (s *Streamer) stopActions() {
	s.mu.Lock()
	stops := s.actionStops
	s.actionStops = nil
	s.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

/*
timelineAction turns one captured DOM event into a mark, or drops it.

Two kinds are dropped rather than translated. A scroll is continuous and would
bury every other mark on the bar. A navigation is already on the timeline from
the page poll, which sees the ones no interaction caused.
*/
func timelineAction(payload gson.JSON) (Action, bool) {
	switch payload.Get("type").Str() {
	case "click", "double_click":
		return Action{Kind: "click", Detail: label(payload)}, true

	case "keypress":
		// A named combination, such as Ctrl+A or Enter. Not a character: the
		// script only reports a keypress when a modifier or a named key is
		// involved, so this cannot spell out what was typed.
		return Action{Kind: "key", Detail: payload.Get("value").Str()}, true

	case "fill", "select_option":
		// No detail, on purpose. The value is what somebody typed or chose,
		// and a passphrase is typed exactly like a search term.
		return Action{Kind: "type"}, true
	}
	return Action{}, false
}

// label is the shortest honest description of what was acted on: its visible
// text if it has any, otherwise the selector that found it.
func label(payload gson.JSON) string {
	if text := strings.TrimSpace(payload.Get("innerText").Str()); text != "" {
		return clip(text, 60)
	}
	return clip(strings.TrimSpace(payload.Get("selector").Str()), 60)
}
