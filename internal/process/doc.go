// Package process answers whether a pid is still running.
//
// It exists because two packages need that answer and one of them cannot ask
// the other: internal/api imports internal/browser, so the copy in
// internal/browser could not be shared and the two drifted. The api copy kept
// the Unix-only signal-0 test long after the browser copy learned that
// Windows has no signals, which is why "atr browser status" reported a live
// daemon as dead there.
package process
