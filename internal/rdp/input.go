package rdp

import (
	"fmt"
	"strings"

	"github.com/go-rod/rod/lib/proto"
)

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

	// Buttons is the CDP bitmask of buttons currently held (1 left, 2 right,
	// 4 middle). Chrome treats a mouseMoved with no held buttons as a hover, so
	// without this a drag never selects text or moves a slider.
	Buttons int `json:"buttons"`
}

// WheelMsg is a scroll event.
type WheelMsg struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	DX      float64 `json:"dx"`
	DY      float64 `json:"dy"`
	Mod     int     `json:"mod"`
	Buttons int     `json:"buttons"`
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

// editingCommands names the built-in editing action that a control shortcut
// should run.
//
// Chrome resolves these in the browser process, from the platform key binding
// attached to a real key press. A synthetic key event carries no such binding,
// so the renderer sees the keystroke and the modifier bit but runs no built-in
// action: Ctrl+A moves the caret nowhere and selects nothing. Naming the
// command explicitly is the documented way to restore it.
//
// Paste is absent on purpose. The client turns it into Input.insertText, which
// carries the viewer's clipboard; "paste" here would use the remote browser's.
func editingCommands(k KeyMsg) []string {
	if k.Mod&(modCtrl|modMeta) == 0 {
		return nil
	}
	switch strings.ToLower(k.Key) {
	case "a":
		return []string{"selectAll"}
	case "z":
		if k.Mod&modShift != 0 {
			return []string{"redo"}
		}
		return []string{"undo"}
	case "y":
		return []string{"redo"}
	case "x":
		return []string{"cut"}
	case "c":
		return []string{"copy"}
	}
	return nil
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
	if err := s.viewOnly(); err != nil {
		return err
	}
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

	buttons := m.Buttons
	ev := proto.InputDispatchMouseEvent{
		Type:       kind,
		X:          m.X,
		Y:          m.Y,
		Button:     mouseButton(m.Button),
		ClickCount: m.Clicks,
		Modifiers:  m.Mod,
		Buttons:    &buttons,
	}
	if kind == proto.InputDispatchMouseEventTypeMouseMoved && m.Button == "" {
		ev.Button = proto.InputMouseButtonNone
		ev.ClickCount = 0
	}
	if err := ev.Call(page); err != nil {
		return fmt.Errorf("failed to dispatch the mouse event: %w", err)
	}
	return nil
}

// Wheel dispatches a scroll event.
func (s *Streamer) Wheel(w WheelMsg) error {
	if err := s.viewOnly(); err != nil {
		return err
	}
	page := s.CurrentPage()
	if page == nil {
		return fmt.Errorf("no page is selected")
	}
	buttons := w.Buttons
	ev := proto.InputDispatchMouseEvent{
		Type:      proto.InputDispatchMouseEventTypeMouseWheel,
		X:         w.X,
		Y:         w.Y,
		Button:    proto.InputMouseButtonNone,
		DeltaX:    w.DX,
		DeltaY:    w.DY,
		Modifiers: w.Mod,
		Buttons:   &buttons,
	}
	if err := ev.Call(page); err != nil {
		return fmt.Errorf("failed to dispatch the wheel event: %w", err)
	}
	return nil
}

// Key dispatches a keyboard event. A printable key also needs a char event,
// otherwise the character never reaches the page.
func (s *Streamer) Key(k KeyMsg) error {
	if err := s.viewOnly(); err != nil {
		return err
	}
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
	if kind == proto.InputDispatchKeyEventTypeKeyDown {
		ev.Commands = editingCommands(k)
	}

	if err := ev.Call(page); err != nil {
		return fmt.Errorf("failed to dispatch the key event: %w", err)
	}
	return nil
}

// Text inserts a string in one step. Use it for a paste.
func (s *Streamer) Text(value string) error {
	if err := s.viewOnly(); err != nil {
		return err
	}
	page := s.CurrentPage()
	if page == nil {
		return fmt.Errorf("no page is selected")
	}
	if err := (proto.InputInsertText{Text: value}).Call(page); err != nil {
		return fmt.Errorf("failed to insert the text: %w", err)
	}
	return nil
}
