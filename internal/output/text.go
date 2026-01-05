package output

import (
	"fmt"
	"strings"
	"time"

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

	// Header
	sb.WriteString("\n")
	sb.WriteString(f.line("=", 70))
	sb.WriteString("\n")
	sb.WriteString(f.bold("ANALYSIS RESULTS"))
	sb.WriteString("\n")
	sb.WriteString(f.line("=", 70))
	sb.WriteString("\n\n")

	// Status
	sb.WriteString(f.bold("Status: "))
	if r.Status == result.StatusSuccess {
		sb.WriteString(f.green("SUCCESS"))
	} else {
		sb.WriteString(f.red("FAILURE"))
	}
	sb.WriteString("\n\n")

	// Summary
	if r.Summary != "" {
		sb.WriteString(f.bold("Summary:"))
		sb.WriteString("\n")
		sb.WriteString(f.indent(r.Summary, 2))
		sb.WriteString("\n\n")
	}

	// Root Cause
	if r.RootCause != "" {
		sb.WriteString(f.bold("Root Cause:"))
		sb.WriteString("\n")
		sb.WriteString(f.indent(r.RootCause, 2))
		sb.WriteString("\n\n")
	}

	// Recommendations
	if len(r.Recommendations) > 0 {
		sb.WriteString(f.bold("Recommendations:"))
		sb.WriteString("\n")
		for i, rec := range r.Recommendations {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, rec))
		}
		sb.WriteString("\n")
	}

	// Metrics
	if r.AgentMetrics != nil {
		sb.WriteString(f.line("-", 70))
		sb.WriteString("\n")
		sb.WriteString(f.dim(fmt.Sprintf("Agent made %d tool calls in %d iterations (%s)",
			r.AgentMetrics.ToolCallsMade,
			r.AgentMetrics.IterationCount,
			r.AgentMetrics.TotalDuration.Round(time.Millisecond))))
		if r.AgentMetrics.TokensUsed > 0 {
			sb.WriteString(f.dim(fmt.Sprintf(" | %d tokens", r.AgentMetrics.TokensUsed)))
		}
		sb.WriteString("\n")
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
