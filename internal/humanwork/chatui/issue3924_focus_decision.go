package chatui

// focusFieldDone says whether focusField can stop watching. It stops once the
// person has moved focus to something else of their own; it must not stop
// while focus is still on the control that was pressed to cause the
// re-render, because that control can still be in the page for a frame or two
// before it is replaced (#3924: the "Close at end of day" button), and
// stopping then leaves focus on the page body once the button goes.
func focusFieldDone(frames int, lost, onTarget, onPressed bool) bool {
	limit := 45
	if onPressed && !lost {
		// Issue 3882: a busy page can take over a second to replace the pressed
		// control, longer than 45 frames; keep watching while it is still there.
		limit = 600
	}
	return frames >= limit || (!lost && !onTarget && !onPressed)
}
