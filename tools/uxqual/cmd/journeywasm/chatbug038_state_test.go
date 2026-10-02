package main

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

const (
	chatbug038Origin = "https://hcm.example"
	chatbug038Link   = chatbug038Origin + "/workspace/app/chat#share=tokenOne"
)

func chatbug038State() *chatState {
	return &chatState{model: chatui.Model{SelectedID: "general", EmbedOrigin: chatbug038Origin}}
}

// chatbug038Resolve puts a ready preview for every link of the draft in the
// state, the way the page does after the read lands.
func chatbug038Resolve(t *testing.T, s *chatState) {
	t.Helper()
	now := time.Now()
	tokens, epoch := s.claimChatEmbeds(now)
	for _, token := range tokens {
		if !s.finishChatEmbed(epoch, token, chatui.LinkEmbed{Token: token, State: "ready", Channel: "design", Body: "Confirmed"}, now) {
			t.Fatalf("preview of %s did not land", token)
		}
	}
}

func chatbug038Previews(s *chatState) int {
	n := 0
	for _, embed := range s.snapshot().Embeds {
		if embed.State != "" {
			n++
		}
	}
	return n
}

// TestTodo_CHATBUG_038: the previews under the composer belong to the draft.
func TestTodo_CHATBUG_038(t *testing.T) {
	// Sending: the draft is cleared, and its preview goes with it.
	s := chatbug038State()
	s.setDraft("general", "did y'all see this "+chatbug038Link)
	chatbug038Resolve(t, s)
	if chatbug038Previews(s) != 1 {
		t.Fatalf("the draft has no preview to remove: %+v", s.snapshot().Embeds)
	}
	s.setDraft("general", "")
	if got := chatbug038Previews(s); got != 0 {
		t.Fatalf("a sent draft left %d preview(s) behind: %+v", got, s.snapshot().Embeds)
	}
	if len(s.embedEntries) != 0 || len(s.embedOrder) != 0 {
		t.Fatalf("the cache still holds the sent draft's link: %v %v", s.embedEntries, s.embedOrder)
	}

	// Removing the link from the draft removes its preview; typing around it does not.
	s = chatbug038State()
	s.setDraft("general", chatbug038Link)
	chatbug038Resolve(t, s)
	s.setDraft("general", "see "+chatbug038Link+" please")
	if chatbug038Previews(s) != 1 {
		t.Fatal("typing around the link removed its preview")
	}
	s.setDraft("general", "see  please")
	if got := chatbug038Previews(s); got != 0 {
		t.Fatalf("a preview stayed after its link left the draft: %+v", s.snapshot().Embeds)
	}

	// One of two links removed: only its preview goes.
	s = chatbug038State()
	second := chatbug038Origin + "/chat/share/tokenTwo"
	s.setDraft("general", chatbug038Link+" "+second)
	chatbug038Resolve(t, s)
	s.setDraft("general", chatbug038Link)
	embeds := s.snapshot().Embeds
	if _, ok := embeds["tokenOne"]; !ok || len(embeds) != 1 {
		t.Fatalf("want only tokenOne left, have %+v", embeds)
	}

	// A message on screen that holds the same link keeps its preview.
	s = chatbug038State()
	s.model.Messages = []chatui.Message{{ID: "m1", Body: "earlier " + chatbug038Link}}
	s.setDraft("general", chatbug038Link)
	chatbug038Resolve(t, s)
	s.setDraft("general", "")
	if _, ok := s.snapshot().Embeds["tokenOne"]; !ok {
		t.Fatal("clearing the draft removed the preview of a link a message still shows")
	}

	// A draft in another conversation does not touch the open one's previews.
	s = chatbug038State()
	s.setDraft("general", chatbug038Link)
	chatbug038Resolve(t, s)
	s.setDraft("random", "hello")
	if chatbug038Previews(s) != 1 {
		t.Fatal("a draft elsewhere removed the open conversation's preview")
	}

	// A send that failed puts the text back, and the link can be read again.
	s = chatbug038State()
	s.setDraft("general", chatbug038Link)
	chatbug038Resolve(t, s)
	s.setDraft("general", "")
	s.restoreDraft("general", chatbug038Link)
	tokens, _ := s.claimChatEmbeds(time.Now())
	if len(tokens) != 1 || s.snapshot().Draft != chatbug038Link {
		t.Fatalf("a restored draft cannot show its preview again: tokens=%v draft=%q", tokens, s.snapshot().Draft)
	}
}
