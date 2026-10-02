package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	gwchtml "github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatbug024Details renders the whole page with the product's own catalog, the
// one that answers an unknown key with a marker, and returns the Conversation
// details panel of an administrator of #general who has the filter settings.
func chatbug024Details(t *testing.T, locale, viewer string) (panel string, icon string) {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	value := agenticon.Generate(agenticon.Input{Name: "Policy Helper", Description: "Answer policy questions"})
	icon, err := ui.RenderToString(agenticon.Node(value))
	if err != nil || icon == "" {
		t.Fatalf("icon fixture: %q %v", icon, err)
	}
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, OwnerID: "owner", MemberCount: 18, Joined: true}
	refs := []chatui.ChatReference{
		{Kind: "AGENT_MENTION", ID: "assistant", TenantID: "t", Display: "Assistant", ConversationID: room.ID},
		{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "t", Display: "Policy Helper", ConversationID: room.ID},
	}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), ShowDetails: true, SelectedID: room.ID, CurrentUser: map[string]string{"admin": "walt", "manager": "walt", "member": "jake"}[viewer], CurrentTenantID: "t", IsTenantAdmin: viewer == "admin",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{room},
		Members:       []chatui.Member{{ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}, {ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}},
		ChannelTeam:   chatui.ChannelTeamWidget{Members: []chatui.ChannelTeamMember{{HomeTenantID: "t", SubjectID: "jake", Role: "MEMBERSHIP_ROLE_MEMBER"}, {HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}},
		ResolvedPersonaMentions: []chatui.ResolvedPersonaMention{
			{Reference: refs[0], Handle: "assistant", Purpose: "Answers everyday questions", Icon: value},
			{Reference: refs[1], Handle: "policy-helper", Purpose: "Answer policy questions", Icon: value},
		},
		PersonaLookup: chatui.PersonaLookupReady, PersonaLookupConversationID: room.ID,
		ChannelStatuses: map[string]chatui.ChannelStatusView{room.ID: {
			Status:      chat.ChannelStatus{TenantID: "t", ConversationID: room.ID, Status: chatpolicy.StatusOpen, Revision: 1},
			Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusLocked, Permission: chatpolicy.PermissionChangeChannelStatus}},
		}},
		ChangeChannelStatus: func(chat.ChangeChannelStatusRequest) {},
		FilterSettings:      func() ui.Node { return gwchtml.P(gwchtml.Props{Text: "settings"}) },
		Callbacks:           chatui.Callbacks{SetConversationNotification: func(string, chatui.NotificationMode) {}, CopyConversationAPICurl: func(string) {}, ToggleDetails: func(bool) {}},
	}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(page, `chat-side chat-details"`)
	if start < 0 {
		t.Fatalf("%s: no details panel", locale)
	}
	start = strings.LastIndex(page[:start], "<aside")
	end := strings.Index(page[start:], "</aside>")
	if end < 0 {
		t.Fatalf("%s: unterminated details panel", locale)
	}
	return page[start : start+end], icon
}

func TestTodo_CHATBUG_024_Browser(t *testing.T) {
	want := map[string][3]string{
		"en-US": {"Filters", "Manage filters", "Direct messages follow only the workspace's hard rules."},
		"de-DE": {"Filter", "Filter verwalten", "Für Direktnachrichten gelten nur verbindliche Arbeitsbereichsregeln."},
		"ar":    {"المرشحات", "أدر المرشحات", "تخضع الرسائل المباشرة للقواعد الإلزامية لمساحة العمل فقط."},
	}
	for locale, words := range want {
		panel, _ := chatbug024Details(t, locale, "admin")
		panel = html.UnescapeString(panel)
		if strings.Contains(panel, "⟦") {
			t.Errorf("%s: the details panel prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(panel))
		}
		for _, text := range words {
			if !strings.Contains(panel, text) {
				t.Errorf("%s: the filter section misses %q", locale, text)
			}
		}
		if strings.Contains(panel, "/workspace/app/chat/filters") {
			t.Errorf("%s: the panel links to a page that does not exist", locale)
		}
		// A channel manager sees it too; a plain member sees no filter section.
		manager, _ := chatbug024Details(t, locale, "manager")
		manager = html.UnescapeString(manager)
		member, _ := chatbug024Details(t, locale, "member")
		member = html.UnescapeString(member)
		for _, text := range []string{words[1], words[2]} {
			if !strings.Contains(manager, text) {
				t.Errorf("%s: a channel manager misses the filter section", locale)
			}
			if strings.Contains(member, text) {
				t.Errorf("%s: a plain member sees the filter section (%q)", locale, text)
			}
		}
		if strings.Contains(member, "⟦") {
			t.Errorf("%s: the member's panel prints a copy key", locale)
		}
	}
}

func TestTodo_CHATBUG_025_Browser(t *testing.T) {
	for locale, words := range map[string]struct{ integrations, none, open string }{
		"en-US": {"Copy sample request for apps", "Not set", "Open"},
		"de-DE": {"Beispielanfrage für Apps kopieren", "Nicht festgelegt", "Offen"},
		"ar":    {"نسخ مثال طلب للتطبيقات", "غير محدد", "مفتوحة"},
	} {
		ctx := productui.ResolveProductLocale(locale)
		panel, icon := chatbug024Details(t, locale, "admin")
		if strings.Contains(panel, "⟦") {
			t.Errorf("%s: the details panel prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(panel))
		}

		// Status: the state in one line, no empty history, change control kept.
		status := regexp.MustCompile(`(?s)<section[^>]*chatstate-section.*?</section>`).FindString(panel)
		if status == "" || !strings.Contains(status, ">"+words.open+"<") || strings.Contains(status, words.none) || strings.Contains(status, "<dl") {
			t.Errorf("%s: status block: %s", locale, status)
		}
		if !strings.Contains(status, "chat-disclosure-button") {
			t.Errorf("%s: the permitted status change control is gone: %s", locale, status)
		}

		// No label ends in a separator; the count is there when it exists.
		if found := regexp.MustCompile(`.{40}·\s*<.{20}`).FindString(panel); found != "" {
			t.Errorf("%s: a label ends in a separator: %s", locale, found)
		}
		if label := ctx.Text(chatui.KeyTeamRoles) + " · 2"; !strings.Contains(panel, label) {
			t.Errorf("%s: missing %q", locale, label)
		}

		// Agents: icon and badge on every agent row. CHATUX-005 lists each agent
		// once, in the Members section, instead of again under "Agents here".
		if got := strings.Count(panel, icon); got < 2 {
			t.Errorf("%s: the agent's stored icon is drawn %d times, want 2", locale, got)
		}
		personas := regexp.MustCompile(`(?s)<li class="member-row persona-member-row">.*?</li>`).FindAllString(panel, -1)
		if len(personas) != 2 {
			t.Fatalf("%s: %d agent rows in the member list", locale, len(personas))
		}
		for _, row := range personas {
			if !strings.Contains(row, "agent-badge") || !strings.Contains(row, icon) {
				t.Errorf("%s: an agent row has no icon or badge: %s", locale, row)
			}
		}

		// One control style: no native select; the choice names the current mode.
		if strings.Contains(panel, "notify-mode") {
			t.Errorf("%s: notifications still use a native select", locale)
		}
		if !strings.Contains(panel, ctx.Text(chatui.KeyNotifications)) || !strings.Contains(panel, ctx.Text(chatui.KeyNotifyAll)) {
			t.Errorf("%s: notifications control misses its words", locale)
		}

		// Developer tools: plain words, only for a person who may use them.
		if !strings.Contains(panel, words.integrations) || strings.Contains(panel, "API curl") {
			t.Errorf("%s: integrations button is not named in plain words", locale)
		}
		manager, _ := chatbug024Details(t, locale, "manager")
		manager = html.UnescapeString(manager)
		member, _ := chatbug024Details(t, locale, "member")
		member = html.UnescapeString(member)
		if !strings.Contains(manager, words.integrations) {
			t.Errorf("%s: a channel manager does not see the integrations section", locale)
		}
		if strings.Contains(member, words.integrations) || strings.Contains(member, "integrations-section") {
			t.Errorf("%s: a plain member is offered the app request", locale)
		}
	}
}
