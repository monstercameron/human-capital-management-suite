package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrPersonaPublicationUnavailable identifies a composition without both
	// durable evidence authorities required by the persona publication gate.
	ErrPersonaPublicationUnavailable = errors.New("application: persona publication evidence is unavailable")
	// ErrPersonaPublicationInvalid identifies malformed publication input.
	ErrPersonaPublicationInvalid = errors.New("application: invalid persona publication request")
)

// PersonaPublicationService is the application boundary for persona
// publication. The store owns the transaction and passes that transaction to
// both authorities, so review-grant revocation and evidence reads are fenced
// by the same commit.
type PersonaPublicationService struct {
	store *agentpersonastore.Store
}

// NewPersonaPublicationService constructs the fail-closed publication
// boundary. The store must have been constructed by
// agentpersonastore.NewWithPublicationAuthorities; that constructor is the
// composition point for durable authorities.
func NewPersonaPublicationService(store *agentpersonastore.Store) (*PersonaPublicationService, error) {
	if store == nil {
		return nil, ErrPersonaPublicationUnavailable
	}
	return &PersonaPublicationService{store: store}, nil
}

// Publish resolves the tenant store and delegates the atomic evidence check
// and lifecycle transition to the durable persona store.
func (s *PersonaPublicationService) Publish(ctx context.Context, tenant values.TenantId, event agentpersonastore.LifecycleEvent, evidence agentpersonastore.PublicationEvidence) error {
	if s == nil || s.store == nil || ctx == nil || tenant.Validate() != nil || event.TenantID != tenant || strings.TrimSpace(event.PersonaID) == "" || event.PersonaVersion <= 0 {
		return ErrPersonaPublicationInvalid
	}
	scoped, err := s.store.ForTenant(ctx, tenant)
	if err != nil {
		return fmt.Errorf("application: scope persona publication store: %w", err)
	}
	if err := scoped.Publish(ctx, event, evidence); err != nil {
		return fmt.Errorf("application: publish persona version: %w", err)
	}
	return nil
}
