package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

func TestIntegrate2ReaderSurfaces_Browser(t *testing.T) {
	msg := Message{ID: "post", Revision: 1, AuthorID: "person", Author: "Alex", Body: "Authored reader paragraph", Sequence: 1}
	base := Model{Locale: "en-US", State: StateReady, SelectedID: "room", CurrentUser: "reader", CurrentTenantID: "tenant", Conversations: []Conversation{{ID: "room", Kind: PublicChannel, Name: "General", Joined: true}}, Messages: []Message{msg}, ThreadParentID: msg.ID, ThreadParent: &msg, ThreadMessages: []Message{msg}, ShowThread: true, ShareSource: &msg, ChannelPins: []ChannelPin{{PostID: msg.ID, Revision: 1, Body: msg.Body}}, Search: "other", SearchMessages: []SearchMessage{{Message: msg, ConversationID: "room", ConversationName: "General"}}}
	surfaces := map[string]func(Model) ui.Node{
		"timeline":        func(m Model) ui.Node { return message(m, handlers{}, msg, false) },
		"thread":          func(m Model) ui.Node { return threadPane(m, handlers{}) },
		"search":          searchResultsPanel,
		"pins":            pinnedSection,
		"todo pin picker": func(m Model) ui.Node { return channelTodoSection(m, handlers{}) },
		"share":           func(m Model) ui.Node { return shareDialog(m, handlers{}) },
		"private answer": func(m Model) ui.Node {
			return renderPersonaPrivateAnswer(m, localUI{}, EphemeralMessage{ID: msg.ID, Body: msg.Body}, PersonaProgressProjection{})
		},
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		base.Locale = locale
		for name, surface := range surfaces {
			t.Run(locale+"/"+name, func(t *testing.T) {
				before := renderNode(t, surface(base))
				if !strings.Contains(before, msg.Body) {
					t.Fatalf("fixture does not display authored paragraph: %s", before)
				}
				empty := base
				empty.ReaderRenderings = map[string]ReaderRendering{}
				empty.ReaderSelections = map[string]ReaderSelection{}
				if after := renderNode(t, surface(empty)); after != before {
					t.Fatalf("no-policy reader changed existing tree:\n%s\n%s", before, after)
				}
				selected := base
				selected.ReaderSelections = map[string]ReaderSelection{msg.ID: {Rendering: chatrender.Rendering{Message: msg.ID, Revision: 1, Text: "Selected reader paragraph", Tone: chatrender.AsWritten}, Mark: chatrender.Mark{State: "ready"}}}
				after := renderNode(t, surface(selected))
				if !strings.Contains(after, "Selected reader paragraph") || strings.Contains(after, msg.Body) {
					t.Fatalf("surface bypassed reader selection: %s", after)
				}
			})
		}
	}
}

func TestIntegrate2ReaderRevisionAndAuthoredControls(t *testing.T) {
	msg := Message{ID: "post", Revision: 2, Body: "Authored paragraph"}
	m := Model{Locale: "de-DE", ReaderSelections: map[string]ReaderSelection{"post": {Rendering: chatrender.Rendering{Message: "post", Revision: 1, Text: "Stale paragraph"}}}}
	if body := ReaderMessageBody(m, msg); body != msg.Body {
		t.Fatal("stale revision selected", body)
	}
	m.ReaderPending = true
	m.ReaderPolicyRequired = true
	m.ReaderSelections = map[string]ReaderSelection{"post": {Revision: msg.Revision, Mark: chatrender.Mark{State: "pending"}}}
	if body := ReaderMessageBody(m, msg); body != RenderingText(m.Locale, "pending") {
		t.Fatal("pending required policy exposed original", body)
	}
	m.ReaderSelections["post"] = ReaderSelection{Mark: chatrender.Mark{State: "unavailable"}}
	if body := ReaderMessageBody(m, msg); body != RenderingText(m.Locale, "unavailable") {
		t.Fatal("failed policy exposed original", body)
	}
	m.ReaderPending = false
	m.ReaderSelections = nil
	if body := readerReplyBody(m, Message{ID: "post", Revision: 2, Body: "Answer\n[control]"}, "Answer"); body != "Answer" {
		t.Fatal("authored envelope changed", body)
	}
	m.ReaderSelections = map[string]ReaderSelection{"post": {Rendering: chatrender.Rendering{Message: "post", Revision: 2, Text: "Selected\n[control]", Tone: chatrender.AsWritten}, Mark: chatrender.Mark{State: "ready"}}}
	if body := readerReplyBody(m, Message{ID: "post", Revision: 2, Body: "Answer\n[control]"}, "Answer"); body != "Selected" {
		t.Fatal("control bytes escaped into reader paragraph", body)
	}
	if heights := messageHeights(m, []Message{msg}, nil); len(heights) != 1 || heights[0] <= 0 {
		t.Fatal("reader height", heights)
	}
}

func TestIntegrate2UnavailableControlsAndSavedCount_Browser(t *testing.T) {
	m := chat4Fixture("en-US", "sent", true)
	m.ChatFeatures = &ChatFeatures{}
	if integrate2ReadingSettings(m) != nil || chatgateDetailsSection(m) != nil || chatmapComposerControl(m, "chat-composer", false) != nil || chattoneToolbar(m, "chat-composer", false) != nil || filterSettingsEntry(m, m.selected()) != nil {
		t.Fatal("unavailable control was exposed")
	}
	for _, n := range []int{0, 4} {
		m.SavedOpenCount = n
		markup := renderNode(t, chatsaveSidebar(m))
		if (n == 0) != strings.Contains(markup, " hidden") {
			t.Fatal("saved empty count", markup)
		}
		if n > 0 && !strings.Contains(markup, ">4</span>") {
			t.Fatal("missing saved count", markup)
		}
	}
	if !strings.Contains(Stylesheet, ".chat-rail-row:has(.agent-badge){display:flex;flex-wrap:nowrap;align-items:baseline}") || !strings.Contains(Stylesheet, "[data-saved-count][hidden]{display:none}") {
		t.Fatal("badge baseline or empty count CSS missing")
	}
}

func TestIntegrate2ControlReceiptsAndStatus_Browser(t *testing.T) {
	m := Model{Locale: "en-US", SelectedID: "room", CurrentUser: "reader"}
	msg := Message{ID: "receipt", Revision: 1, Body: legacyPrivateAnswerReceiptBody}
	before, _ := legacyPrivateAnswerReceipt(m, msg)
	markup := renderNode(t, before)
	m.ReaderSelections = map[string]ReaderSelection{}
	after, _ := legacyPrivateAnswerReceipt(m, msg)
	if renderNode(t, after) != markup {
		t.Fatal("no-policy control receipt changed")
	}
	m.ChannelStatuses = map[string]ChannelStatusView{"room": {Status: chat.ChannelStatus{Status: chatpolicy.StatusLocked, Revision: 1}, CanPost: false}}
	if panel := integrate2StatusDetails(m); panel == nil || !strings.Contains(renderNode(t, panel), chatstateText(m, "locked")) {
		t.Fatal("status details missing")
	}
}

func TestIntegrate2ArchivedAndSearchStatus_Browser(t *testing.T) {
	m := Model{Locale: "en-US", SelectedID: "room", Search: "Past", ChatFeatures: &ChatFeatures{Status: true}, SearchChannels: []Conversation{{ID: "archive", Kind: PublicChannel, Name: "Past work"}}, ChannelStatuses: map[string]ChannelStatusView{"archive": {Status: chat.ChannelStatus{ConversationID: "archive", Name: "Past work", Status: chatpolicy.StatusArchived, Revision: 1}}}}
	for _, tree := range []ui.Node{integrate2ArchivedChannels(m), searchResultsPanel(m)} {
		markup := renderNode(t, tree)
		if !strings.Contains(markup, "Past work") || !strings.Contains(markup, chatstateText(m, "archived")) {
			t.Fatal("archive/status binding missing", markup)
		}
	}
}

func TestIntegrate2AnnouncementSelection_Browser(t *testing.T) {
	original := AgentAnnouncementMessage{AgentName: "Helper", OwnerName: "Alex", Text: "Authored announcement"}
	body, err := AnnouncementMessageBody(original)
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{ID: "announcement", Revision: 1, AuthorID: "helper", Author: "Helper", Body: body, PersonaActor: &PersonaActor{Trusted: true, PersonaID: "helper", AgentID: "helper"}}
	m := Model{Locale: "en-US"}
	before := renderNode(t, message(m, handlers{}, msg, false))
	m.ReaderSelections = map[string]ReaderSelection{}
	if renderNode(t, message(m, handlers{}, msg, false)) != before || !strings.Contains(before, original.Text) {
		t.Fatal("no-policy announcement changed")
	}
	m.ReaderSelections[msg.ID] = ReaderSelection{Rendering: chatrender.Rendering{Message: msg.ID, Revision: 1, Text: "Selected announcement", Tone: chatrender.AsWritten}, Mark: chatrender.Mark{State: "ready"}}
	markup := renderNode(t, message(m, handlers{}, msg, false))
	if !strings.Contains(markup, "Selected announcement") || strings.Contains(markup, original.Text) || strings.Contains(markup, AgentAnnouncementBodyPrefix) {
		t.Fatal("announcement bypassed selection or exposed controls", markup)
	}
	if AuthoredReaderText(body) != original.Text {
		t.Fatal("authored control source")
	}
}

func TestIntegrate2FilterSearchDestination_Browser(t *testing.T) {
	m := Model{Locale: "en-US", SelectedID: "room", CurrentUser: "admin", IsTenantAdmin: true, Conversations: []Conversation{{ID: "room", OwnerID: "admin", Kind: PublicChannel}}, ChatFeatures: &ChatFeatures{Filters: true}, ShowFilterSettings: true, FilterSettings: func() ui.Node { return ui.Text("Filter editor") }}
	if tree := renderNode(t, filterSettingsEntry(m, m.selected())); !strings.Contains(tree, "Filter editor") {
		t.Fatal("search destination did not open filter settings", tree)
	}
}
