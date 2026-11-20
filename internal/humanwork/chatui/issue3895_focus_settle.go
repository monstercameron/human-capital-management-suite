package chatui

// focusSettleFrames is how many frames chatux026Focus keeps checking that the
// caret stayed on the element it asked for (issue 3895). A redraw that lands
// after the first focus replaces the element and drops the caret on the page,
// which collapses a thread reply form that shows only while it has focus.
const focusSettleFrames = 3

// focusNeedsAnotherFrame is whether the caret still has to be put back: the
// element is not the one holding focus and the settle frames are not used up.
func focusNeedsAnotherFrame(onTarget bool, framesChecked int) bool {
	return !onTarget && framesChecked < focusSettleFrames
}
