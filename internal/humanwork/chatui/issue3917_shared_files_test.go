package chatui

import (
	"os"
	"strings"
	"testing"
	"time"
)

func issue3917Model() Model {
	at := time.Date(2025, 11, 11, 9, 41, 0, 0, time.Local)
	return Model{
		Locale:      "en-US",
		SelectedID:  "design",
		CurrentUser: "me",
		Members:     []Member{{ID: "maya", Name: "Maya Patel"}, {ID: "malik", Name: "Malik Johnson"}},
		Conversations: []Conversation{
			{ID: "design", Name: "design", Kind: PublicChannel},
		},
		Callbacks: Callbacks{LoadSharedFiles: func(string) {}, MoreSharedFiles: func() {}, OpenSharedFileSource: func(string) {}, DownloadAttachment: func(string, string) {}},
		SharedFiles: SharedFilesView{ConversationID: "design", Status: SharedFilesReady, Files: []SharedFile{
			{ID: "p1:m1", PostID: "p1", MediaID: "m1", Name: "brand-guide.pdf", SenderID: "maya", At: at},
			{ID: "p2:m2", PostID: "p2", MediaID: "m2", Name: "logo.png", SenderID: "malik", At: at.Add(-24 * time.Hour), ThreadID: "p0"},
			{ID: "p3:m3", PostID: "p3", MediaID: "m3", Name: "kickoff.mp4", SenderID: "ghost", At: at.Add(-48 * time.Hour)},
		}},
	}
}

func issue3917Open() handlers { return handlers{local: localUI{sharedFilesOpen: true}} }

func TestIssue3917_EntryIsACollapsedKeyboardOperableDisclosure(t *testing.T) {
	m := issue3917Model()
	markup := renderNode(t, issue3917Section(m, handlers{}))
	for _, want := range []string{`id="chat-details-files"`, "Shared files", `data-action="shared-files-toggle"`, `aria-expanded="false"`, `aria-controls="chat-details-files-body"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("collapsed entry lacks %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "brand-guide.pdf") || strings.Contains(markup, "shared-file-open") {
		t.Fatalf("a collapsed entry must not list files (or reveal names or counts):\n%s", markup)
	}
	if !strings.Contains(renderNode(t, issue3917Section(m, issue3917Open())), `aria-expanded="true"`) {
		t.Fatal("open state is not announced")
	}
	m.Callbacks.LoadSharedFiles = nil
	if issue3917Section(m, handlers{}) != nil {
		t.Fatal("a client that cannot list files must not offer a dead entry")
	}
}

func TestIssue3917_RowsShowNameTypeSenderDateAndTheWayToTheMessage(t *testing.T) {
	m := issue3917Model()
	markup := renderNode(t, issue3917Section(m, issue3917Open()))
	for _, want := range []string{
		"brand-guide.pdf", "PDF", "Maya Patel", "logo.png", "PNG", "Malik Johnson", "In a thread",
		`data-action="shared-file-open"`, `data-id="p1:m1"`, "View message",
		// Opening and downloading go through the attachment path the message uses.
		`data-action="download-attachment"`, `data-id="p1"`, `data-extra="m1"`, "Download: brand-guide.pdf",
		`id="chat-shared-files-status"`, `aria-live="polite"`, "Files shown: 3",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("list lacks %q:\n%s", want, markup)
		}
	}
	// A sender the roster does not name is never shown as an id.
	if strings.Contains(markup, "ghost") {
		t.Fatalf("an unknown sender leaked its id:\n%s", markup)
	}
	// Linked documents are not files: the list says what it holds.
	if !strings.Contains(markup, "Linked documents") {
		t.Fatalf("the list does not keep document links apart from uploaded files:\n%s", markup)
	}
}

func TestIssue3917_FilterNarrowsByNameAndType(t *testing.T) {
	m := issue3917Model()
	markup := renderNode(t, issue3917Section(m, issue3917Open()))
	for _, want := range []string{`for="chat-shared-files-name"`, `id="chat-shared-files-name"`, `data-action="shared-files-search"`, `role="group"`, `data-action="shared-files-type"`, `data-id="image"`, `aria-pressed="true"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("filters lack %q:\n%s", want, markup)
		}
	}
	h := handlers{local: localUI{sharedFilesOpen: true, sharedFilesType: "image"}}
	narrow := renderNode(t, issue3917Section(m, h))
	if !strings.Contains(narrow, "logo.png") || strings.Contains(narrow, "brand-guide.pdf") || strings.Contains(narrow, "kickoff.mp4") {
		t.Fatalf("the Images chip must keep only images:\n%s", narrow)
	}
	if !strings.Contains(narrow, "Files shown: 1") {
		t.Fatalf("the count must follow the filter:\n%s", narrow)
	}
	m.SharedFiles.Name = "logo"
	named := renderNode(t, issue3917Section(m, issue3917Open()))
	if !strings.Contains(named, `data-chat-value="logo"`) || !strings.Contains(named, `data-action="shared-files-clear"`) {
		t.Fatalf("an applied name stays in the field and can be cleared:\n%s", named)
	}
}

func TestIssue3917_EveryStateIsSaidInWords(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Model, *handlers)
		want   string
		not    string
	}{
		{"loading", func(m *Model, _ *handlers) {
			m.SharedFiles = SharedFilesView{ConversationID: "design", Status: SharedFilesLoading}
		}, "Loading shared files", "brand-guide"},
		{"first open, nothing asked yet", func(m *Model, _ *handlers) { m.SharedFiles = SharedFilesView{} }, "Loading shared files", "No files"},
		{"empty", func(m *Model, _ *handlers) {
			m.SharedFiles = SharedFilesView{ConversationID: "design", Status: SharedFilesReady}
		}, "No files have been shared in this conversation yet.", "Files shown"},
		{"empty after a name filter", func(m *Model, _ *handlers) {
			m.SharedFiles = SharedFilesView{ConversationID: "design", Status: SharedFilesReady, Name: "budget"}
		}, "No files are named like “budget”", "have been shared"},
		{"no file of that type yet loaded, older exist", func(m *Model, h *handlers) {
			m.SharedFiles.Files = m.SharedFiles.Files[:1]
			m.SharedFiles.More = true
			h.local.sharedFilesType = "image"
		}, "None of the files loaded so far are images", "No files have been shared"},
		{"no file of that type at all", func(m *Model, h *handlers) {
			m.SharedFiles.Files = m.SharedFiles.Files[:1]
			h.local.sharedFilesType = "image"
		}, "None of the files in this conversation are images", "loaded so far"},
		{"error", func(m *Model, _ *handlers) {
			m.SharedFiles = SharedFilesView{ConversationID: "design", Status: SharedFilesError}
		}, "could not be loaded", "brand-guide"},
		{"unavailable", func(m *Model, _ *handlers) {
			m.SharedFiles = SharedFilesView{ConversationID: "design", Status: SharedFilesUnavailable}
		}, "not available right now", "brand-guide"},
	}
	for _, c := range cases {
		m, h := issue3917Model(), issue3917Open()
		c.change(&m, &h)
		markup := renderNode(t, issue3917Section(m, h))
		if !strings.Contains(markup, c.want) || (c.not != "" && strings.Contains(markup, c.not)) {
			t.Fatalf("%s: want %q and not %q:\n%s", c.name, c.want, c.not, markup)
		}
		if strings.Contains(c.name, "error") || c.name == "unavailable" {
			if !strings.Contains(markup, `data-action="shared-files-retry"`) || !strings.Contains(markup, `role="alert"`) {
				t.Fatalf("%s: a failure must be announced and offer a retry:\n%s", c.name, markup)
			}
		}
	}
}

func TestIssue3917_PagingIsOfferedWhileOlderFilesExist(t *testing.T) {
	m := issue3917Model()
	if strings.Contains(renderNode(t, issue3917Section(m, issue3917Open())), `data-action="shared-files-more"`) {
		t.Fatal("a finished list offers paging")
	}
	m.SharedFiles.More = true
	markup := renderNode(t, issue3917Section(m, issue3917Open()))
	if !strings.Contains(markup, `data-action="shared-files-more"`) || !strings.Contains(markup, "Show older files") {
		t.Fatalf("no way to older files:\n%s", markup)
	}
	m.SharedFiles.LoadingMore = true
	busy := renderNode(t, issue3917Section(m, issue3917Open()))
	if !strings.Contains(busy, "Loading older files") || !strings.Contains(busy, "disabled") || !strings.Contains(busy, "brand-guide.pdf") {
		t.Fatalf("the files already read stay while older ones load, and the control says it is busy:\n%s", busy)
	}
}

func TestIssue3917_OtherConversationsListIsNeverShown(t *testing.T) {
	m := issue3917Model()
	m.SharedFiles.ConversationID = "finance"
	markup := renderNode(t, issue3917Section(m, issue3917Open()))
	if strings.Contains(markup, "brand-guide.pdf") || strings.Contains(markup, "Files shown") {
		t.Fatalf("another conversation's files leaked into this one:\n%s", markup)
	}
}

func TestIssue3917_FilesAreGroupedByKind(t *testing.T) {
	for name, want := range map[string]string{
		"a.PNG": "image", "b.jpeg": "image", "c.pdf": "document", "d.docx": "document", "e.xlsx": "document", "f.txt": "document",
		"g.mp3": "media", "h.mov": "media", "i.zip": "other", "noext": "other", "": "other",
	} {
		if got := issue3917Kind(name); got != want {
			t.Fatalf("kind of %q = %q, want %q", name, got, want)
		}
	}
}

func TestIssue3917_LocalisedAndMirroredInEveryLanguage(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Geteilte Dateien", "ar": "الملفات المشتركة"} {
		m := issue3917Model()
		m.Locale = locale
		for _, status := range []string{SharedFilesLoading, SharedFilesReady, SharedFilesError, SharedFilesUnavailable} {
			m.SharedFiles.Status = status
			markup := renderNode(t, issue3917Section(m, issue3917Open()))
			if !strings.Contains(markup, want) {
				t.Fatalf("%s %s: no translated title:\n%s", locale, status, markup)
			}
			if strings.Contains(markup, "{") || strings.Contains(markup, "chat.issue3917") || strings.Contains(markup, "Shared files") {
				t.Fatalf("%s %s: English or an unfilled placeholder shows:\n%s", locale, status, markup)
			}
		}
		if locale == "ar" && !strings.Contains(renderNode(t, issue3917Section(m, issue3917Open())), `dir="rtl"`) {
			t.Fatal("the Arabic list is not right-to-left")
		}
	}
	// Every key has three distinct, non-empty columns.
	for key, values := range issue3917Copy {
		for i, v := range values {
			if strings.TrimSpace(v) == "" {
				t.Fatalf("%s column %d is empty", key, i)
			}
		}
		if values[0] == values[1] {
			t.Fatalf("%s has no German text of its own: %q", key, values[1])
		}
	}
}

func TestIssue3917_ViewMessageButtonsAreAtLeastPhoneTouchSize(t *testing.T) {
	if !strings.Contains(issue3917Styles, "min-height:44px") || !strings.Contains(issue3917Styles, "@media(max-width:560px)") {
		t.Fatal("the Shared files controls have no 44px phone touch target")
	}
}

func TestIssue3917_ViewerOffersViewMessageInEveryLanguage(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "View message", "de-DE": "Nachricht anzeigen", "ar": "عرض الرسالة"} {
		for key := range issue3917ViewerCopy {
			text := issue3917ViewerText(locale, key)
			if strings.TrimSpace(text) == "" || strings.Contains(text, "chat.issue3917") {
				t.Fatalf("%s %s: %q", locale, key, text)
			}
		}
		if got := issue3917ViewerText(locale, "view_message"); got != want {
			t.Fatalf("%s: %q, want %q", locale, got, want)
		}
	}
	if issue3917ViewerText("de-DE", "back") == issue3917ViewerText("en-US", "back") {
		t.Fatal("the way back to the image has no German text")
	}
	if !strings.Contains(issue3917ViewerStyles, "min-block-size:44px") || !strings.Contains(issue3917ViewerStyles, ".chat-image-return") {
		t.Fatal("the return bar has no 44px touch target")
	}
}

// The viewer is built by hand in the browser build, so what can be pinned
// without a browser is that View message is wired the way the other tools are:
// it is a tool, it takes part in the focus trap, it reports a vanished message
// in a live region and it hands the move to issue3917ViewMessage.
func TestIssue3917_ViewerWiresViewMessage(t *testing.T) {
	source, err := os.ReadFile("image_viewer_js.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{
		`[]js.Value{zoomButton, openButton, download, viewMessage}`,
		`[]js.Value{closeButton, previous, next, zoomButton, openButton, download, viewMessage}`,
		`case clicked.Equal(viewMessage):`,
		`issue3917ViewMessageGone(post)`,
		`issue3917ViewMessage(post, room, viewerLocale, dir,`,
		`note.Call("setAttribute", "role", "status")`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("image_viewer_js.go lost %q", want)
		}
	}
	if strings.Index(text, "issue3917ViewMessageGone(post)") > strings.Index(text, "issue3917ViewMessage(post, room") {
		t.Fatal("the message must be checked before the viewer is closed")
	}
}
