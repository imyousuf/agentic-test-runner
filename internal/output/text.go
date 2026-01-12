package output

import (
	"fmt"
	"strings"

	"github.com/imyousuf/agentic-test-runner/pkg/result"
)

// TextFormatter formats results as human-readable text.
type TextFormatter struct {
	// UseColors enables ANSI color codes (for terminal output).
	UseColors bool
}

// NewTextFormatter creates a new text formatter.
func NewTextFormatter() *TextFormatter {
	return &TextFormatter{
		UseColors: true,
	}
}

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// Format formats an analysis result as text.
func (f *TextFormatter) Format(r *result.AnalysisResult) (string, error) {
	var sb strings.Builder

	// Status line
	if r.Status == result.StatusSuccess {
		sb.WriteString(f.green("✓ PASS"))
	} else {
		sb.WriteString(f.red("✗ FAIL"))
	}

	// Summary on same line if short, otherwise new line
	if r.Summary != "" {
		summary := strings.TrimSpace(r.Summary)
		if len(summary) < 80 && !strings.Contains(summary, "\n") {
			sb.WriteString(": ")
			sb.WriteString(summary)
		} else {
			sb.WriteString("\n")
			sb.WriteString(f.indent(summary, 2))
		}
	}
	sb.WriteString("\n")

	// Root Cause (only on failure)
	if r.RootCause != "" && r.Status != result.StatusSuccess {
		sb.WriteString(f.bold("Root Cause: "))
		sb.WriteString(r.RootCause)
		sb.WriteString("\n")
	}

	// Recommendations (only on failure)
	if len(r.Recommendations) > 0 && r.Status != result.StatusSuccess {
		sb.WriteString(f.bold("Fix:"))
		sb.WriteString("\n")
		for i, rec := range r.Recommendations {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, rec))
		}
	}

	return sb.String(), nil
}

// FormatError formats an error message.
func (f *TextFormatter) FormatError(err error) string {
	return f.red(fmt.Sprintf("Error: %v", err))
}

// Helper methods

func (f *TextFormatter) line(char string, length int) string {
	return strings.Repeat(char, length)
}

func (f *TextFormatter) bold(s string) string {
	if f.UseColors {
		return colorBold + s + colorReset
	}
	return s
}

func (f *TextFormatter) red(s string) string {
	if f.UseColors {
		return colorRed + s + colorReset
	}
	return s
}

func (f *TextFormatter) green(s string) string {
	if f.UseColors {
		return colorGreen + s + colorReset
	}
	return s
}

func (f *TextFormatter) dim(s string) string {
	if f.UseColors {
		return colorDim + s + colorReset
	}
	return s
}

func (f *TextFormatter) indent(s string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}
