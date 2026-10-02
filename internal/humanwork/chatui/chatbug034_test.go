package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const chatbug034Doc = "doc-47892b80-d600-4401-8244-e2fa2a31caa7"

func chatbug034Visible(markup string) string {
	return strings.Join(strings.Fields(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(markup, " ")), " ")
}

func chatbug034Row() SavedMessageRow {
	return SavedMessageRow{TenantID: "tenant", ConversationID: "room", PostID: "post", Author: "Walt Brennan", Channel: "announcements", TimeLabel: "Sep 29, 12:29 AM", Availability: "readable",
		Body: "Open enrollment runs November 2 to 20. Answers to the most common questions, plus a one-page checklist: doc:" + chatbug034Doc}
}

// TestTodo_CHATBUG_034 pins what the Saved panel says: a refused change reports
// that change and never claims the list failed to load, and a document
// reference in a saved message is the document's title as a link, never the
// identifier.
func TestTodo_CHATBUG_034(t *testing.T) {
	for _, tc := range []struct{ locale, action string }{{"en-US", "save"}, {"en-US", "remove"}, {"en-US", "done"}, {"en-US", "note"}, {"en-US", "due"}, {"de-DE", "save"}, {"ar", "remove"}} {
		text := SavedActionFailedText(tc.locale, tc.action)
		if text == "" || text == SavedActionFailedText(tc.locale, "a different action") && tc.action != "save" {
			t.Fatalf("%s %s: the failed action is not named: %q", tc.locale, tc.action, text)
		}
		view := SavedMessagesView{Locale: tc.locale, Tab: "all", Rows: []SavedMessageRow{chatbug034Row()}, ActionError: "unavailable", Action: tc.action}
		markup := renderNode(t, RenderSavedMessages(view))
		copy := SavedMessagesCopy(tc.locale)
		if !strings.Contains(markup, text) || strings.Contains(markup, copy.Failed) || strings.Contains(markup, `data-saved-action="retry"`) {
			t.Fatalf("%s %s: a refused change was reported as the list failing, or not reported: %s", tc.locale, tc.action, markup)
		}
		if !strings.Contains(markup, "Walt Brennan") && tc.locale == "en-US" {
			t.Fatal("the list that had loaded was taken off the screen")
		}
	}
	// A list that could not be read still says so and offers the retry.
	failed := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Error: "unavailable"}))
	if !strings.Contains(failed, SavedMessagesCopy("en-US").Failed) || !strings.Contains(failed, `data-saved-action="retry"`) {
		t.Fatalf("the list failure lost its message or retry: %s", failed)
	}

	// The document reference is its title, as a link, once the title is known.
	ready := SavedMessagesView{Locale: "en-US", Tab: "all", Rows: []SavedMessageRow{chatbug034Row()}, DocPreviews: map[string]DocPreview{chatbug034Doc: {ID: chatbug034Doc, Title: "Open enrollment guide", Readable: true, State: "ready"}}}
	markup := renderNode(t, RenderSavedMessages(ready))
	if !strings.Contains(markup, `class="chat-doc-reference"`) || !strings.Contains(markup, `href="/workspace/app/docs?document=`+chatbug034Doc+`"`) || !strings.Contains(chatbug034Visible(markup), "Open enrollment guide") {
		t.Fatalf("the document reference is not its title as a link: %s", markup)
	}
	if visible := chatbug034Visible(markup); strings.Contains(visible, "doc:") || strings.Contains(visible, "47892b80") {
		t.Fatalf("an identifier is printed: %s", visible)
	}
	// Before the title is known the link is the plain noun, not the identifier.
	pending := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Tab: "all", Rows: []SavedMessageRow{chatbug034Row()}}))
	if visible := chatbug034Visible(pending); strings.Contains(visible, "doc:") || strings.Contains(visible, "47892b80") || !strings.Contains(pending, `data-action="open-doc-reference"`) {
		t.Fatalf("a document reference without a title printed its identifier or is not a link: %s", visible)
	}
	// A reference the reader may not open reads as restricted, not as a title.
	locked := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Tab: "all", Rows: []SavedMessageRow{chatbug034Row()}, DocPreviews: map[string]DocPreview{chatbug034Doc: {ID: chatbug034Doc, Title: "Salary bands", State: "ready"}}}))
	if strings.Contains(locked, "Salary bands") {
		t.Fatal("a title the reader may not see was shown")
	}
	// The item is not a button (CHATSAVE-002): a link cannot live inside a button,
	// so no button of the panel holds one.
	for _, button := range regexp.MustCompile(`(?s)<button[^>]*>.*?</button>`).FindAllString(markup, -1) {
		if strings.Contains(button, "<a ") {
			t.Fatal("a document link is inside a button")
		}
	}
}

// TestTodo_CHATBUG_034_Browser draws the page: the Saved panel is the thread and
// details panels' own heading with the close icon, a labelled search field with
// its icon, To do, Done and All, and no full-width search button; and it is
// docked beside the conversation under the application header.
func TestTodo_CHATBUG_034_Browser(t *testing.T) {
	m := Model{Locale: "en-US", CurrentUser: "person", CurrentTenantID: "tenant", SelectedID: "room", State: StateReady, Conversations: []Conversation{{ID: "room", Name: "random", Kind: PublicChannel}}}
	page := render(t, m)
	panelAt, layoutAt := strings.Index(page, `id="chatsave-list"`), strings.Index(page, `class="chat-layout"`)
	if panelAt < 0 || layoutAt < 0 || panelAt > layoutAt {
		t.Fatalf("the panel must be a sibling placed before the conversation layout: %d %d", panelAt, layoutAt)
	}
	copy := SavedMessagesCopy("en-US")
	// The search field is drawn once the list is long enough to need it (CHATSAVE-002).
	list := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Tab: "todo", AllCount: 9}))
	for _, want := range []string{
		`class="side-heading chat-panel-head chatsave-header"`,                                                               // the same heading row as the thread and details panels
		`class="icon-button chat-panel-close"`, `aria-label="` + copy.Close + `"`, `data-saved-action="close"`, `icon-close`, // the close icon
		`for="chatsave-search"`, `class="sr-only"`, `icon-search`, `type="search"`, `placeholder="` + copy.Search + `"`, // a labelled field with its icon
	} {
		if !strings.Contains(list, want) {
			t.Fatalf("the Saved panel lacks %s: %s", want, list)
		}
	}
	for _, tab := range []string{"todo", "done", "all"} {
		if !strings.Contains(list, `data-saved-tab="`+tab+`"`) {
			t.Fatalf("tab %s lost", tab)
		}
	}
	if strings.Contains(list, ">"+copy.Close+"<") || regexp.MustCompile(`<button[^>]*type="submit"`).MatchString(list) {
		t.Fatalf("a text Close button or a full-width search button is still drawn: %s", list)
	}
	// Docked under the application header, like the thread and details panels,
	// and the conversation makes room for it.
	for _, want := range []string{"inset-block-start:var(--chatsave-top", "inset-inline-end:0", "border-inline-start:1px solid var(--line)", ":has(>.chatsave-panel:popover-open) .chat-layout{grid-template-columns:minmax(220px,var(--chat-rail)) minmax(0,1fr) minmax(260px,var(--chat-details))}"} {
		if !strings.Contains(ChatsaveStyles, want) {
			t.Fatalf("the panel's stylesheet lacks %q", want)
		}
	}
	if strings.Contains(ChatsaveStyles, "inset-block:8px") {
		t.Fatal("the panel still starts at the very top of the page")
	}
	if _, err := ui.RenderToString(RenderSavedMessages(SavedMessagesView{Locale: "ar", Tab: "all", Rows: []SavedMessageRow{chatbug034Row()}})); err != nil {
		t.Fatal(err)
	}
}
