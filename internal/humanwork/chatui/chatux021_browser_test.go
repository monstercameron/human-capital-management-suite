package chatui_test

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_021_Browser: the details panel, the person pane and the
// system line with the product's own catalog in each language, and no copy key.
func TestTodo_CHATUX_021_Browser(t *testing.T) {
	type words struct{ add, choose, edit, back, added, hint string }
	for locale, w := range map[string]words{
		"en-US": {"Add a purpose", "Choose a status", "Purpose", "Back to conversation details", "Walt Brennan added Loretta Haynes", "Enter to save · Esc to cancel"},
		"de-DE": {"Zweck hinzufügen", "Status wählen", "Zweck", "Zurück zu den Details zur Unterhaltung", "Walt Brennan hat Loretta Haynes hinzugefügt", "Enter zum Speichern · Esc zum Abbrechen"},
		"ar":    {"إضافة غرض", "اختر حالة", "الغرض", "العودة إلى تفاصيل المحادثة", "أضاف Walt Brennan Loretta Haynes", "Enter للحفظ · Esc للإلغاء"},
	} {
		var model chatui.Model
		page := lane3Page(t, locale, func(m *chatui.Model) {
			m.ShowDetails = true
			m.IsTenantAdmin = true
			m.Members = append(m.Members, chatui.Member{ID: "loretta", HomeTenantID: "t", Name: "Loretta Haynes"})
			m.ChannelTeam = chatui.ChannelTeamWidget{Revision: 1, Members: []chatui.ChannelTeamMember{{HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}}
			m.ChannelProject = chatui.ChannelProjectWidget{Revision: 1}
			m.Callbacks.SetChannelTeamPurpose = func(string) {}
			m.ChannelStatuses = map[string]chatui.ChannelStatusView{"general": {
				Status:      chat.ChannelStatus{TenantID: "t", ConversationID: "general", Status: chatpolicy.StatusOpen, Revision: 1},
				Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusLocked, Permission: chatpolicy.PermissionChangeChannelStatus}},
			}}
			m.ChangeChannelStatus = func(chat.ChangeChannelStatusRequest) {}
			m.Messages = []chatui.Message{{ID: "line", AuthorID: "walt", Author: "Walt Brennan", Body: chat.MembershipAddedBody("loretta"), TimeLabel: "9:41"}}
			defer func() { model = *m }()
		})
		lane3NoLeaks(t, locale+" details", page)
		for _, want := range []string{w.add, w.added, `data-action="purpose-edit"`, `data-manage-section="status"`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: the page misses %q", locale, want)
			}
		}
		// The form itself is drawn only inside the open Status row, which a page
		// rendered once cannot press: it is read from the status component, in the
		// language of the page, with nothing chosen.
		form, err := ui.RenderToString(chatui.ChannelStatusPanel(chatui.ChannelStatusPanelProps{Model: model, View: model.ChannelStatuses["general"], Change: model.ChangeChannelStatus}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{w.choose, `value="__none__"`} {
			if !strings.Contains(html.UnescapeString(form), want) {
				t.Errorf("%s: the status form misses %q", locale, want)
			}
		}
		person := lane3Page(t, locale, func(m *chatui.Model) {
			m.ShowDetails, m.ShowPerson = true, true
			m.PersonDetails = &chatui.PersonDetails{ID: "loretta", Name: "Loretta Haynes", Ready: true}
			m.Callbacks.ClosePerson, m.Callbacks.StartDirectMessage = func() {}, func(string) {}
		})
		lane3NoLeaks(t, locale+" person pane", person)
		if !strings.Contains(person, `data-action="person-back"`) || !strings.Contains(person, w.back) || !strings.Contains(person, `data-action="close-person"`) {
			t.Errorf("%s: the person pane has no Back and Close", locale)
		}
	}
}
