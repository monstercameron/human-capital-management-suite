package productui

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatPageUsesAuthenticatedProjectionAndLoadingState(t *testing.T) {
	view := NewView(PageChat, "tenant-1", "principal-1", "scope-1")
	view.Chat = chatui.Model{}
	markup, err := ui.RenderToString(BuildPageContent(view))
	if err != nil {
		t.Fatalf("render chat page: %v", err)
	}
	for _, want := range []string{"chat-workspace", "chat-state=\"loading\"", "principal-1", "Conversations"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("chat markup missing %q: %s", want, markup)
		}
	}
}

func TestChannelPollShortcutWaitsForStablePaneAndCancelsAfterRoomSwitch(t *testing.T) {
	source, err := os.ReadFile("../chatui/todo_focus_js.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(source)
	for _, contract := range []string{
		"func FocusChannelPoll(conversationID string)",
		"generation != channelPollFocusGeneration",
		".chat-row.selected",
		"getAttribute", "data-id",
		"!= conversationID",
		`data-pane="details"`,
		`getAttribute", "data-loading"`,
		"pane.Set(\"scrollTop\"",
		"scheduleChannelPollFocus(channelPollFocusGeneration, conversationID, 120, 0, false)",
		"remaining-1",
		"if paneSeen",
		"stableFrames >= 2",
		"section.Call(\"focus\"",
	} {
		if !strings.Contains(code, contract) {
			t.Errorf("poll shortcut focus race guard missing %q", contract)
		}
	}
}

func TestChatShellContainmentRulesStayOutsideChatScope(t *testing.T) {
	css := Stylesheet()
	scopeStart := strings.Index(css, "@scope (.chat-workspace){")
	shellRuleStart := strings.Index(css, ".main.page-full-bleed{")
	if scopeStart < 0 || shellRuleStart <= scopeStart {
		t.Fatalf("chat scope or full-bleed shell rule missing: scope=%d shell=%d", scopeStart, shellRuleStart)
	}
	scopeAndOverlay := css[scopeStart:shellRuleStart]
	if opens, closes := strings.Count(scopeAndOverlay, "{"), strings.Count(scopeAndOverlay, "}"); opens != closes {
		t.Fatalf("chat scope is not closed before shell rules: %d opening braces, %d closing braces", opens, closes)
	}
	if !strings.Contains(css[shellRuleStart:], ".main.page-full-bleed{max-width:none;padding:0;height:100%;min-height:0;display:flex;flex-direction:column}") || !strings.Contains(css[shellRuleStart:], ".main-scroll.main-scroll-full-bleed{overflow:hidden;scrollbar-gutter:auto}") {
		t.Fatal("chat shell sizing or its single scroll owner rule is missing")
	}
}
