package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATBUG_005_Browser: the Saved row controls one panel that lives
// beside the conversation rather than inside the sidebar, offers To do, Done
// and All, uses no details or summary element, and shows each saved message as
// author, channel, time and its first lines without any identifier.
func TestTodo_CHATBUG_005_Browser(t *testing.T) {
	m := Model{Locale: "en-US", CurrentUser: "person", CurrentTenantID: "tenant", SelectedID: "room", State: StateReady, SavedOpenCount: 2, Conversations: []Conversation{{ID: "room", Name: "Support", Kind: PublicChannel}}, Messages: []Message{{ID: "post", AuthorID: "person", Author: "Alex", Body: "work message"}}}
	sidebar := renderNode(t, chatsaveSidebar(m))
	for _, want := range []string{`data-saved-action="toggle"`, `aria-controls="chatsave-list"`, `aria-expanded="false"`, `data-saved-count="true"`} {
		if !strings.Contains(sidebar, want) {
			t.Fatalf("Saved row lacks %s: %s", want, sidebar)
		}
	}
	if strings.Contains(sidebar, `id="chatsave-list"`) || strings.Contains(sidebar, `role="dialog"`) {
		t.Fatal("the panel is rendered inside the sidebar, where its clipped container hides it")
	}
	page := render(t, m)
	if strings.Count(page, `id="chatsave-list"`) != 1 {
		t.Fatalf("want exactly one Saved panel, got %d", strings.Count(page, `id="chatsave-list"`))
	}
	panelAt, layoutAt := strings.Index(page, `id="chatsave-list"`), strings.Index(page, `class="chat-layout"`)
	if panelAt < 0 || layoutAt < 0 || panelAt > layoutAt {
		t.Fatalf("the Saved panel must be a sibling placed before the conversation layout: panel=%d layout=%d", panelAt, layoutAt)
	}
	for _, forbidden := range []string{"<details", "<summary"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("the page uses %s, which draws the browser's triangle", forbidden)
		}
	}
	copy := SavedMessagesCopy("en-US")
	body := "line one\nline two\nline three\nline four must be cut"
	list, err := ui.RenderToString(RenderSavedMessages(SavedMessagesView{Locale: "en-US", Tab: "done", Rows: []SavedMessageRow{{TenantID: "7f3c2d1e-9a4b-4c6d-8e2f-1a2b3c4d5e6f", ConversationID: "01a2b3c4-d5e6-4f70-8192-a3b4c5d6e7f8", PostID: "9f8e7d6c-5b4a-4392-8170-6f5e4d3c2b1a", Author: "Danny Nguyen", Channel: "random", TimeLabel: "Sep 28, 11:04 PM", Body: body, Availability: "readable", Sequence: 9}}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tab := range []struct{ key, label string }{{"todo", copy.Todo}, {"done", copy.Done}, {"all", copy.All}} {
		want := `data-saved-tab="` + tab.key + `"`
		if !strings.Contains(list, want) || !strings.Contains(list, ">"+tab.label+"<") {
			t.Fatalf("tab %s missing: %s", tab.key, list)
		}
	}
	if strings.Count(list, `role="tab"`) != 3 || strings.Count(list, `aria-selected="true"`) != 1 {
		t.Fatal("the three tabs must be exposed as tabs with one selected")
	}
	visible := strings.Join(strings.Fields(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(list, " ")), " ")
	for _, want := range []string{"Danny Nguyen", "random", "Sep 28, 11:04 PM", "line one", "line three"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("row does not show %q: %s", want, visible)
		}
	}
	// CHATSAVE-002: the text is no longer cut to its first lines in the markup;
	// it is drawn whole inside a box the stylesheet clamps to four lines, with a
	// Show more control, as the conversation's own long messages are.
	if !strings.Contains(visible, "line four") || !strings.Contains(list, `class="chatsave-text"`) || !strings.Contains(list, `class="chatsave-more"`) || !strings.Contains(ChatsaveStyles+Chatsave002Styles, ".chatsave-text{") {
		t.Fatal("a row's text is not drawn whole inside the four-line clamp")
	}
	for _, identifier := range []string{"7f3c2d1e", "01a2b3c4", "9f8e7d6c", "post-"} {
		if strings.Contains(visible, identifier) {
			t.Fatalf("a row shows the identifier %q to the person: %s", identifier, visible)
		}
	}
}
