// Package output provides output formatting for ATR results.
package output

import "github.com/imyousuf/agentic-test-runner/pkg/result"

// Formatter formats analysis results for output.
type Formatter interface {
	// Format formats an analysis result.
	Format(r *result.AnalysisResult) (string, error)

	// FormatError formats an error message.
	FormatError(err error) string
}
