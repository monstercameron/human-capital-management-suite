package chatui

import "testing"

func TestIssue3895_FocusKeepsLookingUntilItHolds(t *testing.T) {
	if focusNeedsAnotherFrame(true, 1) {
		t.Fatal("focus on the target needs no further frame")
	}
	if !focusNeedsAnotherFrame(false, 1) {
		t.Fatal("focus lost after the first try must be retried")
	}
	if focusNeedsAnotherFrame(false, focusSettleFrames) {
		t.Fatal("retries must stop after the settle frames")
	}
}
