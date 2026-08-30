package llm

import (
	"context"
	"fmt"
)

// Unavailable is a Client that refuses every call, saying why.
//
// It exists for the runs that promise not to use a model — `--no-compile`
// replays a committed script and refuses rather than compiling, and skips
// triage outright. Building a real client for those meant a CI job needed
// credentials for calls it would never make, which made the promise a
// half-truth: no tokens spent, but no run at all without a configured backend.
//
// A stub rather than a nil Client, because nil would turn any path that did
// reach the model into a crash. This turns it into a sentence that names the
// flag responsible.
type Unavailable struct {
	// Reason says what made the model unavailable, phrased as the thing the
	// caller chose: "--no-compile is set".
	Reason string
}

// NewUnavailable returns a Client that errors on every call.
func NewUnavailable(reason string) Client {
	return &Unavailable{Reason: reason}
}

func (u *Unavailable) err() error {
	return fmt.Errorf("this run cannot call the model: %s", u.Reason)
}

func (u *Unavailable) Chat(context.Context, []Message, []Tool) (*Response, error) {
	return nil, u.err()
}

func (u *Unavailable) ChatWithHistory(context.Context, []Message, []Tool) (*Response, error) {
	return nil, u.err()
}

func (u *Unavailable) Model() string      { return "none" }
func (u *Unavailable) Provider() Provider { return Provider("none") }
func (u *Unavailable) Close() error       { return nil }
