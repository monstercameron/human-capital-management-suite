package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestTodo_AGENTUX_007_ChatStatePreservesTypedMentionOnProjectionAdoption(t *testing.T) {
	state := newChatStateForTest(t)
	retry := func() {}
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "room"
		model.PersonaLookup = chatui.PersonaLookupLoading
		model.PersonaLookupConversationID = "room"
		model.Callbacks.RetryPersonaMentions = retry
	})
	state.setDraft("room", "@pol")
	loaded := state.snapshot()
	loaded.Draft = ""
	loaded.PersonaLookup = chatui.PersonaLookupIdle
	loaded.PersonaLookupConversationID = ""
	loaded.Callbacks.RetryPersonaMentions = nil
	if !state.adoptLoadedChatProjection(state.currentGeneration(), loaded, chatCursor{ConversationID: "room"}, false) {
		t.Fatal("current projection was rejected")
	}
	got := state.snapshot()
	if got.Draft != "@pol" || got.PersonaLookup != chatui.PersonaLookupLoading || got.PersonaLookupConversationID != "room" || got.Callbacks.RetryPersonaMentions == nil {
		t.Fatalf("projection adoption clobbered mention lookup state: draft=%q lookup=%q room=%q retry=%v", got.Draft, got.PersonaLookup, got.PersonaLookupConversationID, got.Callbacks.RetryPersonaMentions != nil)
	}
}

func TestTodo_AGENTUX_007_ChatStateSwitchClearsPersonaLookup(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "room-a"
		model.Draft = "@pol"
		model.PersonaLookup = chatui.PersonaLookupReady
		model.PersonaLookupConversationID = "room-a"
		model.Callbacks.RetryPersonaMentions = func() {}
	})
	state.setDraft("room-a", "@pol")
	got, _ := state.selectChatConversation("room-b")
	if got.PersonaLookup != chatui.PersonaLookupIdle || got.PersonaLookupConversationID != "" || got.Callbacks.RetryPersonaMentions != nil {
		t.Fatalf("conversation switch retained lookup state: lookup=%q room=%q retry=%v", got.PersonaLookup, got.PersonaLookupConversationID, got.Callbacks.RetryPersonaMentions != nil)
	}
	if got.Draft != "" {
		t.Fatalf("new conversation inherited mention text %q", got.Draft)
	}
	back, _ := state.selectChatConversation("room-a")
	if back.Draft != "@pol" {
		t.Fatalf("original conversation lost its draft: %q", back.Draft)
	}
}
