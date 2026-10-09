package remote

// Sink receives what a Streamer produces.
//
// A Sink must not block. Both methods run on the goroutine that acknowledges
// the screencast frame, and Chrome stops the stream when an acknowledgement is
// late. A sink that needs to do real work has to hand the frame to its own
// goroutine and return.
//
// Hub is the sink that feeds connected viewers. record.Recorder is the sink
// that writes a recording to disk. Both can be attached at once: the spike in
// docs/session-recording.md measured two CDP sessions streaming the same page
// without interfering, and a single stream feeding two sinks is strictly
// cheaper than that.
type Sink interface {
	// Frame delivers one encoded image.
	Frame(*Frame)
	// Text delivers one control message, already encoded as JSON.
	Text([]byte)
}
