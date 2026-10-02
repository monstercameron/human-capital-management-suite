package chatui

import (
	"strings"
	"testing"
	"time"
)

func chatux025BrowseModel() Model {
	return Model{State: StateReady, Locale: "en-US", SelectedID: "general", CurrentUser: "me", ShowBrowse: true,
		Text: func(key string) string { return englishCopy[key] },
		Conversations: []Conversation{
			{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, MemberCount: 12},
			{ID: "dm", Name: "Loretta", Kind: DirectMessage, Joined: true},
		},
		Browse:    []Conversation{{ID: "sales", Name: "sales", Kind: PublicChannel, MemberCount: 4}},
		Callbacks: Callbacks{LeaveConversation: func(string) {}, JoinConversation: func(string) {}, SelectConversation: func(string) {}, CloseBrowse: func() {}},
	}
}

// TestTodo_CHATUX_025: the small presentation defects of the pass of 2026-10-02
// that Chat's own markup owns: Browse channels fills the height and a joined
// row can be left; the document card does not repeat the title the message
// above it links; the thread parent shows its day.
func TestTodo_CHATUX_025(t *testing.T) {
	t.Run("browse leave", func(t *testing.T) {
		m := chatux025BrowseModel()
		dialog := renderNode(t, browseDialog(m, handlers{}))
		joined := dialog[strings.Index(dialog, `data-conversation-id="general"`):]
		joined = joined[:strings.Index(joined, "</li>")]
		unjoined := dialog[strings.Index(dialog, `data-conversation-id="sales"`):]
		unjoined = unjoined[:strings.Index(unjoined, "</li>")]
		if !strings.Contains(joined, `data-action="rail-leave"`) || !strings.Contains(joined, ">Leave<") || !strings.Contains(joined, `aria-label="Leave general"`) {
			t.Fatalf("a joined row has no Leave: %s", joined)
		}
		if strings.Contains(unjoined, "rail-leave") || !strings.Contains(unjoined, `data-action="join"`) {
			t.Fatalf("a channel the person is not in offers Leave: %s", unjoined)
		}
		// The first press asks, naming what the second does.
		asking := renderNode(t, browseDialog(m, handlers{local: localUI{railLeave: "general"}}))
		if !strings.Contains(asking, `data-action="rail-leave-confirm"`) || !strings.Contains(asking, "Press again to leave") || strings.Count(asking, "rail-leave-confirm") != 1 {
			t.Fatalf("the question: %s", asking)
		}
		// Nothing is offered without the callback.
		m.Callbacks.LeaveConversation = nil
		if disabled := renderNode(t, browseDialog(m, handlers{})); !strings.Contains(chatbug074Tag(t, disabled, `data-action="rail-leave"`), "disabled") {
			t.Fatal("Leave can be pressed with nobody to leave for")
		}
		// The dialog takes the height the window has.
		for _, want := range []string{".browse-dialog{display:flex;flex-direction:column", ".browse-dialog>.browse-list{flex:1 1 auto;min-height:0;max-height:none;overflow-y:auto}"} {
			if !strings.Contains(chatux025Styles, want) {
				t.Errorf("styles lack %s", want)
			}
		}
		if strings.Index(Stylesheet, ChatLane2Styles) < strings.Index(Stylesheet, ChatLane3Styles) || !strings.Contains(ChatLane2Styles, chatux025Styles) {
			t.Error("the browse rules do not come last, so the earlier fixed height wins")
		}
	})
	t.Run("document card title", func(t *testing.T) {
		m := Model{Locale: "en-US", Text: func(key string) string { return englishCopy[key] }}
		preview := DocPreview{ID: "d1", State: "ready", Readable: true, Title: "2026 holiday guide", Owner: "Walt Brennan", UpdatedAt: "Oct 1", Snippet: "Offices are closed."}
		m.DocPreviews = map[string]DocPreview{"d1": preview}
		sent := renderNode(t, spanOf(docPreviewEmbeds(m, "see doc:d1")))
		if strings.Contains(sent, "<strong") || strings.Contains(sent, ">2026 holiday guide<") || !strings.Contains(sent, `aria-label="Open document: 2026 holiday guide"`) || !strings.Contains(sent, "Walt Brennan") || !strings.Contains(sent, "Offices are closed.") {
			t.Fatalf("the card under a message repeats the title or lost its parts: %s", sent)
		}
		// The line above carries the title as its link.
		if line := renderNode(t, spanOf(docLinkReferenceBody(m, "see doc:d1"))); !strings.Contains(line, ">2026 holiday guide<") {
			t.Fatalf("the message does not link the title: %s", line)
		}
		draft := renderNode(t, spanOf(docPreviewDraftEmbeds(m, "see doc:d1")))
		if !strings.Contains(draft, "<strong") || !strings.Contains(draft, "2026 holiday guide") {
			t.Fatalf("a draft's card has no title above it and keeps its own: %s", draft)
		}
		// A document still loading, or one the reader cannot open, has no title to drop.
		m.DocPreviews = map[string]DocPreview{"d1": {ID: "d1", State: "ready"}}
		if locked := renderNode(t, spanOf(docPreviewEmbeds(m, "doc:d1"))); !strings.Contains(locked, "chat-doc-embed-locked") {
			t.Fatalf("restricted card: %s", locked)
		}
	})
	t.Run("thread parent day", func(t *testing.T) {
		m := chatux008Model(false)
		sent := time.Now().AddDate(0, 0, -1)
		m.Messages[0].SentAt = sent
		pane := chatux008Pane(t, m)
		root := pane[strings.Index(pane, `class="thread-root"`):]
		root = root[:strings.Index(root, `class="message-body`)]
		if !strings.Contains(root, "Yesterday · ") || !strings.Contains(root, chat5Clock("en-US", sent)) {
			t.Fatalf("the parent has no day: %s", root)
		}
		m.Messages[0].SentAt = time.Time{}
		m.Messages[0].TimeLabel = "5:17 AM"
		if stamp := chatux025ThreadStamp(m, m.Messages[0]); stamp != "5:17 AM" {
			t.Fatalf("a parent with no timestamp keeps its label, got %q", stamp)
		}
	})
	t.Run("copy", func(t *testing.T) {
		for key, row := range chatux025Copy {
			for i, text := range row {
				if strings.TrimSpace(text) == "" {
					t.Errorf("%s has no text in language %d", key, i)
				}
			}
		}
		m := chatux025BrowseModel()
		m.Locale = "de-DE"
		if got := chatux025Text(m, "leave"); got != "Verlassen" {
			t.Fatalf("de-DE %q", got)
		}
	})
}
