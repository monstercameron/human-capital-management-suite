package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentUXR5SrvInvocationActions struct {
	invocation agentinvoke.Invocation
	feedback   agentinvocationstore.AnswerFeedback
	err        error
	submits    int
	undos      int
}

func (s *agentUXR5SrvInvocationActions) ListPersonaInvocations(context.Context, string, string, string) ([]agentinvoke.Invocation, error) {
	return []agentinvoke.Invocation{s.invocation}, s.err
}
func (s *agentUXR5SrvInvocationActions) Lookup(_ context.Context, tenant, owner, id string) (agentinvoke.Invocation, error) {
	if s.err != nil {
		return agentinvoke.Invocation{}, s.err
	}
	if s.invocation.TenantID != tenant || s.invocation.InvokerID != owner || s.invocation.ID != id {
		return agentinvoke.Invocation{}, agentinvocationstore.ErrNotFound
	}
	return s.invocation, nil
}
func (s *agentUXR5SrvInvocationActions) SubmitAnswerFeedback(_ context.Context, tenant, person, invocation string, helpful bool, reason, _ string, at time.Time) (agentinvocationstore.AnswerFeedback, error) {
	s.submits++
	if tenant != s.invocation.TenantID || person != s.invocation.InvokerID || invocation != s.invocation.ID {
		return agentinvocationstore.AnswerFeedback{}, agentinvocationstore.ErrNotFound
	}
	s.feedback = agentinvocationstore.AnswerFeedback{InvocationID: invocation, OutputID: "answer-a", PersonID: person, Helpful: helpful, Reason: reason, Active: true, Revision: uint64(s.submits), UpdatedAt: at}
	return s.feedback, nil
}
func (s *agentUXR5SrvInvocationActions) UndoAnswerFeedback(_ context.Context, tenant, person, invocation, _ string, at time.Time) (agentinvocationstore.AnswerFeedback, error) {
	s.undos++
	if tenant != s.invocation.TenantID || person != s.invocation.InvokerID || invocation != s.invocation.ID {
		return agentinvocationstore.AnswerFeedback{}, agentinvocationstore.ErrNotFound
	}
	s.feedback = agentinvocationstore.AnswerFeedback{InvocationID: invocation, OutputID: "answer-a", PersonID: person, Active: false, Revision: uint64(s.submits + s.undos), UpdatedAt: at}
	return s.feedback, nil
}

func TestAgentUXR5Srv_Cancel(t *testing.T) {
	surface, ctx, invocation, store := agentUXR5SrvActionSurface(t, runstate.StateRunning)
	result, err := surface.Cancel(ctx, invocation.ID, "cancel-click-1")
	if err != nil || result.InvocationID != invocation.ID || result.State != string(runstate.StateCancelled) {
		t.Fatalf("cancel=%+v err=%v", result, err)
	}
	replayed, err := surface.Cancel(ctx, invocation.ID, "cancel-click-1")
	if err != nil || replayed.State != string(runstate.StateCancelled) {
		t.Fatalf("cancel replay=%+v err=%v", replayed, err)
	}
	runID, _ := personaSurfaceRunID(invocation)
	stored, err := store.Get(ctx, runID)
	if err != nil || !stored.CancelRequested || stored.State != runstate.StateCancelled || stored.Version != 2 {
		t.Fatalf("stored cancellation=%+v err=%v", stored, err)
	}
}

func TestAgentUXR5Srv_Cancel_Security(t *testing.T) {
	surface, ctx, invocation, _ := agentUXR5SrvActionSurface(t, runstate.StateRunning)
	foreign := *surface
	foreign.Invocations = &agentUXR5SrvInvocationActions{invocation: agentinvoke.Invocation{ID: invocation.ID, TenantID: invocation.TenantID, InvokerID: "bob", ConversationID: invocation.ConversationID}}
	if _, err := foreign.Cancel(ctx, invocation.ID, "cancel-click-2"); !errors.Is(err, personachat.ErrDenied) {
		t.Fatalf("another asker cancel error=%v", err)
	}

	otherTenant := *surface
	otherTenant.Invocations = &agentUXR5SrvInvocationActions{invocation: agentinvoke.Invocation{ID: invocation.ID, TenantID: "tenant-b", InvokerID: "user-a", ConversationID: invocation.ConversationID}}
	if _, err := otherTenant.Cancel(ctx, invocation.ID, "cancel-click-3"); !errors.Is(err, personachat.ErrDenied) {
		t.Fatalf("other tenant cancel error=%v", err)
	}

	finished, finishedCtx, _, _ := agentUXR5SrvActionSurface(t, runstate.StateCompleted)
	if _, err := finished.Cancel(finishedCtx, invocation.ID, "cancel-click-4"); !errors.Is(err, personachat.ErrConflict) {
		t.Fatalf("finished cancel error=%v", err)
	} else if final, ok := err.(*personachat.FinalStateConflict); !ok || final.State != string(runstate.StateCompleted) {
		t.Fatalf("finished conflict=%T %+v", err, err)
	}
}

func TestAgentUXR5Srv_Feedback(t *testing.T) {
	surface, ctx, invocation, _ := agentUXR5SrvActionSurface(t, runstate.StateCompleted)
	store := surface.Invocations.(*agentUXR5SrvInvocationActions)
	result, err := surface.SubmitFeedback(ctx, invocation.ID, false, "The cited policy is outdated.", "feedback-click-1")
	if err != nil || !result.Active || result.Helpful || result.Reason == "" || store.submits != 1 {
		t.Fatalf("submit=%+v err=%v store=%+v", result, err, store)
	}
	undone, err := surface.UndoFeedback(ctx, invocation.ID, "feedback-undo-1")
	if err != nil || undone.Active || store.undos != 1 {
		t.Fatalf("undo=%+v err=%v store=%+v", undone, err, store)
	}
}

func agentUXR5SrvActionSurface(t *testing.T, state runstate.State) (*PersonaChatSurface, context.Context, agentinvoke.Invocation, *runstate.MemoryStore) {
	t.Helper()
	now := time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "r5-server", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-r5-server",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	invocation := agentinvoke.Invocation{ID: "invocation-a", TenantID: "tenant-a", InvokerID: "user-a", ConversationID: "channel-a", ThreadID: "post-a", PostID: "post-a", PersonaID: "persona-a"}
	runID, err := personaSurfaceRunID(invocation)
	if err != nil {
		t.Fatal(err)
	}
	store := runstate.NewMemoryStore()
	if err := store.Create(ctx, runstate.Run{ID: runID, AdmissionID: runID, TenantID: invocation.TenantID, State: state, Version: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	joined := now.Add(-time.Hour)
	chatStore := &personaSurfaceChatFixture{room: chat.Conversation{ID: invocation.ConversationID, TenantID: invocation.TenantID, Kind: chat.PublicChannel}, members: []chat.Membership{{TenantID: invocation.TenantID, HomeTenantID: invocation.TenantID, SubjectID: invocation.InvokerID, ConversationID: invocation.ConversationID, JoinedAt: &joined}}}
	actions := &agentUXR5SrvInvocationActions{invocation: invocation}
	surface := &PersonaChatSurface{Chat: chatStore, Invocations: actions, Executions: func(context.Context, string) (runstate.Store, error) { return store, nil }, Now: func() time.Time { return now }}
	return surface, ctx, invocation, store
}
