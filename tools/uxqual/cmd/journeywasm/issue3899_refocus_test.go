package main

import "testing"

// A redraw right after the focus move drops focus to the body; it is put back
// on the first new result, but never taken from a control the person chose.
func TestIssue3899_FocusIsRestoredWhenARedrawDropsIt(t *testing.T) {
	if !issue3899ShouldRefocus(true, true) {
		t.Fatal("focus fell to the body and the new result exists: refocus")
	}
	if issue3899ShouldRefocus(false, true) {
		t.Fatal("focus is on a control the person chose: leave it")
	}
	if issue3899ShouldRefocus(true, false) {
		t.Fatal("there is no new result to focus")
	}
	if len(issue3899RefocusDelays) == 0 {
		t.Fatal("the focus must be re-checked after the follow-up redraws")
	}
}
