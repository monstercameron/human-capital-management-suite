package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrPersonaHumanPostClassificationFailed = errors.New("application: committed human post classification unavailable")

// PersonaHumanPostClassificationError identifies a committed, authenticated
// human post whose classification failed. The handoff records this failure and
// preserves the durable chat response while refusing persona admission.
type PersonaHumanPostClassificationError struct {
	PostID string
	Cause  error
}

func (e *PersonaHumanPostClassificationError) Error() string {
	return ErrPersonaHumanPostClassificationFailed.Error()
}
func (e *PersonaHumanPostClassificationError) Unwrap() []error {
	if e == nil || e.Cause == nil {
		return []error{ErrPersonaHumanPostClassificationFailed}
	}
	return []error{ErrPersonaHumanPostClassificationFailed, e.Cause}
}

// PersonaAcceptedHumanPostClassifier classifies current, durably accepted bytes
// before the invocation reads its audience or submits any model work.
type PersonaAcceptedHumanPostClassifier interface {
	ClassifyAcceptedHumanPost(context.Context, chat.Post) error
}

type personaClassifiedHumanConversationReader interface {
	GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
}

// ClassifiedPersonaHumanPostWriter wraps the authenticated durable writer.
// Failed writes and machine identities never reach the classification port.
type ClassifiedPersonaHumanPostWriter struct {
	inner         personaChatPostWriter
	conversations personaClassifiedHumanConversationReader
	classifier    PersonaAcceptedHumanPostClassifier
}

func NewClassifiedPersonaHumanPostWriter(inner personaChatPostWriter, conversations personaClassifiedHumanConversationReader, classifier PersonaAcceptedHumanPostClassifier) (*ClassifiedPersonaHumanPostWriter, error) {
	if isNilPersonaOutputPort(inner) || isNilPersonaOutputPort(conversations) || isNilPersonaOutputPort(classifier) {
		return nil, errPersonaInvocationServedPorts
	}
	return &ClassifiedPersonaHumanPostWriter{inner: inner, conversations: conversations, classifier: classifier}, nil
}

func (w *ClassifiedPersonaHumanPostWriter) SendPost(ctx context.Context, request chat.SendPostRequest) (chat.Post, error) {
	if w == nil || ctx == nil || isNilPersonaOutputPort(w.inner) || isNilPersonaOutputPort(w.conversations) || isNilPersonaOutputPort(w.classifier) {
		return chat.Post{}, errPersonaInvocationServedPorts
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != request.TenantID || request.Principal.TenantID != request.TenantID || request.Principal.SubjectID != principal.Subject() {
		return chat.Post{}, chat.ErrPermissionDenied
	}
	post, err := w.inner.SendPost(ctx, request)
	if err != nil {
		return post, err
	}
	if post.ID == "" || post.TenantID != request.TenantID || post.ConversationID != request.ConversationID || post.AuthorID != principal.Subject() || post.AuthorHomeTenantID != request.TenantID {
		return chat.Post{}, chat.ErrPermissionDenied
	}
	// Forwarded, edited and removed posts are ineligible for admission; they
	// cannot be promoted to a newly classified persona instruction.
	if post.Revision != 1 || post.Deleted || post.SourceAttribution != nil {
		return post, nil
	}
	conversation, err := w.conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: request.Principal, TenantID: post.TenantID, ConversationID: post.ConversationID})
	if err != nil {
		return post, &PersonaHumanPostClassificationError{PostID: post.ID, Cause: err}
	}
	if conversation.ID != post.ConversationID || conversation.TenantID != post.TenantID || conversation.Archived {
		return post, &PersonaHumanPostClassificationError{PostID: post.ID, Cause: chat.ErrPermissionDenied}
	}
	if conversation.Kind != chat.PublicChannel && conversation.Kind != chat.PrivateChannel && conversation.Kind != chat.Direct && conversation.Kind != chat.Group {
		return post, &PersonaHumanPostClassificationError{PostID: post.ID, Cause: chat.ErrPermissionDenied}
	}
	if err := w.classifier.ClassifyAcceptedHumanPost(ctx, post); err != nil {
		return post, &PersonaHumanPostClassificationError{PostID: post.ID, Cause: err}
	}
	return post, nil
}

var _ personaChatPostWriter = (*ClassifiedPersonaHumanPostWriter)(nil)
