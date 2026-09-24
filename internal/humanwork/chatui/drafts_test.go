package chatui

import (
	"strings"
	"testing"
)

type memoryDraftPersistence struct {
	owner  string
	drafts map[string]string
}

func (s *memoryDraftPersistence) load(owner string) map[string]string {
	if owner != s.owner {
		return nil
	}
	out := map[string]string{}
	for id, value := range s.drafts {
		out[id] = value
	}
	return out
}

func (s *memoryDraftPersistence) save(owner string, drafts map[string]string) {
	s.owner = owner
	s.drafts = map[string]string{}
	for id, value := range drafts {
		s.drafts[id] = value
	}
}

func (s *memoryDraftPersistence) clear() { s.owner, s.drafts = "", nil }

func TestTodo_CHAT_035(t *testing.T) {
	drafts := &browserDrafts{}
	model := Model{State: StateReady, SelectedID: "room-a", CurrentTenantID: "tenant-a", CurrentUser: "user-a", Draft: "draft for a"}
	drafts.prepare(&model)
	drafts.set(model.SelectedID, model.Draft)

	if got := drafts.selectConversation("room-a", model.Draft, "room-b"); got != "" {
		t.Fatalf("new conversation inherited draft %q", got)
	}
	drafts.set("room-b", "draft for b")
	model.SelectedID, model.Draft = "room-a", "stale room-b value"
	drafts.prepare(&model)
	if model.Draft != "draft for a" {
		t.Fatalf("restored draft = %q, want room-a draft", model.Draft)
	}
	model.SelectedID = "room-b"
	drafts.prepare(&model)
	if model.Draft != "draft for b" {
		t.Fatalf("restored draft = %q, want room-b draft", model.Draft)
	}
}

func TestTodo_CHAT_035_Browser(t *testing.T) {
	storage := &memoryDraftPersistence{}
	drafts := &browserDrafts{storage: storage}
	model := Model{State: StateReady, SelectedID: "room-a", CurrentTenantID: "tenant-a", CurrentUser: "user-a", Draft: "private draft"}
	drafts.prepare(&model)
	model.SelectedID, model.Draft = "room-b", "private draft"
	drafts.prepare(&model)
	if model.Draft != "" {
		t.Fatalf("browser composer carried another conversation's draft: %q", model.Draft)
	}
	markup := render(t, model)
	if strings.Contains(markup, "private draft") {
		t.Fatal("browser render exposed a draft from another conversation")
	}
	model.SelectedID = "room-a"
	drafts.prepare(&model)
	if model.Draft != "private draft" {
		t.Fatalf("browser composer did not restore its own conversation draft: %q", model.Draft)
	}
	// A new chat page instance gets the live session's sidebar projection.
	model.Preferences.Drafts = copyMap(drafts.values)
	reloaded := &browserDrafts{}
	reloadModel := Model{State: StateReady, SelectedID: "room-a", CurrentTenantID: "tenant-a", CurrentUser: "user-a", Preferences: model.Preferences}
	reloaded.prepare(&reloadModel)
	if reloadModel.Draft != "private draft" {
		t.Fatalf("navigation restored %q, want the room's session draft", reloadModel.Draft)
	}
	reloadModel.SelectedID = "room-b"
	reloaded.prepare(&reloadModel)
	if reloadModel.Draft != "" {
		t.Fatalf("reload exposed another conversation's draft: %q", reloadModel.Draft)
	}
}

func TestTodo_CHAT_035_EmptyConversationKeepsDraft(t *testing.T) {
	storage := &memoryDraftPersistence{}
	drafts := &browserDrafts{storage: storage}
	model := Model{State: StateEmpty, SelectedID: "room-empty", CurrentTenantID: "tenant-a", CurrentUser: "user-a"}
	drafts.prepare(&model)
	drafts.set(model.SelectedID, "draft before first post")
	model.Draft = "draft before first post"
	drafts.prepare(&model)
	if model.Draft != "draft before first post" || storage.drafts["room-empty"] != model.Draft {
		t.Fatalf("empty conversation lost its draft: model=%q storage=%v", model.Draft, storage.drafts)
	}
}

func TestTodo_CHAT_035_Security(t *testing.T) {
	storage := &memoryDraftPersistence{}
	drafts := &browserDrafts{storage: storage}
	model := Model{State: StateReady, SelectedID: "room-a", CurrentTenantID: "tenant-a", CurrentUser: "user-a", Draft: "sensitive"}
	drafts.prepare(&model)
	drafts.set(model.SelectedID, model.Draft)
	model.CurrentUser = "user-b"
	model.Draft = "sensitive"
	drafts.prepare(&model)
	if model.Draft != "" || len(drafts.values) != 0 || len(storage.drafts) != 0 {
		t.Fatal("identity change retained the prior user's draft")
	}

	model.Draft = "new sensitive"
	drafts.set(model.SelectedID, model.Draft)
	model.State = StateError
	drafts.prepare(&model)
	model.State, model.Draft = StateReady, "new sensitive"
	drafts.prepare(&model)
	if model.Draft != "" || len(drafts.values) != 0 || len(storage.drafts) != 0 {
		t.Fatal("chat access failure restored a discarded draft")
	}

	cleared := map[string]string{}
	model = Model{SelectedID: "room-a", Draft: "sensitive", Preferences: Preferences{Drafts: map[string]string{"room-a": "sensitive", "room-b": "other"}}, Callbacks: Callbacks{DraftChanged: func(id, value string) { cleared[id] = value }}}
	model.ClearDrafts()
	if model.Draft != "" || len(model.Preferences.Drafts) != 0 || cleared["room-a"] != "" || cleared["room-b"] != "" {
		t.Fatal("explicit draft clear did not discard all per-conversation state")
	}
	model.Send()
	if len(cleared) != 2 {
		t.Fatal("cleared draft became eligible for send")
	}
}

func TestTodo_CHAT_035_Security_NoSendAcrossIdentityChange(t *testing.T) {
	drafts := &browserDrafts{}
	model := Model{State: StateReady, SelectedID: "room-a", CurrentTenantID: "tenant-a", CurrentUser: "user-a"}
	drafts.prepare(&model)
	if !drafts.canSend(model) {
		t.Fatal("current authenticated identity could not use its composer")
	}
	model.CurrentUser = "user-b"
	if drafts.canSend(model) {
		t.Fatal("stale composer remained sendable after the principal changed")
	}
}

func TestTodo_CHAT_035_Security_ConversationRevocationKeepsOtherDrafts(t *testing.T) {
	storage := &memoryDraftPersistence{}
	drafts := &browserDrafts{storage: storage}
	cleared := map[string]string{}
	model := Model{
		State: StateError, SelectedID: "room-a", CurrentTenantID: "tenant-a", CurrentUser: "user-a",
		Draft: "room a draft", RevokedConversationID: "room-a",
		Preferences: Preferences{Drafts: map[string]string{"room-a": "room a draft", "room-b": "room b draft"}},
		Callbacks:   Callbacks{DraftChanged: func(id, value string) { cleared[id] = value }},
	}
	drafts.prepare(&model)
	if model.Draft != "" || model.Preferences.Drafts["room-a"] != "" || model.Preferences.Drafts["room-b"] != "room b draft" {
		t.Fatalf("conversation revocation draft state = %+v", model.Preferences.Drafts)
	}
	if _, ok := storage.drafts["room-a"]; ok || storage.drafts["room-b"] != "room b draft" {
		t.Fatalf("browser cache after room revocation = %+v", storage.drafts)
	}
	if pending := drafts.takeClearModel(); pending == nil {
		t.Fatal("revoked room draft did not schedule a durable tombstone")
	} else {
		pending.ClearDrafts()
	}
	if len(cleared) != 1 || cleared["room-a"] != "" {
		t.Fatalf("room-scoped clear callback included unrelated drafts: %+v", cleared)
	}
}
