package rdp

import (
	"slices"
	"testing"

	"github.com/go-rod/rod/lib/proto"
)

const (
	dragEnter = proto.InputDispatchDragEventTypeDragEnter
	dragOver  = proto.InputDispatchDragEventTypeDragOver
	drop      = proto.InputDispatchDragEventTypeDrop
)

// An HTML5 drag only reaches a drop target if the page sees the drag events in
// order, so the sequence each pointer event produces is the whole feature.
func TestDragPlanDrivesTheDragToADrop(t *testing.T) {
	cases := []struct {
		name        string
		kind        string
		entered     bool
		wantEvents  []proto.InputDispatchDragEventType
		wantHandled bool
		wantDone    bool
	}{
		{
			name:       "the first move enters the page",
			kind:       "moved",
			entered:    false,
			wantEvents: []proto.InputDispatchDragEventType{dragEnter},
			// Consumed: a mouseMoved alongside it would make the page act twice.
			wantHandled: true,
		},
		{
			name:        "later moves drag over",
			kind:        "moved",
			entered:     true,
			wantEvents:  []proto.InputDispatchDragEventType{dragOver},
			wantHandled: true,
		},
		{
			name:       "a release drops",
			kind:       "released",
			entered:    true,
			wantEvents: []proto.InputDispatchDragEventType{drop},
			// Not consumed: mouseReleased still has to lift the button.
			wantHandled: false,
			wantDone:    true,
		},
		{
			name:        "a release without a move still enters first",
			kind:        "released",
			entered:     false,
			wantEvents:  []proto.InputDispatchDragEventType{dragEnter, drop},
			wantHandled: false,
			wantDone:    true,
		},
		{
			name:        "a press is left to the mouse",
			kind:        "pressed",
			entered:     false,
			wantEvents:  nil,
			wantHandled: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			events, handled, done := dragPlan(c.kind, c.entered)
			if !slices.Equal(events, c.wantEvents) {
				t.Errorf("events = %v, want %v", events, c.wantEvents)
			}
			if handled != c.wantHandled {
				t.Errorf("handled = %v, want %v", handled, c.wantHandled)
			}
			if done != c.wantDone {
				t.Errorf("done = %v, want %v", done, c.wantDone)
			}
		})
	}
}

// With no drag in flight every pointer event must reach the mouse path
// untouched, or ordinary clicking stops working.
func TestDispatchDragIgnoresPointerEventsWithoutADrag(t *testing.T) {
	s := NewStreamer(NewHub(), Options{})

	for _, kind := range []string{"pressed", "moved", "released"} {
		handled, err := s.dispatchDrag(MouseMsg{Kind: kind}, nil)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", kind, err)
		}
		if handled {
			t.Errorf("%s: handled = true, want false with no drag in flight", kind)
		}
	}
}

// A drag belongs to the page it started on. stop() runs on every tab switch, so
// a drag surviving it would drop its payload on a page the user never dragged
// it to.
func TestStopClearsAnInFlightDrag(t *testing.T) {
	s := NewStreamer(NewHub(), Options{})
	s.drag = &dragSession{data: &proto.InputDragData{}}

	s.stop()

	if s.drag != nil {
		t.Error("drag survived stop()")
	}
}
