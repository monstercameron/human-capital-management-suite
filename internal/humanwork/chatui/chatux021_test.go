package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// chatux021Model is #general as a workspace administrator sees it: a purpose, an
// agent and two people, the status form and the callbacks that make the
// controls live.
func chatux021Model() Model {
	room := Conversation{ID: "general", Name: "general", Kind: PublicChannel, OwnerID: "walt", MemberCount: 3, Joined: true}
	ref := ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "t", Display: "Policy Helper", ConversationID: room.ID}
	return Model{State: StateReady, Locale: "en-US", ShowDetails: true, SelectedID: room.ID, CurrentUser: "walt", CurrentTenantID: "t", IsTenantAdmin: true,
		Conversations:           []Conversation{room},
		Members:                 []Member{{ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}, {ID: "loretta", HomeTenantID: "t", Name: "Loretta Haynes"}},
		ChannelTeam:             ChannelTeamWidget{Revision: 1, Purpose: "Policy questions", Members: []ChannelTeamMember{{HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}},
		ChannelProject:          ChannelProjectWidget{Revision: 1, Title: "Handbook"},
		ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ref, Handle: "policy-helper", Purpose: "Answer policy questions"}},
		PersonaLookup:           PersonaLookupReady, PersonaLookupConversationID: room.ID,
		ChannelStatuses: map[string]ChannelStatusView{room.ID: {
			Status:      chat.ChannelStatus{TenantID: "t", ConversationID: room.ID, Status: chatpolicy.StatusOpen, Revision: 1},
			Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusLocked, Permission: chatpolicy.PermissionChangeChannelStatus}, {Status: chatpolicy.StatusArchived, Permission: chatpolicy.PermissionChangeChannelStatus}},
		}},
		ChangeChannelStatus: func(chat.ChangeChannelStatusRequest) {},
		Callbacks: Callbacks{SetChannelTeamPurpose: func(string) {}, SendMessageWithReferences: func(string, string, []ChatReference) {}, SetConversationNotification: func(string, NotificationMode) {},
			CopyConversationAPICurl: func(string) {}, ToggleDetails: func(bool) {}, ClosePerson: func() {}, LoadMembers: func() {}},
	}
}

func chatux021Details(t *testing.T, m Model, h handlers) string {
	t.Helper()
	return renderNode(t, chatux005Details(m, h))
}

// The purpose is edited in place: the row shows it, pressing the row opens one
// box with Save and Cancel and the keys that work, and a save that went through
// says "Saved".
func TestTodo_CHATUX_021(t *testing.T) {
	m := chatux021Model()
	closed := chatux021Details(t, m, handlers{})
	if !strings.Contains(closed, `data-action="purpose-edit"`) || !strings.Contains(closed, "Policy questions") || strings.Contains(closed, `id="channel-team-purpose"`) {
		t.Fatalf("the closed row: %s", closed)
	}
	// The row and the box do not both say "Purpose" at once.
	if strings.Count(closed, ">Purpose<") != 1 {
		t.Errorf("the closed row repeats its label: %d", strings.Count(closed, ">Purpose<"))
	}
	open := chatux021Details(t, m, handlers{local: localUI{purposeEditing: true, purposeDraft: "Policy questions"}})
	for _, want := range []string{`id="channel-team-purpose"`, `data-action="purpose-cancel"`, "Enter to save · Esc to cancel", `type="submit"`, ">Save<", ">Cancel<"} {
		if !strings.Contains(open, want) {
			t.Errorf("the open box misses %q", want)
		}
	}
	if strings.Contains(open, `data-action="purpose-edit"`) || strings.Count(open, ">Purpose<") != 1 {
		t.Errorf("the open box keeps the row beside it or repeats the label: %d", strings.Count(open, ">Purpose<"))
	}
	// No purpose yet: the row invites one.
	empty := m
	empty.ChannelTeam.Purpose = ""
	if got := chatux021Details(t, empty, handlers{}); !strings.Contains(got, "Add a purpose") {
		t.Errorf("an empty purpose does not invite one: %s", got)
	}
	// Saved: two seconds of "Saved" once the model says the save went through,
	// never while it is still going, and what was typed shows while it goes.
	saved := m
	saved.PurposeSaved = true
	if got := chatux021Details(t, saved, handlers{}); !strings.Contains(got, "details-purpose-saved") || !strings.Contains(got, ">Saved<") {
		t.Errorf("no Saved after a save: %s", got)
	}
	pending := saved
	pending.ChannelWidgetsPending = true
	got := chatux021Details(t, pending, handlers{local: localUI{purposeDraft: "Hiring"}})
	if strings.Contains(got, "details-purpose-saved") || !strings.Contains(got, "Hiring") || strings.Contains(got, "Policy questions") {
		t.Errorf("a save in flight: %s", got)
	}
	// A person who may not change it gets the line and no control.
	denied := m
	denied.Callbacks.SetChannelTeamPurpose = nil
	if got := chatux021Details(t, denied, handlers{local: localUI{purposeEditing: true}}); strings.Contains(got, `id="channel-team-purpose"`) {
		t.Errorf("a box without permission: %s", got)
	}
	// Submitting closes the box and keeps the text to show; room changes drop it.
	local := localStore{box: &localUI{purposeEditing: true}}
	chatux021PurposeSubmitted(local, "Hiring")
	if st := local.get(); st.purposeEditing || st.purposeDraft != "Hiring" {
		t.Errorf("after submit: %+v", st)
	}
	local.box.purposeEditing = true
	local.forRoom("elsewhere")
	if st := local.get(); st.purposeEditing || st.purposeDraft != "" {
		t.Errorf("another room keeps the purpose box: %+v", st)
	}
}

// The header keeps visibility and member count and adds the purpose after them.
func TestTodo_CHATUX_021_HeaderKeepsCountsBeforePurpose(t *testing.T) {
	m := chatux021Model()
	header := renderNode(t, timeline(m, handlers{}))
	at := func(s string) int { return strings.Index(header, s) }
	if at("Public") < 0 || at("3 members") < at("Public") || at("Policy questions") < at("3 members") {
		t.Errorf("the header line is not visibility, count, purpose: %s", header)
	}
	m.ChannelTeam.Purpose = ""
	if got := renderNode(t, timeline(m, handlers{})); strings.Contains(got, "topic-purpose") {
		t.Errorf("an empty purpose draws a part: %s", got)
	}
}

// Change status starts with nothing chosen and Confirm waits for a choice; the
// lock's end is asked for only when Locked is chosen; each choice says what it
// changes before anyone confirms.
func TestTodo_CHATUX_021_StatusFormStartsEmpty(t *testing.T) {
	m := chatux021Model()
	view := m.ChannelStatuses["general"]
	props := ChannelStatusPanelProps{Model: m, View: view, Change: func(chat.ChangeChannelStatusRequest) {}}
	markup := renderNode(t, channelStatusForm(m, view, props, "", "", ui.Handler{}, ui.Handler{}))
	if !strings.Contains(markup, `value="__none__"`) || !strings.Contains(markup, "Choose a status") || !strings.Contains(markup, "disabled") {
		t.Errorf("the empty form: %s", markup)
	}
	for _, absent := range []string{"chatstate-until", "End the lock at", "Nobody can post"} {
		if strings.Contains(markup, absent) {
			t.Errorf("the empty form shows %q", absent)
		}
	}
	locked := renderNode(t, channelStatusForm(m, view, props, "LOCKED", "", ui.Handler{}, ui.Handler{}))
	if !strings.Contains(locked, "chatstate-until") || !strings.Contains(locked, "End the lock at") || !strings.Contains(locked, "Nobody can post, reply, react, edit or pin") {
		t.Errorf("Locked chosen: %s", locked)
	}
	if strings.Contains(locked, `type="submit" disabled`) || strings.Contains(locked, `disabled type="submit"`) {
		t.Errorf("Confirm stays disabled after a choice: %s", locked)
	}
	archived := renderNode(t, channelStatusForm(m, view, props, "ARCHIVED", "", ui.Handler{}, ui.Handler{}))
	if strings.Contains(archived, "chatstate-until") || !strings.Contains(archived, "read-only") {
		t.Errorf("Archived chosen: %s", archived)
	}
	if statusChoiceOf("__none__") != "" || statusChoiceOf(" LOCKED ") != "LOCKED" {
		t.Error("the placeholder is a status")
	}
}

func TestTodo_CHATUX_021_PersonPaneHasBackAndClose(t *testing.T) {
	m := chatux021Model()
	m.ShowPerson = true
	both := renderNode(t, personPaneHeading(m))
	if !strings.Contains(both, `data-action="person-back"`) || !strings.Contains(both, "Back to conversation details") || !strings.Contains(both, `data-action="close-person"`) || !strings.Contains(both, "Close person details") {
		t.Errorf("the person pane's head: %s", both)
	}
	if strings.Index(both, "person-back") > strings.Index(both, "person-pane-heading") {
		t.Errorf("Back does not come first: %s", both)
	}
	m.ShowDetails = false
	if only := renderNode(t, personPaneHeading(m)); strings.Contains(only, "person-back") || !strings.Contains(only, "close-person") {
		t.Errorf("with no details behind it there is nothing to go back to: %s", only)
	}
	// Back returns to the details; Close puts the side column away.
	m.ShowDetails = true
	closedPerson, toggled := 0, []bool{}
	m.Callbacks.ClosePerson = func() { closedPerson++ }
	m.Callbacks.ToggleDetails = func(open bool) { toggled = append(toggled, open) }
	m.actWith("person-back", "", "")
	if closedPerson != 1 || len(toggled) != 0 {
		t.Errorf("Back: person closed %d times, details toggled %v", closedPerson, toggled)
	}
	m.actWith("close-person", "", "")
	if closedPerson != 2 || len(toggled) != 1 || toggled[0] {
		t.Errorf("Close: person closed %d times, details toggled %v", closedPerson, toggled)
	}
}

// Adding people posts a system line naming who added whom, written from the
// names the page holds, and it never groups with the messages around it.
func TestTodo_CHATUX_021_AddedPeopleLine(t *testing.T) {
	m := chatux021Model()
	msg := Message{ID: "line", AuthorID: "walt", Author: "Walt Brennan", Body: chat.MembershipAddedBody("loretta"), TimeLabel: "9:41"}
	got := renderNode(t, message(m, handlers{}, msg, false))
	if !strings.Contains(got, "Walt Brennan added Loretta Haynes") || !strings.Contains(got, "chatux021-system-line") || strings.Contains(got, "member-added") || strings.Contains(got, "message-actions") {
		t.Errorf("the system line: %s", got)
	}
	m.Locale = "de-DE"
	if got := renderNode(t, message(m, handlers{}, msg, false)); !strings.Contains(got, "Walt Brennan hat Loretta Haynes hinzugefügt") {
		t.Errorf("German line: %s", got)
	}
	m.Locale = "ar"
	if got := renderNode(t, message(m, handlers{}, msg, false)); !strings.Contains(got, "أضاف Walt Brennan Loretta Haynes") {
		t.Errorf("Arabic line: %s", got)
	}
	// Someone the page does not know is "someone", never an identifier.
	unknown := Message{ID: "line2", AuthorID: "walt", Author: "Walt Brennan", Body: chat.MembershipAddedBody("u-9")}
	if got := renderNode(t, message(chatux021Model(), handlers{}, unknown, false)); !strings.Contains(got, "Walt Brennan added someone") || strings.Contains(got, "u-9") {
		t.Errorf("an unknown person: %s", got)
	}
	ordinary := Message{ID: "m", AuthorID: "walt", Author: "Walt Brennan", Body: "hello"}
	if chatux021IsLine(ordinary) || !chatux021IsLine(msg) {
		t.Error("the line is not told from a message")
	}
}

// The member list refreshes itself: it has no refresh button, and the panel asks
// for the list when it opens.
func TestTodo_CHATUX_021_MembersRefreshThemselves(t *testing.T) {
	m := chatux021Model()
	got := chatux021Details(t, m, handlers{})
	if strings.Contains(got, "refresh-members") || strings.Contains(got, "Refresh members") {
		t.Errorf("the member list still has a refresh button: %s", got)
	}
	if !strings.Contains(got, "open-add-members") {
		t.Errorf("adding people went with the refresh button: %s", got)
	}
}

// A direct message has nothing to manage and no Ask button for the agent the
// person is already writing to; a channel keeps both; the sample request is for
// the people who may create an app token.
func TestTodo_CHATUX_021_DirectMessageAndAppRequest(t *testing.T) {
	m := chatux021Model()
	channel := chatux021Details(t, m, handlers{})
	if !strings.Contains(channel, "persona-member-ask") || !strings.Contains(channel, "chat-details-manage") {
		t.Fatalf("the channel lost its Ask button or Manage channel: %s", channel)
	}
	dm := m
	dm.Conversations = []Conversation{{ID: "helper", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true}}
	dm.SelectedID = "helper"
	dm.ChannelStatuses = nil
	dm.ResolvedPersonaMentions[0].Reference.ConversationID = "helper"
	dm.PersonaLookupConversationID = "helper"
	got := chatux021Details(t, dm, handlers{})
	if strings.Contains(got, "persona-member-ask") || strings.Contains(got, "agent-ask-here") {
		t.Errorf("a direct message offers Ask for the agent it is with: %s", got)
	}
	if strings.Contains(got, "chat-details-manage") || strings.Contains(got, "Manage channel") || strings.Contains(got, "Copy sample request") {
		t.Errorf("a direct message offers Manage channel: %s", got)
	}
	if !strings.Contains(got, "Policy Helper") {
		t.Errorf("the agent left the member list: %s", got)
	}
	// The app request: administrator or owner or manager, never a plain member.
	room := m.Conversations[0]
	plain := m
	plain.IsTenantAdmin, plain.CurrentUser = false, "jake"
	plain.ChannelTeam.Members = nil
	if integrationsSection(plain, room) != nil {
		t.Error("a plain member is offered the app request")
	}
	if got := renderNode(t, integrationsSection(m, room)); !strings.Contains(got, "Copy sample request for apps") {
		t.Errorf("the administrator is not offered the app request: %s", got)
	}
	if strings.Contains(chatux021Details(t, plain, handlers{}), "Copy sample request") {
		t.Error("the member's panel carries the app request")
	}
}

func TestTodo_CHATUX_021_Languages(t *testing.T) {
	for locale, words := range map[string][]string{
		"de-DE": {"Zweck hinzufügen"},
		"ar":    {"إضافة غرض"},
	} {
		m := chatux021Model()
		m.Locale = locale
		m.ChannelTeam.Purpose = ""
		got := chatux021Details(t, m, handlers{})
		for _, w := range words {
			if !strings.Contains(got, w) {
				t.Errorf("%s misses %q", locale, w)
			}
		}
		if strings.Contains(got, "chat.ux021") || strings.Contains(got, "⟦") {
			t.Errorf("%s prints a copy key", locale)
		}
	}
}
