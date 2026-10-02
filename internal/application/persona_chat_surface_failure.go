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
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaInvocationPostFailureSink interface {
	RecordPersonaInvocationPostFailure(context.Context, chat.Post, error)
}

func (c *personaChatInvocation) recordPostFailure(ctx context.Context, post chat.Post, err error) {
	// CHATBUG-063: every committed human post passes through here when its
	// classification or reference resolution fails, including one that asked no
	// agent. Such a post has no answer that could have failed: the cause is
	// logged, and no outcome is stored for the page to draw a card from.
	if !c.postAsksAgent(ctx, post) {
		c.recordFailure(ctx, post.ID, err)
		return
	}
	if sink, ok := c.failures.(personaInvocationPostFailureSink); ok {
		sink.RecordPersonaInvocationPostFailure(ctx, post, err)
		return
	}
	c.recordFailure(ctx, post.ID, err)
}

// postAsksAgent reports whether the committed post asked an agent: it carries
// a typed agent mention, or it was sent in a direct conversation whose other
// member resolves to an agent placed there.
func (c *personaChatInvocation) postAsksAgent(ctx context.Context, post chat.Post) bool {
	if hasTypedAgentReference(post.References) {
		return true
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || c.refs == nil {
		return false
	}
	reference, ok := c.directAgentReference(ctx, principal, post)
	if !ok {
		return false
	}
	mentions, err := c.refs.ResolvePersonaMentions(ctx, post.TenantID, post.ConversationID, []chat.Reference{reference})
	return err == nil && len(mentions) > 0
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
	// A run failure already says whether the person may try again (a daily limit
	// does not, a timeout does); keep that instead of the class default.
	var failure *PersonaRunFailure
	if errors.As(err, &failure) && failure != nil {
		code, retryable := personaPostFailureClassification(failure.Unwrap())
		switch code {
		case "MODEL_UNAVAILABLE", "EXECUTION_UNAVAILABLE", "ADMISSION_UNAVAILABLE":
			retryable = failure.Retryable
		}
		return code, retryable
	}
	switch {
	case errors.Is(err, ErrPersonaRunDailyLimit):
		return "DAILY_LIMIT_REACHED", false
	case errors.Is(err, errPersonaModelNotConfigured):
		return "SERVER_HAS_NO_MODEL", false
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
