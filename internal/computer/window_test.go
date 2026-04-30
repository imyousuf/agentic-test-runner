package computer

import (
	"context"
	"strings"
	"testing"
)

func TestUintToStr(t *testing.T) {
	cases := []struct {
		in  uint32
		out string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{1234567890, "1234567890"},
	}
	for _, tc := range cases {
		if got := uintToStr(tc.in); got != tc.out {
			t.Errorf("uintToStr(%d) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

func TestDescribeWindow(t *testing.T) {
	got := describeWindow("Focus", 12345)
	if !strings.Contains(got, "Focus") || !strings.Contains(got, "12345") {
		t.Errorf("describeWindow = %q, want it to contain 'Focus' and '12345'", got)
	}
}

func TestLaunchAppRejectsEmpty(t *testing.T) {
	c, _ := newTestComputer(t, ModeOff, 0)
	if err := c.LaunchApp(context.Background(), ""); err == nil {
		t.Error("expected error for empty app name")
	}
}

func TestQuitAppRejectsEmpty(t *testing.T) {
	c, _ := newTestComputer(t, ModeOff, 0)
	if err := c.QuitApp(context.Background(), ""); err == nil {
		t.Error("expected error for empty app name")
	}
}

func TestWindowStateConstants(t *testing.T) {
	for _, s := range []WindowState{WindowMinimize, WindowMaximize, WindowRestore, WindowClose} {
		if s == "" {
			t.Errorf("WindowState constant is empty")
		}
	}
}
