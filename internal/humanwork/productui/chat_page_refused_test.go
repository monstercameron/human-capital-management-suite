package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3603_RefusedChannelIsNotWorkspaceSetup: a link to a conversation the
// reader may not see is explained as that conversation, not as a workspace that
// is not set up. A genuine capability absence keeps the setup panel.
func TestIssue3603_RefusedChannelIsNotWorkspaceSetup(t *testing.T) {
	view := View{Locale: ResolveProductLocale("en-US")}
	const setup = "Not set up for this workspace"

	refused := chatui.Model{State: chatui.StateError, Error: "This channel is no longer available.", SelectedID: "qa-private"}
	markup := renderUXBlindK(t, BuildChatPage(view, refused))
	if strings.Contains(markup, setup) {
		t.Fatal("a refused conversation is shown as a workspace that is not set up")
	}
	if !strings.Contains(markup, "This channel is no longer available.") {
		t.Fatal("the refused conversation has no neutral explanation on the page")
	}

	absent := chatui.Model{State: chatui.StateError, Error: "chat client is not configured"}
	if markup := renderUXBlindK(t, BuildChatPage(view, absent)); !strings.Contains(markup, setup) {
		t.Fatal("a workspace without the chat capability lost its setup panel")
	}
}
