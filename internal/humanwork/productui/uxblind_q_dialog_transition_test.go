package productui

import "testing"

// TestIssue3596_DialogAppearedClosesOnlyOnTransition: the shell's disclosure
// controller closes an open menu when a dialog appears, not on every mutation
// while a dialog is already showing.
func TestIssue3596_DialogAppearedClosesOnlyOnTransition(t *testing.T) {
	for _, tc := range []struct {
		name            string
		wasVisible, now bool
		want            bool
	}{
		{name: "dialog appears over the menu", wasVisible: false, now: true, want: true},
		{name: "dialog stays showing while the menu opens", wasVisible: true, now: true, want: false},
		{name: "dialog goes away", wasVisible: true, now: false, want: false},
		{name: "no dialog anywhere", wasVisible: false, now: false, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := uxblindQDialogAppeared(tc.wasVisible, tc.now); got != tc.want {
				t.Fatalf("uxblindQDialogAppeared(%v, %v) = %v, want %v", tc.wasVisible, tc.now, got, tc.want)
			}
		})
	}
}
