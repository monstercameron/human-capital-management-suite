package chatui

// Issue 3890: the section name form (rename, or name for a new section) must
// take focus when it opens, be scrolled fully into the sidebar's scroll area,
// and give focus back to the control that opened it when it is put away.

// issue3890Margin is the breathing room kept between the form and the edge of
// the sidebar's scroll area.
const issue3890Margin = 8.0

// issue3890ScrollDelta is how far the sidebar's scroll area has to scroll so a
// form spanning top..bottom is fully inside viewTop..viewBottom: positive
// scrolls down, negative up, zero when it is already inside. A form taller
// than the area lines its top (the label and the field) up with the area's top.
func issue3890ScrollDelta(viewTop, viewBottom, top, bottom float64) float64 {
	low, high := viewTop+issue3890Margin, viewBottom-issue3890Margin
	switch {
	case bottom-top >= high-low, top < low:
		return top - low
	case bottom > high:
		return bottom - high
	}
	return 0
}

// issue3890ReturnID is the id of the options control (the row or section
// three dots) that opened the name form that is being put away, or "".
func issue3890ReturnID(renaming, moveTo string) string {
	switch {
	case renaming != "":
		return chatside001MenuPrefix + renaming
	case moveTo != "":
		return moveTo
	}
	return ""
}
