package chatui_test

import (
	"strings"
	"testing"

	gwchtml "github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_048_Browser: Manage channel with the product's own catalog in
// each language: three captions, the rows in one style, no copy key, and no
// sentence about direct messages in a channel.
func TestTodo_CHATBUG_048_Browser(t *testing.T) {
	type words struct{ channel, rules, apps, status, filters, workspace, dmNote string }
	for locale, w := range map[string]words{
		"en-US": {"Channel", "Rules and language", "Apps", "Status", "Manage filters", "Workspace filters", "Direct messages follow only"},
		"de-DE": {"Kanal", "Regeln und Sprache", "Apps", "Status", "Filter verwalten", "Arbeitsbereichsfilter", "Für Direktnachrichten gelten nur"},
		"ar":    {"القناة", "القواعد واللغة", "التطبيقات", "الحالة", "أدر المرشحات", "مرشحات مساحة العمل", "تخضع الرسائل المباشرة"},
	} {
		page := lane3Page(t, locale, func(m *chatui.Model) {
			m.ShowDetails = true
			m.IsTenantAdmin = true
			m.ChannelTeam = chatui.ChannelTeamWidget{Revision: 1, Members: []chatui.ChannelTeamMember{{HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}}
			m.ChannelProject = chatui.ChannelProjectWidget{Revision: 1}
			m.ChatFeatures = &chatui.ChatFeatures{Translation: true, Filters: true}
			m.FilterSettings = func() ui.Node { return gwchtml.P(gwchtml.Props{Text: "settings"}) }
			m.WorkspaceFilterSettings = func() ui.Node { return gwchtml.P(gwchtml.Props{Text: "workspace settings"}) }
			m.ChannelStatuses = map[string]chatui.ChannelStatusView{"general": {
				Status:      chat.ChannelStatus{TenantID: "t", ConversationID: "general", Status: chatpolicy.StatusOpen, Revision: 1},
				Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusLocked, Permission: chatpolicy.PermissionChangeChannelStatus}},
			}}
			m.ChangeChannelStatus = func(chat.ChangeChannelStatusRequest) {}
			m.Callbacks.CopyConversationAPICurl = func(string) {}
		})
		lane3NoLeaks(t, locale+" Manage channel", page)
		panel := page[strings.Index(page, `chat-side chat-details"`):]
		panel = panel[:strings.Index(panel, "</aside>")]
		for _, want := range []string{">" + w.channel + "<", ">" + w.rules + "<", ">" + w.apps + "<", ">" + w.status + "<", ">" + w.filters + "<", ">" + w.workspace + "<", "manage-caption"} {
			if !strings.Contains(panel, want) {
				t.Errorf("%s: Manage channel misses %q", locale, want)
			}
		}
		if strings.Contains(panel, w.dmNote) {
			t.Errorf("%s: a channel's panel talks about direct messages", locale)
		}
		if strings.Count(panel, `class="manage-caption"`) != 3 {
			t.Errorf("%s: %d captions", locale, strings.Count(panel, `class="manage-caption"`))
		}
	}
}
