package record

import (
	"fmt"
	"strconv"
	"strings"
)

// humanBytes formats a byte count for a person to read.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// ParseSize reads a size such as "1GB", "200MB" or "1500000".
func ParseSize(s string) (int64, error) {
	t := strings.TrimSpace(strings.ToUpper(s))
	if t == "" {
		return 0, nil
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(t, "KB"), strings.HasSuffix(t, "K"):
		mult, t = 1<<10, strings.TrimRight(strings.TrimSuffix(t, "KB"), "K")
	case strings.HasSuffix(t, "MB"), strings.HasSuffix(t, "M"):
		mult, t = 1<<20, strings.TrimRight(strings.TrimSuffix(t, "MB"), "M")
	case strings.HasSuffix(t, "GB"), strings.HasSuffix(t, "G"):
		mult, t = 1<<30, strings.TrimRight(strings.TrimSuffix(t, "GB"), "G")
	case strings.HasSuffix(t, "B"):
		t = strings.TrimSuffix(t, "B")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size; write it like 200MB or 1GB", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("a size cannot be negative")
	}
	return int64(n * float64(mult)), nil
}
