package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatEmbedDraftSignatureAndInvalidation(t *testing.T) {
	origin := "https://hcm.example"
	s := &chatState{model: chatui.Model{SelectedID: "room", EmbedOrigin: origin, Draft: "https://hcm.example/workspace/app/chat#share=one"}}
	if !s.markChatEmbedDraftChange("room", s.model.Draft) {
		t.Fatal("locator not detected")
	}
	if s.markChatEmbedDraftChange("room", s.model.Draft+" ordinary text") {
		t.Fatal("ordinary typing changed locator signature")
	}
	if !s.markChatEmbedDraftChange("room", "#msg=old") {
		t.Fatal("legacy locator not detected")
	}
	if chatEmbedDraftSignature(s.model.Draft+" more", origin) != chatEmbedDraftSignature(s.model.Draft, origin) {
		t.Fatal("ordinary typing invalidates pending preview")
	}
	before := chatEmbedFingerprint(s.model)
	s.invalidateChatEmbeds()
	if chatEmbedFingerprint(s.model) == before {
		t.Fatal("cache invalidation did not trigger effect")
	}
}

func TestChatChannelDraftSignatureSurvivesOrdinaryTyping(t *testing.T) {
	origin := "https://hcm.example"
	channel := "#general https://hcm.example/workspace/app/chat#channel=room-42"
	state := &chatState{model: chatui.Model{SelectedID: "room", EmbedOrigin: origin}}
	if !state.markChatEmbedDraftChange("room", channel) {
		t.Fatal("channel paste did not schedule preview")
	}
	if state.markChatEmbedDraftChange("room", channel+" ordinary typing") {
		t.Fatal("ordinary typing invalidated channel preview")
	}
	if !state.markChatEmbedDraftChange("room", channel+" /workspace/app/chat#channel=room-43") {
		t.Fatal("new channel locator was missed")
	}
}

func TestChatEmbedBoundedCacheAndExpiry(t *testing.T) {
	origin := "https://hcm.example"
	posts := make([]chatui.Message, 26)
	for i := range posts {
		posts[i].Body = "/chat/share/token" + strings.Repeat("a", i+1)
	}
	s := &chatState{model: chatui.Model{SelectedID: "room", EmbedOrigin: origin, Messages: posts}}
	now := time.Now()
	tokens, epoch := s.claimChatEmbeds(now)
	if len(tokens) != chatEmbedLimit {
		t.Fatalf("claimed %d tokens", len(tokens))
	}
	for _, token := range tokens {
		if !s.finishChatEmbed(epoch, token, chatui.LinkEmbed{Token: token, State: "ready", Body: "secret"}, now) {
			t.Fatal("finish failed")
		}
	}
	if fresh, _ := s.claimChatEmbeds(now.Add(chatEmbedFreshFor - time.Millisecond)); len(fresh) != 0 {
		t.Fatal("fresh body re-resolved")
	}
	if expired, _ := s.claimChatEmbeds(now.Add(chatEmbedFreshFor)); len(expired) != chatEmbedLimit {
		t.Fatalf("expired claims = %d", len(expired))
	}
	for _, value := range s.snapshot().Embeds {
		if value.State == "ready" {
			t.Fatal("expired body remained visible")
		}
	}
	if !s.embedEpochActive(epoch) {
		t.Fatal("active epoch lost")
	}
	s.invalidateChatEmbeds()
	if s.embedEpochActive(epoch) {
		t.Fatal("stale response could be accepted")
	}
}
