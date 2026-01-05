// Package behavior provides public types for browser behavior testing results.
package behavior

import (
	"time"
)

// TestResult represents the result of a behavior test execution.
type TestResult struct {
	// TestFile is the path to the test file that was executed.
	TestFile string `json:"test_file"`

	// TestName is the name/title of the test (parsed from test file).
	TestName string `json:"test_name,omitempty"`

	// Success indicates whether the test passed.
	Success bool `json:"success"`

	// Duration is how long the test took to execute.
	Duration time.Duration `json:"duration"`

	// StartTime when the test began.
	StartTime time.Time `json:"start_time"`

	// EndTime when the test completed.
	EndTime time.Time `json:"end_time"`

	// Steps contains the execution status of each test step.
	Steps []StepResult `json:"steps,omitempty"`

	// FailureReason describes why the test failed (if applicable).
	FailureReason string `json:"failure_reason,omitempty"`

	// Screenshot is a viewport screenshot at test completion (PNG bytes).
	Screenshot []byte `json:"screenshot,omitempty"`

	// ConsoleErrors contains any console errors captured during the test.
	ConsoleErrors []ConsoleError `json:"console_errors,omitempty"`

	// FailedRequests contains any failed network requests.
	FailedRequests []FailedRequest `json:"failed_requests,omitempty"`

	// FinalURL is the page URL when the test completed.
	FinalURL string `json:"final_url,omitempty"`

	// FinalTitle is the page title when the test completed.
	FinalTitle string `json:"final_title,omitempty"`

	// Recommendations from AI analysis (if test failed).
	Recommendations []string `json:"recommendations,omitempty"`
}

// StepResult represents the result of a single test step.
type StepResult struct {
	// Number is the step number (1-based).
	Number int `json:"number"`

	// Description is what the step was supposed to do.
	Description string `json:"description"`

	// Status is the step execution status.
	Status StepStatus `json:"status"`

	// Duration is how long the step took.
	Duration time.Duration `json:"duration,omitempty"`

	// Error is the error message if the step failed.
	Error string `json:"error,omitempty"`

	// Screenshot is a screenshot taken during this step (PNG bytes).
	Screenshot []byte `json:"screenshot,omitempty"`
}

// StepStatus represents the execution status of a test step.
type StepStatus string

const (
	// StepStatusPending means the step hasn't been executed yet.
	StepStatusPending StepStatus = "pending"

	// StepStatusRunning means the step is currently executing.
	StepStatusRunning StepStatus = "running"

	// StepStatusPassed means the step completed successfully.
	StepStatusPassed StepStatus = "passed"

	// StepStatusFailed means the step failed.
	StepStatusFailed StepStatus = "failed"

	// StepStatusSkipped means the step was skipped (due to earlier failure).
	StepStatusSkipped StepStatus = "skipped"
)

// ConsoleError represents a console error captured during test execution.
type ConsoleError struct {
	// Level is the log level (error, warning).
	Level string `json:"level"`

	// Message is the error message.
	Message string `json:"message"`

	// URL is the source URL of the error.
	URL string `json:"url,omitempty"`

	// Line is the line number in the source.
	Line int `json:"line,omitempty"`

	// Timestamp when the error occurred.
	Timestamp time.Time `json:"timestamp"`
}

// FailedRequest represents a failed network request.
type FailedRequest struct {
	// URL of the failed request.
	URL string `json:"url"`

	// Method is the HTTP method.
	Method string `json:"method"`

	// Status is the HTTP status code (0 if request didn't complete).
	Status int `json:"status"`

	// Error is the error message.
	Error string `json:"error,omitempty"`
}

// SuiteResult represents the result of running multiple behavior tests.
type SuiteResult struct {
	// Total is the total number of tests.
	Total int `json:"total"`

	// Passed is the number of tests that passed.
	Passed int `json:"passed"`

	// Failed is the number of tests that failed.
	Failed int `json:"failed"`

	// Skipped is the number of tests that were skipped.
	Skipped int `json:"skipped"`

	// Duration is the total time for all tests.
	Duration time.Duration `json:"duration"`

	// Tests contains individual test results.
	Tests []TestResult `json:"tests"`

	// FailedTests contains paths to tests that failed.
	FailedTests []string `json:"failed_tests,omitempty"`
}

// PassRate returns the percentage of tests that passed.
func (s *SuiteResult) PassRate() float64 {
	if s.Total == 0 {
		return 0
	}
	return float64(s.Passed) / float64(s.Total) * 100
}

// IsSuccess returns true if all tests passed.
func (s *SuiteResult) IsSuccess() bool {
	return s.Failed == 0
}

// NewTestResult creates a new TestResult with initial values.
func NewTestResult(testFile string) *TestResult {
	return &TestResult{
		TestFile:  testFile,
		StartTime: time.Now(),
	}
}

// MarkSuccess marks the test as successful.
func (r *TestResult) MarkSuccess() {
	r.Success = true
	r.EndTime = time.Now()
	r.Duration = r.EndTime.Sub(r.StartTime)
}

// MarkFailure marks the test as failed with a reason.
func (r *TestResult) MarkFailure(reason string) {
	r.Success = false
	r.FailureReason = reason
	r.EndTime = time.Now()
	r.Duration = r.EndTime.Sub(r.StartTime)
}

// AddStep adds a step result.
func (r *TestResult) AddStep(step StepResult) {
	r.Steps = append(r.Steps, step)
}

// PassedSteps returns the number of steps that passed.
func (r *TestResult) PassedSteps() int {
	count := 0
	for _, step := range r.Steps {
		if step.Status == StepStatusPassed {
			count++
		}
	}
	return count
}

// FailedStep returns the first failed step, or nil if none.
func (r *TestResult) FailedStep() *StepResult {
	for i := range r.Steps {
		if r.Steps[i].Status == StepStatusFailed {
			return &r.Steps[i]
		}
	}
	return nil
}
