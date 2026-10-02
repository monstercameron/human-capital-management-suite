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
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatux005Page renders the whole page with the product's own catalog and
// returns the Conversation details panel of #general for a channel manager or a
// plain member, with the person's choice of open groups.
func chatux005Page(t *testing.T, locale, viewer string) (panel string, ctx productui.LocaleContext) {
	t.Helper()
	ctx = productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, OwnerID: "walt", MemberCount: 18, Joined: true}
	refs := []chatui.ChatReference{
		{Kind: "AGENT_MENTION", ID: "assistant", TenantID: "t", Display: "Assistant", ConversationID: room.ID},
		{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "t", Display: "Policy Helper", ConversationID: room.ID},
	}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), ShowDetails: true, SelectedID: room.ID, CurrentTenantID: "t",
		CurrentUser:   map[string]string{"manager": "walt", "member": "jake"}[viewer],
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{room},
		Members:       []chatui.Member{{ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}, {ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}},
		ChannelTeam: chatui.ChannelTeamWidget{Revision: 1, Purpose: "Policy questions", Members: []chatui.ChannelTeamMember{
			{HomeTenantID: "t", SubjectID: "jake", Role: "MEMBERSHIP_ROLE_MEMBER"}, {HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}},
		ChannelProject: chatui.ChannelProjectWidget{Revision: 1, Title: "Handbook", Milestones: []chatui.ChannelProjectMilestone{{ID: "ms1", Text: "Draft", Status: "PLANNED"}}},
		ChannelPins:    []chatui.ChannelPin{{PostID: "p1", Author: "Jake Sullivan", Body: "First pin", Sequence: 1}},
		ResolvedPersonaMentions: []chatui.ResolvedPersonaMention{
			{Reference: refs[0], Handle: "assistant", Purpose: "Answers everyday questions"},
			{Reference: refs[1], Handle: "policy-helper", Purpose: "Answer policy questions"},
		},
		PersonaLookup: chatui.PersonaLookupReady, PersonaLookupConversationID: room.ID,
		FilterSettings: func() ui.Node { return gwchtml.P(gwchtml.Props{Text: "settings"}) },
		Callbacks:      chatui.Callbacks{SetConversationNotification: func(string, chatui.NotificationMode) {}, CopyConversationAPICurl: func(string) {}, ToggleDetails: func(bool) {}, SendMessageWithReferences: func(string, string, []chatui.ChatReference) {}},
	}
	if viewer == "manager" {
		m.ChannelStatuses = map[string]chatui.ChannelStatusView{room.ID: {
			Status:      chat.ChannelStatus{TenantID: "t", ConversationID: room.ID, Status: chatpolicy.StatusOpen, Revision: 1},
			Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusLocked, Permission: chatpolicy.PermissionChangeChannelStatus}},
		}}
		m.ChangeChannelStatus = func(chat.ChangeChannelStatusRequest) {}
	}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(page, `chat-side chat-details"`)
	if start < 0 {
		t.Fatalf("%s/%s: no details panel", locale, viewer)
	}
	start = strings.LastIndex(page[:start], "<aside")
	end := strings.Index(page[start:], "</aside>")
	if end < 0 {
		t.Fatalf("%s/%s: unterminated details panel", locale, viewer)
	}
	return html.UnescapeString(page[start : start+end]), ctx
}

func TestTodo_CHATUX_005_Browser(t *testing.T) {
	words := map[string]struct{ about, manage, project, notifications, agents, people, copyLink string }{
		"en-US": {"About", "Manage channel", "Project and milestones", "Notifications for me", "Agents · 2", "People · 2", "Copy link"},
		"de-DE": {"Info", "Kanal verwalten", "Projekt und Meilensteine", "Benachrichtigungen für mich", "Agenten · 2", "Personen · 2", "Link kopieren"},
		"ar":    {"نبذة", "إدارة القناة", "المشروع والمراحل الرئيسية", "الإشعارات الخاصة بي", "الوكلاء · ٢", "الأشخاص · ٢", "نسخ الرابط"},
	}
	keyLeak := regexp.MustCompile(`\bchat\.[a-z0-9_]+\.[a-z0-9_.]+`)
	for locale, w := range words {
		for _, viewer := range []string{"manager", "member"} {
			panel, ctx := chatux005Page(t, locale, viewer)
			if strings.Contains(panel, "⟦") || keyLeak.MatchString(panel) || strings.Contains(panel, ` style="`) {
				t.Errorf("%s/%s: a copy key, marker or style attribute reaches the panel: %s", locale, viewer, keyLeak.FindString(panel))
			}
			order := []string{`id="chat-details-about"`, "<h3>" + w.about + "</h3>", `id="chat-details-pinned"`, ctx.Text(chatui.KeyPinned) + s31Digits(locale, " · 1"), w.notifications, ctx.Text(chatui.KeyMembers) + s31Digits(locale, " · 18"), `id="chat-details-members"`, `id="chat-agents-here"`, w.agents, w.people}
			if viewer == "manager" {
				order = append(order, `id="chat-details-manage"`, w.manage, ctx.Text(chatui.KeyTeamRoles), `data-manage-section="project"`, w.project, "chatfilter-entry", "integrations-section")
			}
			last := -1
			for _, marker := range order {
				at := strings.Index(panel, marker)
				if at < 0 || at < last {
					t.Fatalf("%s/%s: %q is missing or out of order in the panel", locale, viewer, marker)
				}
				last = at
			}
			if viewer == "member" {
				for _, gone := range []string{"chat-details-manage", w.manage, w.project, "chatfilter-entry", "integrations-section", "manage-status"} {
					if strings.Contains(panel, gone) {
						t.Errorf("%s: a plain member sees %q", locale, gone)
					}
				}
			} else if !regexp.MustCompile(`(?s)data-details-group="manage".*?aria-expanded="false".*?hidden`).MatchString(panel) {
				t.Errorf("%s: Manage channel is not collapsed at first", locale)
			}
			// Under a pinned message the button copies its link, in every language.
			if !strings.Contains(panel, ">"+w.copyLink+"<") || strings.Contains(panel, ctx.Text(chatui.KeyPinCopy)) {
				t.Errorf("%s/%s: the pinned message does not offer %q", locale, viewer, w.copyLink)
			}
			if locale == "ar" && ctx.Direction != "rtl" {
				t.Errorf("ar is not right to left: %q", ctx.Direction)
			}

			// The panel's own rules are the same at every width, and none of them
			// fixes a width a 390 px phone could not hold.
			sheet := chatui.ScopedStylesheet()
			for _, width := range []int{1440, 800, 390} {
				for _, rule := range []string{".details-group-body[hidden]{display:none}", ".persona-member-purpose{", "#chat-agents-here:focus-visible", "-webkit-line-clamp:2;line-clamp:2"} {
					if !strings.Contains(sheet, rule) {
						t.Errorf("%s/%s/%d: the stylesheet has no %s", locale, viewer, width, rule)
					}
				}
			}
		}
	}
	// An agent's description wraps to two lines before it is cut, never one.
	purpose := regexp.MustCompile(`\.persona-member-purpose\{[^}]*\}`).FindString(chatui.ChatUX005Styles)
	if purpose == "" || strings.Contains(purpose, "nowrap") {
		t.Errorf("the agent description is cut on one line: %q", purpose)
	}
	if fixed := regexp.MustCompile(`(?:^|[;{\s])(?:min-|max-)?(?:width|inline-size):\s*\d+px`).FindString(chatui.ChatUX005Styles); fixed != "" {
		t.Errorf("the panel's styles fix a width in pixels: %q", fixed)
	}
}
