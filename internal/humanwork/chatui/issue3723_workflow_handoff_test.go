package chatui

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3723Model(locale string) (Model, Message, *[]string) {
	m, msg := chatbug064Model()
	m.Locale = locale
	calls := &[]string{}
	m.MessageWorkflow = MessageWorkflowState{Enabled: true, Start: func(conversationID, messageID string) {
		*calls = append(*calls, conversationID+"|"+messageID)
	}}
	return m, msg, calls
}

func issue3723Names(m Model) []string {
	state := composerCommandMenuNext(m, composerCommandMenu{}, "chat-composer", "/", 1)
	return composerCommandShown(m, state, "chat-composer")
}

// Issue 3723: the message menu offers a promotion handoff where the viewer's
// workspace can reach the promotion flow, and nowhere else.
func TestIssue3723_MenuOffersStartPromotionOnlyWhereItWorks(t *testing.T) {
	for locale, label := range map[string]string{
		"en-US": "Start a promotion from this message",
		"de-DE": "Beförderung aus dieser Nachricht starten",
		"ar":    "بدء ترقية من هذه الرسالة",
	} {
		m, msg, _ := issue3723Model(locale)
		menu := renderNode(t, spanOf(chatux022MenuItems(m, msg)))
		if !strings.Contains(menu, `data-action="workflow-start"`) || !strings.Contains(menu, label) {
			t.Errorf("%s: menu has no %q row: %s", locale, label, menu)
		}
		if !strings.Contains(menu, `role="menuitem"`) {
			t.Errorf("%s: row is not a menu item: %s", locale, menu)
		}
	}
	for name, mutate := range map[string]func(*Model, *Message){
		"workspace cannot reach the flow": func(m *Model, _ *Message) { m.MessageWorkflow.Enabled = false },
		"no way to open it":               func(m *Model, _ *Message) { m.MessageWorkflow.Start = nil },
		"signed out":                      func(m *Model, _ *Message) { m.CurrentUser = "" },
		"removed message":                 func(_ *Model, msg *Message) { msg.Body = chat.RemovedByAdministrator },
		"another company hosts the room": func(m *Model, _ *Message) {
			for i := range m.Conversations {
				if m.Conversations[i].ID == m.SelectedID {
					m.Conversations[i].HostTenantID = "other-tenant"
				}
			}
			m.CurrentTenantID = "this-tenant"
		},
		"conversation with an agent": func(m *Model, _ *Message) {
			for i := range m.Conversations {
				if m.Conversations[i].ID == m.SelectedID {
					m.Conversations[i].Agent = true
				}
			}
		},
	} {
		off, offMsg, _ := issue3723Model("en-US")
		mutate(&off, &offMsg)
		if strings.Contains(renderNode(t, spanOf(chatux022MenuItems(off, offMsg))), "workflow-start") {
			t.Errorf("%s: the promotion row is offered", name)
		}
	}
}

// Choosing the row hands off with the conversation and the message ids and
// closes the menu.
func TestIssue3723_ChoosingStartPromotionHandsOffWithBothIDs(t *testing.T) {
	m, _, calls := issue3723Model("en-US")
	var menu []string
	m.Callbacks.OpenMenu = func(id string) { menu = append(menu, "menu:"+id) }
	m.actWith("workflow-start", "m1", "")
	if got := *calls; !reflect.DeepEqual(got, []string{m.SelectedID + "|m1"}) {
		t.Fatalf("handoff calls=%v selected=%q", got, m.SelectedID)
	}
	if strings.Join(menu, ",") != "menu:" {
		t.Fatalf("menu not closed: %v", menu)
	}
	// Not enabled: nothing happens.
	off, _, offCalls := issue3723Model("en-US")
	off.MessageWorkflow.Enabled = false
	off.actWith("workflow-start", "m1", "")
	if len(*offCalls) != 0 {
		t.Fatalf("handoff ran while disabled: %v", *offCalls)
	}
}

// The handoff address carries only opaque ids into the existing governed
// promotion picker, never message text, and the picker still decides rights.
func TestIssue3723_HandoffHrefCarriesIdsOnly(t *testing.T) {
	href := WorkflowHandoffHref("en-US", "conv 1", "post&2")
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/workspace/app/people" {
		t.Fatalf("path=%q", parsed.Path)
	}
	q := parsed.Query()
	if q.Get("eligible") != "1" || q.Get("from_conversation") != "conv 1" || q.Get("from_message") != "post&2" || q.Get("locale") != "" {
		t.Fatalf("query=%v", q)
	}
	if got := WorkflowHandoffHref("de-DE", "c", ""); !strings.Contains(got, "locale=de-DE") || strings.Contains(got, "from_message") {
		t.Fatalf("conversation-only handoff = %q", got)
	}
	if WorkflowHandoffHref("en-US", "", "m") != "" {
		t.Fatal("a handoff with no conversation must have no address")
	}
	if WorkflowHandoffHref("en-US", strings.Repeat("x", 300), "m") != "" {
		t.Fatal("an oversized id must not be carried")
	}
}

func TestIssue3723_PromoteCommandIsListedOnlyWhereItWorks(t *testing.T) {
	m, _, _ := issue3723Model("en-US")
	found := false
	for _, name := range issue3723Names(m) {
		found = found || name == "promote"
	}
	if !found {
		t.Fatalf("/ lists %v", issue3723Names(m))
	}
	for locale, want := range map[string]string{
		"en-US": "Start a promotion request from this conversation",
		"de-DE": "Eine Beförderung aus dieser Unterhaltung heraus starten",
		"ar":    "بدء طلب ترقية من هذه المحادثة",
	} {
		l, _, _ := issue3723Model(locale)
		state := composerCommandMenuNext(l, composerCommandMenu{}, "chat-composer", "/", 1)
		if markup := renderNode(t, composerCommandMenuView(l, state, "chat-composer", 1)); !strings.Contains(markup, ">/promote<") || !strings.Contains(markup, want) {
			t.Errorf("%s: list lacks the promote row (%q): %s", locale, want, markup)
		}
	}
	off, _, _ := issue3723Model("en-US")
	off.MessageWorkflow.Enabled = false
	for _, name := range issue3723Names(off) {
		if name == "promote" {
			t.Fatal("/promote offered while the workspace cannot reach the flow")
		}
	}
}

func TestIssue3723_PromoteCommandHandsOffAndConsumesTheLine(t *testing.T) {
	m, _, calls := issue3723Model("en-US")
	cleared := 0
	rt := composerCommandRuntime{Model: m, ClearDraft: func() { cleared++ }, Notice: func(string) {}}
	outcome, _ := defaultComposerCommands().dispatch(rt, "/promote")
	if outcome != composerCommandRan || cleared != 1 {
		t.Fatalf("outcome=%v cleared=%d", outcome, cleared)
	}
	if got := *calls; !reflect.DeepEqual(got, []string{m.SelectedID + "|"}) {
		t.Fatalf("handoff calls=%v", got)
	}
	// Disabled: the word is not a command here, so nothing is handed off.
	off, _, offCalls := issue3723Model("en-US")
	off.MessageWorkflow.Enabled = false
	offRT := composerCommandRuntime{Model: off, ClearDraft: func() {}, Notice: func(string) {}, Unknown: func(string, string) {}}
	if outcome, _ := defaultComposerCommands().dispatch(offRT, "/promote"); outcome != composerCommandUnknown || len(*offCalls) != 0 {
		t.Fatalf("outcome=%v calls=%v", outcome, *offCalls)
	}
}
