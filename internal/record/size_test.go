package record

import "testing"

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"":       0,
		"0":      0,
		"1024":   1024,
		"1500B":  1500,
		"200MB":  200 << 20,
		"200m":   200 << 20,
		"1GB":    1 << 30,
		"1.5GB":  1610612736,
		" 2 KB ": 2048,
	}
	for in, want := range cases {
		got, err := ParseSize(in)
		if err != nil {
			t.Errorf("ParseSize(%q) returned %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseSize(%q) = %d, want %d", in, got, want)
		}
	}

	for _, in := range []string{"lots", "-5MB", "MB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) returned no error", in)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:             "512 B",
		2048:            "2.0 KB",
		5 * 1024 * 1024: "5.0 MB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
