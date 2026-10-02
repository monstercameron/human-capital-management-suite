package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestTodo_CHATATTACH_001(t *testing.T) {
	s := &chatattach001State{}
	key, ok := s.add("room", "photo.png", "image/png", "blob:photo", 8)
	if !ok || s.projection("room").Ready() || len(s.projection("other").Files) != 0 {
		t.Fatal("upload readiness or room scope wrong")
	}
	s.progress("room", key, 52)
	if got := s.projection("room").Files[0].Progress; got != 52 {
		t.Fatalf("progress=%d", got)
	}
	if _, _, ok := s.begin("room", "tenant", "hello", nil); ok {
		t.Fatal("sent before upload finished")
	}
	if s.complete("room", key, "artifact", "image/png", 8, 200) != chatattach001Uploaded || !s.projection("room").Ready() {
		t.Fatal("completion did not enable send")
	}
	mention := chatui.ChatReference{Kind: "PERSON_MENTION", TenantID: "tenant", ConversationID: "room", ID: "alex", Display: "Alex"}
	refs, identity, ok := s.begin("room", "tenant", "hello", []chatui.ChatReference{mention})
	if !ok || len(refs) != 2 || refs[1].Kind != chatv1.ReferenceKind_REFERENCE_KIND_MEDIA || refs[1].ContentType != "image/png" || refs[1].ByteSize != 8 || refs[1].TenantId != "tenant" || refs[1].ConversationId != "room" {
		t.Fatalf("send refs=%+v ready=%v", refs, ok)
	}
	if s.projection("room").Ready() {
		t.Fatal("send flight enabled another send")
	}
	if _, _, ok := s.begin("room", "tenant", "hello", nil); ok {
		t.Fatal("double send")
	}
	s.finish("room", false)
	if len(s.projection("room").Files) != 1 {
		t.Fatal("failed send discarded attachment")
	}
	_, retry, ok := s.begin("room", "tenant", "hello", []chatui.ChatReference{mention})
	if !ok || retry != identity {
		t.Fatal("retry identity changed")
	}
	urls := s.finish("room", true)
	if len(urls) != 1 || urls[0] != "blob:photo" || len(s.projection("room").Files) != 0 {
		t.Fatalf("acknowledged send did not clear: %+v", urls)
	}
	removed, _ := s.add("room", "note.txt", "text/plain", "", 3)
	s.drop("room", removed)
	if s.complete("room", removed, "late", "text/plain", 3, 200) != chatattach001Dropped || len(s.projection("room").Files) != 0 {
		t.Fatal("late upload resurrected removed file")
	}
	for i := 0; i < 10; i++ {
		if _, ok := s.add("room", "note.txt", "text/plain", "", 3); !ok {
			t.Fatalf("file %d refused", i)
		}
	}
	if _, ok := s.add("room", "eleven.txt", "text/plain", "", 3); ok || s.projection("room").Error != "limit" {
		t.Fatal("ten-file limit absent")
	}
}

func TestTodo_CHATATTACH_001_Security(t *testing.T) {
	if !chatattach001Allowed("table.csv", "text/csv") || !chatattach001Allowed("notes.md", "text/markdown") {
		t.Fatal("plain-text file picker variants refused")
	}
	for _, tc := range []struct {
		name, kind string
		size       int64
		want       string
	}{
		{"empty.txt", "text/plain", 0, "empty"},
		{"big.png", "image/png", chatattach001MaxBytes + 1, "size"},
		{"run.exe", "text/plain", 8, "type"}, {"archive.zip", "application/zip", 8, "type"},
		{"macro.docm", "application/octet-stream", 8, "type"}, {"active.svg", "image/svg+xml", 8, "type"},
	} {
		s := &chatattach001State{}
		if _, ok := s.add("room", tc.name, tc.kind, "", tc.size); ok || s.projection("room").Error != tc.want {
			t.Fatalf("%s refusal=%s", tc.name, s.projection("room").Error)
		}
	}
	s := &chatattach001State{}
	key, _ := s.add("room", "note.txt", "text/plain", "", 8)
	// An answer whose size is not the file's is not an upload: nothing is
	// attached, and the message cannot go out as if it were.
	if s.complete("room", key, "artifact", "text/plain", 7, 200) == chatattach001Uploaded || s.projection("room").Files[0].ID != "" || s.projection("room").Ready() {
		t.Fatal("forged size admitted")
	}
	if _, _, ok := s.begin("room", "tenant", "hello", nil); ok {
		t.Fatal("a message was sent with a file that never uploaded")
	}
	s.drop("room", key)
	// The server refused the file itself: it leaves the draft with the reason.
	for _, status := range []int{413, 415, 422, 429} {
		key, _ := s.add("room", "note.txt", "text/plain", "", 8)
		if s.complete("room", key, "", "", 0, status) != chatattach001Dropped {
			t.Fatalf("status %d kept a refused file", status)
		}
		if s.projection("room").Error != chatattach001Refusal(status) || len(s.projection("room").Files) != 0 || !s.projection("room").Ready() {
			t.Fatalf("status %d leaves pending upload", status)
		}
	}
	key, _ = s.add("room", "note.txt", "text/plain", "", 8)
	s.complete("room", key, "artifact", "text/plain", 8, 200)
	if _, _, ok := s.begin("room", "tenant", "hello", []chatui.ChatReference{{Kind: "PERSON_MENTION", ID: "alex", TenantID: "other", ConversationID: "room"}}); ok {
		t.Fatal("cross-tenant mention admitted")
	}
}

// TestTodo_CHATATTACH_001_Retry: an upload that did not get through (no
// answer, a server fault) keeps its file under the draft, holds Send, and goes
// again on Retry without the file being chosen a second time; a refusal of one
// file in a drop of several is still said after the others are accepted.
func TestTodo_CHATATTACH_001_Retry(t *testing.T) {
	for _, status := range []int{0, 500, 503} {
		s := &chatattach001State{}
		key, _ := s.add("room", "photo.png", "image/png", "blob:photo", 8)
		s.progress("room", key, 60)
		if got := s.complete("room", key, "", "", 0, status); got != chatattach001Kept {
			t.Fatalf("status %d: the upload ended %q, want the file kept for a retry", status, got)
		}
		view := s.projection("room")
		if len(view.Files) != 1 || !view.Files[0].Failed || view.Files[0].Uploading || view.Files[0].Progress != 0 || view.Error != "" {
			t.Fatalf("status %d: the failed file is not kept as failed: %+v error %q", status, view.Files, view.Error)
		}
		if view.Ready() {
			t.Fatalf("status %d: Send is on with a file that is not uploaded", status)
		}
		if _, _, ok := s.begin("room", "tenant", "hello", nil); ok {
			t.Fatalf("status %d: the message went out without its file", status)
		}
		if s.preview("room", key) != "blob:photo" {
			t.Fatalf("status %d: the failed file lost its picture", status)
		}
		if !s.retry("room", key) {
			t.Fatalf("status %d: Retry did nothing", status)
		}
		if view = s.projection("room"); view.Files[0].Failed || !view.Files[0].Uploading {
			t.Fatalf("status %d: Retry did not start the upload again: %+v", status, view.Files[0])
		}
		if s.retry("room", key) {
			t.Fatalf("status %d: a second Retry started a second upload of the same file", status)
		}
		if s.complete("room", key, "artifact", "image/png", 8, 200) != chatattach001Uploaded || !s.projection("room").Ready() {
			t.Fatalf("status %d: the retried upload did not finish", status)
		}
		if refs, _, ok := s.begin("room", "tenant", "hello", nil); !ok || len(refs) != 1 || refs[0].Id != "artifact" {
			t.Fatalf("status %d: the retried file is not sent with the message: %+v", status, refs)
		}
	}
	// Retry is for a failed upload only, and never while a send is in flight.
	s := &chatattach001State{}
	key, _ := s.add("room", "note.txt", "text/plain", "", 3)
	if s.retry("room", key) || s.retry("room", "missing") {
		t.Fatal("Retry restarted an upload that had not failed")
	}
	s.complete("room", key, "", "", 0, 0)
	s.drop("room", key)
	if s.retry("room", key) || len(s.projection("room").Files) != 0 {
		t.Fatal("a removed file was retried")
	}

	// One drop of three files: the middle one is refused. The line stays.
	s = &chatattach001State{}
	s.clearError("room")
	s.add("room", "a.txt", "text/plain", "", 3)
	s.add("room", "run.exe", "application/octet-stream", "", 3)
	s.add("room", "b.txt", "text/plain", "", 3)
	if view := s.projection("room"); len(view.Files) != 2 || view.Error != "type" {
		t.Fatalf("a refused file in a drop of several is not said: %d files, error %q", len(view.Files), view.Error)
	}
	// The next choice starts clean.
	s.clearError("room")
	if s.projection("room").Error != "" {
		t.Fatal("the last refusal is still shown after the person chose again")
	}
}
