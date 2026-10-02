package chatui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_CHATSAVE_001_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		copy := SavedMessagesCopy(locale)
		fields := reflect.ValueOf(copy)
		for i := 0; i < fields.NumField(); i++ {
			if fields.Field(i).String() == "" {
				t.Fatal("missing localized copy", locale, i)
			}
		}
		view := SavedMessagesView{Locale: locale, Rows: []SavedMessageRow{{TenantID: "tenant", ConversationID: "room", PostID: "post", Author: "Alex", Channel: "Support", Body: "<script>alert('unsafe')</script>", Note: "private note", Availability: "readable", Sequence: 42}, {PostID: "revoked", Body: "SECRET TEXT", Availability: "no_access"}, {PostID: "deleted", Body: "DELETED SECRET", Availability: "deleted"}}, HasMore: true}
		markup, err := ui.RenderToString(RenderSavedMessages(view))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{copy.Todo, copy.Done, copy.All, copy.Remind, copy.Unsave, copy.More, copy.NoAccess, copy.Deleted, `data-saved-sequence="42"`, `data-saved-action="open"`, "private note"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s missing %q", locale, want)
			}
		}
		for _, forbidden := range []string{"<script>", "SECRET TEXT", "DELETED SECRET"} {
			if strings.Contains(markup, forbidden) {
				t.Fatal("untrusted or revoked content", forbidden)
			}
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("RTL missing")
		}
		for _, tab := range []string{"todo", "done", "all"} {
			empty, err := ui.RenderToString(RenderSavedMessages(SavedMessagesView{Locale: locale, Tab: tab}))
			if err != nil {
				t.Fatal(err)
			}
			want := copy.EmptyTodo
			if tab == "done" {
				want = copy.EmptyDone
			}
			if tab == "all" {
				want = copy.EmptyAll
			}
			if !strings.Contains(empty, want) {
				t.Fatal("empty next step")
			}
		}
	}
}

func TestTodo_CHATSAVE_001_Accessibility(t *testing.T) {
	// The note field and the date field open from the item's own controls; the
	// segments are tabs, so the chosen one is selected (not "pressed").
	markup, err := ui.RenderToString(RenderSavedMessages(SavedMessagesView{Locale: "en-US", Error: "saved_limit", EditingNote: SavedItemKey("", "", "post"), ReminderMenu: SavedItemKey("", "", "post"), PickingDate: true, Rows: []SavedMessageRow{{PostID: "post", Availability: "readable"}}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="status"`, `aria-live="polite"`, `aria-selected="true"`, `for="chatsave-note-field-post"`, `id="chatsave-note-field-post"`, `for="chatsave-due-post"`, `type="datetime-local"`, `data-saved-action="retry"`, SavedMessagesCopy("en-US").Limit} {
		if !strings.Contains(markup, want) {
			t.Fatal("missing accessibility or limit", want)
		}
	}
	for _, want := range []string{"min-height:44px", "focus-visible", "max-width:calc(100vw - 16px)", "prefers-reduced-motion"} {
		if !strings.Contains(ChatsaveStyles, want) {
			t.Fatal("style contract", want)
		}
	}
	if strings.Contains(ChatsaveStyles, "#") || strings.Contains(ChatsaveStyles, "font-family") {
		t.Fatal("hardcoded branding")
	}
	m := Model{CurrentUser: "person", CurrentTenantID: "tenant", SelectedID: "room", Locale: "en-US"}
	for _, menu := range []bool{false, true} {
		action, err := ui.RenderToString(chatsaveAction(m, Message{ID: "post"}, menu))
		if err != nil || !strings.Contains(action, `aria-keyshortcuts="Alt+Shift+S"`) || !strings.Contains(action, "Save for later") {
			t.Fatal("save control", err)
		}
	}
	sidebar, err := ui.RenderToString(chatsaveSidebar(m))
	if err != nil || !strings.Contains(sidebar, "Saved") || !strings.Contains(sidebar, `data-saved-action="toggle"`) || !strings.Contains(renderNode(t, chatsavePanel(m)), "Loading saved messages") {
		t.Fatal("sidebar")
	}
}

func TestTodo_CHATSAVE_001_Browser_InContext(t *testing.T) {
	m := Model{Locale: "en-US", CurrentUser: "person", CurrentTenantID: "tenant", SelectedID: "room", State: StateReady, MenuID: "post", Conversations: []Conversation{{ID: "room", Name: "Support", Kind: PublicChannel}}, Messages: []Message{{ID: "post", AuthorID: "person", Author: "Alex", Body: "work message", Attachments: []Attachment{{ID: "file", Name: "Report", ContentType: "application/pdf"}}}}, Callbacks: Callbacks{OpenMenu: func(string) {}}}
	markup := render(t, m)
	for _, want := range []string{`data-saved-sidebar="true"`, `data-saved-post="post"`, `data-saved-conversation="room"`, `data-saved-action="save"`, `aria-keyshortcuts="Alt+Shift+S"`} {
		if !strings.Contains(markup, want) {
			t.Fatal("workspace hook missing", want)
		}
	}
	thread, err := ui.RenderToString(html.Div(html.Props{}, threadMessageMenu(m, Message{ID: "reply", Body: "thread reply"})...))
	if err != nil || !strings.Contains(thread, `data-saved-post="reply"`) {
		t.Fatal("thread save hook missing", err)
	}
}
