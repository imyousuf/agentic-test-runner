package computer

import "errors"

// ErrAborted is returned when the user cancels an action during the
// countdown gate (e.g. via Ctrl+C).
var ErrAborted = errors.New("aborted by user")
