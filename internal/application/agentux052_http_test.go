package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

func agentux052Post(t *testing.T, handler http.Handler, ctx context.Context, action string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, ChatAgentReactionPath+action, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r.WithContext(ctx))
	return w.Code, w.Body.Bytes()
}

// The switches over HTTP, against the real store: the workspace page reads its
// switch, an administrator's writes land and are read back, and everyone else
// — or a call outside the tenant — is refused.
func TestAgentUX052_HTTPSwitches(t *testing.T) {
	room := newAgentUX075Chat(t)
	ctx := transport.WithInvocation(room.ctx, &transport.Invocation{})
	admin := AgentReactionHTTP{Service: AgentReactionService{Store: room.store, Admin: agentux052FilterAdmin{allow: map[string]bool{"": true, "general": true}}, Conversations: room.service}}
	outsider := AgentReactionHTTP{Service: AgentReactionService{Store: room.store, Admin: agentux052FilterAdmin{}, Conversations: room.service}}

	var settings AgentReactionPolicyView
	if code, raw := agentux052Post(t, admin, ctx, "settings", map[string]string{}); code != http.StatusOK {
		t.Fatalf("settings: %d %s", code, raw)
	} else if err := json.Unmarshal(raw, &settings); err != nil || !settings.Workspace || !settings.CanManageWorkspace {
		t.Fatalf("settings answer: %+v %v", settings, err)
	}
	if code, raw := agentux052Post(t, admin, ctx, "switch", AgentReactionSwitchRequest{Scope: AgentReactionScopeWorkspace, Enabled: false}); code != http.StatusOK {
		t.Fatalf("the workspace switch write: %d %s", code, raw)
	}
	if code, raw := agentux052Post(t, admin, ctx, "settings", map[string]string{}); code != http.StatusOK {
		t.Fatalf("settings reread: %d %s", code, raw)
	} else if err := json.Unmarshal(raw, &settings); err != nil || settings.Workspace {
		t.Fatalf("the workspace switch did not land: %+v %v", settings, err)
	}
	var policy AgentReactionPolicyView
	if code, raw := agentux052Post(t, admin, ctx, "policy", map[string]string{"TenantID": "tenant-a", "ConversationID": "general"}); code != http.StatusOK {
		t.Fatalf("policy: %d %s", code, raw)
	} else if err := json.Unmarshal(raw, &policy); err != nil || policy.Workspace || !policy.Channel || !policy.IsChannel || !policy.CanManageChannel {
		t.Fatalf("policy answer: %+v %v", policy, err)
	}
	// Everyone else is refused, and so is a call outside the tenant.
	if code, raw := agentux052Post(t, outsider, ctx, "switch", AgentReactionSwitchRequest{Scope: AgentReactionScopeWorkspace, Enabled: true}); code != http.StatusForbidden {
		t.Fatalf("an unprivileged write: %d %s", code, raw)
	}
	if code, raw := agentux052Post(t, admin, ctx, "switch", AgentReactionSwitchRequest{TenantID: "tenant-b", Scope: AgentReactionScopeWorkspace, Enabled: true}); code != http.StatusBadRequest {
		t.Fatalf("a cross-tenant write: %d %s", code, raw)
	}
	if code, raw := agentux052Post(t, admin, ctx, "policy", map[string]string{"TenantID": "tenant-a", "ConversationID": "no-such-room"}); code != http.StatusNotFound {
		t.Fatalf("a conversation the caller is not in: %d %s", code, raw)
	}
	if code, _ := agentux052Post(t, admin, ctx, "policy", map[string]string{"TenantID": "tenant-a", "ConversationID": "general", "Surprise": "no"}); code != http.StatusBadRequest {
		t.Fatalf("an unknown field: %d", code)
	}
	r := httptest.NewRequest(http.MethodGet, ChatAgentReactionPath+"settings", nil)
	w := httptest.NewRecorder()
	admin.ServeHTTP(w, r.WithContext(ctx))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", w.Code)
	}
	if code, _ := agentux052Post(t, admin, ctx, "no-such-action", map[string]string{}); code != http.StatusNotFound {
		t.Fatalf("an unknown action: %d", code)
	}
	w = httptest.NewRecorder()
	admin.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatAgentReactionPath+"settings", bytes.NewReader([]byte("{}"))))
	if w.Code != http.StatusForbidden {
		t.Fatalf("a call without a verified caller: %d", w.Code)
	}
}

// The commit-time chain over the real store: the persona owner's switch (proved
// in AGENTUX-075) and these rows compose — with the rows off, the agent's
// first and outcome reactions are refused and nothing is stored; back on, the
// outcome reaction takes the first one's place as before.
func TestAgentUX052_CommitHonorsStoredSwitches(t *testing.T) {
	room := newAgentUX075Chat(t)
	question := room.ask(t, room.channel, "agentux052-chain", "@Assistant list all the holidays")
	off := chatcore.AgentReactionPolicy{ReactionsEnabled: false}
	if err := room.store.WriteAgentReactionPolicy(room.ctx, "tenant-a", room.channel, off, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := room.react(room.channel, question.ID, "assistant", "🔎", ""); !errors.Is(err, chatcore.ErrAgentReactionsDisabled) {
		t.Fatalf("the first reaction with the switch off: %v", err)
	}
	if err := room.react(room.channel, question.ID, "assistant", chatcore.AgentOutcomeAnswered(), "🔎"); !errors.Is(err, chatcore.ErrAgentReactionsDisabled) {
		t.Fatalf("the outcome reaction with the switch off: %v", err)
	}
	if got := room.reactions(t, room.channel, question.ID); len(got) != 0 {
		t.Fatalf("a refused reaction left %v", got)
	}
	// The workspace row rules until the conversation has its own: off at the
	// workspace still refuses, and the conversation's on wins back.
	other := room.ask(t, room.direct, "agentux052-workspace", "a direct question")
	if err := room.store.WriteAgentReactionPolicy(room.ctx, "tenant-a", "", off, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := room.react(room.direct, other.ID, "assistant", "👀", ""); !errors.Is(err, chatcore.ErrAgentReactionsDisabled) {
		t.Fatalf("the direct reaction with the workspace off: %v", err)
	}
	if err := room.store.WriteAgentReactionPolicy(room.ctx, "tenant-a", room.direct, chatcore.AgentReactionPolicy{ReactionsEnabled: true}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := room.react(room.direct, other.ID, "assistant", "👀", ""); err != nil {
		t.Fatalf("the direct reaction with its own switch on: %v", err)
	}
	if err := room.react(room.direct, other.ID, "assistant", chatcore.AgentOutcomeAnswered(), "👀"); err != nil {
		t.Fatalf("the direct outcome reaction: %v", err)
	}
	if got := room.reactions(t, room.direct, other.ID); !got["assistant ✅"] || got["assistant 👀"] || len(got) != 1 {
		t.Fatalf("reactions after the outcome: %v", got)
	}
	// And the channel's own off still wins over a workspace turned back on.
	if err := room.store.WriteAgentReactionPolicy(room.ctx, "tenant-a", "", chatcore.AgentReactionPolicy{ReactionsEnabled: true}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := room.react(room.channel, question.ID, "assistant", "🔎", ""); !errors.Is(err, chatcore.ErrAgentReactionsDisabled) {
		t.Fatalf("the channel reaction with only its own switch off: %v", err)
	}
}
