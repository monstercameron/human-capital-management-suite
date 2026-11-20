package chatui

import "testing"

// Returning focus to the opening thumbnail must leave it on screen: when the
// list moved while the viewer was open and the thumbnail is outside the part of
// the list a reader can see, it is brought into view (#3853).
func TestIssue3853_OpenerOutsideTheVisibleListIsScrolledIn(t *testing.T) {
	tests := []struct {
		name                                 string
		top, bottom, regionTop, regionBottom float64
		want                                 bool
	}{
		{"fully inside", 200, 236, 134, 900, false},
		{"above the list", -164, -128, 134, 900, true},
		{"partly under the list top", 120, 156, 134, 900, true},
		{"below the list", 950, 986, 134, 900, true},
		{"partly below the list", 880, 916, 134, 900, true},
		{"exactly fits", 134, 900, 134, 900, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatImageViewerNeedsScroll(tc.top, tc.bottom, tc.regionTop, tc.regionBottom); got != tc.want {
				t.Fatalf("chatImageViewerNeedsScroll(%v,%v,%v,%v) = %v, want %v", tc.top, tc.bottom, tc.regionTop, tc.regionBottom, got, tc.want)
			}
		})
	}
}
