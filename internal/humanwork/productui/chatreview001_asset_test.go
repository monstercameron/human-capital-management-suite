package productui

import "testing"

// CHATREVIEW-001: the cached control-sheet asset names the same bytes as a
// fresh naming, so linked documents keep byte-identical style elements.
func TestChatReview001_ControlStylesheetAssetMatchesFresh(t *testing.T) {
	fresh := NewStylesheetAsset(ControlStylesheetAssetPrefix, ControlStylesheet())
	cached := ControlStylesheetAsset()
	if cached != fresh {
		t.Fatalf("cached asset %+v differs from fresh %+v", cached, fresh)
	}
	if got := linkedDefaultStyleElements(PageChat); got != linkedStyleElements(ControlStylesheet(), PageChat) {
		t.Fatalf("cached style elements differ: %q", got)
	}
	if got := linkedDefaultStyleElements(PageHome); got != linkedStyleElements(ControlStylesheet(), PageHome) {
		t.Fatalf("cached style elements differ off chat: %q", got)
	}
}
