package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestPersonaChatRejectedFetchIsFalseBeforeStatusAccess(t *testing.T) {
	if personaChatResponseOK(nil, errors.New("fetch rejected")) {
		t.Fatal("rejected fetch was treated as a usable response")
	}
	if personaChatResponseOK(nil, nil) {
		t.Fatal("falsey fetch result was treated as a usable response")
	}
	if !personaChatResponseOK(&http.Response{StatusCode: http.StatusOK}, nil) {
		t.Fatal("successful fetch was rejected")
	}
}

func TestTodo_AGENTUX_006_Fault(t *testing.T) {
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer failed.Close()
	never := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer never.Close()
	refused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	refusedURL := refused.URL
	refused.Close()

	failedConfig := journeyclient.Config{TunnelURL: strings.Replace(failed.URL, "http:", "ws:", 1), Tenant: "tenant"}
	neverConfig := journeyclient.Config{TunnelURL: strings.Replace(never.URL, "http:", "ws:", 1), Tenant: "tenant"}
	refusedConfig := journeyclient.Config{TunnelURL: strings.Replace(refusedURL, "http:", "ws:", 1), Tenant: "tenant"}

	for _, tc := range []struct {
		name    string
		cfg     journeyclient.Config
		timeout time.Duration
	}{
		{name: "refused", cfg: refusedConfig, timeout: time.Second},
		{name: "failed", cfg: failedConfig, timeout: time.Second},
		{name: "never answered", cfg: neverConfig, timeout: 40 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			var payload personaChatDirectory
			err := personaChatRequest(ctx, http.DefaultClient, tc.cfg, http.MethodGet, "/personas", "room", &payload)
			if err == nil {
				t.Fatal("faulting lookup unexpectedly succeeded")
			}
			model := chatui.Model{}
			retried := false
			applyPersonaDirectoryResult(&model, payload, tc.cfg, "room", err, func() { retried = true })
			if model.PersonaLookup != chatui.PersonaLookupFailed || model.Callbacks.RetryPersonaMentions == nil {
				t.Fatalf("lookup state = %q, retry=%v", model.PersonaLookup, model.Callbacks.RetryPersonaMentions != nil)
			}
			model.Callbacks.RetryPersonaMentions()
			if !retried {
				t.Fatal("retry callback was not retained")
			}
		})
	}
}

func TestPersonaChatWatchFailurePolicy(t *testing.T) {
	refused := &url.Error{Op: "Get", URL: "http://127.0.0.1", Err: syscall.ECONNREFUSED}
	if !personaWatchTerminal(nil, refused) || !personaWatchTerminal(nil, errors.New("net::ERR_CONNECTION_REFUSED")) || !personaWatchTerminal(&http.Response{StatusCode: http.StatusUnauthorized}, nil) || !personaWatchTerminal(&http.Response{StatusCode: http.StatusForbidden}, nil) {
		t.Fatal("refused watch was not terminal")
	}
	if personaWatchTerminal(nil, context.DeadlineExceeded) || personaWatchTerminal(&http.Response{StatusCode: http.StatusServiceUnavailable}, nil) {
		t.Fatal("transient watch failure was classified as terminal")
	}
	want := []time.Duration{3 * time.Second, 6 * time.Second, 12 * time.Second, 24 * time.Second, 48 * time.Second, 60 * time.Second, 60 * time.Second}
	for attempt, expected := range want {
		if got := personaWatchBackoff(attempt); got != expected {
			t.Fatalf("attempt %d backoff = %s, want %s", attempt, got, expected)
		}
	}
}

// TestTodo_CHATBUG_071_MemberMention: a person picked from the member list has
// no home tenant of their own, and the send must not be dropped for it.
func TestTodo_CHATBUG_071_MemberMention(t *testing.T) {
	refs := []chatui.ChatReference{{Kind: "PERSON_MENTION", ID: "loretta", Display: "Loretta Haynes", ConversationID: "room"}}
	canonical, err := personaChatReferences(refs, "tenant", "room")
	if err != nil || len(canonical) != 1 || canonical[0].GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION || canonical[0].GetTenantId() != "tenant" || canonical[0].GetId() != "loretta" {
		t.Fatalf("member mention: %+v %v", canonical, err)
	}
	if refs[0].TenantID != "" {
		t.Fatal("the caller's reference was changed")
	}
	if _, err := personaChatReferences([]chatui.ChatReference{{Kind: "AGENT_MENTION", ID: "agent", ConversationID: "room"}}, "tenant", "room"); !errors.Is(err, errPersonaChat) {
		t.Fatal("an agent mention with no tenant was accepted")
	}
	if _, err := personaChatReferences([]chatui.ChatReference{{Kind: "PERSON_MENTION", TenantID: "other", ID: "x", ConversationID: "room"}}, "tenant", "room"); !errors.Is(err, errPersonaChat) {
		t.Fatal("a person from another tenant was accepted")
	}
}

func TestTodo_AGENTP_019_CanonicalSend(t *testing.T) {
	refs := []chatui.ChatReference{{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "agent", Display: "Policy Helper", ConversationID: "room"}, {Kind: "PERSON_MENTION", TenantID: "tenant", ID: "person", Display: "Pat Lee", ConversationID: "room"}}
	canonical, err := personaChatReferences(refs, "tenant", "room")
	if err != nil || len(canonical) != 2 || canonical[0].GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION || canonical[0].GetId() != "agent" || canonical[1].GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION {
		t.Fatalf("canonical send: %+v %v", canonical, err)
	}
	if _, err := personaChatReferences(refs, "tenant", "other"); !errors.Is(err, errPersonaChat) {
		t.Fatal("cross-room send accepted")
	}
	post := &chatv1.Post{References: canonical}
	if mapped := personaChatPostReferences(post); len(mapped) != 2 || mapped[0].ID != "agent" || mapped[1].ID != "person" {
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
	if len(rows) != 1 || rows[0].PostID != "post" || rows[0].ThreadID != "root" || rows[0].Projection.Progress.CurrentStep != 2 || !rows[0].Projection.Task.AwaitingApproval || rows[0].Projection.Task.Revision != "3" {
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
	privateProjection := personaChatInvocations([]personaChatInvocation{invocation}, cfg, "room")[0].Projection
	if href := privateProjection.PrivateReplyHref; href != chatui.ChannelReferenceURL("private-room") {
		t.Fatalf("private result link=%q", href)
	}
	if privateProjection.DurablePostID != "private-post" {
		t.Fatalf("private durable post id=%q", privateProjection.DurablePostID)
	}
	directProjection := personaChatInvocations([]personaChatInvocation{invocation}, cfg, "private-room")
	if len(directProjection) != 1 || directProjection[0].Projection.DurablePostID != "private-post" || directProjection[0].Projection.PrivateReplyHref != "" {
		t.Fatalf("direct agent-conversation projection=%+v", directProjection)
	}
	if !privateProjection.AnswerStored || privateProjection.Progress != nil {
		t.Fatalf("private completion must keep its place as a stored answer, not as work in progress: %+v", privateProjection)
	}
	invocation.PrivateConversationID, invocation.PrivatePostID = "", ""
	if personaChatInvocations([]personaChatInvocation{invocation}, cfg, "room")[0].Projection.Progress != nil {
		t.Fatal("public completion retained a duplicate progress row")
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
