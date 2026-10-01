// Package authn owns subscriber-account and digital-identity lifecycle
// authority. It records immutable lifecycle evidence and fences every
// derived authorization relationship with an account revocation epoch.
package authn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// AccountStatus is the subscriber account lifecycle.
type AccountStatus string

const (
	AccountActive     AccountStatus = "ACTIVE"
	AccountSuspended  AccountStatus = "SUSPENDED"
	AccountDisabled   AccountStatus = "DISABLED"
	AccountTerminated AccountStatus = "TERMINATED"

	StatusActive     = AccountActive
	StatusSuspended  = AccountSuspended
	StatusDisabled   = AccountDisabled
	StatusTerminated = AccountTerminated
)

// IdentityStatus is the lifecycle of a tenant-bound digital identity.
type IdentityStatus string

const (
	IdentityLinked    IdentityStatus = "LINKED"
	IdentitySuspended IdentityStatus = "SUSPENDED"
	IdentityUnlinked  IdentityStatus = "UNLINKED"
	IdentityRevoked   IdentityStatus = "REVOKED"
)

// DependentKind identifies authority derived from an account. The values are
// intentionally explicit so a fan-out cannot silently omit a security object.
type DependentKind string

const (
	DependentPrincipal     DependentKind = "PRINCIPAL"
	DependentSession       DependentKind = "SESSION"
	DependentTokenFamily   DependentKind = "TOKEN_FAMILY"
	DependentAuthenticator DependentKind = "AUTHENTICATOR"
	DependentFederation    DependentKind = "FEDERATION"
	DependentDelegation    DependentKind = "DELEGATION"
	DependentSubjectLink   DependentKind = "SUBJECT_LINK"
)

func (k DependentKind) valid() bool {
	switch k {
	case DependentPrincipal, DependentSession, DependentTokenFamily, DependentAuthenticator, DependentFederation, DependentDelegation, DependentSubjectLink:
		return true
	default:
		return false
	}
}

// DependentStatus is the state of a derived authority reference.
type DependentStatus string

const (
	DependentActive  DependentStatus = "ACTIVE"
	DependentRevoked DependentStatus = "REVOKED"
)

// Account is the current projection of one tenant-scoped subscriber account.
// RevocationEpoch is monotonic and is the fence checked by every dependent.
type Account struct {
	ID              string
	PersonID        string
	Tenant          values.TenantId
	Status          AccountStatus
	RevocationEpoch uint64
	Revision        uint64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Digest          string
}

// Identity is a provider-backed digital identity. SubjectDigest is retained
// instead of a raw federated subject, preserving the subjectlink boundary.
type Identity struct {
	ID              string
	AccountID       string
	Tenant          values.TenantId
	ProviderRef     string
	SubjectDigest   string
	Status          IdentityStatus
	RevocationEpoch uint64
	Revision        uint64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Digest          string
}

// Dependent is an authority or credential derived from an account or
// identity. Its epoch must equal the current account epoch to be usable.
type Dependent struct {
	ID              string
	AccountID       string
	IdentityID      string
	Tenant          values.TenantId
	Kind            DependentKind
	Status          DependentStatus
	RevocationEpoch uint64
	Assurance       trust.Assurance
	Queued          bool
	CreatedAt       time.Time
	RevokedAt       time.Time
	Digest          string
}

// Reference is the redaction-safe identity of one fan-out target.
type Reference struct {
	Kind DependentKind
	ID   string
}

// LifecycleEvent is immutable, append-only evidence. It contains references
// and digests only; it never contains a raw federated subject or credential.
type LifecycleEvent struct {
	AccountID      string
	Tenant         values.TenantId
	From           AccountStatus
	To             AccountStatus
	IdentityID     string
	IdentityFrom   IdentityStatus
	IdentityTo     IdentityStatus
	Epoch          uint64
	Actor          string
	Authority      string
	Reason         string
	At             time.Time
	Affected       []Reference
	PreviousDigest string
	Digest         string
}

// Evidence supplies the accountable actor and authority for a transition.
type Evidence struct {
	Actor     string
	Authority string
	Reason    string
	At        time.Time
}

// AccountSpec creates a new active account.
type AccountSpec struct {
	ID       string
	PersonID string
	Tenant   values.TenantId
	At       time.Time
}

// IdentitySpec links a provider identity to an account.
type IdentitySpec struct {
	ID            string
	AccountID     string
	Tenant        values.TenantId
	ProviderRef   string
	SubjectDigest string
	At            time.Time
}

// DependentSpec registers authority derived from an account or identity.
type DependentSpec struct {
	ID         string
	AccountID  string
	IdentityID string
	Tenant     values.TenantId
	Kind       DependentKind
	Assurance  trust.Assurance
	Queued     bool
	At         time.Time
}

// AuthenticationRequest asks whether an identity and optional dependent can
// still authenticate at a specific boundary instant.
type AuthenticationRequest struct {
	AccountID   string
	IdentityID  string
	DependentID string
	At          time.Time
	Assurance   trust.Assurance
}

// AuthenticationResult is the trusted, epoch-bound result of authentication.
type AuthenticationResult struct {
	AccountID       string
	IdentityID      string
	DependentID     string
	Tenant          values.TenantId
	RevocationEpoch uint64
	Assurance       trust.Assurance
	At              time.Time
}

// RecoveryRequest describes a recovery attempt. Recovery may preserve or
// lower assurance, never raise it above the already trusted level.
type RecoveryRequest struct {
	AccountID          string
	IdentityID         string
	CurrentAssurance   trust.Assurance
	RequestedAssurance trust.Assurance
	Evidence           Evidence
}

// RevocationTarget is sent after the account epoch has been committed.
type RevocationTarget struct {
	Reference
	AccountID  string
	IdentityID string
	Tenant     values.TenantId
	Epoch      uint64
	Reason     string
	At         time.Time
}

// RevocationSink is the adapter boundary for concrete token, session,
// federation, delegation and subject-link authorities. The account epoch is
// already committed before this callback runs, so a failed callback cannot
// make the old authority valid again.
type RevocationSink interface {
	Revoke(context.Context, RevocationTarget) error
}

// SessionRevoker is implemented by both trust/session managers.
type SessionRevoker interface {
	Revoke(context.Context, session.ID, string) (session.Record, error)
}

// SessionRevocationSink adapts the canonical trust/session manager to the
// generic fan-out port.
type SessionRevocationSink struct {
	Revoker SessionRevoker
}

func (s SessionRevocationSink) Revoke(ctx context.Context, target RevocationTarget) error {
	if target.Kind != DependentSession && target.Kind != DependentTokenFamily {
		return nil
	}
	if s.Revoker == nil {
		return fmt.Errorf("authn: session revoker is required")
	}
	_, err := s.Revoker.Revoke(ctx, session.ID(target.ID), target.Reason)
	return err
}

// Store is the atomic persistence port for lifecycle projections and events.
// The provided MemoryStore is kernel-pure; a durable adapter can implement
// the same commit boundary without changing authentication semantics.
type Store interface {
	CreateAccount(Account, LifecycleEvent) error
	GetAccount(string) (Account, bool, error)
	GetIdentity(string) (Identity, bool, error)
	ListIdentities(string) ([]Identity, error)
	GetDependent(string) (Dependent, bool, error)
	ListDependents(string) ([]Dependent, error)
	Commit(Account, []Identity, []Dependent, LifecycleEvent) error
	AppendIdentity(Identity, LifecycleEvent) error
	AppendDependent(Dependent) error
	Events(string) ([]LifecycleEvent, error)
}

// MemoryStore is a concurrency-safe in-memory Store. It is intentionally an
// adapter, not authority separate from Service's lifecycle decisions.
type MemoryStore struct {
	mu         sync.RWMutex
	accounts   map[string]Account
	identities map[string]Identity
	dependents map[string]Dependent
	events     map[string][]LifecycleEvent
}

// NewMemoryStore returns an empty lifecycle store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		accounts: make(map[string]Account), identities: make(map[string]Identity),
		dependents: make(map[string]Dependent), events: make(map[string][]LifecycleEvent),
	}
}

var _ Store = (*MemoryStore)(nil)

// Service owns account and identity lifecycle decisions.
type Service struct {
	mu    sync.Mutex
	store Store
	now   func() time.Time
	sink  RevocationSink
}

// Lifecycle and Manager are descriptive names used by composition code.
type Lifecycle = Service
type Manager = Service

// New returns a lifecycle service. The optional clock is useful for replayable
// tests and deterministic adapters; omitted clocks use time.Now.
func New(store Store, clocks ...func() time.Time) *Service {
	now := time.Now
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &Service{store: store, now: now}
}

// NewMemory returns a kernel-pure service backed by MemoryStore.
func NewMemory(clocks ...func() time.Time) *Service {
	return New(NewMemoryStore(), clocks...)
}

// SetRevocationSink installs the post-commit fan-out adapter.
func (s *Service) SetRevocationSink(sink RevocationSink) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sink = sink
	s.mu.Unlock()
}

// CreateAccount creates the active account at epoch one.
func (s *Service) CreateAccount(spec AccountSpec) (Account, LifecycleEvent, error) {
	if s == nil || s.store == nil {
		return Account{}, LifecycleEvent{}, ErrStore
	}
	if err := validateID(spec.ID, "account id"); err != nil {
		return Account{}, LifecycleEvent{}, err
	}
	if err := validateID(spec.PersonID, "person id"); err != nil {
		return Account{}, LifecycleEvent{}, err
	}
	if err := spec.Tenant.Validate(); err != nil {
		return Account{}, LifecycleEvent{}, fmt.Errorf("%w: tenant: %v", ErrInvalidAccount, err)
	}
	at := spec.At.UTC()
	if spec.At.IsZero() {
		at = s.now().UTC()
	}
	account := Account{ID: spec.ID, PersonID: spec.PersonID, Tenant: spec.Tenant, Status: AccountActive, RevocationEpoch: 1, Revision: 1, CreatedAt: at, UpdatedAt: at}
	account.Digest = digestAccount(account)
	event := newEvent(account, "", "", "", Evidence{Actor: "system", Authority: "account-provisioning", Reason: "account created", At: at}, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.CreateAccount(account, event); err != nil {
		return Account{}, LifecycleEvent{}, err
	}
	return account, event, nil
}

// LinkIdentity links a new digital identity to an active account.
func (s *Service) LinkIdentity(spec IdentitySpec) (Identity, LifecycleEvent, error) {
	if s == nil || s.store == nil {
		return Identity{}, LifecycleEvent{}, ErrStore
	}
	if err := validateID(spec.ID, "identity id"); err != nil {
		return Identity{}, LifecycleEvent{}, err
	}
	if err := validateID(spec.AccountID, "account id"); err != nil {
		return Identity{}, LifecycleEvent{}, err
	}
	if strings.TrimSpace(spec.ProviderRef) == "" || !validDigest(spec.SubjectDigest) {
		return Identity{}, LifecycleEvent{}, ErrInvalidIdentity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok, err := s.store.GetAccount(spec.AccountID)
	if err != nil || !ok {
		return Identity{}, LifecycleEvent{}, ErrAccountNotFound
	}
	if err := requireAccountActive(account); err != nil {
		return Identity{}, LifecycleEvent{}, err
	}
	if spec.Tenant != account.Tenant {
		return Identity{}, LifecycleEvent{}, ErrTenantMismatch
	}
	at := spec.At.UTC()
	if spec.At.IsZero() {
		at = s.now().UTC()
	}
	identity := Identity{ID: spec.ID, AccountID: account.ID, Tenant: account.Tenant, ProviderRef: spec.ProviderRef, SubjectDigest: spec.SubjectDigest, Status: IdentityLinked, RevocationEpoch: account.RevocationEpoch, Revision: 1, CreatedAt: at, UpdatedAt: at}
	identity.Digest = digestIdentity(identity)
	event := newEvent(account, "", identity.ID, IdentityLinked, Evidence{Actor: "identity-linker", Authority: "identity-governance", Reason: "identity linked", At: at}, nil)
	if err := s.store.AppendIdentity(identity, event); err != nil {
		return Identity{}, LifecycleEvent{}, err
	}
	return identity, event, nil
}

// RegisterDependent records a derived authority at the current epoch.
func (s *Service) RegisterDependent(spec DependentSpec) (Dependent, error) {
	if s == nil || s.store == nil {
		return Dependent{}, ErrStore
	}
	if err := validateID(spec.ID, "dependent id"); err != nil || !spec.Kind.valid() || spec.Assurance == trust.AssuranceUnspecified {
		return Dependent{}, ErrInvalidDependent
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok, err := s.store.GetAccount(spec.AccountID)
	if err != nil || !ok {
		return Dependent{}, ErrAccountNotFound
	}
	if err := requireAccountActive(account); err != nil {
		return Dependent{}, err
	}
	if spec.Tenant != account.Tenant {
		return Dependent{}, ErrTenantMismatch
	}
	if spec.IdentityID != "" {
		identity, ok, err := s.store.GetIdentity(spec.IdentityID)
		if err != nil || !ok || identity.AccountID != account.ID || identity.Status != IdentityLinked {
			return Dependent{}, ErrIdentityNotUsable
		}
	}
	at := spec.At.UTC()
	if spec.At.IsZero() {
		at = s.now().UTC()
	}
	dependent := Dependent{ID: spec.ID, AccountID: account.ID, IdentityID: spec.IdentityID, Tenant: account.Tenant, Kind: spec.Kind, Status: DependentActive, RevocationEpoch: account.RevocationEpoch, Assurance: spec.Assurance, Queued: spec.Queued, CreatedAt: at}
	dependent.Digest = digestDependent(dependent)
	if err := s.store.AppendDependent(dependent); err != nil {
		return Dependent{}, err
	}
	return dependent, nil
}

// Suspend fences every dependent and marks identities suspended.
func (s *Service) Suspend(ctx context.Context, accountID string, evidence Evidence) (Account, LifecycleEvent, error) {
	return s.transition(ctx, accountID, AccountSuspended, evidence)
}

// Disable permanently disables an account and revokes every derived authority.
func (s *Service) Disable(ctx context.Context, accountID string, evidence Evidence) (Account, LifecycleEvent, error) {
	return s.transition(ctx, accountID, AccountDisabled, evidence)
}

// Terminate irreversibly terminates an account and revokes every dependency.
func (s *Service) Terminate(ctx context.Context, accountID string, evidence Evidence) (Account, LifecycleEvent, error) {
	return s.transition(ctx, accountID, AccountTerminated, evidence)
}

// Resume restores a suspended account but never resurrects revoked authority.
func (s *Service) Resume(ctx context.Context, accountID string, evidence Evidence) (Account, LifecycleEvent, error) {
	return s.transition(ctx, accountID, AccountActive, evidence)
}

// SuspendAccount is the descriptive account-lifecycle command form.
func (s *Service) SuspendAccount(ctx context.Context, accountID string, evidence Evidence) (Account, LifecycleEvent, error) {
	return s.Suspend(ctx, accountID, evidence)
}

// DisableAccount is the descriptive account-lifecycle command form.
func (s *Service) DisableAccount(ctx context.Context, accountID string, evidence Evidence) (Account, LifecycleEvent, error) {
	return s.Disable(ctx, accountID, evidence)
}

// TerminateAccount is the descriptive account-lifecycle command form.
func (s *Service) TerminateAccount(ctx context.Context, accountID string, evidence Evidence) (Account, LifecycleEvent, error) {
	return s.Terminate(ctx, accountID, evidence)
}

func (s *Service) transition(ctx context.Context, accountID string, to AccountStatus, evidence Evidence) (Account, LifecycleEvent, error) {
	if s == nil || s.store == nil {
		return Account{}, LifecycleEvent{}, ErrStore
	}
	if err := validateEvidence(evidence); err != nil {
		return Account{}, LifecycleEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok, err := s.store.GetAccount(accountID)
	if err != nil || !ok {
		return Account{}, LifecycleEvent{}, ErrAccountNotFound
	}
	if !allowedTransition(account.Status, to) {
		return Account{}, LifecycleEvent{}, ErrLifecycleConflict
	}
	identities, err := s.store.ListIdentities(accountID)
	if err != nil {
		return Account{}, LifecycleEvent{}, err
	}
	dependents, err := s.store.ListDependents(accountID)
	if err != nil {
		return Account{}, LifecycleEvent{}, err
	}
	updated := account
	updated.Status = to
	updated.Revision++
	updated.RevocationEpoch++
	updated.UpdatedAt = evidence.At.UTC()
	identityFrom, identityTo := IdentityStatus(""), IdentityStatus("")
	for i := range identities {
		identityFrom = identities[i].Status
		switch to {
		case AccountSuspended:
			identities[i].Status = IdentitySuspended
		case AccountActive:
			if identities[i].Status == IdentitySuspended {
				identities[i].Status = IdentityLinked
			}
		default:
			identities[i].Status = IdentityRevoked
		}
		identityTo = identities[i].Status
		identities[i].RevocationEpoch = updated.RevocationEpoch
		identities[i].Revision++
		identities[i].UpdatedAt = evidence.At.UTC()
		identities[i].Digest = digestIdentity(identities[i])
	}
	targets := make([]RevocationTarget, 0, len(dependents))
	refs := make([]Reference, 0, len(dependents))
	for i := range dependents {
		dependents[i].Status = DependentRevoked
		dependents[i].RevocationEpoch = updated.RevocationEpoch
		dependents[i].RevokedAt = evidence.At.UTC()
		dependents[i].Digest = digestDependent(dependents[i])
		ref := Reference{Kind: dependents[i].Kind, ID: dependents[i].ID}
		refs = append(refs, ref)
		targets = append(targets, RevocationTarget{Reference: ref, AccountID: account.ID, IdentityID: dependents[i].IdentityID, Tenant: account.Tenant, Epoch: updated.RevocationEpoch, Reason: evidence.Reason, At: evidence.At.UTC()})
	}
	updated.Digest = digestAccount(updated)
	previous := lastEventDigest(s.store, account.ID)
	event := newEvent(updated, previous, account.ID, identityFrom, Evidence{Actor: evidence.Actor, Authority: evidence.Authority, Reason: evidence.Reason, At: evidence.At}, refs)
	event.From, event.To = account.Status, to
	event.IdentityID, event.IdentityFrom, event.IdentityTo = identityID(identities, identityFrom), identityFrom, identityTo
	event.Digest = digestEvent(event)
	if err := s.store.Commit(updated, identities, dependents, event); err != nil {
		return Account{}, LifecycleEvent{}, err
	}
	if err := s.fanout(ctx, targets); err != nil {
		return updated, event, err
	}
	return updated, event, nil
}

// Unlink fences account-derived authority and makes the identity unusable.
func (s *Service) Unlink(ctx context.Context, identityID string, evidence Evidence) (Identity, LifecycleEvent, error) {
	return s.identityTransition(ctx, identityID, IdentityUnlinked, evidence)
}

// Relink restores a previously unlinked identity's link state, preserves its
// revision history, and leaves all old derived authority revoked.
func (s *Service) Relink(ctx context.Context, identityID string, evidence Evidence) (Identity, LifecycleEvent, error) {
	return s.identityTransition(ctx, identityID, IdentityLinked, evidence)
}

// UnlinkIdentity is the descriptive identity-lifecycle command form.
func (s *Service) UnlinkIdentity(ctx context.Context, identityID string, evidence Evidence) (Identity, LifecycleEvent, error) {
	return s.Unlink(ctx, identityID, evidence)
}

// RelinkIdentity is the descriptive identity-lifecycle command form.
func (s *Service) RelinkIdentity(ctx context.Context, identityID string, evidence Evidence) (Identity, LifecycleEvent, error) {
	return s.Relink(ctx, identityID, evidence)
}

func (s *Service) identityTransition(ctx context.Context, identityID string, to IdentityStatus, evidence Evidence) (Identity, LifecycleEvent, error) {
	if s == nil || s.store == nil {
		return Identity{}, LifecycleEvent{}, ErrStore
	}
	if err := validateEvidence(evidence); err != nil {
		return Identity{}, LifecycleEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, ok, err := s.store.GetIdentity(identityID)
	if err != nil || !ok {
		return Identity{}, LifecycleEvent{}, ErrIdentityNotFound
	}
	account, ok, err := s.store.GetAccount(identity.AccountID)
	if err != nil || !ok {
		return Identity{}, LifecycleEvent{}, ErrAccountNotFound
	}
	if account.Status != AccountActive {
		return Identity{}, LifecycleEvent{}, refusal(CodeAccountNotActive, ErrAccountNotActive, account.ID, identity.ID, "")
	}
	if to == IdentityLinked && identity.Status != IdentityUnlinked && identity.Status != IdentityRevoked || to == IdentityUnlinked && identity.Status != IdentityLinked {
		return Identity{}, LifecycleEvent{}, ErrLifecycleConflict
	}
	identities, err := s.store.ListIdentities(account.ID)
	if err != nil {
		return Identity{}, LifecycleEvent{}, err
	}
	dependents, err := s.store.ListDependents(account.ID)
	if err != nil {
		return Identity{}, LifecycleEvent{}, err
	}
	updatedAccount := account
	updatedAccount.RevocationEpoch++
	updatedAccount.Revision++
	updatedAccount.UpdatedAt = evidence.At.UTC()
	updatedIdentity := identity
	updatedIdentity.Status = to
	updatedIdentity.RevocationEpoch = updatedAccount.RevocationEpoch
	updatedIdentity.Revision++
	updatedIdentity.UpdatedAt = evidence.At.UTC()
	updatedIdentity.Digest = digestIdentity(updatedIdentity)
	for i := range identities {
		if identities[i].ID == identity.ID {
			identities[i] = updatedIdentity
		}
	}
	targets := make([]RevocationTarget, 0, len(dependents))
	refs := make([]Reference, 0, len(dependents))
	for i := range dependents {
		dependents[i].Status = DependentRevoked
		dependents[i].RevocationEpoch = updatedAccount.RevocationEpoch
		dependents[i].RevokedAt = evidence.At.UTC()
		dependents[i].Digest = digestDependent(dependents[i])
		ref := Reference{Kind: dependents[i].Kind, ID: dependents[i].ID}
		refs = append(refs, ref)
		targets = append(targets, RevocationTarget{Reference: ref, AccountID: account.ID, IdentityID: dependents[i].IdentityID, Tenant: account.Tenant, Epoch: updatedAccount.RevocationEpoch, Reason: evidence.Reason, At: evidence.At.UTC()})
	}
	updatedAccount.Digest = digestAccount(updatedAccount)
	previous := lastEventDigest(s.store, account.ID)
	event := newEvent(updatedAccount, previous, updatedIdentity.ID, updatedIdentity.Status, evidence, refs)
	event.From, event.To = account.Status, account.Status
	event.IdentityFrom, event.IdentityTo = identity.Status, to
	if err := s.store.Commit(updatedAccount, identities, dependents, event); err != nil {
		return Identity{}, LifecycleEvent{}, err
	}
	if err := s.fanout(ctx, targets); err != nil {
		return updatedIdentity, event, err
	}
	return updatedIdentity, event, nil
}

// Authenticate is the only successful authentication decision in this
// package. It reads current state under the same mutex used by lifecycle
// commits, so an authentication cannot win after a committed revoke.
func (s *Service) Authenticate(req AuthenticationRequest) (AuthenticationResult, error) {
	if s == nil || s.store == nil {
		return AuthenticationResult{}, ErrStore
	}
	if req.At.IsZero() {
		return AuthenticationResult{}, ErrInvalidAuthentication
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok, err := s.store.GetAccount(req.AccountID)
	if err != nil || !ok {
		return AuthenticationResult{}, ErrAccountNotFound
	}
	if err := requireAccountActive(account); err != nil {
		return AuthenticationResult{}, err
	}
	identity, ok, err := s.store.GetIdentity(req.IdentityID)
	if err != nil || !ok || identity.AccountID != account.ID {
		return AuthenticationResult{}, ErrIdentityNotFound
	}
	if identity.Status != IdentityLinked || identity.RevocationEpoch != account.RevocationEpoch {
		return AuthenticationResult{}, refusal(CodeIdentityRevoked, ErrIdentityNotUsable, account.ID, identity.ID, "")
	}
	result := AuthenticationResult{AccountID: account.ID, IdentityID: identity.ID, Tenant: account.Tenant, RevocationEpoch: account.RevocationEpoch, At: req.At.UTC()}
	if req.DependentID != "" {
		dependent, ok, err := s.store.GetDependent(req.DependentID)
		if err != nil || !ok || dependent.AccountID != account.ID {
			return AuthenticationResult{}, ErrDependentNotFound
		}
		if dependent.Status != DependentActive || dependent.RevocationEpoch != account.RevocationEpoch {
			return AuthenticationResult{}, refusal(CodeDependentRevoked, ErrDependentRevoked, account.ID, identity.ID, dependent.ID)
		}
		if dependent.IdentityID != "" && dependent.IdentityID != identity.ID {
			return AuthenticationResult{}, ErrIdentityNotUsable
		}
		if req.Assurance != trust.AssuranceUnspecified && !dependent.Assurance.AtLeast(req.Assurance) {
			return AuthenticationResult{}, refusal(CodeAssuranceInsufficient, ErrAssuranceInsufficient, account.ID, identity.ID, dependent.ID)
		}
		result.DependentID, result.Assurance = dependent.ID, dependent.Assurance
	}
	return result, nil
}

// CheckDependent performs the same epoch fence for queued or live authority.
func (s *Service) CheckDependent(accountID, dependentID string, at time.Time) error {
	if s == nil || s.store == nil {
		return ErrStore
	}
	if at.IsZero() {
		return ErrInvalidAuthentication
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok, err := s.store.GetAccount(accountID)
	if err != nil || !ok {
		return ErrAccountNotFound
	}
	if err := requireAccountActive(account); err != nil {
		return err
	}
	dependent, ok, err := s.store.GetDependent(dependentID)
	if err != nil || !ok || dependent.AccountID != account.ID {
		return ErrDependentNotFound
	}
	if dependent.Status != DependentActive || dependent.RevocationEpoch != account.RevocationEpoch {
		return refusal(CodeDependentRevoked, ErrDependentRevoked, account.ID, dependent.IdentityID, dependent.ID)
	}
	return nil
}

// Recover performs assurance-bounded recovery without changing lifecycle
// state. A recovery request can never upgrade its trusted assurance.
func (s *Service) Recover(req RecoveryRequest) (AuthenticationResult, error) {
	if req.RequestedAssurance == trust.AssuranceUnspecified || req.CurrentAssurance == trust.AssuranceUnspecified || !req.CurrentAssurance.AtLeast(req.RequestedAssurance) {
		return AuthenticationResult{}, ErrRecoveryAssurance
	}
	if err := validateEvidence(req.Evidence); err != nil {
		return AuthenticationResult{}, err
	}
	return s.Authenticate(AuthenticationRequest{AccountID: req.AccountID, IdentityID: req.IdentityID, At: req.Evidence.At, Assurance: req.RequestedAssurance})
}

// Account returns a defensive account snapshot.
func (s *Service) Account(id string) (Account, bool, error) {
	if s == nil || s.store == nil {
		return Account{}, false, ErrStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.GetAccount(id)
}

// Identity returns a defensive identity snapshot.
func (s *Service) Identity(id string) (Identity, bool, error) {
	if s == nil || s.store == nil {
		return Identity{}, false, ErrStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.GetIdentity(id)
}

// Dependent returns a defensive dependent snapshot.
func (s *Service) Dependent(id string) (Dependent, bool, error) {
	if s == nil || s.store == nil {
		return Dependent{}, false, ErrStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.GetDependent(id)
}

// Events returns immutable lifecycle evidence oldest first.
func (s *Service) Events(accountID string) ([]LifecycleEvent, error) {
	if s == nil || s.store == nil {
		return nil, ErrStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Events(accountID)
}

func (s *Service) fanout(ctx context.Context, targets []RevocationTarget) error {
	if s.sink == nil {
		return nil
	}
	for _, target := range targets {
		if err := s.sink.Revoke(ctx, target); err != nil {
			return &FanoutError{Target: target, Err: err}
		}
	}
	return nil
}

var (
	ErrStore                 = errors.New("authn: lifecycle store is unavailable")
	ErrInvalidAccount        = errors.New("authn: invalid account")
	ErrInvalidIdentity       = errors.New("authn: invalid digital identity")
	ErrInvalidDependent      = errors.New("authn: invalid dependent")
	ErrInvalidAuthentication = errors.New("authn: invalid authentication request")
	ErrAccountNotFound       = errors.New("authn: account not found")
	ErrIdentityNotFound      = errors.New("authn: identity not found")
	ErrDependentNotFound     = errors.New("authn: dependent not found")
	ErrAccountNotActive      = errors.New("authn: account is not active")
	ErrIdentityNotUsable     = errors.New("authn: identity is not usable")
	ErrDependentRevoked      = errors.New("authn: dependent authority is revoked")
	ErrAssuranceInsufficient = errors.New("authn: assurance is insufficient")
	ErrRecoveryAssurance     = errors.New("authn: recovery cannot elevate assurance")
	ErrTenantMismatch        = errors.New("authn: tenant mismatch")
	ErrLifecycleConflict     = errors.New("authn: lifecycle transition conflict")
	ErrFanoutIncomplete      = errors.New("authn: revocation fan-out incomplete")
)

const (
	CodeAccountNotActive      = "ACCOUNT_NOT_ACTIVE"
	CodeIdentityRevoked       = "IDENTITY_REVOKED"
	CodeDependentRevoked      = "DEPENDENT_REVOKED"
	CodeAssuranceInsufficient = "ASSURANCE_INSUFFICIENT"
	CodeFanoutIncomplete      = "REVOCATION_FANOUT_INCOMPLETE"
)

// Refusal is a typed, stable lifecycle denial.
type Refusal struct {
	code        string
	AccountID   string
	IdentityID  string
	DependentID string
	Err         error
}

func (e *Refusal) Error() string { return fmt.Sprintf("authn: %s", e.code) }
func (e *Refusal) Unwrap() error { return e.Err }
func (e *Refusal) Code() string  { return e.code }

// CodeOf returns a stable refusal code when err carries one.
func CodeOf(err error) string {
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

// FanoutError reports a committed epoch with one adapter still needing
// repair. The derived object remains fenced in MemoryStore.
type FanoutError struct {
	Target RevocationTarget
	Err    error
}

func (e *FanoutError) Error() string {
	return fmt.Sprintf("authn: %s for %s/%s", CodeFanoutIncomplete, e.Target.Kind, e.Target.ID)
}
func (e *FanoutError) Unwrap() []error { return []error{ErrFanoutIncomplete, e.Err} }
func (e *FanoutError) Code() string    { return CodeFanoutIncomplete }

func refusal(code string, err error, accountID, identityID, dependentID string) error {
	return &Refusal{code: code, Err: err, AccountID: accountID, IdentityID: identityID, DependentID: dependentID}
}

func requireAccountActive(account Account) error {
	if account.Status == AccountActive {
		return nil
	}
	return refusal(CodeAccountNotActive, ErrAccountNotActive, account.ID, "", "")
}

func allowedTransition(from, to AccountStatus) bool {
	switch {
	case from == AccountActive && (to == AccountSuspended || to == AccountDisabled || to == AccountTerminated):
		return true
	case from == AccountSuspended && (to == AccountActive || to == AccountDisabled || to == AccountTerminated):
		return true
	default:
		return false
	}
}

func validateID(id, label string) error {
	if id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n\t") || len(id) > 200 {
		return fmt.Errorf("%w: %s", ErrInvalidAccount, label)
	}
	return nil
}

func validDigest(d string) bool {
	if len(d) != sha256.Size*2 || strings.ToLower(d) != d {
		return false
	}
	_, err := hex.DecodeString(d)
	return err == nil
}

func validateEvidence(e Evidence) error {
	if e.Actor == "" || e.Authority == "" || e.Reason == "" || e.At.IsZero() {
		return ErrInvalidAuthentication
	}
	return nil
}

func newEvent(account Account, previous, identityID string, identityTo IdentityStatus, evidence Evidence, refs []Reference) LifecycleEvent {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind == refs[j].Kind {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Kind < refs[j].Kind
	})
	e := LifecycleEvent{AccountID: account.ID, Tenant: account.Tenant, From: account.Status, To: account.Status, IdentityID: identityID, IdentityTo: identityTo, Epoch: account.RevocationEpoch, Actor: evidence.Actor, Authority: evidence.Authority, Reason: evidence.Reason, At: evidence.At.UTC(), Affected: append([]Reference(nil), refs...), PreviousDigest: previous}
	e.Digest = digestEvent(e)
	return e
}

func digestAccount(a Account) string {
	return digestParts("account", a.ID, a.PersonID, a.Tenant.String(), string(a.Status), fmt.Sprint(a.RevocationEpoch), fmt.Sprint(a.Revision), a.CreatedAt.UTC().Format(time.RFC3339Nano), a.UpdatedAt.UTC().Format(time.RFC3339Nano))
}

func digestIdentity(i Identity) string {
	return digestParts("identity", i.ID, i.AccountID, i.Tenant.String(), i.ProviderRef, i.SubjectDigest, string(i.Status), fmt.Sprint(i.RevocationEpoch), fmt.Sprint(i.Revision), i.CreatedAt.UTC().Format(time.RFC3339Nano), i.UpdatedAt.UTC().Format(time.RFC3339Nano))
}

func digestDependent(d Dependent) string {
	return digestParts("dependent", d.ID, d.AccountID, d.IdentityID, d.Tenant.String(), string(d.Kind), string(d.Status), fmt.Sprint(d.RevocationEpoch), d.CreatedAt.UTC().Format(time.RFC3339Nano), d.RevokedAt.UTC().Format(time.RFC3339Nano))
}

func digestEvent(e LifecycleEvent) string {
	parts := []string{"event", e.AccountID, e.Tenant.String(), string(e.From), string(e.To), e.IdentityID, string(e.IdentityFrom), string(e.IdentityTo), fmt.Sprint(e.Epoch), e.Actor, e.Authority, e.Reason, e.At.UTC().Format(time.RFC3339Nano), e.PreviousDigest}
	for _, ref := range e.Affected {
		parts = append(parts, string(ref.Kind), ref.ID)
	}
	return digestParts(parts...)
}

func digestParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%d:", len(part))
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func identityID(identities []Identity, status IdentityStatus) string {
	for _, identity := range identities {
		if identity.Status == status {
			return identity.ID
		}
	}
	return ""
}

func lastEventDigest(store Store, accountID string) string {
	events, err := store.Events(accountID)
	if err != nil || len(events) == 0 {
		return ""
	}
	return events[len(events)-1].Digest
}

func cloneEvent(e LifecycleEvent) LifecycleEvent {
	e.Affected = append([]Reference(nil), e.Affected...)
	return e
}

// MemoryStore methods.
func (m *MemoryStore) CreateAccount(account Account, event LifecycleEvent) error {
	if m == nil {
		return ErrStore
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureMapsLocked()
	if _, exists := m.accounts[account.ID]; exists {
		return ErrLifecycleConflict
	}
	m.accounts[account.ID] = account
	m.events[account.ID] = []LifecycleEvent{cloneEvent(event)}
	return nil
}

func (m *MemoryStore) GetAccount(id string) (Account, bool, error) {
	if m == nil {
		return Account{}, false, ErrStore
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.accounts[id]
	return a, ok, nil
}

func (m *MemoryStore) GetIdentity(id string) (Identity, bool, error) {
	if m == nil {
		return Identity{}, false, ErrStore
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	i, ok := m.identities[id]
	return i, ok, nil
}

func (m *MemoryStore) ListIdentities(accountID string) ([]Identity, error) {
	if m == nil {
		return nil, ErrStore
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Identity, 0)
	for _, identity := range m.identities {
		if identity.AccountID == accountID {
			out = append(out, identity)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryStore) GetDependent(id string) (Dependent, bool, error) {
	if m == nil {
		return Dependent{}, false, ErrStore
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.dependents[id]
	return d, ok, nil
}

func (m *MemoryStore) ListDependents(accountID string) ([]Dependent, error) {
	if m == nil {
		return nil, ErrStore
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Dependent, 0)
	for _, dependent := range m.dependents {
		if dependent.AccountID == accountID {
			out = append(out, dependent)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryStore) Commit(account Account, identities []Identity, dependents []Dependent, event LifecycleEvent) error {
	if m == nil {
		return ErrStore
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureMapsLocked()
	if _, exists := m.accounts[account.ID]; !exists {
		return ErrAccountNotFound
	}
	m.accounts[account.ID] = account
	for _, identity := range identities {
		m.identities[identity.ID] = identity
	}
	for _, dependent := range dependents {
		m.dependents[dependent.ID] = dependent
	}
	m.events[account.ID] = append(m.events[account.ID], cloneEvent(event))
	return nil
}

func (m *MemoryStore) AppendIdentity(identity Identity, event LifecycleEvent) error {
	if m == nil {
		return ErrStore
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureMapsLocked()
	if _, exists := m.identities[identity.ID]; exists {
		return ErrLifecycleConflict
	}
	m.identities[identity.ID] = identity
	m.events[identity.AccountID] = append(m.events[identity.AccountID], cloneEvent(event))
	return nil
}

func (m *MemoryStore) AppendDependent(dependent Dependent) error {
	if m == nil {
		return ErrStore
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureMapsLocked()
	if _, exists := m.dependents[dependent.ID]; exists {
		return ErrLifecycleConflict
	}
	m.dependents[dependent.ID] = dependent
	return nil
}

func (m *MemoryStore) Events(accountID string) ([]LifecycleEvent, error) {
	if m == nil {
		return nil, ErrStore
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entries, ok := m.events[accountID]
	if !ok {
		return nil, ErrAccountNotFound
	}
	out := make([]LifecycleEvent, len(entries))
	for i, event := range entries {
		out[i] = cloneEvent(event)
	}
	return out, nil
}

func (m *MemoryStore) ensureMapsLocked() {
	if m.accounts == nil {
		m.accounts = make(map[string]Account)
	}
	if m.identities == nil {
		m.identities = make(map[string]Identity)
	}
	if m.dependents == nil {
		m.dependents = make(map[string]Dependent)
	}
	if m.events == nil {
		m.events = make(map[string][]LifecycleEvent)
	}
}

// Version is the account/identity lifecycle contract version.
func Version() int { return 1 }

// ValidateServingContract exercises the lifecycle authority that a composed
// serving cell depends on. It deliberately uses the kernel-pure store: the
// production persistence adapter supplies the same Store contract, while this
// check proves the serving composition cannot omit an authority kind or the
// committed epoch fence.
func ValidateServingContract() error {
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	service := NewMemory(func() time.Time { return at })
	if _, _, err := service.CreateAccount(AccountSpec{ID: "serving-account", PersonID: "serving-person", Tenant: "tenant-serving", At: at}); err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	identity, _, err := service.LinkIdentity(IdentitySpec{
		ID: "serving-identity", AccountID: "serving-account", Tenant: "tenant-serving",
		ProviderRef: "serving-provider", SubjectDigest: strings.Repeat("a", sha256.Size*2), At: at,
	})
	if err != nil {
		return fmt.Errorf("link identity: %w", err)
	}
	kinds := []DependentKind{
		DependentPrincipal, DependentSession, DependentTokenFamily,
		DependentAuthenticator, DependentFederation, DependentDelegation, DependentSubjectLink,
	}
	for i, kind := range kinds {
		if _, err := service.RegisterDependent(DependentSpec{
			ID: fmt.Sprintf("serving-dependent-%d", i), AccountID: "serving-account", IdentityID: identity.ID,
			Tenant: "tenant-serving", Kind: kind, Assurance: trust.AssuranceSubstantial, At: at,
		}); err != nil {
			return fmt.Errorf("register %s authority: %w", kind, err)
		}
	}
	fanoutCount := 0
	service.SetRevocationSink(revocationSinkFunc(func(context.Context, RevocationTarget) error {
		fanoutCount++
		return nil
	}))
	account, event, err := service.Disable(context.Background(), "serving-account", Evidence{
		Actor: "serving-contract", Authority: "account-governance", Reason: "serving contract", At: at,
	})
	if err != nil {
		return fmt.Errorf("disable account: %w", err)
	}
	if account.Status != AccountDisabled || account.RevocationEpoch != 2 || len(event.Affected) != len(kinds) || fanoutCount != len(kinds) {
		return fmt.Errorf("revocation contract incomplete: status=%s epoch=%d affected=%d fanout=%d", account.Status, account.RevocationEpoch, len(event.Affected), fanoutCount)
	}
	if _, err := service.Authenticate(AuthenticationRequest{AccountID: account.ID, IdentityID: identity.ID, At: at}); !errors.Is(err, ErrAccountNotActive) {
		return fmt.Errorf("disabled account authentication error = %v", err)
	}
	for i, kind := range kinds {
		dependent, ok, err := service.Dependent(fmt.Sprintf("serving-dependent-%d", i))
		if err != nil || !ok || dependent.Kind != kind || dependent.Status != DependentRevoked || dependent.RevocationEpoch != account.RevocationEpoch {
			return fmt.Errorf("%s authority is not fenced: dependent=%+v ok=%t err=%v", kind, dependent, ok, err)
		}
	}
	return nil
}

type revocationSinkFunc func(context.Context, RevocationTarget) error

func (f revocationSinkFunc) Revoke(ctx context.Context, target RevocationTarget) error {
	return f(ctx, target)
}

// Explain returns a redaction-safe summary of lifecycle authority.
func Explain() string {
	return "subscriber account epochs fence principals, sessions, token families, authenticators, federation, delegation and subject links; recovery cannot elevate assurance."
}
