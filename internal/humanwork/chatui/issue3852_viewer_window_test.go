package chatui

import (
	"strings"
	"testing"
)

// The viewer steps through the thumbnails that are on the page. When the
// conversation has more messages than are loaded, its count must not read as
// the whole gallery.
func TestIssue3852_PositionSaysLoadedWhenHistoryIsPartial(t *testing.T) {
	want := map[string]string{
		"en-US": "2 of 8 loaded images",
		"de-DE": "2 von 8 geladenen Bildern",
		"ar":    "٢ من ٨ صور محملة",
	}
	for locale, expect := range want {
		labels := chatbug083ViewerLabels(locale, "t", "c", "d", "a", "f")
		if got := chatImageViewerPosition(labels, 2, 8, true); got != expect {
			t.Fatalf("%s partial position = %q, want %q", locale, got, expect)
		}
	}
}

func TestIssue3852_PositionStaysPlainWhenEverythingIsLoaded(t *testing.T) {
	labels := chatbug083ViewerLabels("en-US", "t", "c", "d", "a", "f")
	got := chatImageViewerPosition(labels, 2, 8, false)
	if got != "2 of 8" || strings.Contains(got, "loaded") {
		t.Fatalf("complete position = %q, want %q", got, "2 of 8")
	}
}
