package rdp

import (
	"fmt"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// dragSession is an HTML5 drag that Chrome handed back instead of running.
//
// Chrome's drag controller sits above the renderer's mouse handling. On a real
// press-and-move over a draggable element it starts an OS drag loop and stops
// delivering ordinary mouse moves to the page. A synthetic mouse event never
// enters that loop, so dragstart never fires and no DataTransfer is built.
//
// Input.setInterceptDrags makes Chrome report the drag through
// Input.dragIntercepted rather than running it, leaving us to drive it with
// Input.dispatchDragEvent. That is the only route by which a dragged element
// reaches a drop target over a remote view.
type dragSession struct {
	data *proto.InputDragData
	// entered records whether dragEnter has been dispatched. The intercept
	// carries no coordinates, so the drag can only enter the page once a
	// pointer position is known, which is the next move or the release.
	entered bool
}

// interceptDrags asks Chrome to hand drags back rather than running them, and
// listens for the ones it intercepts.
//
// gen guards against a stale stream: an event from a screencast that has since
// been replaced must not install a drag on its successor.
func (s *Streamer) interceptDrags(bound *rod.Page, gen int) error {
	if err := (proto.InputSetInterceptDrags{Enabled: true}).Call(bound); err != nil {
		return fmt.Errorf("failed to intercept drags: %w", err)
	}

	go bound.EachEvent(func(e *proto.InputDragIntercepted) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.gen != gen {
			return
		}
		s.drag = &dragSession{data: e.Data}
	})()

	return nil
}

// dragEvent dispatches one drag event at the given point.
func dragEvent(
	page *rod.Page,
	kind proto.InputDispatchDragEventType,
	x, y float64,
	data *proto.InputDragData,
	mod int,
) error {
	ev := proto.InputDispatchDragEvent{
		Type:      kind,
		X:         x,
		Y:         y,
		Data:      data,
		Modifiers: mod,
	}
	if err := ev.Call(page); err != nil {
		return fmt.Errorf("failed to dispatch the %s event: %w", kind, err)
	}
	return nil
}

// dragPlan decides what an intercepted drag makes of one pointer event.
//
// events are dispatched in order. handled reports that the pointer event is
// spent, so no mouse event should follow it: Chrome ignores a mouseMoved when
// deciding what a drop target sees, and sending both makes the page act twice.
// A release is deliberately not handled, because the button still has to lift
// once the drop is done. done ends the drag.
func dragPlan(kind string, entered bool) (events []proto.InputDispatchDragEventType, handled, done bool) {
	switch kind {
	case "moved":
		if entered {
			return []proto.InputDispatchDragEventType{
				proto.InputDispatchDragEventTypeDragOver,
			}, true, false
		}
		return []proto.InputDispatchDragEventType{
			proto.InputDispatchDragEventTypeDragEnter,
		}, true, false

	case "released":
		// Chrome drops nothing on a target it never saw the drag enter, so a
		// drag released without an intervening move needs both events.
		if entered {
			return []proto.InputDispatchDragEventType{
				proto.InputDispatchDragEventTypeDrop,
			}, false, true
		}
		return []proto.InputDispatchDragEventType{
			proto.InputDispatchDragEventTypeDragEnter,
			proto.InputDispatchDragEventTypeDrop,
		}, false, true
	}

	// A press starts a drag rather than continuing one, so leave it alone.
	return nil, false, false
}

// dispatchDrag advances an intercepted drag and reports whether it consumed the
// pointer event.
func (s *Streamer) dispatchDrag(m MouseMsg, page *rod.Page) (bool, error) {
	s.mu.Lock()
	drag := s.drag
	if drag == nil {
		s.mu.Unlock()
		return false, nil
	}
	data := drag.data
	events, handled, done := dragPlan(m.Kind, drag.entered)
	if m.Kind == "moved" {
		drag.entered = true
	}
	if done {
		s.drag = nil
	}
	s.mu.Unlock()

	for _, kind := range events {
		if err := dragEvent(page, kind, m.X, m.Y, data, m.Mod); err != nil {
			return handled, err
		}
	}
	return handled, nil
}
