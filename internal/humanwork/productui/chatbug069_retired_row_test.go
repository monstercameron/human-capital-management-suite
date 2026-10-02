package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATBUG_069_RetiredRow renders an announcement whose agent is no
// longer in its conversation beside one that is: the retired record says so in
// plain words, in each language, and offers only Delete.
func TestTodo_CHATBUG_069_RetiredRow(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{
		{"en-US", "Agent no longer in this conversation"},
		{"de-DE", "Agent nicht mehr in dieser Unterhaltung"},
		{"ar", "الوكيل لم يعد في هذه المحادثة"},
	} {
		snapshot := AgentAnnouncementsSnapshot{Available: true, CanCreate: true, Rows: []AgentAnnouncementRow{
			{ID: "live", AgentName: "Assistant", ConversationName: "#general", Instruction: "Tell employees about holidays.", State: "ACTIVE", Revision: 1},
			{ID: "gone", Retired: true, Instruction: "Tell employees about birthdays.", State: "PAUSED", Revision: 3, ResultCode: "", OwnerName: "Alex Example"},
		}}
		markup, err := ui.RenderToString(RenderAgentAnnouncements(ResolveProductLocale(tc.locale), snapshot))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, tc.want) || !strings.Contains(markup, "Assistant · #general") {
			t.Fatalf("%s: the retired record is not labelled beside a live one: %s", tc.locale, markup)
		}
		gone := markup[strings.Index(markup, `data-announcement-id="gone"`):]
		gone = gone[:strings.Index(gone, "</article>")]
		if strings.Contains(gone, `data-announcement-action="post"`) || strings.Contains(gone, `data-announcement-action="edit"`) || strings.Contains(gone, `data-announcement-action="resume"`) || !strings.Contains(gone, `data-announcement-action="confirm-delete"`) {
			t.Fatalf("%s: a retired record offers more than Delete: %s", tc.locale, gone)
		}
	}
}

// TestTodo_CHATBUG_069_TabAddress: the address Agent operations is linked and
// reloaded with names its tab, and the server renders that tab as the selected
// one with its panel visible.
func TestTodo_CHATBUG_069_TabAddress(t *testing.T) {
	view := ApplyLocale(NewView(PageAgentOperations, "tenant", "owner", ""), ResolveProductLocale("en-US"))
	view.Query = "tab=announcements"
	view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
	markup, err := ui.RenderToString(BuildAgentOperationsPage(view))
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(markup, `id="agent-operations-tab-announcements"`)
	tab := markup[strings.LastIndex(markup[:at], "<a "):]
	tab = tab[:strings.Index(tab, ">")]
	if !strings.Contains(tab, `aria-selected="true"`) || !strings.Contains(tab, `href="`) || !strings.Contains(tab, "tab=announcements") {
		t.Fatalf("the announcements tab is not selected by its address: %s", tab)
	}
	at = strings.Index(markup, `id="agent-announcements"`)
	panel := markup[strings.LastIndex(markup[:at], "<div "):]
	panel = panel[:strings.Index(panel, ">")]
	if strings.Contains(panel, " hidden") {
		t.Fatalf("the announcements panel is hidden at its own address: %s", panel)
	}
}
