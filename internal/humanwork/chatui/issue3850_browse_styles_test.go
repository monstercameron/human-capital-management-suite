package chatui

import (
	"strings"
	"testing"
)

// TestIssue3850_BrowseJoinVisibleAndRowsReflow pins the Browse channels list:
// the row actions are visible before any hover or focus, and at phone widths a
// row wraps (names wrap, actions drop under them) instead of truncating names
// and scrolling sideways.
func TestIssue3850_BrowseJoinVisibleAndRowsReflow(t *testing.T) {
	css := issue3850BrowseStyles
	if css == "" || !strings.Contains(ChatLane2Styles, css) {
		t.Fatal("issue 3850 browse styles are not part of the lane-2 stylesheet")
	}
	if strings.Index(Stylesheet, ChatLane2Styles) < strings.Index(Stylesheet, ".browse-row .browse-actions>.button{opacity:0") {
		t.Fatal("the browse override must come after the hover-reveal rule")
	}
	for _, want := range []string{
		".chat-workspace .browse-row .browse-actions>.button{opacity:1}",
		".chat-workspace .browse-list{overflow-x:hidden}",
		"@media(max-width:560px)",
		".chat-workspace .browse-row{flex-wrap:wrap}",
		".chat-workspace .browse-text strong{white-space:normal;overflow-wrap:anywhere}",
		".chat-workspace .browse-actions{flex:1 1 100%",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("browse styles missing %q", want)
		}
	}
}
