//go:build !(js && wasm)

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestIntegrate1SavedBackoff(t *testing.T) {
	now := time.Unix(1000, 0)
	var refresh chatsaveRefresh
	if !refresh.ready(now) {
		t.Fatal("initial read blocked")
	}
	for _, delay := range []time.Duration{2, 4, 8, 16, 32, 64, 64} {
		refresh.complete(now, errors.New("offline"))
		if refresh.ready(now.Add(delay*time.Second-time.Nanosecond)) || !refresh.ready(now.Add(delay*time.Second)) {
			t.Fatal("failure did not back off", delay)
		}
		now = now.Add(delay * time.Second)
	}
	refresh.complete(now, nil)
	if !refresh.ready(now) || refresh.failures != 0 {
		t.Fatal("successful refresh did not reset backoff")
	}
}

func TestIntegrate1AgentIconProjection(t *testing.T) {
	icon := agenticon.Generate(agenticon.Input{Name: "Policy Helper"})
	model := chatui.Model{Conversations: []chatui.Conversation{{ID: "dm", Kind: chatui.DirectMessage}}, Sections: []chatui.SidebarSection{{Chats: []chatui.Conversation{{ID: "dm", Kind: chatui.DirectMessage}}}}, Browse: []chatui.Conversation{{ID: "dm", Kind: chatui.DirectMessage}}, Members: []chatui.Member{{ID: "agent"}}}
	identity := agentDirectIdentity{Icon: icon, Revision: 3}
	if !applyAgentDirectConversation(&model, "dm", "agent", "Policy Helper", identity) {
		t.Fatal("no projection")
	}
	for _, projected := range []chatui.Conversation{model.Conversations[0], model.Sections[0].Chats[0], model.Browse[0]} {
		if projected.Icon != icon || projected.IconRevision != 3 || !projected.Agent {
			t.Fatal("lost agent identity", projected)
		}
	}
	if model.Members[0].Icon != icon || model.Members[0].IconRevision != 3 {
		t.Fatal("member lost icon")
	}
	if applyAgentDirectConversation(&model, "dm", "agent", "Policy Helper", identity) {
		t.Fatal("unchanged projection repainted")
	}
	identity.Revision = 4
	if !applyAgentDirectConversation(&model, "dm", "agent", "Policy Helper", identity) {
		t.Fatal("revision change ignored")
	}
}

func TestIntegrate1AgentIconClient(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/agent-controls/icons/preview" || r.URL.RawQuery != "" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer session" {
			t.Error("wrong icon boundary", r.URL, r.Header)
		}
		_, _ = w.Write([]byte(`{"revision":7}`))
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL + "/tunnel?old=1", Bearer: "session"}
	var out struct {
		Revision int64 `json:"revision"`
	}
	if err := integrate1IconRequest(t.Context(), server.Client(), cfg, "preview", map[string]any{"revision": 6}, &out); err != nil || out.Revision != 7 {
		t.Fatal(out, err)
	}
	if err := integrate1IconRequest(context.Background(), server.Client(), cfg, "delete", nil, &out); err == nil || calls != 1 {
		t.Fatal("unsupported action sent", err, calls)
	}
}

func TestIntegrate1ModerationPageClient(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/chat/moderation/page" || r.URL.Query().Get("action") != "remove" || r.Header.Get("Authorization") != "Bearer session" {
			t.Error("wrong admitted page request", r.URL)
		}
		w.Write([]byte(`<section class="chatremove">Review message</section>`))
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL + "/tunnel", Bearer: "session"}
	markup, err := chatremovePageRequest(t.Context(), server.Client(), cfg, "/api/chat/moderation/page?action=remove&post=one")
	if err != nil || markup != `<section class="chatremove">Review message</section>` {
		t.Fatal(markup, err)
	}
	for _, href := range []string{"https://outside.example/api/chat/moderation/page", "//outside.example/api/chat/moderation/page", "/api/chat/moderation/page-other"} {
		if _, err = chatremovePageRequest(t.Context(), server.Client(), cfg, href); err == nil {
			t.Fatal("untrusted page accepted", href)
		}
	}
	if calls != 1 {
		t.Fatal("untrusted origin was contacted", calls)
	}
}
