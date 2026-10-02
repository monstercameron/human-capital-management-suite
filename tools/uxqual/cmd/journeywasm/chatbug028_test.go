package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATBUG_028 is the client half of the composer fixes: an agent
// directory that answers an empty list, or refuses the person for this
// conversation, is "no agents here" and not a failed lookup; and a draft
// belongs to the conversation it was typed in.
func TestTodo_CHATBUG_028(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "walt", Bearer: "token"}
	serve := func(status int, body string) (*httptest.Server, journeyclient.Config) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		c := cfg
		c.TunnelURL = server.URL
		return server, c
	}
	lookup := func(status int, body string) *chatui.Model {
		server, c := serve(status, body)
		defer server.Close()
		var payload personaChatDirectory
		err := personaChatRequest(context.Background(), server.Client(), c, http.MethodGet, "/api/chat/personas", "random", &payload)
		model := &chatui.Model{SelectedID: "random", PersonaLookup: chatui.PersonaLookupLoading}
		applyPersonaDirectoryResult(model, payload, cfg, "random", err, func() {})
		return model
	}

	// An empty directory is ready, with no agents and no error.
	if model := lookup(http.StatusOK, `{"personas":[],"post_actors":[]}`); model.PersonaLookup != chatui.PersonaLookupReady || len(model.ResolvedPersonaMentions) != 0 {
		t.Fatalf("an empty directory left the lookup %q with %d agents, want ready and none", model.PersonaLookup, len(model.ResolvedPersonaMentions))
	}
	// A refusal for this conversation is the same quiet answer.
	if model := lookup(http.StatusForbidden, `{"error":"denied"}`); model.PersonaLookup != chatui.PersonaLookupReady || len(model.ResolvedPersonaMentions) != 0 {
		t.Fatalf("a denied directory left the lookup %q, want ready with no agents", model.PersonaLookup)
	}
	// Every other failure is still a failure the person can retry.
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusUnauthorized, http.StatusInternalServerError} {
		if model := lookup(status, `{"error":"unavailable"}`); model.PersonaLookup != chatui.PersonaLookupFailed {
			t.Fatalf("status %d left the lookup %q, want failed", status, model.PersonaLookup)
		}
	}
	// The watch stream tells a refusal apart from a signed-out session.
	if !personaWatchDenied(&http.Response{StatusCode: http.StatusForbidden}) || personaWatchDenied(&http.Response{StatusCode: http.StatusUnauthorized}) || personaWatchDenied(nil) {
		t.Fatal("the watch stream does not tell a refusal from a signed-out session")
	}
	if !personaWatchTerminal(&http.Response{StatusCode: http.StatusForbidden}, nil) {
		t.Fatal("a refused watch must still end the watch loop")
	}

	// A draft belongs to the conversation it was typed in.
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "random" })
	state.setDraft("random", "/")
	if model, _ := state.selectChatConversation("general"); model.Draft != "" {
		t.Fatalf("composer in #general = %q after \"/\" was typed in #random, want empty", model.Draft)
	}
	if got := state.draft("general"); got != "" {
		t.Fatalf("stored draft of #general = %q, want none", got)
	}
	if model, _ := state.selectChatConversation("announcements"); model.Draft != "" {
		t.Fatalf("composer in #announcements = %q, want empty", model.Draft)
	}
	drafts, _, _ := state.draftsForWrite()
	if len(drafts) != 1 || drafts["random"] != "/" {
		t.Fatalf("persisted drafts = %+v, want only \"/\" under #random", drafts)
	}
	// A persisted copy for one room is never offered in another.
	state.loadDrafts(map[string]string{"random": "/"})
	if model := state.snapshot(); model.Draft != "" || state.draft("general") != "" {
		t.Fatalf("loading the persisted draft of #random offered it in #announcements: %q", model.Draft)
	}
	if model, _ := state.selectChatConversation("random"); model.Draft != "/" {
		t.Fatalf("composer back in #random = %q, want the \"/\" typed there", model.Draft)
	}
	if strings.TrimSpace(state.draft("random")) == "" {
		t.Fatal("the draft of #random was lost on the round trip")
	}
}
