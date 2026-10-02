package chatui

import (
	stdhtml "html"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// chatattach001Fixture is a ready conversation with one uploaded file under
// the composer and, when thread is set, an open thread with one under its
// reply box.
func chatattach001Fixture(locale string, thread bool) (Model, *[]string) {
	sent := &[]string{}
	file := Chatattach001Draft{Key: "f1", Attachment: Attachment{ID: "artifact", Name: "photo.png", ContentType: "image/png", Bytes: 2048, URL: "blob:photo"}, Progress: 100}
	record := func(scope string) func(string, []ChatReference) {
		return func(body string, _ []ChatReference) { *sent = append(*sent, scope+":"+body) }
	}
	m := Model{State: StateReady, Locale: locale, Direction: direction(locale), SelectedID: "room", CurrentUser: "alice", CurrentTenantID: "tenant",
		Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel, Joined: true}},
		Messages:      []Message{{ID: "parent", AuthorID: "bob", Author: "Bob", Body: "the question", Replies: 1}},
		Callbacks:     Callbacks{SendMessage: func(string, string) {}, ReplyInThread: func(string, string) {}},
		Chatattach001: &Chatattach001Composer{Choose: func() {}, Send: record("room"), Files: []Chatattach001Draft{file}},
	}
	if thread {
		m.ShowThread, m.ThreadParentID = true, "parent"
		m.ThreadParent = &m.Messages[0]
		m.Chatattach001.Thread = &Chatattach001Composer{Choose: func() {}, Send: record("thread"), Files: []Chatattach001Draft{file}}
	}
	return m, sent
}

func chatattach001Form(t *testing.T, m Model, class string) *xhtml.Node {
	t.Helper()
	markup := stdhtml.UnescapeString(chatremoveMarkup(t, Build(m)))
	forms := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "form" && chatPolishHasClass(n, class) })
	if len(forms) != 1 {
		t.Fatalf("%d %s forms", len(forms), class)
	}
	return forms[0]
}

func chatattach001SendButton(t *testing.T, form *xhtml.Node) *xhtml.Node {
	t.Helper()
	buttons := chatPolishNodesIn(form, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishHasClass(n, "send-button") })
	if len(buttons) != 1 {
		t.Fatalf("%d send buttons", len(buttons))
	}
	return buttons[0]
}

// TestTodo_CHATATTACH_001_FilesOnly: uploaded files with no text can be sent.
// Send is on for them, off while one is uploading, and pressing it sends the
// files with an empty body exactly once.
func TestTodo_CHATATTACH_001_FilesOnly(t *testing.T) {
	m, sent := chatattach001Fixture("en-US", false)
	if !m.Chatattach001.Sendable() {
		t.Fatal("one uploaded file is not a message")
	}
	if chatmodHasAttr(chatattach001SendButton(t, chatattach001Form(t, m, "chat-composer")), "disabled") {
		t.Fatal("Send is off for an uploaded file with no text")
	}
	// No file and no text: off, as it always was.
	bare, _ := chatattach001Fixture("en-US", false)
	bare.Chatattach001.Files = nil
	if bare.Chatattach001.Sendable() || !chatmodHasAttr(chatattach001SendButton(t, chatattach001Form(t, bare, "chat-composer")), "disabled") {
		t.Fatal("Send is on with neither text nor a file")
	}
	// A file still uploading, or one that failed: off, with or without text.
	for name, change := range map[string]func(*Chatattach001Draft){"uploading": func(f *Chatattach001Draft) { f.Uploading = true }, "failed": func(f *Chatattach001Draft) { f.Failed = true }} {
		busy, _ := chatattach001Fixture("en-US", false)
		change(&busy.Chatattach001.Files[0])
		busy.Draft = "with text"
		if busy.Chatattach001.Sendable() || !chatmodHasAttr(chatattach001SendButton(t, chatattach001Form(t, busy, "chat-composer")), "disabled") {
			t.Fatalf("Send is on while a file is %s", name)
		}
		if chatattach001SendFilesOnly(busy, "") {
			t.Fatalf("files were sent while one is %s", name)
		}
	}
	var none *Chatattach001Composer
	if none.Sendable() || none.thread() != nil {
		t.Fatal("a composer with no upload owner has files")
	}

	if !chatattach001SendFilesOnly(m, "  \n") || len(*sent) != 1 || (*sent)[0] != "room:" {
		t.Fatalf("files with no text were not sent with an empty body: %v", *sent)
	}
	// With text the ordinary send path carries the files; this one stays out.
	if chatattach001SendFilesOnly(m, "hello") || len(*sent) != 1 {
		t.Fatalf("the files-only path sent a message that has text: %v", *sent)
	}
	typed := m
	typed.Draft = "a stored draft"
	if chatattach001SendFilesOnly(typed, "") || len(*sent) != 1 {
		t.Fatal("the files-only path dropped a stored draft")
	}
	nowhere := m
	nowhere.SelectedID = ""
	if chatattach001SendFilesOnly(nowhere, "") {
		t.Fatal("files were sent with no conversation open")
	}
	for _, want := range []string{
		".chat-workspace .chat-composer:has(.chatattach001-chip) .send-button:not(:disabled),.chat-workspace .thread-composer:has(.chatattach001-chip) .send-button:not(:disabled){opacity:1}",
		".chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown):has(.chatattach001-chip){flex-wrap:wrap}",
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("the stylesheet misses %q", want)
		}
	}
}

// TestTodo_CHATATTACH_001_Browser_Thread: the reply box takes files. It has a
// paperclip, its own picker and its own chips; Send from it sends the reply
// box's files, not the composer's; where uploads are not available neither box
// offers a way to attach.
func TestTodo_CHATATTACH_001_Browser_Thread(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m, sent := chatattach001Fixture(locale, true)
		thread := chatattach001Form(t, m, "thread-composer")
		clips := chatPolishNodesIn(thread, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-chatattach001-choose") == "thread"
		})
		if len(clips) != 1 || chatPolishAttr(clips[0], "aria-label") != Chatattach001Text(locale, "attach") || chatPolishAttr(clips[0], "type") != "button" {
			t.Fatalf("%s: the reply box has no named paperclip", locale)
		}
		pickers := chatPolishNodesIn(thread, func(n *xhtml.Node) bool { return n.Data == "input" && chatPolishAttr(n, "type") == "file" })
		if len(pickers) != 1 || chatPolishAttr(pickers[0], "id") != Chatattach001ThreadPickerID {
			t.Fatalf("%s: the reply box has no picker of its own", locale)
		}
		chips := chatPolishNodesIn(thread, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chatattach001-chip") })
		removes := chatPolishNodesIn(thread, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chatattach001-remove") == "f1" })
		if len(chips) != 1 || len(removes) != 1 || chatPolishAttr(removes[0], "aria-label") != Chatattach001Text(locale, "remove")+": photo.png" {
			t.Fatalf("%s: the reply box does not show its file with a named Remove", locale)
		}
		// The conversation composer keeps its own picker and chip.
		main := chatattach001Form(t, m, "chat-composer")
		if ids := chatPolishNodesIn(main, func(n *xhtml.Node) bool { return n.Data == "input" && chatPolishAttr(n, "id") == Chatattach001PickerID }); len(ids) != 1 {
			t.Fatalf("%s: the composer lost its picker", locale)
		}
		// Send from the reply box sends the reply box's files.
		if !chatattach001ReplyWithFiles(m, mentionStore{box: &mentionBox{}}) || len(*sent) != 1 || (*sent)[0] != "thread:" {
			t.Fatalf("%s: a reply of files alone was not sent from the reply box: %v", locale, *sent)
		}
		// While the reply's file is uploading the reply waits and nothing else
		// sends it as plain text.
		waiting, held := chatattach001Fixture(locale, true)
		waiting.Chatattach001.Thread.Files[0].Uploading = true
		if !chatattach001ReplyWithFiles(waiting, mentionStore{box: &mentionBox{}}) || len(*held) != 0 {
			t.Fatalf("%s: a reply was sent while its file is uploading: %v", locale, *held)
		}
		// No files under the reply box: the ordinary reply path has it.
		plain, _ := chatattach001Fixture(locale, true)
		plain.Chatattach001.Thread.Files = nil
		if chatattach001ReplyWithFiles(plain, mentionStore{box: &mentionBox{}}) {
			t.Fatalf("%s: a reply with no files was taken by the files path", locale)
		}
		// Uploads are not available: no paperclip, no picker, and no "Attach a
		// file" in the Add menu.
		closed, _ := chatattach001Fixture(locale, true)
		closed.Chatattach001.Choose, closed.Chatattach001.Thread.Choose = nil, nil
		closed.Chatattach001.Files, closed.Chatattach001.Thread.Files = nil, nil
		page := stdhtml.UnescapeString(chatremoveMarkup(t, Build(closed)))
		if strings.Contains(page, `data-chatattach001-choose`) || strings.Contains(page, Chatattach001ThreadPickerID) || strings.Contains(page, `data-extra="attachment"`) || strings.Contains(page, Chatattach001Text(locale, "attach")) {
			t.Fatalf("%s: a way to attach is offered where the server takes no uploads", locale)
		}
		open := stdhtml.UnescapeString(chatremoveMarkup(t, Build(m)))
		if !strings.Contains(open, `data-extra="attachment"`) {
			t.Fatalf("%s: the Add menu lost Attach a file where uploads are available", locale)
		}
	}
	// No thread open: no reply-box files at all.
	alone, _ := chatattach001Fixture("en-US", false)
	if chatattach001ThreadDrafts(alone, false) != nil || chatattach001ReplyWithFiles(alone, mentionStore{box: &mentionBox{}}) {
		t.Fatal("a reply box's files exist with no thread open")
	}
	for _, want := range []string{
		".chat-workspace .thread-composer:has(.chatattach001-chip) .thread-also{display:none}",
		"@container chat (max-width:1100px){.chat-workspace .thread-composer:not(:focus-within) .chatattach001-thread:not(:has(.chatattach001-chip)){display:none}}",
		"@media(pointer:coarse){.chat-workspace .chatattach001-thread-attach{min-inline-size:44px;min-block-size:44px}}",
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("the stylesheet misses %q", want)
		}
	}
	if strings.Contains(Chatattach001Styles, "#") {
		t.Error("the attachment styles carry a literal colour")
	}
}
