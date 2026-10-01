package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// personaRegisteredChatIdentity is returned only for an identity registered
// in the tenant's canonical persona namespace. Reference IDs are the stable
// chat Agent.ID; labels are intentionally not part of this contract.
type personaRegisteredChatIdentity struct {
	TenantID    string
	ReferenceID string
	PersonaID   string
	Active      bool
}

type personaChatIdentityDirectory interface {
	LookupPersonaChatIdentity(context.Context, string, string) (personaRegisteredChatIdentity, error)
}

type personaChatIdentityStore interface {
	personaReferenceTenantStores
	ListPersonaChatIdentities(context.Context, string) ([]agentpersonastore.PersonaChatIdentity, error)
}

// productionPersonaChatIdentityDirectory adapts the isolated persona store's
// canonical Agent.ID registry to the application reference contract. The
// tenant is selected before the query; no display name or persona ID search is
// involved.
type productionPersonaChatIdentityDirectory struct {
	stores personaChatIdentityStore
}

func newProductionPersonaChatIdentityDirectory(stores personaChatIdentityStore) (*productionPersonaChatIdentityDirectory, error) {
	if stores == nil {
		return nil, fmt.Errorf("application: persona identity stores are required")
	}
	return &productionPersonaChatIdentityDirectory{stores: stores}, nil
}

func (d *productionPersonaChatIdentityDirectory) LookupPersonaChatIdentity(ctx context.Context, tenant, referenceID string) (personaRegisteredChatIdentity, error) {
	if d == nil || d.stores == nil || ctx == nil || strings.TrimSpace(tenant) != tenant || tenant == "" || !validPersonaReferenceID(referenceID) {
		return personaRegisteredChatIdentity{}, errPersonaReferenceInvalid
	}
	store, err := d.stores.Scoped(values.TenantId(tenant))
	if err != nil {
		return personaRegisteredChatIdentity{}, fmt.Errorf("application: scope persona identity store: %w", err)
	}
	if store == nil {
		return personaRegisteredChatIdentity{}, fmt.Errorf("%w: scoped persona identity store is nil", errPersonaReferenceInvalid)
	}
	identity, err := store.LookupPersonaChatIdentity(ctx, referenceID)
	if err != nil {
		if errors.Is(err, agentpersonastore.ErrNotFound) {
			return personaRegisteredChatIdentity{}, errPersonaReferenceNotPersona
		}
		return personaRegisteredChatIdentity{}, fmt.Errorf("application: lookup persona chat identity: %w", err)
	}
	if identity.TenantID != values.TenantId(tenant) || identity.AgentID != referenceID || !validPersonaReferenceID(identity.PersonaID) {
		return personaRegisteredChatIdentity{}, errPersonaReferenceInvalid
	}
	return personaRegisteredChatIdentity{TenantID: string(identity.TenantID), ReferenceID: identity.AgentID, PersonaID: identity.PersonaID, Active: identity.Active}, nil
}

// ListPersonaChatIdentities returns the active canonical persona chat
// identities in one tenant. The underlying catalog deliberately excludes
// revoked rows from invocable discovery while retaining their namespace
// reservations durably. It never derives identity from a display label and
// keeps the tenant binding on the store query.
func (d *productionPersonaChatIdentityDirectory) ListPersonaChatIdentities(ctx context.Context, tenant string) ([]personaRegisteredChatIdentity, error) {
	if d == nil || d.stores == nil || ctx == nil || strings.TrimSpace(tenant) != tenant || tenant == "" {
		return nil, errPersonaReferenceInvalid
	}
	store, err := d.stores.Scoped(values.TenantId(tenant))
	if err != nil {
		return nil, fmt.Errorf("application: scope persona identity store: %w", err)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: scoped persona identity store is nil", errPersonaReferenceInvalid)
	}
	identities, err := d.stores.ListPersonaChatIdentities(ctx, tenant)
	if err != nil {
		return nil, fmt.Errorf("application: list persona chat identities: %w", err)
	}
	out := make([]personaRegisteredChatIdentity, 0, len(identities))
	for _, identity := range identities {
		if identity.TenantID != values.TenantId(tenant) || !validPersonaReferenceID(identity.AgentID) || !validPersonaReferenceID(identity.PersonaID) {
			return nil, errPersonaReferenceInvalid
		}
		out = append(out, personaRegisteredChatIdentity{TenantID: string(identity.TenantID), ReferenceID: identity.AgentID, PersonaID: identity.PersonaID, Active: identity.Active})
	}
	return out, nil
}

type personaReferenceTenantStores interface {
	Scoped(values.TenantId) (*agentpersonastore.TenantStore, error)
}

// productionPersonaReferenceLookup joins the registered chat identity to the
// tenant-scoped durable persona lifecycle and exact-conversation installation.
// It performs no invocation, authority, or chat composition work.
type productionPersonaReferenceLookup struct {
	identities personaChatIdentityDirectory
	stores     personaReferenceTenantStores
}

func newProductionPersonaReferenceLookup(identities personaChatIdentityDirectory, stores personaReferenceTenantStores) (*productionPersonaReferenceLookup, error) {
	if identities == nil || stores == nil {
		return nil, fmt.Errorf("application: persona reference dependencies are required")
	}
	return &productionPersonaReferenceLookup{identities: identities, stores: stores}, nil
}

func (l *productionPersonaReferenceLookup) LookupPersonaReference(ctx context.Context, tenant, conversation, referenceID string) (personaReferenceFacts, error) {
	if l == nil || l.identities == nil || l.stores == nil || ctx == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(conversation) == "" || !validPersonaReferenceID(referenceID) {
		return personaReferenceFacts{}, errPersonaReferenceInvalid
	}
	identity, err := l.identities.LookupPersonaChatIdentity(ctx, tenant, referenceID)
	if err != nil {
		if errors.Is(err, errPersonaReferenceNotPersona) {
			return personaReferenceFacts{}, err
		}
		return personaReferenceFacts{}, fmt.Errorf("application: registered persona lookup: %w", err)
	}
	if identity.TenantID != tenant || identity.ReferenceID != referenceID || !validPersonaReferenceID(identity.PersonaID) {
		return personaReferenceFacts{}, errPersonaReferenceInvalid
	}
	if !identity.Active {
		return personaReferenceFacts{}, fmt.Errorf("%w: persona chat identity is inactive", errPersonaReferenceInactive)
	}
	store, err := l.stores.Scoped(values.TenantId(tenant))
	if err != nil {
		return personaReferenceFacts{}, fmt.Errorf("application: scope persona store: %w", err)
	}
	if store == nil {
		return personaReferenceFacts{}, fmt.Errorf("%w: scoped persona store is nil", errPersonaReferenceInvalid)
	}
	state, err := store.LookupCurrentReferenceInstallation(ctx, identity.PersonaID, conversation)
	if err != nil {
		if errors.Is(err, agentpersonastore.ErrNotFound) {
			return personaReferenceFacts{}, fmt.Errorf("%w: %v", errPersonaReferenceInactive, err)
		}
		return personaReferenceFacts{}, fmt.Errorf("application: durable persona reference lookup: %w", err)
	}
	if state.TenantID != values.TenantId(tenant) || state.PersonaID != identity.PersonaID ||
		state.ConversationID != conversation || !validPersonaReferenceID(state.InstallationID) ||
		state.PersonaVersion <= 0 || state.PersonaVersion != state.CurrentVersion ||
		state.InstallationState != agentpersonastore.InstallationActive ||
		state.Lifecycle != agentpersonastore.StatePublished {
		return personaReferenceFacts{}, fmt.Errorf("%w: durable state is not the exact active published installation", errPersonaReferenceInactive)
	}
	return personaReferenceFacts{
		ReferenceID: referenceID, TenantID: string(state.TenantID), ConversationID: state.ConversationID,
		PersonaID: state.PersonaID, InstallationID: state.InstallationID,
		PersonaVersion: uint64(state.PersonaVersion), CurrentVersion: uint64(state.CurrentVersion),
		InstallationState: personaReferenceInstallationState(state.InstallationState),
		PersonaLifecycle:  personaReferenceLifecycle(state.Lifecycle),
	}, nil
}
