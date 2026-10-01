package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_AGENTP_019_ClientBoundary(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "viewer", Bearer: "test-token"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.URL.Query().Get("conversation_id") != "room & one" {
			t.Error("missing authenticated conversation request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"personas":[{"reference":{"kind":"AGENT_MENTION","tenant_id":"tenant","id":"policy","display":"Policy Helper","conversation_id":"room & one"},"skills":[{"name":"Search policy","tier":"T0"}],"reply_placement":"thread"}]}`))
	}))
	defer server.Close()
	cfg.TunnelURL = strings.Replace(server.URL, "http:", "ws:", 1) + "/tunnel"
	var payload struct {
		Personas []personaChatProfile `json:"personas"`
	}
	if err := personaChatRequest(context.Background(), server.Client(), cfg, http.MethodGet, "/api/chat/personas", "room & one", &payload); err != nil {
		t.Fatal(err)
	}
	profiles := personaChatProfiles(payload.Personas, cfg, "room & one")
	if len(profiles) != 1 || len(profiles[0].Skills) != 1 || profiles[0].Reference.ID != "policy" {
		t.Fatalf("profile mapping: %+v", profiles)
	}
	if len(personaChatProfiles(payload.Personas, cfg, "other room")) != 0 {
		t.Fatal("stale conversation profile retained")
	}
	payload.Personas[0].Reference.TenantID = "other tenant"
	if len(personaChatProfiles(payload.Personas, cfg, "room & one")) != 0 {
		t.Fatal("foreign tenant profile retained")
	}
}

func TestTodo_AGENTP_019_CanonicalSend(t *testing.T) {
	refs := []chatui.ChatReference{{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "agent", Display: "Policy Helper", ConversationID: "room"}}
	canonical, err := personaChatReferences(refs, "tenant", "room")
	if err != nil || len(canonical) != 1 || canonical[0].GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION || canonical[0].GetId() != "agent" {
		t.Fatalf("canonical send: %+v %v", canonical, err)
	}
	if _, err := personaChatReferences(refs, "tenant", "other"); !errors.Is(err, errPersonaChat) {
		t.Fatal("cross-room send accepted")
	}
	post := &chatv1.Post{References: canonical}
	if mapped := personaChatPostReferences(post); len(mapped) != 1 || mapped[0].ID != "agent" {
		t.Fatalf("post profile references: %+v", mapped)
	}
	first := personaChatSendIdentity("@Policy Helper search", refs)
	refs[0].ID = "other-agent"
	if first == personaChatSendIdentity("@Policy Helper search", refs) {
		t.Fatal("different canonical persona reused send identity")
	}
}

func TestTodo_AGENTP_020_ClientProjection(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "viewer"}
	invocation := personaChatInvocation{InvocationID: "invoke", ConversationID: "room", PostID: "post", ThreadID: "root", InvokerID: "viewer", Status: "running", Activity: "reading 3 sources", CurrentStep: 2, TotalSteps: 5, TaskID: "task", TaskTitle: "Review policy", TaskState: "awaiting_approval", TaskRevision: 3}
	rows := personaChatInvocations([]personaChatInvocation{invocation}, cfg, "room")
	if len(rows) != 1 || rows[0].PostID != "root" || rows[0].Projection.Progress.CurrentStep != 2 || !rows[0].Projection.Task.AwaitingApproval || rows[0].Projection.Task.Revision != "3" {
		t.Fatalf("progress mapping: %+v", rows)
	}
	invocation.Status, invocation.FailureCode, invocation.FailureMessage, invocation.Retryable = "failed", "MODEL_UNAVAILABLE", "Provider unavailable", true
	rows = personaChatInvocations([]personaChatInvocation{invocation}, cfg, "room")
	if rows[0].Projection.Progress != nil || rows[0].Projection.Failure.Code != "MODEL_UNAVAILABLE" || !rows[0].Projection.Failure.Retryable {
		t.Fatalf("failure mapping: %+v", rows[0])
	}
	invocation.Status, invocation.FailureCode, invocation.Retryable = "NEEDS_REPAIR", "DELIVERY_FAILED", false
	rows = personaChatInvocations([]personaChatInvocation{invocation}, cfg, "room")
	if rows[0].Projection.Progress != nil || rows[0].Projection.Failure.Code != "DELIVERY_FAILED" || rows[0].Projection.Failure.Retryable {
		t.Fatalf("repair state presented as working: %+v", rows[0])
	}
	invocation.Status = "COMPLETED"
	invocation.PrivateConversationID, invocation.PrivatePostID = "private-room", "private-post"
	if href := personaChatInvocations([]personaChatInvocation{invocation}, cfg, "room")[0].Projection.PrivateReplyHref; href != chatui.ChannelReferenceURL("private-room") {
		t.Fatalf("private result link=%q", href)
	}
	if personaChatInvocations([]personaChatInvocation{invocation}, cfg, "room")[0].Projection.Progress != nil {
		t.Fatal("terminal spinner retained")
	}
	invocation.InvokerID = "other"
	if len(personaChatInvocations([]personaChatInvocation{invocation}, cfg, "room")) != 0 {
		t.Fatal("another invoker's status exposed")
	}
}

func TestTodo_AGENTP_019_ActorReceiptProjection(t *testing.T) {
	actors := personaChatActorProjection([]personaChatPostActor{{PostID: "reply", PersonaID: "persona", AgentID: "registered-agent", Display: "Policy Helper", InvokerHandle: "walt"}, {PostID: "invalid", Display: "lookalike"}})
	if len(actors) != 1 || !actors["reply"].Actor.Trusted || actors["reply"].Actor.AgentID != "registered-agent" {
		t.Fatalf("receipt projection: %+v", actors)
	}
}
