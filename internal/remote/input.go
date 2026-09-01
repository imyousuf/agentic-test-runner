package remote

import (
	"fmt"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// typeBurst is the pause that ends one stretch of typing.
//
// A timeline mark per keystroke would be a wall of marks and would say
// nothing. One mark per burst says "somebody typed here", which is the thing a
// person is looking for.
const typeBurst = 1500 * time.Millisecond

// markedKeys are the keys worth a mark of their own. Enter is where a form is
// sent, and that is usually the moment somebody is scrubbing to find.
var markedKeys = map[string]bool{"Enter": true, "Tab": true, "Escape": true}

// Modifier bits, as CDP defines them.
const (
	modAlt   = 1
	modCtrl  = 2
	modMeta  = 4
	modShift = 8
)

// MouseMsg is a pointer event from the client. The coordinates are page
// pixels: the client converts from its canvas before sending.
type MouseMsg struct {
	Kind   string  `json:"kind"` // moved, pressed, released
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Button string  `json:"button"`
	Clicks int     `json:"clicks"`
	Mod    int     `json:"mod"`
}

// WheelMsg is a scroll event.
type WheelMsg struct {
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	DX  float64 `json:"dx"`
	DY  float64 `json:"dy"`
	Mod int     `json:"mod"`
}

// KeyMsg is a keyboard event.
type KeyMsg struct {
	Kind string `json:"kind"` // down, up
	Key  string `json:"key"`
	Code string `json:"code"`
	VK   int    `json:"vk"`
	Text string `json:"text"`
	Mod  int    `json:"mod"`
}

func mouseButton(name string) proto.InputMouseButton {
	switch name {
	case "right":
		return proto.InputMouseButtonRight
	case "middle":
		return proto.InputMouseButtonMiddle
	case "none", "":
		return proto.InputMouseButtonNone
	default:
		return proto.InputMouseButtonLeft
	}
}

// Mouse dispatches a pointer event.
func (s *Streamer) Mouse(m MouseMsg) error {
	page := s.CurrentPage()
	if page == nil {
		return fmt.Errorf("no page is selected")
	}

	var kind proto.InputDispatchMouseEventType
	switch m.Kind {
	case "pressed":
		kind = proto.InputDispatchMouseEventTypeMousePressed
	case "released":
		kind = proto.InputDispatchMouseEventTypeMouseReleased
	default:
		kind = proto.InputDispatchMouseEventTypeMouseMoved
	}

	ev := proto.InputDispatchMouseEvent{
		Type:       kind,
		X:          m.X,
		Y:          m.Y,
		Button:     mouseButton(m.Button),
		ClickCount: m.Clicks,
		Modifiers:  m.Mod,
	}
	if kind == proto.InputDispatchMouseEventTypeMouseMoved && m.Button == "" {
		ev.Button = proto.InputMouseButtonNone
		ev.ClickCount = 0
	}
	if err := ev.Call(page); err != nil {
		return fmt.Errorf("failed to dispatch the mouse event: %w", err)
	}
	if kind == proto.InputDispatchMouseEventTypeMousePressed {
		s.act(Action{Kind: "click", Detail: m.Button})
	}
	return nil
}

// Wheel dispatches a scroll event.
func (s *Streamer) Wheel(w WheelMsg) error {
	page := s.CurrentPage()
	if page == nil {
		return fmt.Errorf("no page is selected")
	}
	ev := proto.InputDispatchMouseEvent{
		Type:      proto.InputDispatchMouseEventTypeMouseWheel,
		X:         w.X,
		Y:         w.Y,
		Button:    proto.InputMouseButtonNone,
		DeltaX:    w.DX,
		DeltaY:    w.DY,
		Modifiers: w.Mod,
	}
	if err := ev.Call(page); err != nil {
		return fmt.Errorf("failed to dispatch the wheel event: %w", err)
	}
	return nil
}

// Key dispatches a keyboard event. A printable key also needs a char event,
// otherwise the character never reaches the page.
func (s *Streamer) Key(k KeyMsg) error {
	page := s.CurrentPage()
	if page == nil {
		return fmt.Errorf("no page is selected")
	}

	kind := proto.InputDispatchKeyEventTypeKeyDown
	if k.Kind == "up" {
		kind = proto.InputDispatchKeyEventTypeKeyUp
	}

	ev := proto.InputDispatchKeyEvent{
		Type:                  kind,
		Key:                   k.Key,
		Code:                  k.Code,
		WindowsVirtualKeyCode: k.VK,
		NativeVirtualKeyCode:  k.VK,
		Modifiers:             k.Mod,
	}
	// A control combination must not carry text, or the page receives the
	// character as well as the shortcut.
	if kind == proto.InputDispatchKeyEventTypeKeyDown && k.Text != "" &&
		k.Mod&(modCtrl|modMeta) == 0 {
		ev.Text = k.Text
		ev.UnmodifiedText = k.Text
	}

	if err := ev.Call(page); err != nil {
		return fmt.Errorf("failed to dispatch the key event: %w", err)
	}
	if kind == proto.InputDispatchKeyEventTypeKeyDown {
		s.markKey(k)
	}
	return nil
}

// markKey puts a key on the timeline of any recording that is running.
//
// It marks a named key straight away, and it marks a stretch of typing once,
// on the first character after a pause.
func (s *Streamer) markKey(k KeyMsg) {
	if markedKeys[k.Key] {
		s.act(Action{Kind: "key", Detail: k.Key})
		return
	}
	if k.Text == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	fresh := now.Sub(s.lastType) > typeBurst
	s.lastType = now
	s.mu.Unlock()
	if fresh {
		s.act(Action{Kind: "type"})
	}
}

// Text inserts a string in one step. Use it for a paste.
func (s *Streamer) Text(value string) error {
	page := s.CurrentPage()
	if page == nil {
		return fmt.Errorf("no page is selected")
	}
	if err := (proto.InputInsertText{Text: value}).Call(page); err != nil {
		return fmt.Errorf("failed to insert the text: %w", err)
	}
	return nil
}
