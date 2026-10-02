package chatui

import (
	"strings"
	"testing"
)

// The Quiet hours content sits inside its panel in normal flow. An older rule
// positioned it absolutely, which left the panel 17 px tall and the content
// clipped: the "empty strip" of CHATBUG-026.
func TestQuietHoursContentIsInFlowInsideItsPanel(t *testing.T) {
	if !strings.Contains(ChatPolishStyles, ".chat-settings-layer>.rail-prefs-body{position:static;") {
		t.Fatal("the quiet hours content must be static inside .chat-settings-layer")
	}
}

// The Saved panel is docked under the page header at the end of the row. A
// floating placement (8 px from the top) covered the header, and no placement
// at all let the generic popover rule put it in the top-left corner.
func TestSavedPanelIsDockedUnderThePageHeader(t *testing.T) {
	if !strings.Contains(ChatPolishStyles, ".chatsave-panel{inset-block:var(--chatsave-top,82px) 0;inset-inline:auto 0;") {
		t.Fatal("the Saved panel must start under the page header and sit at the inline end")
	}
}
