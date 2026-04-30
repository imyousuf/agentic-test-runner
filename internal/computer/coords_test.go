package computer

import (
	"image"
	"testing"
)

func TestCoordsTranslationIdentityWhenAllPositive(t *testing.T) {
	// Single-monitor or layouts where every monitor sits to the right of
	// and below primary — primary-centric and root coords are identical.
	c := coords{offsetX: 0, offsetY: 0}

	tests := []struct {
		x, y int
	}{
		{0, 0},
		{100, 200},
		{2559, 1439},
	}
	for _, tt := range tests {
		gotX, gotY := c.primaryToRoot(tt.x, tt.y)
		if gotX != tt.x || gotY != tt.y {
			t.Errorf("primaryToRoot(%d,%d) = (%d,%d); want identity", tt.x, tt.y, gotX, gotY)
		}
		gotX, gotY = c.rootToPrimary(tt.x, tt.y)
		if gotX != tt.x || gotY != tt.y {
			t.Errorf("rootToPrimary(%d,%d) = (%d,%d); want identity", tt.x, tt.y, gotX, gotY)
		}
	}
}

func TestCoordsTranslationWithLeftMonitor(t *testing.T) {
	// Layout: 1440x2560 portrait secondary at primary-centric (-1440, 0),
	// 2560x1440 landscape primary at primary-centric (0, 0). This is the
	// dev box where the multi-monitor bug was found.
	c := coords{offsetX: 1440, offsetY: 0}

	tests := []struct {
		name                 string
		primary              [2]int // primary-centric (x, y)
		root                 [2]int // expected root (x, y)
		describesPrimaryEdge bool
	}{
		{
			name:                 "primary top-left",
			primary:              [2]int{0, 0},
			root:                 [2]int{1440, 0},
			describesPrimaryEdge: true,
		},
		{
			name:    "primary middle",
			primary: [2]int{1280, 720},
			root:    [2]int{2720, 720},
		},
		{
			name:                 "secondary top-left",
			primary:              [2]int{-1440, 0},
			root:                 [2]int{0, 0},
			describesPrimaryEdge: true,
		},
		{
			name:    "secondary middle",
			primary: [2]int{-700, 1280},
			root:    [2]int{740, 1280},
		},
		{
			name:    "Software Updater seen by EWMH at root x=1492",
			primary: [2]int{52, 288},
			root:    [2]int{1492, 288},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotX, gotY := c.primaryToRoot(tt.primary[0], tt.primary[1])
			if gotX != tt.root[0] || gotY != tt.root[1] {
				t.Errorf("primaryToRoot%v = (%d,%d); want (%d,%d)",
					tt.primary, gotX, gotY, tt.root[0], tt.root[1])
			}
			gotX, gotY = c.rootToPrimary(tt.root[0], tt.root[1])
			if gotX != tt.primary[0] || gotY != tt.primary[1] {
				t.Errorf("rootToPrimary%v = (%d,%d); want (%d,%d)",
					tt.root, gotX, gotY, tt.primary[0], tt.primary[1])
			}
		})
	}
}

func TestCoordsTranslationRoundTrip(t *testing.T) {
	c := coords{offsetX: 1440, offsetY: 200}
	for _, p := range []struct{ x, y int }{
		{0, 0},
		{-500, -100},
		{2000, 1000},
		{-1440, 0},
	} {
		rx, ry := c.primaryToRoot(p.x, p.y)
		bx, by := c.rootToPrimary(rx, ry)
		if bx != p.x || by != p.y {
			t.Errorf("round trip (%d,%d) -> root (%d,%d) -> (%d,%d)", p.x, p.y, rx, ry, bx, by)
		}
	}
}

func TestPrimaryRectToRoot(t *testing.T) {
	c := coords{offsetX: 1440, offsetY: 0}
	in := image.Rect(0, 0, 2560, 1440) // primary in primary-centric
	got := c.primaryRectToRoot(in)
	want := image.Rect(1440, 0, 4000, 1440) // primary in root
	if got != want {
		t.Errorf("primaryRectToRoot(%v) = %v; want %v", in, got, want)
	}

	in2 := image.Rect(-1440, 0, 0, 2560) // secondary in primary-centric
	got2 := c.primaryRectToRoot(in2)
	want2 := image.Rect(0, 0, 1440, 2560) // secondary in root
	if got2 != want2 {
		t.Errorf("primaryRectToRoot(%v) = %v; want %v", in2, got2, want2)
	}
}
