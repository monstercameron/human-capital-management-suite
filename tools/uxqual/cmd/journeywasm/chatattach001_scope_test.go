package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATATTACH_001_FilesOnly: uploaded files with no text begin a send;
// the Send button is decided by the files where they change the answer and
// left to the text where they do not.
func TestTodo_CHATATTACH_001_FilesOnly(t *testing.T) {
	s := &chatattach001State{}
	key, _ := s.add("room", "photo.png", "image/png", "blob:photo", 8)
	if _, _, ok := s.begin("room", "tenant", "", nil); ok {
		t.Fatal("files were sent before the upload finished")
	}
	s.complete("room", key, "artifact", "image/png", 8, 200)
	refs, identity, ok := s.begin("room", "tenant", "", nil)
	if !ok || len(refs) != 1 || refs[0].Kind != chatv1.ReferenceKind_REFERENCE_KIND_MEDIA || refs[0].Id != "artifact" || refs[0].ConversationId != "room" {
		t.Fatalf("a message of one file and no text did not begin: refs=%+v ok=%v", refs, ok)
	}
	s.finish("room", false)
	// The same files with no text again are the same attempt; with text they are
	// another message.
	if _, again, ok := s.begin("room", "tenant", "", nil); !ok || again != identity {
		t.Fatal("a retry of a files-only message is a different attempt")
	}
	s.finish("room", false)
	if _, other, ok := s.begin("room", "tenant", "with words", nil); !ok || other == identity {
		t.Fatal("a message with words is the same attempt as one without")
	}
	s.finish("room", true)
	// Nothing to send without files, text or no text.
	if _, _, ok := s.begin("room", "tenant", "", nil); ok {
		t.Fatal("an empty composer began a send")
	}
	if _, _, ok := s.begin("", "tenant", "", nil); ok {
		t.Fatal("a send began with no conversation")
	}

	for _, tc := range []struct {
		name          string
		capable       bool
		text          string
		files         int
		ready         bool
		want          int
		wantsDisabled bool
	}{
		{"uploaded files, no text", true, "", 2, true, chatattach001SendOn, false},
		{"uploaded files, blank text", true, "  \n", 1, true, chatattach001SendOn, false},
		{"uploaded files and text", true, "hello", 1, true, chatattach001SendLeave, false},
		{"no files", true, "", 0, true, chatattach001SendLeave, false},
		{"a file uploading, with text", true, "hello", 1, false, chatattach001SendOff, true},
		{"a send in flight", true, "", 1, false, chatattach001SendOff, true},
		{"a composer that cannot send", false, "", 1, true, chatattach001SendLeave, false},
	} {
		if got := chatattach001SendState(tc.capable, tc.text, tc.files, tc.ready); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestTodo_CHATATTACH_001_Thread: a reply box's files are their own. They do
// not appear under the conversation's composer or another thread's reply box,
// they are uploaded to and referenced in the conversation, and the scope names
// the parent the reply goes under.
func TestTodo_CHATATTACH_001_Thread(t *testing.T) {
	if got := chatattach001Scope("room", ""); got != "room" {
		t.Fatalf("the conversation's scope is %q", got)
	}
	scope := chatattach001Scope("room", "parent")
	if room, parent := chatattach001ScopeParts(scope); room != "room" || parent != "parent" {
		t.Fatalf("scope parts: %q %q", room, parent)
	}
	if room, parent := chatattach001ScopeParts("room"); room != "room" || parent != "" {
		t.Fatalf("conversation scope parts: %q %q", room, parent)
	}
	if got := chatattach001Scope("", "parent"); got != "" {
		t.Fatalf("a thread of no conversation has scope %q", got)
	}

	chose := ""
	s := &chatattach001State{available: true, choose: func() { chose = "composer" }, chooseThread: func() { chose = "thread" }}
	key, ok := s.add(scope, "notes.txt", "text/plain", "", 5)
	if !ok {
		t.Fatal("a reply box refused a file")
	}
	s.complete(scope, key, "artifact", "text/plain", 5, 200)
	view := s.projectionFor("room", "parent")
	if len(view.Files) != 0 || view.Thread == nil || len(view.Thread.Files) != 1 || !view.Thread.Sendable() {
		t.Fatalf("the reply box's file is not its own: composer=%+v thread=%+v", view.Files, view.Thread)
	}
	if other := s.projectionFor("room", "another-parent"); len(other.Thread.Files) != 0 {
		t.Fatal("one thread's file shows under another thread's reply box")
	}
	if alone := s.projectionFor("room", ""); alone.Thread != nil || s.projection("room").Thread != nil {
		t.Fatal("a reply box's files exist with no thread open")
	}
	view.Choose()
	if chose != "composer" {
		t.Fatalf("the composer's Attach opened the %q picker", chose)
	}
	view.Thread.Choose()
	if chose != "thread" {
		t.Fatalf("the reply box's paperclip opened the %q picker", chose)
	}
	// The references name the conversation, never the scope.
	mention := chatui.ChatReference{Kind: "PERSON_MENTION", TenantID: "tenant", ConversationID: "room", ID: "alex", Display: "Alex"}
	refs, _, ok := s.begin(scope, "tenant", "", []chatui.ChatReference{mention})
	if !ok || len(refs) != 2 || refs[1].ConversationId != "room" || refs[1].Id != "artifact" {
		t.Fatalf("a reply's references: %+v ok=%v", refs, ok)
	}
	// The composer is free to send while the reply is in flight.
	if s.projectionFor("room", "parent").Thread.Ready() || !s.projectionFor("room", "parent").Ready() {
		t.Fatal("a reply in flight is not held, or holds the conversation's composer")
	}
	if urls := s.finish(scope, true); len(urls) != 0 || len(s.projectionFor("room", "parent").Thread.Files) != 0 {
		t.Fatalf("an acknowledged reply kept its files: %v", urls)
	}
	// Remove and Send of the reply box act on the reply box's scope.
	var removed, sent []string
	s.remove = func(scope, key string) { removed = append(removed, scope+"/"+key) }
	s.send = func(scope, body string, _ []chatui.ChatReference) { sent = append(sent, scope+"/"+body) }
	thread := s.projectionFor("room", "parent").Thread
	thread.Remove("k")
	thread.Send("words", nil)
	if len(removed) != 1 || removed[0] != scope+"/k" || len(sent) != 1 || sent[0] != scope+"/words" {
		t.Fatalf("removed=%q sent=%q", removed, sent)
	}
}

// TestTodo_CHATATTACH_001_Security_Availability: nothing is offered until the
// server says it takes uploads, and only a plain yes counts.
func TestTodo_CHATATTACH_001_Security_Availability(t *testing.T) {
	s := &chatattach001State{choose: func() {}, chooseThread: func() {}}
	if s.takes() {
		t.Fatal("uploads are taken before the server was asked")
	}
	view := s.projectionFor("room", "parent")
	if view.Choose != nil || view.Thread.Choose != nil {
		t.Fatal("Attach is offered before the server said it takes uploads")
	}
	s.setAvailable(true)
	view = s.projectionFor("room", "parent")
	if !s.takes() || view.Choose == nil || view.Thread.Choose == nil {
		t.Fatal("Attach is not offered where the server takes uploads")
	}
	for _, tc := range []struct {
		status int
		body   string
		want   bool
	}{
		{200, `{"uploads":true}`, true},
		{200, `{"uploads":false}`, false},
		{200, `{}`, false},
		{200, `true`, false},
		{200, ``, false},
		{200, `<html>sign in</html>`, false},
		{404, `{"uploads":true}`, false},
		{503, `{"uploads":true}`, false},
		{0, ``, false},
	} {
		if got := chatattach001Available(tc.status, tc.body); got != tc.want {
			t.Errorf("status %d body %q: %v, want %v", tc.status, tc.body, got, tc.want)
		}
	}
	if chatattach001AvailabilityRoute != chatMediaRouteForTest+"attachments/availability" {
		t.Fatalf("the route is %q", chatattach001AvailabilityRoute)
	}
}

// chatMediaRouteForTest is the media prefix; the wasm build's own constant is
// behind the js tag.
const chatMediaRouteForTest = "/v1/chat/media/"
