package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_CHAT_030_ShareAttemptState(t *testing.T) {
	s := &chatState{cfg: journeyclient.Config{Tenant: "tenant", Subject: "alice"}}
	s.model = chatui.Model{SelectedID: "source", Messages: []chatui.Message{{ID: "post", Body: "hello", Author: "Alice"}}}
	gen, ok := s.openShare("post")
	if !ok || s.snapshot().ShareSource == nil || !s.snapshot().ShareLoading {
		t.Fatal("source not captured")
	}
	if s.shareListing(gen+1, []chatui.Conversation{{ID: "stale", Kind: chatui.PublicChannel}}, "") {
		t.Fatal("stale listing accepted")
	}
	rooms := []chatui.Conversation{{ID: "dest", Kind: chatui.PrivateChannel}, {ID: "direct", Kind: chatui.DirectMessage}}
	if !s.shareListing(gen, rooms, "") {
		t.Fatal("fresh listing rejected")
	}
	if s.shareSelect("direct") || s.shareSelect("source") {
		t.Fatal("ineligible destination selected")
	}
	if !s.shareSelect("dest") {
		t.Fatal("eligible destination rejected")
	}
	s.shareFilter("nothing matches")
	if s.snapshot().ShareDestinationID != "" {
		t.Fatal("filtered-out destination stayed selected")
	}
	s.shareFilter("")
	if !s.shareSelect("dest") {
		t.Fatal("destination was not selectable after clearing filter")
	}
	a, ok := s.beginShare(func() string { return "key-1" })
	if !ok || a.Key != "key-1" || a.SourceRoom != "source" || a.SourcePost != "post" || a.Destination != "dest" {
		t.Fatalf("attempt = %#v, %v", a, ok)
	}
	s.shareFilter("hide destination")
	if s.snapshot().ShareDestinationID != "dest" {
		t.Fatal("pending filter changed destination")
	}
	if _, ok := s.beginShare(func() string { return "key-2" }); ok {
		t.Fatal("double submit accepted")
	}
	if !s.finishShare(a, "failed") || s.snapshot().ShareError != "failed" {
		t.Fatal("failure not shown")
	}
	b, ok := s.beginShare(func() string { return "key-2" })
	if !ok || b.Key != a.Key {
		t.Fatal("retry changed idempotency key")
	}
	if !s.finishShare(b, "") || s.snapshot().SharePostID != "" {
		t.Fatal("success did not close")
	}
}

func TestTodo_CHAT_030_ShareStateGuards(t *testing.T) {
	s := &chatState{cfg: journeyclient.Config{Tenant: "tenant", Subject: "alice"}}
	s.model = chatui.Model{SelectedID: "source", Messages: []chatui.Message{{ID: "media", Attachments: []chatui.Attachment{{ID: "a"}}}, {ID: "post", Body: "hello"}}}
	if _, ok := s.openShare("media"); ok {
		t.Fatal("media-only source accepted")
	}
	gen, ok := s.openShare("post")
	if !ok {
		t.Fatal("source rejected")
	}
	s.closeShare()
	if s.shareListing(gen, []chatui.Conversation{{ID: "dest", Kind: chatui.PublicChannel}}, "") {
		t.Fatal("closed picker accepted late listing")
	}
	gen, _ = s.openShare("post")
	s.shareListing(gen, []chatui.Conversation{{ID: "dest", Kind: chatui.PublicChannel}}, "")
	s.shareSelect("dest")
	a, _ := s.beginShare(func() string { return "key" })
	s.model.SelectedID = "other"
	if s.finishShare(a, "") {
		t.Fatal("stale response accepted after room change")
	}
}

func TestTodo_CHAT_030_ShareSurvivesProjection(t *testing.T) {
	s := &chatState{cfg: journeyclient.Config{Tenant: "tenant", Subject: "alice"}}
	s.model = chatui.Model{SelectedID: "source", Messages: []chatui.Message{{ID: "post", Body: "hello"}}, Draft: "unsent"}
	s.drafts = map[string]string{"source": "unsent"}
	gen, ok := s.openShare("post")
	if !ok {
		t.Fatal("picker did not open")
	}
	s.shareListing(gen, []chatui.Conversation{{ID: "dest", Kind: chatui.PublicChannel}}, "")
	s.shareSelect("dest")
	loaded := chatui.Model{SelectedID: "source", Messages: []chatui.Message{{ID: "post", Body: "hello"}}}
	if !s.adoptLoadedChatProjection(s.currentGeneration(), loaded, chatCursor{}, false) {
		t.Fatal("projection rejected")
	}
	m := s.snapshot()
	if m.SharePostID != "post" || m.ShareDestinationID != "dest" || m.Draft != "unsent" {
		t.Fatalf("projection erased local share or draft: %#v", m)
	}
}
