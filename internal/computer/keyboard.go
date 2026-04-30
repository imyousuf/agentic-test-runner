package computer

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-vgo/robotgo"
)

// Type sends the given text as a sequence of key events. delayMs is the
// delay between characters; pass 0 for robotgo's default.
func (c *Computer) Type(ctx context.Context, text string, delayMs int) error {
	desc := fmt.Sprintf("Type %d characters", len(text))
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	if delayMs > 0 {
		robotgo.MilliSleep(delayMs)
	}
	robotgo.Type(text)
	return nil
}

// PressKey taps a single named key (e.g. "enter", "esc", "f5", "tab").
// See robotgo's keycode list for valid names.
func (c *Computer) PressKey(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("key must not be empty")
	}
	desc := fmt.Sprintf("Press key %q", key)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	robotgo.KeyTap(key)
	return nil
}

// KeyChord taps a key combination expressed as "mod+mod+key", e.g.
// "ctrl+shift+t" or "cmd+c". The last segment is the primary key; all
// preceding segments are modifiers.
func (c *Computer) KeyChord(ctx context.Context, chord string) error {
	parts := splitChord(chord)
	if len(parts) == 0 {
		return fmt.Errorf("chord must not be empty")
	}
	desc := fmt.Sprintf("Press chord %q", chord)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	key := parts[len(parts)-1]
	mods := parts[:len(parts)-1]
	if len(mods) == 0 {
		robotgo.KeyTap(key)
		return nil
	}
	// robotgo.KeyTap accepts variadic modifiers as separate strings.
	args := make([]any, 0, len(mods))
	for _, m := range mods {
		args = append(args, m)
	}
	robotgo.KeyTap(key, args...)
	return nil
}

// splitChord parses "ctrl+shift+t" into ["ctrl", "shift", "t"], lowercased
// and trimmed. Empty segments are dropped.
func splitChord(chord string) []string {
	raw := strings.Split(chord, "+")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(strings.ToLower(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
