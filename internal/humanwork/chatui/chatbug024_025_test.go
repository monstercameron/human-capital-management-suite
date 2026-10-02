package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// chatbug024Catalog behaves like the product catalog for a key it does not
// carry: it answers with a marker, never with the key itself.
func chatbug024Catalog(key string) string {
	if text, ok := englishCopy[key]; ok {
		return text
	}
	return "⟦" + key + "⟧"
}

func TestTodo_CHATBUG_024(t *testing.T) {
	// A catalog with no chat.filters.* keys no longer leaks its markers: the
	// reviewed copy answers in all three languages.
	for locale, want := range map[string][3]string{
		"en-US": {"Filters", "Manage filters", "Direct messages follow only the workspace's hard rules."},
		"de-DE": {"Filter", "Filter verwalten", "Für Direktnachrichten gelten nur verbindliche Arbeitsbereichsregeln."},
		"ar":    {"المرشحات", "أدر المرشحات", "تخضع الرسائل المباشرة للقواعد الإلزامية لمساحة العمل فقط."},
	} {
		m := Model{Locale: locale, Text: chatbug024Catalog}
		for i, key := range []string{"title", "manage", "direct"} {
			if got := chatfilterText(m, key); got != want[i] {
				t.Errorf("%s %s = %q, want %q", locale, key, got, want[i])
			}
		}
	}

	// The section shows only to a person who may manage filters.
	room := Conversation{ID: "room", Name: "room", Kind: PublicChannel, OwnerID: "owner"}
	base := Model{Locale: "en-US", SelectedID: "room", CurrentTenantID: "t", Text: chatbug024Catalog, Conversations: []Conversation{room}}
	member := base
	member.CurrentUser = "member"
	if filterSettingsEntry(member, room) != nil {
		t.Fatal("a plain member sees the filter section")
	}
	manager := member
	manager.ChannelTeam = ChannelTeamWidget{Members: []ChannelTeamMember{{HomeTenantID: "t", SubjectID: "member", Role: "MEMBERSHIP_ROLE_MANAGER"}}}
	if filterSettingsEntry(manager, room) == nil {
		t.Fatal("a channel manager does not see the filter section")
	}
	other := manager
	other.SelectedID = "other"
	if filterSettingsEntry(other, Conversation{ID: "elsewhere"}) != nil {
		t.Fatal("a manager of another conversation sees the filter section")
	}
	owner := base
	owner.CurrentUser = "owner"
	admin := member
	admin.IsTenantAdmin = true
	for name, viewer := range map[string]Model{"owner": owner, "admin": admin} {
		if filterSettingsEntry(viewer, room) == nil {
			t.Fatalf("%s does not see the filter section", name)
		}
	}
	off := admin
	off.ChatFeatures = &ChatFeatures{}
	if filterSettingsEntry(off, room) != nil {
		t.Fatal("the section shows while the filters feature is off")
	}

	// With the client's settings view the control is a disclosure inside the
	// panel, closed at first; it never links to a page of its own.
	admin.FilterSettings = func() ui.Node { return html.P(html.Props{Text: "filter settings body"}) }
	markup := renderNode(t, filterSettingsEntry(admin, room))
	for _, want := range []string{"Manage filters", `aria-expanded="false"`, "manage-row-summary"} {
		if !strings.Contains(markup, want) {
			t.Errorf("filter section missing %q: %s", want, markup)
		}
	}
	// CHATBUG-048: the note about direct messages is said only where it applies.
	if strings.Contains(markup, "Direct messages follow only") {
		t.Errorf("a channel's filter section talks about direct messages: %s", markup)
	}
	group := admin
	group.Conversations = []Conversation{{ID: "huddle", Name: "huddle", Kind: GroupChat, OwnerID: "owner"}}
	group.SelectedID = "huddle"
	if got := renderNode(t, filterSettingsEntry(group, group.Conversations[0])); !strings.Contains(got, "Direct messages follow only") {
		t.Errorf("a group conversation's filter section lacks the direct-message note: %s", got)
	}
	if strings.Contains(markup, "⟦") || strings.Contains(markup, "/workspace/app/chat/filters") || strings.Contains(markup, "filter settings body") {
		t.Errorf("filter section shows a marker, a dead link or an unopened body: %s", markup)
	}
	// Without a settings view there is nothing to open: the row stands alone.
	admin.FilterSettings = nil
	markup = renderNode(t, filterSettingsEntry(admin, room))
	if !strings.Contains(markup, "Filters") || strings.Contains(markup, "<button") || strings.Contains(markup, "<a ") {
		t.Errorf("a model with no settings view offers a control: %s", markup)
	}
}

func chatbug025Model(locale string, icon agenticon.Value) Model {
	room := Conversation{ID: "general", Name: "general", Kind: PublicChannel, OwnerID: "owner", MemberCount: 18, Joined: true}
	refs := []ChatReference{
		{Kind: "AGENT_MENTION", ID: "assistant", TenantID: "t", Display: "Assistant", ConversationID: room.ID},
		{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "t", Display: "Policy Helper", ConversationID: room.ID},
	}
	m := Model{State: StateReady, Locale: locale, ShowDetails: true, SelectedID: room.ID, CurrentUser: "walt", CurrentTenantID: "t", IsTenantAdmin: true, Text: chatbug024Catalog,
		Conversations: []Conversation{room},
		Members:       []Member{{ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}, {ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}},
		ChannelTeam:   ChannelTeamWidget{Members: []ChannelTeamMember{{HomeTenantID: "t", SubjectID: "jake", Role: "MEMBERSHIP_ROLE_MEMBER"}, {HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{
			{Reference: refs[0], Handle: "assistant", Purpose: "Answers everyday questions", Icon: icon},
			{Reference: refs[1], Handle: "policy-helper", Purpose: "Answer policy questions", Icon: icon},
		},
		PersonaLookup: PersonaLookupReady, PersonaLookupConversationID: room.ID,
		ChannelStatuses: map[string]ChannelStatusView{room.ID: {
			Status:      chat.ChannelStatus{TenantID: "t", ConversationID: room.ID, Status: chatpolicy.StatusOpen, Revision: 1},
			Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusLocked, Permission: chatpolicy.PermissionChangeChannelStatus}},
		}},
		ChangeChannelStatus: func(chat.ChangeChannelStatusRequest) {},
		Callbacks:           Callbacks{SetConversationNotification: func(string, NotificationMode) {}, CopyConversationAPICurl: func(string) {}, ToggleDetails: func(bool) {}},
	}
	return m
}

func TestTodo_CHATBUG_025(t *testing.T) {
	stored, fallback, icon := chatbug013Icons(t)
	m := chatbug025Model("en-US", icon)
	status := func(m Model) string { return renderNode(t, integrate2StatusDetails(m)) }

	// A channel whose status never changed shows its state in one line and no
	// history, in all three languages.
	for locale, none := range map[string]string{"en-US": "Not set", "de-DE": "Nicht festgelegt", "ar": "غير محدد"} {
		m.Locale = locale
		markup := status(m)
		if strings.Contains(markup, none) || strings.Contains(markup, "<dl") || strings.Contains(markup, "<dt") {
			t.Errorf("%s: an unchanged status lists empty history: %s", locale, markup)
		}
	}
	m.Locale = "en-US"
	markup := status(m)
	if !strings.Contains(markup, ">Open<") || !strings.Contains(markup, "Change status") || !strings.Contains(markup, "manage-row-summary") {
		t.Errorf("the state or the permitted change control is missing: %s", markup)
	}
	// The change control stays behind its permission.
	denied := m
	denied.ChangeChannelStatus = nil
	if got := status(denied); strings.Contains(got, "Change status") || strings.Contains(got, "chatstate-reason") {
		t.Errorf("the change control shows without permission: %s", got)
	}
	// A status that did change lists only the facts it has.
	at := time.Date(2026, 9, 30, 9, 15, 0, 0, time.UTC)
	view := m.ChannelStatuses["general"]
	view.Status.Status, view.Status.ChangedBy, view.Status.ChangedAt, view.Status.Reason = chatpolicy.StatusLocked, "walt", &at, "Incident review"
	changed := m
	changed.ChannelStatuses = map[string]ChannelStatusView{"general": view}
	// The history is About's, one line for the state and the facts under it.
	markup = renderNode(t, chatux005About(changed, handlers{}, m.Conversations[0], nil))
	for _, want := range []string{"Changed by", "Walt Brennan", "Changed at", "2026-09-30 09:15", "Reason", "Incident review"} {
		if !strings.Contains(markup, want) {
			t.Errorf("a changed status misses %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Ends at") || strings.Contains(markup, "Not set") {
		t.Errorf("a lock with no end lists an empty row: %s", markup)
	}

	// No label ends in a separator, whatever the number formatter answers.
	if got := countedLabel(m, "Role labels", 18); got != "Role labels · 18" {
		t.Errorf("counted label = %q", got)
	}
	blind := m
	blind.Number = func(int) string { return "" }
	if got := countedLabel(blind, "Role labels", 18); got != "Role labels" {
		t.Errorf("a count that cannot be written leaves %q", got)
	}
	if got := countedLabel(m, "Role labels", 0); got != "Role labels" {
		t.Errorf("an empty count leaves %q", got)
	}
	panel := chatbug024Panel(t, renderNode(t, Build(blind)))
	if found := regexp.MustCompile(`.{40}·\s*<.{20}`).FindString(panel); found != "" {
		t.Errorf("a label in the panel ends in a separator: %s", found)
	}

	// The agents at the end of the member list carry their stored icon and the
	// Agent badge, not a bare "Name · Agent" row.
	rows := renderNode(t, personaMemberRow(m, m.ResolvedPersonaMentions[0]))
	if !strings.Contains(rows, stored) || strings.Contains(rows, fallback) || !strings.Contains(rows, "agent-badge") || !strings.Contains(rows, "Assistant") || strings.Contains(rows, "Assistant · Agent") {
		t.Errorf("agent member row: %s", rows)
	}

	// Notifications use the panel's own disclosure, not a native select, and
	// name the current choice.
	notify := renderNode(t, notificationsControl(m, "general", NotifyMention))
	if strings.Contains(notify, "<select") || strings.Contains(notify, "<option") || !strings.Contains(notify, "Mentions only") {
		t.Errorf("notifications control: %s", notify)
	}
	if strings.Count(notify, `data-action="rail-notify-`) != 3 || strings.Count(notify, " checked ") != 1 || !strings.Contains(notify, `role="radiogroup"`) || !strings.Contains(notify, `data-chat-disclosure-toggle="true"`) {
		t.Errorf("notification choices: %s", notify)
	}

	// Developer tools: only for a person who may create an app token, and named
	// in plain words.
	room := m.Conversations[0]
	plain := m
	plain.IsTenantAdmin, plain.CurrentUser, plain.ChannelTeam = false, "jake", ChannelTeamWidget{Members: []ChannelTeamMember{{HomeTenantID: "t", SubjectID: "jake", Role: "MEMBERSHIP_ROLE_MEMBER"}}}
	if integrationsSection(plain, room) != nil {
		t.Error("a plain member is offered the app request")
	}
	for name, viewer := range map[string]Model{"admin": m, "manager": func() Model { v := plain; v.CurrentUser = "walt"; v.ChannelTeam = m.ChannelTeam; return v }()} {
		got := renderNode(t, integrationsSection(viewer, room))
		if !strings.Contains(got, "Copy sample request for apps") || strings.Contains(got, "Copy API curl") {
			t.Errorf("%s: integrations section: %s", name, got)
		}
	}
}

// chatbug024Panel is the Conversation details panel out of a whole page.
func chatbug024Panel(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, `chat-side chat-details"`)
	if start < 0 {
		t.Fatal("no Conversation details panel in the page")
	}
	start = strings.LastIndex(page[:start], "<aside")
	end := strings.Index(page[start:], "</aside>")
	if start < 0 || end < 0 {
		t.Fatal("unterminated Conversation details panel")
	}
	return page[start : start+end]
}
