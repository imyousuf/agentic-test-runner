package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// hudScript is the panel implementation, injected into the isolated world.
//
//go:embed hud.js
var hudScript string

// The HUD is an in-page control surface for the agent: a floating panel,
// injected into every page of a headed browser, that lets a human sitting in
// front of the window hand work to the agent without leaving it.
//
// It is injected into a named isolated world rather than the page's main
// world. Two reasons:
//
//  1. Content Security Policy. A HUD that reached the daemon over fetch() or
//     a WebSocket would be blocked outright by the connect-src directive on
//     any site with a strict policy (GitHub, for one). A CDP binding is not a
//     network request and no CSP directive applies to it.
//  2. Isolation. Globals defined in an isolated world are invisible to page
//     scripts, so a page cannot detect, call, or tamper with the bridge.
//
// Isolated worlds still share the DOM, so the panel itself is a normal
// element hosting a closed shadow root. The closed root is what keeps the HUD
// out of the agent's own view of the page: Snapshot() walks the DOM with
// querySelectorAll, which does not pierce shadow boundaries.
const (
	// hudWorldName names the isolated world. Chrome recreates a world with
	// this name on every navigation and the binding below re-binds to it.
	hudWorldName = "__atr_hud"
	// hudBinding is the JS→Go entry point, scoped to hudWorldName.
	hudBinding = "__atrHudSend"
	// hudDeliver is the Go→JS entry point the injected script installs.
	hudDeliver = "__atrHudDeliver"
	// hudHostID identifies the panel's host element in the main DOM. The
	// element deliberately carries no role, aria-label or data-testid, so it
	// does not match the Snapshot() selector.
	hudHostID = "__atr_hud_host"
)

// hudMaxHistory caps the retained transcript. It is replayed in full into
// every panel that appears — on each navigation and each new tab — so an
// unbounded transcript would make page loads progressively slower in a long
// session.
const hudMaxHistory = 200

// hudEvalTimeout bounds a single push into the page. A page that is
// mid-navigation can leave Runtime.evaluate hanging, and Chrome serialises
// commands per target — so a stuck push delays whatever the agent does next
// on that page.
const hudEvalTimeout = 2 * time.Second

// hudOutboxSize bounds the queue of pending pushes. Deep enough that a burst
// of tool events never blocks the turn producing them.
const hudOutboxSize = 128

// HudEvent is one item in the HUD transcript, and also the wire format for
// everything pushed from Go to the panel.
type HudEvent struct {
	// Type is one of: "user", "delta", "tool", "done", "error", "status",
	// "state".
	Type string `json:"t"`
	// Turn correlates events belonging to one request.
	Turn string `json:"turn,omitempty"`
	// Text carries prose for user/delta/done/error/status events.
	Text string `json:"text,omitempty"`
	// Name is the tool name for "tool" events.
	Name string `json:"name,omitempty"`
	// Detail is a one-line summary of a tool call's arguments or result.
	Detail string `json:"detail,omitempty"`
	// Busy tells the panel whether a turn is currently running. It is set on
	// "state" and "status" events.
	Busy bool `json:"busy,omitempty"`
	// History replays the transcript on "state" events, so a reloaded page or
	// a newly opened tab shows the conversation so far.
	History []HudEvent `json:"history,omitempty"`
}

// HudHandler runs one agent turn. Implementations stream progress through
// emit and return when the turn is finished. The handler is called on its own
// goroutine and must respect ctx cancellation.
//
// It lives behind a function type so that internal/browser does not import
// internal/agent, which would be a cycle: the agent's tools drive the browser.
type HudHandler func(ctx context.Context, prompt string, emit func(HudEvent))

// hudMessage is the JS→Go envelope.
type hudMessage struct {
	Op   string `json:"op"`
	Text string `json:"text,omitempty"`
}

// hudSession holds the state of an enabled HUD.
type hudSession struct {
	handler HudHandler

	mu sync.Mutex
	// contexts maps a live isolated-world context to the page hosting it.
	// Entries are added when a panel says hello and pruned when a push to
	// them fails, which is how a navigated-away world gets collected.
	contexts map[proto.RuntimeExecutionContextID]*rod.Page
	// history is the transcript, replayed into every panel that appears.
	history []HudEvent
	// cancel aborts the in-flight turn, if any.
	cancel context.CancelFunc
	busy   bool
	// attached tracks targets that already have a binding and init script, so
	// re-attaching is idempotent.
	//
	// Keyed by target ID, not by *rod.Page: PageFromTarget hands back a fresh
	// Page value for a target it has already returned, so a pointer key lets
	// the same tab be attached twice. Two subscriptions means every message
	// from that panel is dispatched twice, and the second copy of an "ask"
	// lands on a session that is already busy.
	attached map[proto.TargetTargetID]bool
	// stops undoes the bindings and init scripts on DisableHud.
	stops []func()

	// outbox carries pushes to the panels. Deliveries happen on one
	// goroutine, off whatever produced the event.
	//
	// Pushing inline would put a Runtime.evaluate between every pair of the
	// agent's own CDP commands, and Chrome serialises commands per target:
	// the panel repainting its transcript would eat into the budget of the
	// very next click or fill, which then fails with "context deadline
	// exceeded". Ordering still matters, so it is one queue, not a goroutine
	// per event.
	//
	// Never closed — closing would race with senders and panic. deliverLoop
	// exits on done instead.
	outbox chan HudEvent
	done   chan struct{}
	// stopped guards against closing done twice.
	stopped bool
}

// EnableHud installs the in-page agent panel on every current and future
// page. Turns are executed by handler.
func (b *Browser) EnableHud(handler HudHandler) error {
	if handler == nil {
		return fmt.Errorf("hud handler is required")
	}

	b.mu.Lock()
	if b.browser == nil {
		b.mu.Unlock()
		return fmt.Errorf("browser not launched")
	}
	if b.hud != nil {
		// Swap the handler but keep the transcript: re-enabling should not
		// silently discard the conversation.
		b.hud.mu.Lock()
		b.hud.handler = handler
		b.hud.mu.Unlock()
		b.mu.Unlock()
		return nil
	}

	hud := &hudSession{
		handler:  handler,
		contexts: make(map[proto.RuntimeExecutionContextID]*rod.Page),
		attached: make(map[proto.TargetTargetID]bool),
		outbox:   make(chan HudEvent, hudOutboxSize),
		done:     make(chan struct{}),
	}
	go hud.deliverLoop()
	b.hud = hud
	pages := append([]*rod.Page(nil), b.pages...)
	// Release before injecting: attachment makes blocking CDP calls, and
	// holding the browser lock across those stalls every other event
	// listener that needs it.
	b.mu.Unlock()

	var firstErr error
	for _, page := range pages {
		if err := b.attachHudSession(hud, page); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		b.mu.Lock()
		b.hud = nil
		b.mu.Unlock()
		return firstErr
	}

	return nil
}

// DisableHud removes the panel from every page and drops the transcript.
func (b *Browser) DisableHud() error {
	b.mu.Lock()
	hud := b.hud
	b.hud = nil
	pages := append([]*rod.Page(nil), b.pages...)
	b.mu.Unlock()

	if hud == nil {
		return nil
	}

	hud.mu.Lock()
	if hud.cancel != nil {
		hud.cancel()
		hud.cancel = nil
	}
	stops := hud.stops
	hud.stops = nil
	alreadyStopped := hud.stopped
	hud.stopped = true
	hud.mu.Unlock()

	// Ends deliverLoop. The outbox itself is never closed: senders race with
	// this and a send on a closed channel panics.
	if !alreadyStopped {
		close(hud.done)
	}

	for _, stop := range stops {
		stop()
	}

	// Tear down the panel that is already on screen. A failure here means the
	// page is gone or navigating, which is not worth reporting.
	for _, page := range pages {
		_, _ = page.Eval(fmt.Sprintf(
			`() => { const el = document.getElementById(%q); if (el) el.remove(); }`, hudHostID))
	}

	return nil
}

// HudEnabled reports whether the panel is installed.
func (b *Browser) HudEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.hud != nil
}

// attachHud installs the panel on one page. The caller must hold b.mu; it is
// the entry point used by the page-registration paths.
func (b *Browser) attachHud(page *rod.Page) error {
	return b.attachHudSession(b.hud, page)
}

// attachHudSession installs the binding and init script on one page. It takes
// the session explicitly rather than reading b.hud, so that nothing on this
// path needs the browser lock.
func (b *Browser) attachHudSession(hud *hudSession, page *rod.Page) error {
	if hud == nil {
		return nil
	}

	target := page.TargetID

	hud.mu.Lock()
	already := hud.attached[target]
	if !already {
		// Claim it inside the same critical section as the check, so two
		// concurrent attaches for one target cannot both proceed.
		hud.attached[target] = true
	}
	hud.mu.Unlock()
	if already {
		return nil
	}

	// Bind by world *name*, not context id: Chrome re-creates the isolated
	// world on every navigation, and a name-scoped binding follows it. A
	// context-scoped one would survive exactly one page load.
	if err := (proto.RuntimeAddBinding{
		Name:                 hudBinding,
		ExecutionContextName: hudWorldName,
	}).Call(page); err != nil {
		hud.mu.Lock()
		delete(hud.attached, target)
		hud.mu.Unlock()
		return fmt.Errorf("installing hud binding: %w", err)
	}

	// EachEvent must be spawned, not called inline. It subscribes to the
	// event stream and only then makes a blocking Runtime.enable call, before
	// returning the wait() that drains that subscription — so calling it
	// synchronously stalls the CDP read loop against its own response.
	//
	// That leaves a window in which the script below has already run but the
	// subscription is not yet live, and the panel's opening hello is dropped.
	// The panel retries the handshake until it is answered rather than
	// assuming the first one lands.
	// Bind the subscription to a cancellable copy of the page so DisableHud
	// can actually end it. An EachEvent goroutine left running holds a live
	// CDP event subscription for the lifetime of the page; leaking one per
	// page per enable slows the whole event stream down, which shows up as
	// unrelated element lookups blowing their timeouts.
	sub, cancelSub := page.WithCancel()
	go sub.EachEvent(func(e *proto.RuntimeBindingCalled) {
		if e.Name != hudBinding {
			return
		}
		b.handleHudMessage(hud, page, e)
	})()

	// RunImmediately covers the page that is already loaded; the registration
	// itself covers every subsequent document.
	res, err := (proto.PageAddScriptToEvaluateOnNewDocument{
		Source:         hudScript,
		WorldName:      hudWorldName,
		RunImmediately: true,
	}).Call(page)
	if err != nil {
		cancelSub()
		hud.mu.Lock()
		delete(hud.attached, target)
		hud.mu.Unlock()
		return fmt.Errorf("installing hud script: %w", err)
	}

	scriptID := res.Identifier
	stop := func() {
		cancelSub()
		_ = proto.RuntimeRemoveBinding{Name: hudBinding}.Call(page)
		_ = proto.PageRemoveScriptToEvaluateOnNewDocument{Identifier: scriptID}.Call(page)
	}

	hud.mu.Lock()
	hud.stops = append(hud.stops, stop)
	hud.mu.Unlock()

	return nil
}

// handleHudMessage dispatches one JS→Go call.
//
// Nothing on this path may take b.mu or make a blocking CDP call: it runs on
// rod's event-dispatch goroutine, and blocking there stops every other
// listener on the browser. The session is passed in for the same reason, and
// the reply to a hello is pushed from a goroutine of its own.
func (b *Browser) handleHudMessage(hud *hudSession, page *rod.Page, e *proto.RuntimeBindingCalled) {
	if hud == nil {
		return
	}

	var msg hudMessage
	if err := json.Unmarshal([]byte(e.Payload), &msg); err != nil {
		return
	}

	// Every message re-registers the calling context. That is how a panel
	// re-attaches after a navigation without any explicit handshake.
	hud.mu.Lock()
	hud.contexts[e.ExecutionContextID] = page
	hud.mu.Unlock()

	switch msg.Op {
	case "hello":
		hud.mu.Lock()
		state := HudEvent{Type: "state", Busy: hud.busy, History: append([]HudEvent(nil), hud.history...)}
		hud.mu.Unlock()
		go hudPushTo(hud, e.ExecutionContextID, page, state)

	case "ask":
		b.startHudTurn(hud, msg.Text)

	case "cancel":
		hud.mu.Lock()
		cancel := hud.cancel
		hud.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

// startHudTurn runs one agent turn, streaming its progress to every panel.
func (b *Browser) startHudTurn(hud *hudSession, prompt string) {
	if prompt == "" {
		return
	}

	hud.mu.Lock()
	if hud.busy {
		hud.mu.Unlock()
		go hudBroadcast(hud, HudEvent{Type: "error", Text: "A request is already running. Cancel it first."})
		return
	}
	turn := fmt.Sprintf("t%d", len(hud.history)+1)
	hud.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	hud.cancel = cancel
	handler := hud.handler
	hud.mu.Unlock()

	go func() {
		defer func() {
			// A tool panicking must not take the daemon down or leave the
			// panel spinning forever.
			if r := recover(); r != nil {
				hudRecord(hud, HudEvent{Type: "error", Turn: turn, Text: fmt.Sprintf("internal error: %v", r)})
			}
			cancel()
			hud.mu.Lock()
			hud.busy = false
			hud.cancel = nil
			hud.mu.Unlock()
			hudBroadcast(hud, HudEvent{Type: "status", Busy: false})
		}()

		hudRecord(hud, HudEvent{Type: "user", Turn: turn, Text: prompt})
		hudBroadcast(hud, HudEvent{Type: "status", Busy: true})

		handler(ctx, prompt, func(ev HudEvent) {
			ev.Turn = turn
			hudRecord(hud, ev)
		})
	}()
}

// hudRecord appends an event to the transcript and pushes it to every panel.
func hudRecord(hud *hudSession, ev HudEvent) {
	hud.mu.Lock()
	hud.history = append(hud.history, ev)
	if excess := len(hud.history) - hudMaxHistory; excess > 0 {
		// Copy rather than reslice: reslicing keeps the whole backing array
		// alive, so the dropped events would never actually be collected.
		hud.history = append([]HudEvent(nil), hud.history[excess:]...)
	}
	hud.mu.Unlock()
	hudBroadcast(hud, ev)
}

// hudBroadcast queues an event for delivery to every live panel. It never
// blocks: a full outbox means the panels are far behind, and dropping a
// progress line is better than stalling the agent turn.
func hudBroadcast(hud *hudSession, ev HudEvent) {
	if hud == nil {
		return
	}

	select {
	case hud.outbox <- ev:
	case <-hud.done:
	default:
	}
}

// deliverLoop is the single sender. It exits when the session is disabled.
func (hud *hudSession) deliverLoop() {
	for {
		select {
		case ev := <-hud.outbox:
			hud.deliver(ev)
		case <-hud.done:
			return
		}
	}
}

// deliver pushes one event to every live panel.
func (hud *hudSession) deliver(ev HudEvent) {
	hud.mu.Lock()
	targets := make(map[proto.RuntimeExecutionContextID]*rod.Page, len(hud.contexts))
	for id, page := range hud.contexts {
		targets[id] = page
	}
	hud.mu.Unlock()

	for id, page := range targets {
		hudPushTo(hud, id, page, ev)
	}
}

// hudPushTo delivers one event into a single isolated-world context. A
// failure means the context is gone — the page navigated or closed — so the
// entry is pruned rather than reported.
func hudPushTo(hud *hudSession, id proto.RuntimeExecutionContextID, page *rod.Page, ev HudEvent) {
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), hudEvalTimeout)
	defer cancel()

	_, err = (proto.RuntimeEvaluate{
		Expression:    fmt.Sprintf("%s(%s)", hudDeliver, string(payload)),
		ContextID:     id,
		ReturnByValue: true,
	}).Call(page.Context(ctx))
	if err == nil {
		return
	}

	hud.mu.Lock()
	delete(hud.contexts, id)
	hud.mu.Unlock()
}

// setHudVisible shows or hides the panel's host element. Screenshots call
// this so the HUD does not end up baked into captured images — the host lives
// in the main DOM, so a plain evaluate reaches it.
func (b *Browser) setHudVisible(page *rod.Page, visible bool) {
	display := "none"
	if visible {
		display = ""
	}
	_, _ = page.Eval(fmt.Sprintf(
		`() => { const el = document.getElementById(%q); if (el) el.style.display = %q; }`,
		hudHostID, display))
}

// hideHud hides the panel and returns a function that restores it. Screenshot
// paths defer the restore, so a capture never includes the agent's own
// control surface. It is a no-op when the HUD is off.
func (b *Browser) hideHud(page *rod.Page) func() {
	if !b.HudEnabled() {
		return func() {}
	}
	b.setHudVisible(page, false)
	return func() { b.setHudVisible(page, true) }
}

// hudActive reports whether a panel is installed, without taking b.mu. Only
// safe for callers that already hold it.
func (b *Browser) hudActive() bool { return b.hud != nil }
