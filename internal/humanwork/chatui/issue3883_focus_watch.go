package chatui

// issue3883WatchStep decides one frame of the watch that returns focus to the
// control that opened a panel once the panel is closed. lost says focus is on
// the page; openerUsable says the opener is in the page and laid out (at 1280 px
// the header's Pinned and Members shortcuts have no width while the details
// panel is open); onOpener and onPressed say where focus is. It reports whether
// to focus the opener now and whether the watch is over.
func issue3883WatchStep(frames int, lost, openerUsable, onOpener, onPressed bool) (focus, done bool) {
	focus = lost && openerUsable
	return focus, focusFieldDone(frames, lost && !focus, onOpener, onPressed)
}
