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
	if !s.complete("room", key, "artifact", "image/png", 8, 200) || !s.projection("room").Ready() {
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
	if s.complete("room", removed, "late", "text/plain", 3, 200) || len(s.projection("room").Files) != 0 {
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
	if s.complete("room", key, "artifact", "text/plain", 7, 200) || len(s.projection("room").Files) != 0 || s.projection("room").Error != "failed" {
		t.Fatal("forged size admitted")
	}
	for _, status := range []int{413, 415, 429, 503} {
		key, _ := s.add("room", "note.txt", "text/plain", "", 8)
		s.complete("room", key, "", "", 0, status)
		if s.projection("room").Error != chatattach001Refusal(status) || !s.projection("room").Ready() {
			t.Fatalf("status %d leaves pending upload", status)
		}
	}
	key, _ = s.add("room", "note.txt", "text/plain", "", 8)
	s.complete("room", key, "artifact", "text/plain", 8, 200)
	if _, _, ok := s.begin("room", "tenant", "hello", []chatui.ChatReference{{Kind: "PERSON_MENTION", ID: "alex", TenantID: "other", ConversationID: "room"}}); ok {
		t.Fatal("cross-tenant mention admitted")
	}
}
