package application

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrPersonaShareConfirmUnavailable identifies an unwired private-result
// sharing service. The application boundary never invents a source, audience
// authority, disclosure decision, or post committer.
var ErrPersonaShareConfirmUnavailable = errors.New("application: persona share confirmation unavailable")

// PersonaShareConfirmPort is the server-owned preview and confirmation port.
// Its implementation must reload the private result and reauthorize the
// current audience on every confirmation.
type PersonaShareConfirmPort interface {
	PreviewPersonaShare(context.Context, chatrecipient.PersonaSharePreviewRequest) (chatrecipient.PersonaSharePreview, error)
	ConfirmPersonaShare(context.Context, chatrecipient.PersonaShareConfirmRequest) (chatrecipient.PersonaShareConfirmResult, error)
}

// PersonaShareConfirmConfig composes the server-owned persona share port.
type PersonaShareConfirmConfig struct {
	Service PersonaShareConfirmPort
}

// PersonaShareConfirmation verifies the authenticated human before invoking
// the digest-bound persona share workflow. It has no disclosure authority of
// its own and cannot create a persona-authored post.
type PersonaShareConfirmation struct {
	service PersonaShareConfirmPort
}

// PersonaSharePreviewRequest selects the invoker's private ephemeral result.
type PersonaSharePreviewRequest struct {
	Principal         *trust.Principal
	EphemeralResultID string
}

// PersonaShareConfirmRequest confirms one server-issued preview token for its
// original invoker.
type PersonaShareConfirmRequest struct {
	Principal *trust.Principal
	Token     string
}

// NewPersonaShareConfirmation requires the complete server-owned share port.
// Missing composition fails closed before a result is read or a post can be
// committed.
func NewPersonaShareConfirmation(config PersonaShareConfirmConfig) (*PersonaShareConfirmation, error) {
	if isNilPersonaShareConfirmPort(config.Service) {
		return nil, ErrPersonaShareConfirmUnavailable
	}
	return &PersonaShareConfirmation{service: config.Service}, nil
}

// PreviewPersonaShare verifies the trusted human context and creates an
// invoker-bound preview whose items and audience revision come from the
// server-owned workflow.
func (s *PersonaShareConfirmation) PreviewPersonaShare(ctx context.Context, request PersonaSharePreviewRequest) (chatrecipient.PersonaSharePreview, error) {
	principal, err := s.actor(ctx, request.Principal)
	if err != nil {
		return chatrecipient.PersonaSharePreview{}, err
	}
	if strings.TrimSpace(request.EphemeralResultID) == "" {
		return chatrecipient.PersonaSharePreview{}, chat.ErrInvalidArgument
	}
	return s.service.PreviewPersonaShare(ctx, chatrecipient.PersonaSharePreviewRequest{Principal: principal, EphemeralResultID: request.EphemeralResultID})
}

// ConfirmPersonaShare verifies the trusted human context and delegates one
// explicit confirmation. The underlying port rebinds the exact result,
// provenance digest, disclosure set and current audience before committing.
func (s *PersonaShareConfirmation) ConfirmPersonaShare(ctx context.Context, request PersonaShareConfirmRequest) (chatrecipient.PersonaShareConfirmResult, error) {
	principal, err := s.actor(ctx, request.Principal)
	if err != nil {
		return chatrecipient.PersonaShareConfirmResult{}, err
	}
	if strings.TrimSpace(request.Token) == "" {
		return chatrecipient.PersonaShareConfirmResult{}, chat.ErrInvalidArgument
	}
	return s.service.ConfirmPersonaShare(ctx, chatrecipient.PersonaShareConfirmRequest{Principal: principal, Token: request.Token})
}

func (s *PersonaShareConfirmation) actor(ctx context.Context, principal *trust.Principal) (chat.Principal, error) {
	if s == nil || isNilPersonaShareConfirmPort(s.service) {
		return chat.Principal{}, ErrPersonaShareConfirmUnavailable
	}
	if ctx == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return chat.Principal{}, chat.ErrUnauthenticated
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() {
		return chat.Principal{}, chat.ErrUnauthenticated
	}
	tenant := principal.Tenant().String()
	subject := principal.Subject()
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(subject) == "" {
		return chat.Principal{}, chat.ErrUnauthenticated
	}
	return chat.Principal{TenantID: tenant, SubjectID: subject}, nil
}

func isNilPersonaShareConfirmPort(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
