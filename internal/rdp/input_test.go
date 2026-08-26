package rdp

import (
	"slices"
	"testing"
)

// Chrome runs no built-in editing action for a synthetic key event unless the
// command travels with it, so the mapping is the whole of what makes Ctrl+A
// select anything.
func TestEditingCommandsCoverTheControlShortcuts(t *testing.T) {
	cases := []struct {
		name string
		key  KeyMsg
		want []string
	}{
		{"ctrl a selects all", KeyMsg{Key: "a", Mod: modCtrl}, []string{"selectAll"}},
		{"cmd a selects all", KeyMsg{Key: "a", Mod: modMeta}, []string{"selectAll"}},
		{"upper case a", KeyMsg{Key: "A", Mod: modCtrl}, []string{"selectAll"}},
		{"ctrl z undoes", KeyMsg{Key: "z", Mod: modCtrl}, []string{"undo"}},
		{"ctrl shift z redoes", KeyMsg{Key: "z", Mod: modCtrl | modShift}, []string{"redo"}},
		{"ctrl y redoes", KeyMsg{Key: "y", Mod: modCtrl}, []string{"redo"}},
		{"ctrl x cuts", KeyMsg{Key: "x", Mod: modCtrl}, []string{"cut"}},
		{"ctrl c copies", KeyMsg{Key: "c", Mod: modCtrl}, []string{"copy"}},

		// The client sends a paste as Input.insertText carrying the viewer's
		// clipboard. A "paste" command would use the remote browser's instead.
		{"ctrl v is left to insertText", KeyMsg{Key: "v", Mod: modCtrl}, nil},

		{"a on its own types", KeyMsg{Key: "a"}, nil},
		{"shift a on its own types", KeyMsg{Key: "A", Mod: modShift}, nil},
		{"alt a is not an editing command", KeyMsg{Key: "a", Mod: modAlt}, nil},
		{"ctrl b has no command", KeyMsg{Key: "b", Mod: modCtrl}, nil},
		{"enter has no command", KeyMsg{Key: "Enter", Mod: modCtrl}, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := editingCommands(c.key)
			if !slices.Equal(got, c.want) {
				t.Errorf("editingCommands(%+v) = %v, want %v", c.key, got, c.want)
			}
		})
	}
}
