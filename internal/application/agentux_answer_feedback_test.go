package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func qualityFeedbackHandler(t *testing.T, surface *PersonaChatSurface) (http.Handler, string) {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(strings.Repeat("q", 32)), Issuer: "quality-test", Audience: "quality-chat", Now: surface.Now})
	if err != nil {
		t.Fatal(err)
	}
	now := surface.Now()
	token, err := verifier.Issue(trust.Claims{Issuer: "quality-test", Audience: "quality-chat", Tenant: "tenant-a", Subject: "user-a", SubjectKind: "human", AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "quality-test", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return OverlayPersonaChatSurface(http.NotFoundHandler(), surface, transport.Config{Verifier: verifier, Now: surface.Now}), token
}

func qualityFeedbackRequest(handler http.Handler, token, suffix, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/chat/personas/invocations/invocation-a/"+suffix, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestAgentUXQuality_Feedback_Integration(t *testing.T) {
	surface, _, invocation, _ := agentUXR5SrvActionSurface(t, runstate.StateCompleted)
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantID := pgstore.TenantID("tenant-a")
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenantID)
	pool := commonAgentOpenIntegrationStore(t, db)
	wiring := &personaServeWiring{refs: &lazyPersonaReferenceSource{}, avail: &TenantAvailablePersonaReader{}, skill: &AgentSkillSource{}, now: surface.Now}
	surface, err := wiring.chatSurface(qualityFeedbackChat{source: surface.Chat}, pool)
	if err != nil {
		t.Fatal(err)
	}
	repository, ok := surface.Invocations.(*agentinvocationstore.Store)
	if !ok {
		t.Fatal("served assembly erased the feedback-capable store")
	}
	invocation.PersonaVersion, invocation.InstallationID, invocation.Mode = "1", "installation-a", agentinvoke.OnBehalfOf
	invocation.Actor = agentinvoke.ActorChain{UserID: invocation.InvokerID, PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, InstallationID: invocation.InstallationID, ConversationID: invocation.ConversationID, InvokingPostID: invocation.PostID, InvocationID: invocation.ID}
	if _, created, err := repository.Claim(ctx, invocation); err != nil || !created {
		t.Fatalf("claim invocation: created=%t err=%v", created, err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	db.Exec(t, `INSERT INTO persona_final_outputs (tenant_id,invocation_id,output_id,invoker_id,conversation_id,thread_id,parent_post_id,persona_id,persona_version,installation_id,admission_digest,agent026_digest,persistence_digest,recovery_receipt,materials,citations,sealed_payload,created_at,admission_id,run_id) VALUES ($1,'invocation-a','output-a','user-a','channel-a','post-a','post-a','persona-a','1','installation-a',$2,$2,$2,'fixture-receipt','[]','[]','{}',now(),'run-a','run-a')`, tenantID, digest)
	if err := repository.RecordReplyReceipt(ctx, agentinvocationstore.ReplyReceipt{TenantID: "tenant-a", InvocationID: invocation.ID, RunID: "run-a", OutputID: "output-a", OutputDigest: digest, InvokerID: invocation.InvokerID, ConversationID: invocation.ConversationID, ThreadID: invocation.ThreadID, InvokingPostID: invocation.PostID, PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, AgentID: "agent-a", Display: "Policy Helper", InvokerHandle: "user-a", PublicPostID: "answer-a"}); err != nil {
		t.Fatal(err)
	}
	handler, token := qualityFeedbackHandler(t, surface)
	response := qualityFeedbackRequest(handler, token, "feedback", `{"helpful":true,"idempotency_key":"quality-feedback-1"}`)
	var result personachat.FeedbackResult
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.Active || !result.Helpful {
		t.Fatalf("served feedback: %d %s", response.Code, response.Body)
	}
	var active, helpful bool
	if err := db.SQL.QueryRowContext(ctx, `SELECT active,helpful FROM persona_answer_feedback WHERE tenant_id=$1 AND output_id='output-a' AND person_id='user-a'`, tenantID).Scan(&active, &helpful); err != nil || !active || !helpful {
		t.Fatalf("persisted rating: active=%t helpful=%t err=%v", active, helpful, err)
	}
	// Reconstruct the served handler before undo: state is durable, not a flag
	// retained by the first handler or browser.
	handler, token = qualityFeedbackHandler(t, surface)
	response = qualityFeedbackRequest(handler, token, "feedback/undo", `{"idempotency_key":"quality-feedback-undo"}`)
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Active {
		t.Fatalf("served undo: %d %s", response.Code, response.Body)
	}
	request := personaChatAdmissionRequestFixture()
	bindPersonaChatAdmissionRequest(&request, agentinvoke.RunRequest{InvocationID: invocation.ID, TenantID: invocation.TenantID, InvokerID: invocation.InvokerID, ConversationID: invocation.ConversationID, ThreadID: invocation.ThreadID, InvokingPostID: invocation.PostID, PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, InstallationID: invocation.InstallationID, Grant: agentinvoke.DelegationGrant{ID: "quality-grant"}})
	request.Deadline = surface.Now().Add(time.Minute)
	admissionStore, err := agentrunstore.NewAdmissionRepository(pool, tenantID, values.TenantId(invocation.TenantID))
	if err != nil {
		t.Fatal(err)
	}
	admissions, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: personaChatAdmissionAuthorityFake{}, Store: admissionStore, Now: surface.Now})
	if err != nil {
		t.Fatal(err)
	}
	admission, _, err := admissions.Admit(ctx, request)
	if err != nil || admission.Decision != agentrun.DecisionAccepted {
		t.Fatalf("stop admission: %+v %v", admission, err)
	}
	executionStore, err := surface.Executions(ctx, invocation.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	executions, err := runstate.New(executionStore, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = executions.Start(ctx, admission); err != nil {
		t.Fatal(err)
	}
	response = qualityFeedbackRequest(handler, token, "cancel", `{"idempotency_key":"quality-stop-1"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"CANCELLED"`) {
		t.Fatalf("served stop: %d %s", response.Code, response.Body)
	}
	stopped, err := executionStore.Get(ctx, admission.ID)
	if err != nil || !stopped.CancelRequested || stopped.State != runstate.StateCancelled {
		t.Fatalf("stop was not durable: %+v %v", stopped, err)
	}
}

func TestAgentUXQuality_Feedback_Fault(t *testing.T) {
	surface, _, _, _ := agentUXR5SrvActionSurface(t, runstate.StateCompleted)
	surface.Invocations = qualityFeedbackUnavailable{surface.Invocations.(*agentUXR5SrvInvocationActions)}
	handler, token := qualityFeedbackHandler(t, surface)
	response := qualityFeedbackRequest(handler, token, "feedback", `{"helpful":true,"idempotency_key":"quality-feedback-failure"}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "unavailable") || strings.Contains(response.Body.String(), `"active":true`) {
		t.Fatalf("refused rating looked saved: %d %s", response.Code, response.Body)
	}
}

type qualityFeedbackUnavailable struct{ *agentUXR5SrvInvocationActions }

func (qualityFeedbackUnavailable) SubmitAnswerFeedback(context.Context, string, string, string, bool, string, string, time.Time) (agentinvocationstore.AnswerFeedback, error) {
	return agentinvocationstore.AnswerFeedback{}, personachat.ErrUnavailable
}

type qualityFeedbackChat struct {
	chat.ConversationService
	source personaSurfaceChat
}

func (s qualityFeedbackChat) GetConversation(ctx context.Context, request chat.GetConversationRequest) (chat.Conversation, error) {
	return s.source.GetConversation(ctx, request)
}

func (s qualityFeedbackChat) ListMemberships(ctx context.Context, request chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	return s.source.ListMemberships(ctx, request)
}
