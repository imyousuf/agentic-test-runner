package rdp

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

// A viewer must keep only the newest frame. Queuing frames would make a slow
// viewer fall further behind on every frame instead of skipping ahead.
func TestViewerKeepsOnlyTheNewestFrame(t *testing.T) {
	v := newViewer()

	for i := 1; i <= 5; i++ {
		v.put(&Frame{Seq: i})
	}

	frame, msgs := v.take()
	if frame == nil {
		t.Fatal("expected a frame")
	}
	if frame.Seq != 5 {
		t.Fatalf("expected the newest frame 5, got %d", frame.Seq)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no control messages, got %d", len(msgs))
	}

	frame, _ = v.take()
	if frame != nil {
		t.Fatal("expected the slot to be empty after take")
	}
}

// Control messages carry state changes, so they must not be dropped.
func TestViewerKeepsEveryControlMessage(t *testing.T) {
	v := newViewer()
	v.send([]byte(`{"t":"one"}`))
	v.send([]byte(`{"t":"two"}`))

	_, msgs := v.take()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 control messages, got %d", len(msgs))
	}
}

func TestClosedViewerAcceptsNothing(t *testing.T) {
	v := newViewer()
	v.close()
	v.put(&Frame{Seq: 1})
	v.send([]byte(`{"t":"x"}`))

	frame, msgs := v.take()
	if frame != nil || len(msgs) != 0 {
		t.Fatal("a closed viewer must not accept a frame or a message")
	}
}

func TestHubBroadcastsToEveryViewer(t *testing.T) {
	hub := NewHub()
	a, b := newViewer(), newViewer()
	hub.add(a)
	hub.add(b)

	if hub.Count() != 2 {
		t.Fatalf("expected 2 viewers, got %d", hub.Count())
	}

	hub.Broadcast(&Frame{Seq: 7})
	for name, v := range map[string]*viewer{"a": a, "b": b} {
		frame, _ := v.take()
		if frame == nil || frame.Seq != 7 {
			t.Fatalf("viewer %s did not receive the frame", name)
		}
	}

	hub.remove(a)
	if hub.Count() != 1 {
		t.Fatalf("expected 1 viewer after remove, got %d", hub.Count())
	}
}

// The client reads the header length before the JSON, so the layout must be
// exact: one type byte, four length bytes, the header, then the image.
func TestEncodeFrameLayout(t *testing.T) {
	frame := &Frame{Seq: 3, DeviceWidth: 1280, DeviceHeight: 800, JPEG: []byte{0xFF, 0xD8, 0xFF}}

	out, err := encodeFrame(frame)
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	if out[0] != msgFrame {
		t.Fatalf("expected the type byte %#x, got %#x", msgFrame, out[0])
	}

	headerLen := binary.BigEndian.Uint32(out[1:5])
	var header Frame
	if err := json.Unmarshal(out[5:5+headerLen], &header); err != nil {
		t.Fatalf("header is not valid JSON: %v", err)
	}
	if header.Seq != 3 || header.DeviceWidth != 1280 {
		t.Fatalf("header lost data: %+v", header)
	}

	body := out[5+headerLen:]
	if string(body) != string(frame.JPEG) {
		t.Fatalf("image bytes changed: %v", body)
	}
}

// The error guard must collapse a consecutive run and nothing more. Without a
// reset it means "ever sent" rather than "the last one sent", so the first
// "no page is selected" of a session silences every later one and a browser
// that wedges hours in swallows every click with no banner.
func TestViewerReportsAnErrorAgainAfterItIsCleared(t *testing.T) {
	v := newViewer()
	const msg = "no page is selected"

	if v.repeatError(msg) {
		t.Fatal("the first error must be reported")
	}
	if !v.repeatError(msg) {
		t.Fatal("an immediate repeat must be suppressed")
	}

	v.clearError()

	if v.repeatError(msg) {
		t.Fatal("the same error after a clear must be reported again")
	}
}
