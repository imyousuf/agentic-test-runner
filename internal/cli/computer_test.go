package cli

import "testing"

func TestParseXY(t *testing.T) {
	cases := []struct {
		x, y string
		ex   int
		ey   int
		ok   bool
	}{
		{"100", "200", 100, 200, true},
		{"-50", "0", -50, 0, true},
		{"abc", "200", 0, 0, false},
		{"100", "abc", 0, 0, false},
	}
	for _, tc := range cases {
		x, y, err := parseXY(tc.x, tc.y)
		if tc.ok {
			if err != nil {
				t.Errorf("parseXY(%q,%q) unexpected err: %v", tc.x, tc.y, err)
				continue
			}
			if x != tc.ex || y != tc.ey {
				t.Errorf("parseXY(%q,%q) = (%d,%d), want (%d,%d)", tc.x, tc.y, x, y, tc.ex, tc.ey)
			}
		} else if err == nil {
			t.Errorf("parseXY(%q,%q) expected error, got nil", tc.x, tc.y)
		}
	}
}

func TestParsePair(t *testing.T) {
	cases := []struct {
		in     string
		ex, ey int
		ok     bool
	}{
		{"100,200", 100, 200, true},
		{" 1 , 2 ", 1, 2, true},
		{"1,2,3", 0, 0, false},
		{"x,y", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tc := range cases {
		x, y, err := parsePair(tc.in)
		if tc.ok {
			if err != nil {
				t.Errorf("parsePair(%q) unexpected err: %v", tc.in, err)
				continue
			}
			if x != tc.ex || y != tc.ey {
				t.Errorf("parsePair(%q) = (%d,%d), want (%d,%d)", tc.in, x, y, tc.ex, tc.ey)
			}
		} else if err == nil {
			t.Errorf("parsePair(%q) expected error, got nil", tc.in)
		}
	}
}

func TestParseRegion(t *testing.T) {
	x, y, w, h, err := parseRegion("10,20,300,400")
	if err != nil {
		t.Fatalf("parseRegion: %v", err)
	}
	if x != 10 || y != 20 || w != 300 || h != 400 {
		t.Errorf("got (%d,%d,%d,%d) want (10,20,300,400)", x, y, w, h)
	}

	if _, _, _, _, err := parseRegion("1,2,3"); err == nil {
		t.Error("expected error for too few parts")
	}
	if _, _, _, _, err := parseRegion("1,2,abc,4"); err == nil {
		t.Error("expected error for non-integer width")
	}
}

func TestNewComputerCmdHasSubcommands(t *testing.T) {
	cmd := newComputerCmd()
	want := []string{"start", "stop", "status", "serve", "screenshot", "click", "move", "drag", "scroll", "hover", "type", "key", "chord", "position", "displays", "reset-approvals"}
	have := map[string]bool{}
	for _, sub := range cmd.Commands() {
		have[sub.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand %q", w)
		}
	}
}
