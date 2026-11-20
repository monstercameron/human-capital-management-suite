package chatui

// A search hit opened from another channel is focused before, or while, the
// destination timeline is laid out: the room switch lands the list on its newest
// message after the first focus, and images change row heights afterwards. So the
// browser half keeps checking the target for a short settle window and puts it
// back in view whenever something moved it out, until the reader scrolls
// themselves (#3989).
const (
	// issue3989SettleFrames is how many animation frames the target is watched
	// for after the search hit is opened (about two seconds at 60 Hz).
	issue3989SettleFrames = 120
	// issue3989MissingFrames is how many frames the target may be absent from the
	// page before the hit is reported as unavailable.
	issue3989MissingFrames = 90
)

// issue3989NeedsRecenter reports whether the target row has left the visible part
// of its timeline and has to be scrolled back. All values are viewport
// coordinates (getBoundingClientRect). A row taller than the timeline counts as
// visible while it covers the timeline's middle, so a long hit is not pulled
// back and forth.
func issue3989NeedsRecenter(rowTop, rowBottom, listTop, listBottom float64) bool {
	if listBottom <= listTop {
		return false
	}
	if rowBottom <= listTop || rowTop >= listBottom {
		return true
	}
	if rowBottom-rowTop > listBottom-listTop {
		middle := (listTop + listBottom) / 2
		return rowTop > middle || rowBottom < middle
	}
	return rowTop < listTop || rowBottom > listBottom
}
