package agentconnect

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
)

// Store is the optional durable write-through seam behind a Registry. A nil
// Store leaves the Registry purely in memory. When one is configured the
// Registry persists every state change before it becomes visible, and
// Registry.Restore rebuilds an equivalent Registry after a restart.
//
// Only metadata is stored: connection revisions (skills, grant scopes and the
// second administrator approval), per-user account links (external account id
// plus the credential reference and custody handle the types already expose,
// never credential material), per-user epochs and connection revocation state.
//
// Short-lived credential leases are deliberately NOT persisted. They live in
// the Registry's memory only, so a restart invalidates every outstanding lease
// and callers must issue a new one. The live *connectivity.ConnectorConnection
// aggregate is owned by the connectivity plane and is also not persisted here;
// Restore takes a ConnectionResolver to re-attach it.
type Store interface {
	// PutRevision records a new immutable revision at epoch one. It must fail
	// when the connection already has a revision. Revision.Connection is nil.
	PutRevision(RevisionRecord) error
	// ListRevisions returns every revision, with revocation state, of a tenant.
	ListRevisions(tenant string) ([]RevisionRecord, error)
	// PutUserState upserts one user's link state and epoch for a connection.
	// The epoch never decreases; Link is nil when the account is unlinked.
	PutUserState(UserLinkState) error
	// ListUserStates returns every user link state of a tenant.
	ListUserStates(tenant string) ([]UserLinkState, error)
	// PutRevocation records that a connection was revoked and advances its
	// epoch. It is idempotent: the first actor, reason and time are kept.
	PutRevocation(RevocationRecord) error
}

// RevisionRecord is the persisted form of a ConnectionRevision.
type RevisionRecord struct {
	// Revision is the agent-facing revision with Connection left nil, and with
	// Skills[i].Tool.Validate dropped (it is behaviour, not data).
	Revision         ConnectionRevision
	ConnectorID      string
	ConnectorVersion string
	Epoch            uint64
	Revocation       *RevocationRecord
}

// RevocationRecord is the durable evidence of a connection disconnect.
type RevocationRecord struct {
	TenantID     string
	ConnectionID string
	Actor        string
	Reason       string
	EvidenceRef  string
	RevokedAt    time.Time
	// Epoch is the connection epoch after the revocation.
	Epoch uint64
}

// UserLinkState is one user's account link and epoch for a connection.
type UserLinkState struct {
	TenantID     string
	ConnectionID string
	UserID       string
	Epoch        uint64
	Link         *AccountLink
}

// ConnectionResolver re-attaches the connectivity aggregate for a persisted
// revision during Restore.
type ConnectionResolver func(tenant, connectionID string) (*connectivity.ConnectorConnection, error)

// NewPersistentRegistry is NewRegistry with a durable Store. A nil store is
// refused so persistence cannot be silently skipped.
func NewPersistentRegistry(issuer LeaseIssuer, now func() time.Time, store Store) (*Registry, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalid)
	}
	registry, err := NewRegistry(issuer, now)
	if err != nil {
		return nil, err
	}
	registry.store = store
	return registry, nil
}

// Restore loads one tenant's persisted revisions, links, epochs and revocation
// state into the Registry. Every revision is re-validated (including the
// brokered approval digest) against the connection the resolver returns, and a
// connection the store records as revoked is driven to REVOKED if the resolver
// returned it in another state, so a restart can never reactivate it. The
// whole tenant loads or nothing does.
func (r *Registry) Restore(tenant string, resolve ConnectionResolver) error {
	if r == nil || r.store == nil || resolve == nil {
		return fmt.Errorf("%w: restore needs a store and a connection resolver", ErrInvalid)
	}
	revisions, err := r.store.ListRevisions(tenant)
	if err != nil {
		return err
	}
	states, err := r.store.ListUserStates(tenant)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make(map[string]*connectionRecord, len(revisions))
	for _, record := range revisions {
		key := itemKey(record.Revision.TenantID, record.Revision.ID)
		if record.Revision.TenantID != tenant {
			return fmt.Errorf("%w: store returned a revision for another tenant", ErrInvalid)
		}
		if _, exists := r.items[key]; exists {
			return fmt.Errorf("%w: connection %q is already registered", ErrInvalid, record.Revision.ID)
		}
		connection, err := resolve(tenant, record.Revision.ID)
		if err != nil {
			return err
		}
		if connection == nil || connection.ConnectorID() != record.ConnectorID || connection.ConnectorVersion().String() != record.ConnectorVersion {
			return fmt.Errorf("%w: resolved connection %q does not match the persisted connector", ErrInvalid, record.Revision.ID)
		}
		revision := record.Revision
		revision.Connection = connection
		if err := revision.validate(); err != nil {
			return err
		}
		if record.Revocation != nil && connection.State() != connectivity.StateRevoked {
			if err := connection.Transition(connectivity.StateRevoked, connectivity.TransitionEvidence{Reason: record.Revocation.Reason, ActorRef: record.Revocation.Actor, EvidenceRef: record.Revocation.EvidenceRef, OccurredAt: record.Revocation.RevokedAt}); err != nil {
				return err
			}
		}
		items[key] = &connectionRecord{revision: copyRevision(revision), epoch: record.Epoch}
	}
	userEp := make(map[string]uint64, len(states))
	links := make(map[string]CredentialBinding)
	accounts := make(map[string]AccountLink)
	for _, state := range states {
		item, ok := items[itemKey(tenant, state.ConnectionID)]
		if !ok {
			return fmt.Errorf("%w: user state references unknown connection %q", ErrInvalid, state.ConnectionID)
		}
		key := userKey(tenant, state.ConnectionID, state.UserID)
		userEp[key] = state.Epoch
		if state.Link == nil {
			continue
		}
		if err := state.Link.Binding.validateUserOAuth(item.revision.TenantID); err != nil {
			return err
		}
		links[key] = copyBinding(state.Link.Binding)
		accounts[key] = copyAccount(*state.Link)
	}
	for key, item := range items {
		r.items[key] = item
	}
	for key, epoch := range userEp {
		r.userEp[key] = epoch
	}
	for key, binding := range links {
		r.links[key] = binding
	}
	for key, account := range accounts {
		r.accounts[key] = account
	}
	return nil
}

func revisionRecord(revision ConnectionRevision) RevisionRecord {
	record := RevisionRecord{Revision: copyRevision(revision), Epoch: 1}
	if revision.Connection != nil {
		record.ConnectorID = revision.Connection.ConnectorID()
		record.ConnectorVersion = revision.Connection.ConnectorVersion().String()
	}
	record.Revision.Connection = nil
	for i := range record.Revision.Skills {
		record.Revision.Skills[i].Tool.Validate = nil
	}
	return record
}
