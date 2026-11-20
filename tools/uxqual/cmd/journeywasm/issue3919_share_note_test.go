package main

import (
	"strings"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func issue3919State(t *testing.T) (*chatState, uint64) {
	t.Helper()
	s := &chatState{cfg: journeyclient.Config{Tenant: "tenant", Subject: "alice"}}
	s.model = chatui.Model{SelectedID: "source", Messages: []chatui.Message{{ID: "post", Body: "hello", Author: "Ari"}}}
	gen, ok := s.openShare("post")
	if !ok {
		t.Fatal("share did not open")
	}
	s.shareListing(gen, []chatui.Conversation{
		{ID: "chan", Name: "engineering", Kind: chatui.PublicChannel},
		{ID: "dm", Name: "Ana", Kind: chatui.DirectMessage},
		{ID: "crew", Name: "Crew", Kind: chatui.GroupChat},
		{ID: "bot", Name: "Helper", Kind: chatui.DirectMessage, Agent: true},
	}, "")
	return s, gen
}

func TestIssue3919_DirectAndGroupConversationsAreDestinations(t *testing.T) {
	s, _ := issue3919State(t)
	for _, id := range []string{"chan", "dm", "crew"} {
		if !s.shareSelect(id) {
			t.Errorf("%s was refused as a destination", id)
		}
		if _, ok := s.beginShare(func() string { return "k-" + id }); !ok {
			t.Errorf("%s could not be shared to", id)
		}
		s.mu.Lock()
		s.model.SharePending = false
		s.mu.Unlock()
	}
	if s.shareSelect("bot") {
		t.Error("an agent conversation was accepted as a destination")
	}
}

func TestIssue3919_NoteTravelsWithTheAttemptAndSurvivesARefusal(t *testing.T) {
	s, _ := issue3919State(t)
	s.shareSelect("dm")
	s.shareNote("  Can we talk?  ")
	a, ok := s.beginShare(func() string { return "key-1" })
	if !ok || a.Note != "Can we talk?" {
		t.Fatalf("attempt = %#v, %v; want the trimmed note", a, ok)
	}
	if !s.finishShare(a, "refused") {
		t.Fatal("failure not recorded")
	}
	if got := s.snapshot(); got.ShareNote != "  Can we talk?  " || got.ShareError != "refused" || got.SharePostID == "" {
		t.Fatalf("after a refusal: note %q error %q open %q; want the note kept and the dialog open", got.ShareNote, got.ShareError, got.SharePostID)
	}
	retry, _ := s.beginShare(func() string { return "key-2" })
	if retry.Key != "key-1" {
		t.Fatalf("retrying the same share minted a new key %q", retry.Key)
	}
	s.finishShare(retry, "refused")
	s.shareNote("A different note")
	edited, _ := s.beginShare(func() string { return "key-3" })
	if edited.Key != "key-3" || edited.Note != "A different note" {
		t.Fatalf("an edited note reused the old request: %#v", edited)
	}
	if !s.finishShare(edited, "") || s.snapshot().ShareNote != "" {
		t.Fatal("a sent share kept its note")
	}
}

func TestIssue3919_NoteIsBoundedAndDroppedOnCancel(t *testing.T) {
	s, _ := issue3919State(t)
	s.shareNote(strings.Repeat("é", chatui.ShareNoteMaxRunes+50))
	if got := []rune(s.snapshot().ShareNote); len(got) != chatui.ShareNoteMaxRunes {
		t.Fatalf("note length = %d, want %d", len(got), chatui.ShareNoteMaxRunes)
	}
	s.closeShare()
	if s.snapshot().ShareNote != "" {
		t.Fatal("cancel kept the note for the next share")
	}
	s.shareNote("ignored")
	if s.snapshot().ShareNote != "" {
		t.Fatal("a note was accepted with no share open")
	}
}

func TestIssue3919_ANewShareStartsWithNoNote(t *testing.T) {
	s, _ := issue3919State(t)
	s.shareNote("old")
	s.closeShare()
	if _, ok := s.openShare("post"); !ok || s.snapshot().ShareNote != "" {
		t.Fatalf("a new share started with note %q", s.snapshot().ShareNote)
	}
}

func TestIssue3919_TimelineMessageCarriesTheSharersNote(t *testing.T) {
	post := &chatv1.Post{Id: "f", AuthorId: "maya", Body: "launch moved", SourceAttribution: &chatv1.SourceAttribution{OriginalAuthorId: "ben", Note: "Can we talk about this?"}}
	got := chatMessage(post, "en-US", map[string]string{"ben": "Ben", "maya": "Maya"}, time.Now())
	if got.ForwardNote != "Can we talk about this?" || got.ForwardedAuthor != "Ben" || got.Body != "launch moved" {
		t.Fatalf("message = %+v; want the note, the original author and the copied text", got)
	}
	plain := chatMessage(&chatv1.Post{Id: "p", AuthorId: "maya", Body: "hi"}, "en-US", nil, time.Now())
	if plain.ForwardNote != "" {
		t.Fatalf("an ordinary message carries note %q", plain.ForwardNote)
	}
}
