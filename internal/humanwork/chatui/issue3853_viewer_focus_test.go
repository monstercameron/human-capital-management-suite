package chatui

import "testing"

// Closing the viewer after stepping through the gallery returns focus to the
// thumbnail that opened it, not to the last image viewed.
func TestIssue3853_FocusReturnsToOpeningThumbnail(t *testing.T) {
	tests := []struct {
		name                                   string
		originConnected, originFoundByID, last bool
		want                                   chatImageViewerFocus
	}{
		{"direct close keeps the opener", true, false, true, chatImageViewerFocusOrigin},
		{"opener wins even when the last viewed image is gone", true, false, false, chatImageViewerFocusOrigin},
		{"opener re-rendered, found again by its id", false, true, true, chatImageViewerFocusOriginByID},
		{"opener unmounted, last viewed is scrolled into view", false, false, true, chatImageViewerFocusLast},
		{"nothing left to focus", false, false, false, chatImageViewerFocusNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatImageViewerFocusPlan(tc.originConnected, tc.originFoundByID, tc.last); got != tc.want {
				t.Fatalf("plan = %v, want %v", got, tc.want)
			}
		})
	}
}

// Only the fallback may move the page; keeping the original scroll position is
// the point of returning to the opener.
func TestIssue3853_OnlyTheFallbackScrolls(t *testing.T) {
	for focus, want := range map[chatImageViewerFocus]bool{
		chatImageViewerFocusOrigin:     false,
		chatImageViewerFocusOriginByID: false,
		chatImageViewerFocusLast:       true,
		chatImageViewerFocusNone:       false,
	} {
		if got := focus.scrolls(); got != want {
			t.Fatalf("%v.scrolls() = %v, want %v", focus, got, want)
		}
	}
}
