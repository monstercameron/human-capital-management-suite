package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

type personaInvocationPostFailureSink interface {
	RecordPersonaInvocationPostFailure(context.Context, chat.Post, error)
}

func (c *personaChatInvocation) recordPostFailure(ctx context.Context, post chat.Post, err error) {
	if sink, ok := c.failures.(personaInvocationPostFailureSink); ok {
		sink.RecordPersonaInvocationPostFailure(ctx, post, err)
		return
	}
	c.recordFailure(ctx, post.ID, err)
}

type personaPostFailureStore interface {
	RecordPostFailure(context.Context, agentinvocationstore.PostFailure) error
	ListPostFailures(context.Context, string, string, string) ([]agentinvocationstore.PostFailure, error)
	LookupPostFailure(context.Context, string, string, string) (agentinvocationstore.PostFailure, error)
}

// personaDurableInvocationFailureSink records sanitized durable status and
// preserves operational reporting through the existing logger.
type personaDurableInvocationFailureSink struct {
	store  personaPostFailureStore
	logger personaInvocationFailureSink
}

func newPersonaDurableInvocationFailureSink(db *agentstore.Store, logger personaInvocationFailureSink) (personaInvocationFailureSink, error) {
	store, err := agentinvocationstore.NewWithTenantUUID(db, pgstore.TenantID)
	if err != nil {
		return nil, err
	}
	return &personaDurableInvocationFailureSink{store: store, logger: logger}, nil
}

func (s *personaDurableInvocationFailureSink) RecordPersonaInvocationFailure(ctx context.Context, postID string, err error) {
	if s != nil && s.logger != nil {
		s.logger.RecordPersonaInvocationFailure(ctx, postID, err)
	}
}

func (s *personaDurableInvocationFailureSink) RecordPersonaInvocationPostFailure(ctx context.Context, post chat.Post, cause error) {
	if s == nil || cause == nil {
		return
	}
	s.RecordPersonaInvocationFailure(ctx, post.ID, cause)
	p, ok := personaSurfacePrincipal(ctx)
	if !ok || s.store == nil || post.ID == "" || post.TenantID != p.Tenant().String() || post.AuthorID != p.Subject() || post.ConversationID == "" || post.Deleted || post.Revision != 1 {
		return
	}
	code, retryable := personaPostFailureClassification(cause)
	thread := post.ParentID
	if thread == "" {
		thread = post.ID
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := s.store.RecordPostFailure(writeCtx, agentinvocationstore.PostFailure{TenantID: post.TenantID, InvokerID: p.Subject(), ConversationID: post.ConversationID, ThreadID: thread, PostID: post.ID, Code: code, Retryable: retryable})
	if err != nil {
		s.RecordPersonaInvocationFailure(ctx, post.ID, err)
	}
}

func personaPostFailureClassification(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrPersonaRunModelFailure):
		return "MODEL_UNAVAILABLE", true
	case errors.Is(err, ErrPersonaRunOutputRejected):
		return "OUTPUT_REJECTED", false
	case errors.Is(err, ErrPersonaRunDeliveryFailure):
		return "DELIVERY_FAILED", false
	case errors.Is(err, agentinvoke.ErrDenied), errors.Is(err, agentinvoke.ErrInvalidRequest), errors.Is(err, errPersonaReferenceInvalid), errors.Is(err, errPersonaReferenceInactive):
		return "ADMISSION_REFUSED", false
	case errors.Is(err, ErrPersonaRunExecutorUnavailable):
		return "EXECUTION_UNAVAILABLE", true
	case errors.Is(err, ErrAgentDirectoryUnavailable), errors.Is(err, ErrPersonaAudienceDirectoryFactsMissing):
		return "ADMISSION_UNAVAILABLE", true
	default:
		return "INVOCATION_FAILED", false
	}
}
