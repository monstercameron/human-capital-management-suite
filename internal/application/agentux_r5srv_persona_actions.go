package application

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

type personaAnswerFeedbackStore interface {
	SubmitAnswerFeedback(context.Context, string, string, string, bool, string, string, time.Time) (agentinvocationstore.AnswerFeedback, error)
	UndoAnswerFeedback(context.Context, string, string, string, string, time.Time) (agentinvocationstore.AnswerFeedback, error)
}

type personaCancelRechecker struct{}

func (personaCancelRechecker) Recheck(context.Context, string, string) error { return nil }

func (s *PersonaChatSurface) Cancel(ctx context.Context, invocationID, idempotencyKey string) (personachat.CancelResult, error) {
	if s == nil || s.Invocations == nil || s.Executions == nil {
		return personachat.CancelResult{}, personachat.ErrUnavailable
	}
	if !validPersonaActionInput(invocationID, idempotencyKey) {
		return personachat.CancelResult{}, personachat.ErrInvalid
	}
	p, ok := personaSurfacePrincipal(ctx)
	if !ok {
		if p != nil {
			return personachat.CancelResult{}, personachat.ErrDenied
		}
		return personachat.CancelResult{}, personachat.ErrUnauthenticated
	}
	invocation, err := s.Invocations.Lookup(ctx, p.Tenant().String(), p.Subject(), invocationID)
	if err != nil || !ownedPersonaInvocation(invocation, p.Tenant().String(), p.Subject(), invocationID) {
		return personachat.CancelResult{}, personachat.ErrDenied
	}
	if _, _, err := s.member(ctx, invocation.ConversationID); err != nil {
		return personachat.CancelResult{}, err
	}
	store, err := s.Executions(ctx, invocation.TenantID)
	if err != nil || store == nil {
		return personachat.CancelResult{}, personachat.ErrUnavailable
	}
	runID, err := personaSurfaceRunID(invocation)
	if err != nil {
		return personachat.CancelResult{}, personachat.ErrUnavailable
	}
	service, err := runstate.New(store, personaCancelRechecker{})
	if err != nil {
		return personachat.CancelResult{}, personachat.ErrUnavailable
	}
	for attempt := 0; attempt < 2; attempt++ {
		run, getErr := store.Get(ctx, runID)
		if getErr != nil || run.ID != runID || run.TenantID != invocation.TenantID || run.AdmissionID != runID {
			return personachat.CancelResult{}, personachat.ErrUnavailable
		}
		if run.CancelRequested && (run.State == runstate.StateCancelled || run.State == runstate.StateReconciling) {
			return personachat.CancelResult{InvocationID: invocationID, State: string(run.State)}, nil
		}
		if personaRunTerminal(run.State) {
			return personachat.CancelResult{}, &personachat.FinalStateConflict{State: string(run.State)}
		}
		cancelled, cancelErr := service.Cancel(ctx, runID, run.Version, s.personaActionNow())
		if cancelErr == nil {
			return personachat.CancelResult{InvocationID: invocationID, State: string(cancelled.State)}, nil
		}
		if !errors.Is(cancelErr, runstate.ErrConflict) {
			if errors.Is(cancelErr, runstate.ErrTerminal) {
				continue
			}
			return personachat.CancelResult{}, personachat.ErrUnavailable
		}
	}
	return personachat.CancelResult{}, personachat.ErrConflict
}

func (s *PersonaChatSurface) SubmitFeedback(ctx context.Context, invocationID string, helpful bool, reason, idempotencyKey string) (personachat.FeedbackResult, error) {
	if !utf8.ValidString(reason) || len([]rune(reason)) > 500 {
		return personachat.FeedbackResult{}, personachat.ErrInvalid
	}
	return s.changeFeedback(ctx, invocationID, idempotencyKey, func(store personaAnswerFeedbackStore, tenant, person string, at time.Time) (agentinvocationstore.AnswerFeedback, error) {
		return store.SubmitAnswerFeedback(ctx, tenant, person, invocationID, helpful, reason, idempotencyKey, at)
	})
}

func (s *PersonaChatSurface) UndoFeedback(ctx context.Context, invocationID, idempotencyKey string) (personachat.FeedbackResult, error) {
	return s.changeFeedback(ctx, invocationID, idempotencyKey, func(store personaAnswerFeedbackStore, tenant, person string, at time.Time) (agentinvocationstore.AnswerFeedback, error) {
		return store.UndoAnswerFeedback(ctx, tenant, person, invocationID, idempotencyKey, at)
	})
}

func (s *PersonaChatSurface) changeFeedback(ctx context.Context, invocationID, idempotencyKey string, change func(personaAnswerFeedbackStore, string, string, time.Time) (agentinvocationstore.AnswerFeedback, error)) (personachat.FeedbackResult, error) {
	if s == nil || s.Invocations == nil || change == nil {
		return personachat.FeedbackResult{}, personachat.ErrUnavailable
	}
	if !validPersonaActionInput(invocationID, idempotencyKey) {
		return personachat.FeedbackResult{}, personachat.ErrInvalid
	}
	p, ok := personaSurfacePrincipal(ctx)
	if !ok {
		if p != nil {
			return personachat.FeedbackResult{}, personachat.ErrDenied
		}
		return personachat.FeedbackResult{}, personachat.ErrUnauthenticated
	}
	invocation, err := s.Invocations.Lookup(ctx, p.Tenant().String(), p.Subject(), invocationID)
	if err != nil || !ownedPersonaInvocation(invocation, p.Tenant().String(), p.Subject(), invocationID) {
		return personachat.FeedbackResult{}, personachat.ErrDenied
	}
	if _, _, err := s.member(ctx, invocation.ConversationID); err != nil {
		return personachat.FeedbackResult{}, err
	}
	store, ok := s.Invocations.(personaAnswerFeedbackStore)
	if !ok {
		return personachat.FeedbackResult{}, personachat.ErrUnavailable
	}
	feedback, err := change(store, p.Tenant().String(), p.Subject(), s.personaActionNow())
	if err != nil {
		switch {
		case errors.Is(err, agentinvocationstore.ErrInvalid):
			return personachat.FeedbackResult{}, personachat.ErrInvalid
		case errors.Is(err, agentinvocationstore.ErrConflict):
			return personachat.FeedbackResult{}, personachat.ErrConflict
		case errors.Is(err, agentinvocationstore.ErrNotFound):
			return personachat.FeedbackResult{}, personachat.ErrDenied
		default:
			return personachat.FeedbackResult{}, personachat.ErrUnavailable
		}
	}
	return personachat.FeedbackResult{InvocationID: invocationID, Helpful: feedback.Helpful, Reason: feedback.Reason, Active: feedback.Active}, nil
}

func (s *PersonaChatSurface) personaActionNow() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func validPersonaActionInput(invocationID, idempotencyKey string) bool {
	return invocationID != "" && strings.TrimSpace(invocationID) == invocationID && utf8.ValidString(invocationID) && len(invocationID) <= 512 &&
		len(idempotencyKey) >= 8 && len(idempotencyKey) <= 128 && strings.TrimSpace(idempotencyKey) == idempotencyKey && utf8.ValidString(idempotencyKey)
}

func ownedPersonaInvocation(invocation agentinvoke.Invocation, tenant, person, id string) bool {
	return invocation.ID == id && invocation.TenantID == tenant && invocation.InvokerID == person && invocation.ConversationID != ""
}

func personaRunTerminal(state runstate.State) bool {
	switch state {
	case runstate.StateCompleted, runstate.StateFailed, runstate.StateCancelled, runstate.StateExpired, runstate.StateNeedsRepair:
		return true
	default:
		return false
	}
}

var _ personachat.ActionSurface = (*PersonaChatSurface)(nil)
