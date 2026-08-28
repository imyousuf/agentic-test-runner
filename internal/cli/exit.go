package cli

import "fmt"

// Exit codes. 1 means the application under test is broken and nothing else,
// so a red CI build has one meaning; everything that says nothing about the
// application — a missing input, an unreachable model, a browser that would
// not start — is 2, which a CI job can retry rather than escalate.
//
// docs/cli-reference.md and skills/atr-analyze/SKILL.md already documented a 2
// that nothing produced.
const (
	ExitOK          = 0
	ExitTestFailure = 1
	ExitInfra       = 2
)

// ExitError carries a process exit code out to main.
//
// The commands used to call os.Exit directly, which skipped every deferred
// close in the calling function — the browser, the LLM client, the daemon
// state file and the context cancel. Returning the code instead lets those run.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// exitWith returns an error carrying code, or nil for ExitOK.
func exitWith(code int, err error) error {
	if code == ExitOK {
		return nil
	}
	return &ExitError{Code: code, Err: err}
}
