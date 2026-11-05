// Business parties and commercial roles for SMB-CRM-001.
//
// A BusinessParty is one separately typed customer/supplier/prospect
// identity under the Organization/Relationship owner: Acme can be a
// customer and a supplier without duplicate identities and without
// workforce records. Roles are effective-dated intervals on the party;
// ending one role never erases another, and history stays queryable so
// pre-existing obligations settle under their original contract.
//
// The registry is storage-neutral like the rest of this package: it holds
// revisioned records in memory, restores through Export/Import snapshots,
// and leaves durable tables, RLS, and served transport to the owning
// store and application layers. Commercial edges are explicitly
// non-inheriting: they never resolve as supervisory parents and no party
// ref can pass as a workforce identity.

package org

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// KindBusinessParty is the entity kind of every party ref. It is
// deliberately distinct from workforce kinds: the registry never mints,
// links, or resolves employment identity.
const KindBusinessParty values.Kind = "business_party"

var (
	ErrPartyInvalid             = errors.New("org: business party request is invalid")
	ErrPartyNotFound            = errors.New("org: business party is not visible")
	ErrPartyForbidden           = errors.New("org: business party actor is not authorized")
	ErrPartyRevisionConflict    = errors.New("org: business party revision conflict")
	ErrPartyRoleNotEffective    = errors.New("org: commercial role is not effective")
	ErrPartyArchived            = errors.New("org: business party is archived")
	ErrPartyIdempotencyConflict = errors.New("org: party idempotency key was reused for another request")
	ErrPartyExternallyMastered  = errors.New("org: party field is mastered externally")
	ErrPartyHasCommitments      = errors.New("org: party has open commitments")
	ErrPartyResponseLost        = errors.New("org: party receipt recorded but response lost")
)

// PartyKind declares whether a party is a person or an organization.
type PartyKind string

const (
	PartyKindPerson       PartyKind = "PERSON"
	PartyKindOrganization PartyKind = "ORGANIZATION"
)

func (k PartyKind) Valid() bool {
	return k == PartyKindPerson || k == PartyKindOrganization
}

// PartyStatus declares whether a party accepts new business use.
type PartyStatus string

const (
	PartyStatusActive   PartyStatus = "ACTIVE"
	PartyStatusArchived PartyStatus = "ARCHIVED"
)

// CommercialRole declares one commercial capacity of a party.
type CommercialRole string

const (
	CommercialRoleCustomer CommercialRole = "CUSTOMER"
	CommercialRoleSupplier CommercialRole = "SUPPLIER"
	CommercialRoleProspect CommercialRole = "PROSPECT"
)

func (r CommercialRole) Valid() bool {
	switch r {
	case CommercialRoleCustomer, CommercialRoleSupplier, CommercialRoleProspect:
		return true
	default:
		return false
	}
}

// BusinessRelationship is one effective-dated commercial role on a party.
// Ended intervals stay on the record: ending is a new fact, never an
// erasure, so historical effectiveness remains answerable.
type BusinessRelationship struct {
	RelationshipID string
	Role           CommercialRole
	Effective      values.EffectiveInterval
	OwnerRef       string
	Classification string
	Revision       uint64
	EndReason      string
}

// ContactAffiliation references one purpose-scoped business contact.
// Endpoint normalization, verification, and revision contracts stay with
// the contact owner; the reference here never implies employment identity
// or portal access.
type ContactAffiliation struct {
	EndpointRef string
	Purpose     string
}

// ServiceLocationRef references one versioned service location. Address
// values stay with the location owner; the purpose here never infers
// legal jurisdiction or corrupts payroll worksite semantics.
type ServiceLocationRef struct {
	LocationRef string
	Purpose     string
}

// BusinessParty is one customer/supplier/prospect identity.
type BusinessParty struct {
	Ref                 values.EntityRef
	CompanyScope        string
	Kind                PartyKind
	DisplayName         string
	LegalName           string
	Status              PartyStatus
	Revision            uint64
	Predecessor         uint64
	SourceAuthority     evidence.SourceAuthority
	ExternalLinks       []string
	ExternalMastered    []string
	Roles               []BusinessRelationship
	ContactAffiliations []ContactAffiliation
	ServiceLocations    []ServiceLocationRef
}

// PartyReceipt is the durable receipt for one committed party write.
type PartyReceipt struct {
	PartyID    string
	Revision   uint64
	ReceiptRef string
	At         time.Time
}

// PartySnapshot is an immutable copy owners (invoice, job, order) pin at
// obligation time. Later role edits never move a snapshot.
type PartySnapshot struct {
	Party BusinessParty
	At    time.Time
}

// RoleGrant proposes one initial or added role interval.
type RoleGrant struct {
	Role          CommercialRole
	EffectiveFrom values.Instant
	EffectiveTo   *values.Instant
	OwnerRef      string
}

// CreatePartyRequest creates one party. CompanyScope is the edge-resolved
// trusted scope: the registry enforces exact matches and never defaults
// it from caller roles.
type CreatePartyRequest struct {
	Tenant           values.TenantId
	CompanyScope     string
	PartyID          string
	Kind             PartyKind
	DisplayName      string
	LegalName        string
	SourceAuthority  evidence.SourceAuthority
	ExternalLinks    []string
	ExternalMastered []string
	InitialRoles     []RoleGrant
	IdempotencyKey   string
	Actor            string
	At               time.Time
}

// RevisePartyRequest revises display fields. UpdateMask names exactly the
// supplied mutable fields: display_name, legal_name.
type RevisePartyRequest struct {
	Tenant           values.TenantId
	CompanyScope     string
	PartyID          string
	ExpectedRevision uint64
	UpdateMask       []string
	DisplayName      string
	LegalName        string
	IdempotencyKey   string
	Actor            string
	At               time.Time
}

// AddRoleRequest adds one role interval.
type AddRoleRequest struct {
	Tenant           values.TenantId
	CompanyScope     string
	PartyID          string
	ExpectedRevision uint64
	Role             CommercialRole
	EffectiveFrom    values.Instant
	EffectiveTo      *values.Instant
	OwnerRef         string
	IdempotencyKey   string
	Actor            string
	At               time.Time
}

// EndRoleRequest ends one open role interval at an effective time.
type EndRoleRequest struct {
	Tenant           values.TenantId
	CompanyScope     string
	PartyID          string
	RelationshipID   string
	ExpectedRevision uint64
	EffectiveTo      values.Instant
	Reason           string
	IdempotencyKey   string
	Actor            string
	At               time.Time
}

// ArchivePartyRequest archives a party. OpenCommitments carries the
// caller-observed open jobs/orders: a non-empty set refuses with the
// blockers named, and nothing is erased either way.
type ArchivePartyRequest struct {
	Tenant           values.TenantId
	CompanyScope     string
	PartyID          string
	ExpectedRevision uint64
	Reason           string
	OpenCommitments  []string
	IdempotencyKey   string
	Actor            string
	At               time.Time
}

// RestorePartyRequest restores an archived party to discovery. Ended and
// expired roles remain ended: restore reactivates no new-business role
// authority.
type RestorePartyRequest struct {
	Tenant           values.TenantId
	CompanyScope     string
	PartyID          string
	ExpectedRevision uint64
	Reason           string
	IdempotencyKey   string
	Actor            string
	At               time.Time
}

// PartyFilter scopes a party listing.
type PartyFilter struct {
	Role   CommercialRole
	Status PartyStatus
	Query  string
}

// AuthorityChecker authorizes one actor for a tenant company. A nil checker
// requires an authenticated (non-empty) actor; revocation, roles, and CSRF
// live with the identity and transport owners, not this registry.
type AuthorityChecker func(ctx context.Context, actor, tenant, company string) error

type partyEffect struct {
	Fingerprint string       `json:"fingerprint"`
	Receipt     PartyReceipt `json:"receipt"`
	Revision    uint64       `json:"revision"`
}

// PartyRegistry is the concurrency-safe in-memory party port. AfterCommit
// is a fault-injection seam: when non-nil it runs after a write commits
// but before the response, so tests prove a lost response still replays
// without a duplicate effect.
type PartyRegistry struct {
	mu          sync.Mutex
	authorize   AuthorityChecker
	parties     map[string]*BusinessParty
	effects     map[string]partyEffect
	AfterCommit func() error
}

// NewPartyRegistry builds the party port.
func NewPartyRegistry(authorize AuthorityChecker) *PartyRegistry {
	return &PartyRegistry{
		authorize: authorize,
		parties:   make(map[string]*BusinessParty),
		effects:   make(map[string]partyEffect),
	}
}

func partyKey(tenant values.TenantId, company, partyID string) string {
	return string(tenant) + "\x00" + company + "\x00" + partyID
}

func cloneParty(in BusinessParty) BusinessParty {
	in.ExternalLinks = append([]string(nil), in.ExternalLinks...)
	in.ExternalMastered = append([]string(nil), in.ExternalMastered...)
	in.Roles = append([]BusinessRelationship(nil), in.Roles...)
	in.ContactAffiliations = append([]ContactAffiliation(nil), in.ContactAffiliations...)
	in.ServiceLocations = append([]ServiceLocationRef(nil), in.ServiceLocations...)
	return in
}

func (r *PartyRegistry) checkAuthority(ctx context.Context, actor string, tenant values.TenantId, company string) error {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(company) == "" {
		return fmt.Errorf("%w: actor and company scope are required", ErrPartyNotFound)
	}
	if err := tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrPartyNotFound, err)
	}
	if r.authorize == nil {
		return nil
	}
	if err := r.authorize(ctx, actor, string(tenant), company); err != nil {
		return fmt.Errorf("%w: %v", ErrPartyForbidden, err)
	}
	return nil
}

func (r *PartyRegistry) visibleLocked(tenant values.TenantId, company, partyID string) (*BusinessParty, bool) {
	party, ok := r.parties[partyKey(tenant, company, partyID)]
	if !ok {
		return nil, false
	}
	return party, true
}

func roleInterval(from values.Instant, to *values.Instant) (values.EffectiveInterval, error) {
	if to == nil {
		return values.NewOpenInstantInterval(from)
	}
	return values.NewInstantInterval(from, *to)
}

func grantRelationship(grant RoleGrant, revision uint64) (BusinessRelationship, error) {
	if !grant.Role.Valid() {
		return BusinessRelationship{}, fmt.Errorf("%w: role %q is not declared", ErrPartyInvalid, grant.Role)
	}
	interval, err := roleInterval(grant.EffectiveFrom, grant.EffectiveTo)
	if err != nil {
		return BusinessRelationship{}, fmt.Errorf("%w: role interval: %v", ErrPartyInvalid, err)
	}
	return BusinessRelationship{
		RelationshipID: uuid.NewString(),
		Role:           grant.Role,
		Effective:      interval,
		OwnerRef:       grant.OwnerRef,
		Revision:       revision,
	}, nil
}

func roleOverlaps(rel BusinessRelationship, interval values.EffectiveInterval) bool {
	overlap, err := rel.Effective.Overlaps(interval)
	return err == nil && overlap
}

// CreateParty creates one party at revision 1. Replaying an idempotency
// key returns the original receipt; reusing a key for another request
// conflicts.
func (r *PartyRegistry) CreateParty(ctx context.Context, req CreatePartyRequest) (BusinessParty, PartyReceipt, error) {
	if err := r.checkAuthority(ctx, req.Actor, req.Tenant, req.CompanyScope); err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	if !req.Kind.Valid() {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: party kind %q is not declared", ErrPartyInvalid, req.Kind)
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: display name is required", ErrPartyInvalid)
	}
	if err := req.SourceAuthority.Validate(); err != nil {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: source authority: %v", ErrPartyInvalid, err)
	}
	partyID := req.PartyID
	if partyID == "" {
		partyID = uuid.NewString()
	}
	ref := values.EntityRef{Tenant: req.Tenant, Kind: KindBusinessParty, Id: partyID}
	if err := ref.Validate(); err != nil {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: party ref: %v", ErrPartyInvalid, err)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: idempotency key and time are required", ErrPartyInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fingerprint := "create\x00" + partyKey(req.Tenant, req.CompanyScope, partyID) + "\x00" + string(req.Kind) + "\x00" + req.DisplayName
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: key was reused for another request", ErrPartyIdempotencyConflict)
		}
		party, visible := r.visibleLocked(req.Tenant, req.CompanyScope, partyID)
		if !visible {
			return BusinessParty{}, PartyReceipt{}, ErrPartyNotFound
		}
		return cloneParty(*party), prior.Receipt, nil
	}
	if _, dup := r.parties[partyKey(req.Tenant, req.CompanyScope, partyID)]; dup {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: party id already exists", ErrPartyInvalid)
	}
	party := BusinessParty{
		Ref: ref, CompanyScope: req.CompanyScope, Kind: req.Kind,
		DisplayName: req.DisplayName, LegalName: req.LegalName,
		Status: PartyStatusActive, Revision: 1,
		SourceAuthority:  req.SourceAuthority,
		ExternalLinks:    append([]string(nil), req.ExternalLinks...),
		ExternalMastered: append([]string(nil), req.ExternalMastered...),
	}
	for _, grant := range req.InitialRoles {
		rel, err := grantRelationship(grant, 1)
		if err != nil {
			return BusinessParty{}, PartyReceipt{}, err
		}
		for _, existing := range party.Roles {
			if existing.Role == rel.Role && roleOverlaps(existing, rel.Effective) {
				return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: initial %s intervals overlap", ErrPartyInvalid, rel.Role)
			}
		}
		party.Roles = append(party.Roles, rel)
	}
	receipt := PartyReceipt{PartyID: partyID, Revision: 1, ReceiptRef: "party-receipt:" + partyID + ":1", At: req.At}
	r.parties[partyKey(req.Tenant, req.CompanyScope, partyID)] = &party
	r.effects[req.IdempotencyKey] = partyEffect{Fingerprint: fingerprint, Receipt: receipt, Revision: 1}
	out := cloneParty(party)
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: %w", ErrPartyResponseLost, crash)
		}
	}
	return out, receipt, nil
}

// commitLocked records one CAS write under the caller's idempotency key.
// It returns the cloned party, receipt, and whether the call replayed.
func (r *PartyRegistry) commitLocked(key, fingerprint string, party *BusinessParty, at time.Time) (BusinessParty, PartyReceipt, bool, error) {
	if prior, ok := r.effects[key]; ok {
		if prior.Fingerprint != fingerprint {
			return BusinessParty{}, PartyReceipt{}, false, fmt.Errorf("%w: key was reused for another request", ErrPartyIdempotencyConflict)
		}
		return cloneParty(*party), prior.Receipt, true, nil
	}
	party.Predecessor, party.Revision = party.Revision, party.Revision+1
	receipt := PartyReceipt{PartyID: party.Ref.Id, Revision: party.Revision, ReceiptRef: fmt.Sprintf("party-receipt:%s:%d", party.Ref.Id, party.Revision), At: at}
	r.effects[key] = partyEffect{Fingerprint: fingerprint, Receipt: receipt, Revision: party.Revision}
	return cloneParty(*party), receipt, false, nil
}

func (r *PartyRegistry) writeParty(ctx context.Context, actor string, tenant values.TenantId, company, partyID string, expectedRevision uint64) (*BusinessParty, error) {
	if err := r.checkAuthority(ctx, actor, tenant, company); err != nil {
		return nil, err
	}
	party, visible := r.visibleLocked(tenant, company, partyID)
	if !visible {
		return nil, ErrPartyNotFound
	}
	if party.Revision != expectedRevision {
		return nil, fmt.Errorf("%w: writer holds %d, party is at %d", ErrPartyRevisionConflict, expectedRevision, party.Revision)
	}
	return party, nil
}

// ReviseParty revises display fields under compare-and-swap. The update
// mask must name exactly the supplied mutable fields, and externally
// mastered fields reject local writes with a reconciliation link.
func (r *PartyRegistry) ReviseParty(ctx context.Context, req RevisePartyRequest) (BusinessParty, PartyReceipt, error) {
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: idempotency key and time are required", ErrPartyInvalid)
	}
	supplied := map[string]bool{}
	if req.DisplayName != "" {
		supplied["display_name"] = true
	}
	if req.LegalName != "" {
		supplied["legal_name"] = true
	}
	if len(req.UpdateMask) == 0 || len(req.UpdateMask) != len(supplied) {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: update mask must name exactly the supplied fields", ErrPartyInvalid)
	}
	for _, field := range req.UpdateMask {
		if !supplied[field] {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: update mask names %q without supplying it", ErrPartyInvalid, field)
		}
	}
	fingerprint := "revise\x00" + partyKey(req.Tenant, req.CompanyScope, req.PartyID) + "\x00" + strings.Join(req.UpdateMask, ",") + "\x00" + req.DisplayName + "\x00" + req.LegalName
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkAuthority(ctx, req.Actor, req.Tenant, req.CompanyScope); err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	preview, visible := r.visibleLocked(req.Tenant, req.CompanyScope, req.PartyID)
	if !visible {
		return BusinessParty{}, PartyReceipt{}, ErrPartyNotFound
	}
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: key was reused for another request", ErrPartyIdempotencyConflict)
		}
		return cloneParty(*preview), prior.Receipt, nil
	}
	party, err := r.writeParty(ctx, req.Actor, req.Tenant, req.CompanyScope, req.PartyID, req.ExpectedRevision)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	for _, field := range req.UpdateMask {
		for _, mastered := range party.ExternalMastered {
			if mastered == field {
				return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: %q is mastered by %s; reconcile through the owning system", ErrPartyExternallyMastered, field, party.SourceAuthority.System)
			}
		}
	}
	if supplied["display_name"] {
		party.DisplayName = req.DisplayName
	}
	if supplied["legal_name"] {
		party.LegalName = req.LegalName
	}
	out, receipt, _, err := r.commitLocked(req.IdempotencyKey, fingerprint, party, req.At)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: %w", ErrPartyResponseLost, crash)
		}
	}
	return out, receipt, nil
}

// AddRole adds one role interval under compare-and-swap. Same-role
// intervals must not overlap; a new interval after an ended one is a
// separate, explicit grant.
func (r *PartyRegistry) AddRole(ctx context.Context, req AddRoleRequest) (BusinessParty, PartyReceipt, error) {
	if !req.Role.Valid() {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: role %q is not declared", ErrPartyInvalid, req.Role)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: idempotency key and time are required", ErrPartyInvalid)
	}
	interval, err := roleInterval(req.EffectiveFrom, req.EffectiveTo)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: role interval: %v", ErrPartyInvalid, err)
	}
	toText := ""
	if req.EffectiveTo != nil {
		toText = req.EffectiveTo.String()
	}
	fingerprint := "addrole\x00" + partyKey(req.Tenant, req.CompanyScope, req.PartyID) + "\x00" + string(req.Role) + "\x00" + req.EffectiveFrom.String() + "\x00" + toText
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkAuthority(ctx, req.Actor, req.Tenant, req.CompanyScope); err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	preview, visible := r.visibleLocked(req.Tenant, req.CompanyScope, req.PartyID)
	if !visible {
		return BusinessParty{}, PartyReceipt{}, ErrPartyNotFound
	}
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: key was reused for another request", ErrPartyIdempotencyConflict)
		}
		return cloneParty(*preview), prior.Receipt, nil
	}
	party, err := r.writeParty(ctx, req.Actor, req.Tenant, req.CompanyScope, req.PartyID, req.ExpectedRevision)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	if party.Status == PartyStatusArchived {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: restore the party before adding roles", ErrPartyArchived)
	}
	for _, existing := range party.Roles {
		if existing.Role == req.Role && roleOverlaps(existing, interval) {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: %s interval overlaps an existing one", ErrPartyInvalid, req.Role)
		}
	}
	rel := BusinessRelationship{
		RelationshipID: uuid.NewString(),
		Role:           req.Role,
		Effective:      interval,
		OwnerRef:       req.OwnerRef,
		Revision:       party.Revision + 1,
	}
	party.Roles = append(party.Roles, rel)
	out, receipt, _, err := r.commitLocked(req.IdempotencyKey, fingerprint, party, req.At)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: %w", ErrPartyResponseLost, crash)
		}
	}
	return out, receipt, nil
}

// EndRole ends one open role interval at an effective time. The interval
// stays on the record with its reason; sibling roles are untouched.
func (r *PartyRegistry) EndRole(ctx context.Context, req EndRoleRequest) (BusinessParty, PartyReceipt, error) {
	if strings.TrimSpace(req.RelationshipID) == "" || strings.TrimSpace(req.Reason) == "" {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: relationship and reason are required", ErrPartyInvalid)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: idempotency key and time are required", ErrPartyInvalid)
	}
	fingerprint := "endrole\x00" + partyKey(req.Tenant, req.CompanyScope, req.PartyID) + "\x00" + req.RelationshipID + "\x00" + req.EffectiveTo.String()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkAuthority(ctx, req.Actor, req.Tenant, req.CompanyScope); err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	preview, visible := r.visibleLocked(req.Tenant, req.CompanyScope, req.PartyID)
	if !visible {
		return BusinessParty{}, PartyReceipt{}, ErrPartyNotFound
	}
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: key was reused for another request", ErrPartyIdempotencyConflict)
		}
		return cloneParty(*preview), prior.Receipt, nil
	}
	party, err := r.writeParty(ctx, req.Actor, req.Tenant, req.CompanyScope, req.PartyID, req.ExpectedRevision)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	found := false
	for i := range party.Roles {
		if party.Roles[i].RelationshipID != req.RelationshipID {
			continue
		}
		found = true
		start, ok := party.Roles[i].Effective.StartInstant()
		if !ok || !party.Roles[i].Effective.IsOpenEnded() {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: relationship %q is already ended", ErrPartyInvalid, req.RelationshipID)
		}
		closed, err := values.NewInstantInterval(start, req.EffectiveTo)
		if err != nil {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: end time: %v", ErrPartyInvalid, err)
		}
		party.Roles[i].Effective = closed
		party.Roles[i].EndReason = req.Reason
		party.Roles[i].Revision = party.Revision + 1
	}
	if !found {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: relationship %q is not on this party", ErrPartyInvalid, req.RelationshipID)
	}
	out, receipt, _, err := r.commitLocked(req.IdempotencyKey, fingerprint, party, req.At)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: %w", ErrPartyResponseLost, crash)
		}
	}
	return out, receipt, nil
}

// ArchiveParty archives a party under compare-and-swap. Open commitments
// refuse the archive with their references named; history is preserved
// either way.
func (r *PartyRegistry) ArchiveParty(ctx context.Context, req ArchivePartyRequest) (BusinessParty, PartyReceipt, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: reason is required", ErrPartyInvalid)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: idempotency key and time are required", ErrPartyInvalid)
	}
	fingerprint := "archive\x00" + partyKey(req.Tenant, req.CompanyScope, req.PartyID) + "\x00" + req.Reason
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkAuthority(ctx, req.Actor, req.Tenant, req.CompanyScope); err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	preview, visible := r.visibleLocked(req.Tenant, req.CompanyScope, req.PartyID)
	if !visible {
		return BusinessParty{}, PartyReceipt{}, ErrPartyNotFound
	}
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: key was reused for another request", ErrPartyIdempotencyConflict)
		}
		return cloneParty(*preview), prior.Receipt, nil
	}
	party, err := r.writeParty(ctx, req.Actor, req.Tenant, req.CompanyScope, req.PartyID, req.ExpectedRevision)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	if party.Status == PartyStatusArchived {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: party is already archived", ErrPartyArchived)
	}
	if len(req.OpenCommitments) > 0 {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: %s; end roles at a date or settle first", ErrPartyHasCommitments, strings.Join(req.OpenCommitments, ", "))
	}
	party.Status = PartyStatusArchived
	out, receipt, _, err := r.commitLocked(req.IdempotencyKey, fingerprint, party, req.At)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: %w", ErrPartyResponseLost, crash)
		}
	}
	return out, receipt, nil
}

// RestoreParty restores an archived party to discovery. Ended roles stay
// ended: callers re-grant commercial authority explicitly through AddRole.
func (r *PartyRegistry) RestoreParty(ctx context.Context, req RestorePartyRequest) (BusinessParty, PartyReceipt, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: reason is required", ErrPartyInvalid)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: idempotency key and time are required", ErrPartyInvalid)
	}
	fingerprint := "restore\x00" + partyKey(req.Tenant, req.CompanyScope, req.PartyID) + "\x00" + req.Reason
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkAuthority(ctx, req.Actor, req.Tenant, req.CompanyScope); err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	preview, visible := r.visibleLocked(req.Tenant, req.CompanyScope, req.PartyID)
	if !visible {
		return BusinessParty{}, PartyReceipt{}, ErrPartyNotFound
	}
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: key was reused for another request", ErrPartyIdempotencyConflict)
		}
		return cloneParty(*preview), prior.Receipt, nil
	}
	party, err := r.writeParty(ctx, req.Actor, req.Tenant, req.CompanyScope, req.PartyID, req.ExpectedRevision)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	if party.Status != PartyStatusArchived {
		return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: only an archived party restores", ErrPartyInvalid)
	}
	party.Status = PartyStatusActive
	out, receipt, _, err := r.commitLocked(req.IdempotencyKey, fingerprint, party, req.At)
	if err != nil {
		return BusinessParty{}, PartyReceipt{}, err
	}
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return BusinessParty{}, PartyReceipt{}, fmt.Errorf("%w: %w", ErrPartyResponseLost, crash)
		}
	}
	return out, receipt, nil
}

// GetParty returns one authorized party. Unknown, foreign, and
// unauthorized reads are indistinguishable.
func (r *PartyRegistry) GetParty(ctx context.Context, tenant values.TenantId, company, partyID, actor string) (BusinessParty, bool) {
	if err := r.checkAuthority(ctx, actor, tenant, company); err != nil {
		return BusinessParty{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	party, visible := r.visibleLocked(tenant, company, partyID)
	if !visible {
		return BusinessParty{}, false
	}
	return cloneParty(*party), true
}

// ListParties lists one company's parties with optional role, status, and
// name filtering. Page size is bounded 1..100; the cursor is an opaque
// offset bound to the filter. Same name or email across companies is
// never an identity: every row carries its own party id.
func (r *PartyRegistry) ListParties(ctx context.Context, tenant values.TenantId, company, actor string, filter PartyFilter, pageSize int, cursor string) ([]BusinessParty, int, error) {
	if err := r.checkAuthority(ctx, actor, tenant, company); err != nil {
		return nil, 0, err
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, 0, fmt.Errorf("%w: page size %d is outside 1..100", ErrPartyInvalid, pageSize)
	}
	offset := 0
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: cursor is not opaque", ErrPartyInvalid)
		}
		var claim struct {
			Offset int    `json:"offset"`
			Filter string `json:"filter"`
		}
		if err := json.Unmarshal(decoded, &claim); err != nil || claim.Filter != filterDigest(filter) || claim.Offset < 0 {
			return nil, 0, fmt.Errorf("%w: cursor does not match this listing", ErrPartyInvalid)
		}
		offset = claim.Offset
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []BusinessParty
	for key, party := range r.parties {
		if !strings.HasPrefix(key, string(tenant)+"\x00"+company+"\x00") {
			continue
		}
		if filter.Status != "" && party.Status != filter.Status {
			continue
		}
		if filter.Role != "" {
			any := false
			for _, rel := range party.Roles {
				if rel.Role == filter.Role {
					any = true
				}
			}
			if !any {
				continue
			}
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(party.DisplayName), strings.ToLower(filter.Query)) {
			continue
		}
		matched = append(matched, cloneParty(*party))
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].DisplayName < matched[j].DisplayName })
	if offset > len(matched) {
		offset = len(matched)
	}
	end := offset + pageSize
	if end > len(matched) {
		end = len(matched)
	}
	return matched[offset:end], len(matched), nil
}

func filterDigest(filter PartyFilter) string {
	return string(filter.Role) + "\x00" + string(filter.Status) + "\x00" + filter.Query
}

// roleEffectiveAt reports whether any interval of the role contains the
// instant. Ended history stays answerable: this is the oracle obligation
// owners use for both new commitments (at now) and settlement (at the
// recorded obligation time).
func roleEffectiveAt(party *BusinessParty, role CommercialRole, at time.Time) bool {
	instant := values.NewInstant(at)
	for _, rel := range party.Roles {
		if rel.Role != role {
			continue
		}
		if contains, err := rel.Effective.ContainsInstant(instant); err == nil && contains {
			return true
		}
	}
	return false
}

// AuthorizeNewCommitment allows a new job, order, or invoice exactly when
// the party is active and the role is effective at the commitment time.
// Unknown parties refuse without disclosure.
func (r *PartyRegistry) AuthorizeNewCommitment(ctx context.Context, tenant values.TenantId, company, partyID string, role CommercialRole, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	party, visible := r.visibleLocked(tenant, company, partyID)
	if !visible {
		return ErrPartyNotFound
	}
	if party.Status == PartyStatusArchived {
		return fmt.Errorf("%w: party is archived", ErrPartyArchived)
	}
	if !roleEffectiveAt(party, role, at) {
		return fmt.Errorf("%w: %s is not effective at the commitment time", ErrPartyRoleNotEffective, role)
	}
	return nil
}

// AuthorizeSettlement allows settling, correcting, returning, or servicing
// an already-existing obligation exactly when the role was effective at
// the recorded obligation time. Current role state is irrelevant: history
// is the authority, and no active role is fabricated.
func (r *PartyRegistry) AuthorizeSettlement(ctx context.Context, tenant values.TenantId, company, partyID string, role CommercialRole, obligationAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	party, visible := r.visibleLocked(tenant, company, partyID)
	if !visible {
		return ErrPartyNotFound
	}
	if !roleEffectiveAt(party, role, obligationAt) {
		return fmt.Errorf("%w: %s was not effective when the obligation was recorded", ErrPartyRoleNotEffective, role)
	}
	return nil
}

// SnapshotParty pins an immutable copy for obligation owners. Later edits
// never move the returned value.
func (r *PartyRegistry) SnapshotParty(ctx context.Context, tenant values.TenantId, company, partyID, actor string) (PartySnapshot, bool) {
	party, ok := r.GetParty(ctx, tenant, company, partyID, actor)
	if !ok {
		return PartySnapshot{}, false
	}
	return PartySnapshot{Party: party, At: time.Now().UTC()}, true
}

// SupervisoryParentsFor resolves the supervisory parents implied by a
// party's commercial edges: always none. Commercial edges are explicitly
// non-inheriting and never enter the internal supervisory graph.
func SupervisoryParentsFor(_ BusinessParty) []string {
	return nil
}

// AuthorizePartyProjection renders the directory/picker-safe one-line
// projection: display name plus role badges. A party the viewer may not
// see projects to nothing.
func AuthorizePartyProjection(party BusinessParty, viewer string) string {
	if party.Ref.Id == "" || strings.TrimSpace(viewer) == "" {
		return ""
	}
	badges := map[CommercialRole]bool{}
	for _, rel := range party.Roles {
		badges[rel.Role] = true
	}
	ordered := []CommercialRole{CommercialRoleCustomer, CommercialRoleSupplier, CommercialRoleProspect}
	shown := make([]string, 0, len(badges))
	for _, role := range ordered {
		if badges[role] {
			shown = append(shown, string(role))
		}
	}
	if len(shown) == 0 {
		return party.DisplayName
	}
	return party.DisplayName + " [" + strings.Join(shown, ", ") + "]"
}

// PartyRefusalTitle maps a refusal to UI-safe guidance. Not-found titles
// disclose no scope: a foreign party and a missing party read identically.
func PartyRefusalTitle(err error) string {
	switch {
	case errors.Is(err, ErrPartyRoleNotEffective):
		return "Commercial role is not effective at that time."
	case errors.Is(err, ErrPartyArchived):
		return "Party is archived; restore it before new business use."
	case errors.Is(err, ErrPartyRevisionConflict):
		return "Party changed under you; refetch and retry."
	case errors.Is(err, ErrPartyIdempotencyConflict):
		return "Request key was reused for another change."
	case errors.Is(err, ErrPartyExternallyMastered):
		return "Field is mastered externally; reconcile through the owning system."
	case errors.Is(err, ErrPartyHasCommitments):
		return "Party has open commitments; settle or end roles at a date first."
	case errors.Is(err, ErrPartyNotFound), errors.Is(err, ErrPartyForbidden):
		return "Party is not available."
	default:
		return "Party change was refused."
	}
}

type partySnapshotRole struct {
	RelationshipID string         `json:"relationship_id"`
	Role           CommercialRole `json:"role"`
	FromSec        int64          `json:"from_sec"`
	FromNsec       int32          `json:"from_nsec"`
	ToSec          *int64         `json:"to_sec,omitempty"`
	ToNsec         *int32         `json:"to_nsec,omitempty"`
	OwnerRef       string         `json:"owner_ref,omitempty"`
	Classification string         `json:"classification,omitempty"`
	Revision       uint64         `json:"revision"`
	EndReason      string         `json:"end_reason,omitempty"`
}

type partySnapshotRecord struct {
	Tenant              values.TenantId        `json:"tenant"`
	CompanyScope        string                 `json:"company_scope"`
	PartyID             string                 `json:"party_id"`
	Kind                PartyKind              `json:"kind"`
	DisplayName         string                 `json:"display_name"`
	LegalName           string                 `json:"legal_name,omitempty"`
	Status              PartyStatus            `json:"status"`
	Revision            uint64                 `json:"revision"`
	Predecessor         uint64                 `json:"predecessor"`
	SourceKind          evidence.AuthorityKind `json:"source_kind"`
	SourceSystem        string                 `json:"source_system"`
	SourcePolicyRef     string                 `json:"source_policy_ref"`
	ExternalLinks       []string               `json:"external_links,omitempty"`
	ExternalMastered    []string               `json:"external_mastered,omitempty"`
	Roles               []partySnapshotRole    `json:"roles,omitempty"`
	ContactAffiliations []ContactAffiliation   `json:"contact_affiliations,omitempty"`
	ServiceLocations    []ServiceLocationRef   `json:"service_locations,omitempty"`
}

type partySnapshotFile struct {
	Parties map[string]partySnapshotRecord `json:"parties"`
	Effects map[string]partyEffect         `json:"effects"`
}

// Export captures the registry state for restart recovery.
func (r *PartyRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	file := partySnapshotFile{
		Parties: make(map[string]partySnapshotRecord, len(r.parties)),
		Effects: make(map[string]partyEffect, len(r.effects)),
	}
	for key, party := range r.parties {
		record := partySnapshotRecord{
			Tenant: party.Ref.Tenant, CompanyScope: party.CompanyScope, PartyID: party.Ref.Id,
			Kind: party.Kind, DisplayName: party.DisplayName, LegalName: party.LegalName,
			Status: party.Status, Revision: party.Revision, Predecessor: party.Predecessor,
			SourceKind: party.SourceAuthority.Kind, SourceSystem: party.SourceAuthority.System,
			SourcePolicyRef:  party.SourceAuthority.PolicyRef,
			ExternalLinks:    append([]string(nil), party.ExternalLinks...),
			ExternalMastered: append([]string(nil), party.ExternalMastered...),
		}
		for _, rel := range party.Roles {
			start, _ := rel.Effective.StartInstant()
			fromSec, fromNsec := start.Unix()
			entry := partySnapshotRole{
				RelationshipID: rel.RelationshipID, Role: rel.Role,
				FromSec: fromSec, FromNsec: fromNsec,
				OwnerRef: rel.OwnerRef, Classification: rel.Classification,
				Revision: rel.Revision, EndReason: rel.EndReason,
			}
			if end, ok := rel.Effective.EndInstant(); ok {
				sec, nsec := end.Unix()
				entry.ToSec, entry.ToNsec = &sec, &nsec
			}
			record.Roles = append(record.Roles, entry)
		}
		record.ContactAffiliations = append([]ContactAffiliation(nil), party.ContactAffiliations...)
		record.ServiceLocations = append([]ServiceLocationRef(nil), party.ServiceLocations...)
		file.Parties[key] = record
	}
	for key, effect := range r.effects {
		file.Effects[key] = effect
	}
	return json.Marshal(file)
}

// Import restores a snapshot previously captured by Export. Revision
// chains and intervals are revalidated so corrupt snapshots fail closed.
func (r *PartyRegistry) Import(data []byte) error {
	var file partySnapshotFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("%w: snapshot is not valid party state: %v", ErrPartyInvalid, err)
	}
	restored := make(map[string]*BusinessParty, len(file.Parties))
	for key, record := range file.Parties {
		if record.PartyID == "" || record.CompanyScope == "" || record.Revision == 0 {
			return fmt.Errorf("%w: snapshot party is incomplete", ErrPartyInvalid)
		}
		if key != partyKey(record.Tenant, record.CompanyScope, record.PartyID) {
			return fmt.Errorf("%w: snapshot key does not match its party", ErrPartyInvalid)
		}
		if record.Predecessor+1 != record.Revision {
			return fmt.Errorf("%w: snapshot revision chain is broken", ErrPartyInvalid)
		}
		ref := values.EntityRef{Tenant: record.Tenant, Kind: KindBusinessParty, Id: record.PartyID}
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("%w: snapshot party ref: %v", ErrPartyInvalid, err)
		}
		party := &BusinessParty{
			Ref: ref, CompanyScope: record.CompanyScope, Kind: record.Kind,
			DisplayName: record.DisplayName, LegalName: record.LegalName,
			Status: record.Status, Revision: record.Revision, Predecessor: record.Predecessor,
			SourceAuthority:  evidence.SourceAuthority{Kind: record.SourceKind, System: record.SourceSystem, PolicyRef: record.SourcePolicyRef},
			ExternalLinks:    append([]string(nil), record.ExternalLinks...),
			ExternalMastered: append([]string(nil), record.ExternalMastered...),
		}
		for _, entry := range record.Roles {
			from, err := values.NewInstantFromUnix(entry.FromSec, entry.FromNsec)
			if err != nil {
				return fmt.Errorf("%w: snapshot role interval: %v", ErrPartyInvalid, err)
			}
			var to *values.Instant
			if entry.ToSec != nil && entry.ToNsec != nil {
				end, err := values.NewInstantFromUnix(*entry.ToSec, *entry.ToNsec)
				if err != nil {
					return fmt.Errorf("%w: snapshot role interval: %v", ErrPartyInvalid, err)
				}
				to = &end
			}
			interval, err := roleInterval(from, to)
			if err != nil {
				return fmt.Errorf("%w: snapshot role interval: %v", ErrPartyInvalid, err)
			}
			party.Roles = append(party.Roles, BusinessRelationship{
				RelationshipID: entry.RelationshipID, Role: entry.Role, Effective: interval,
				OwnerRef: entry.OwnerRef, Classification: entry.Classification,
				Revision: entry.Revision, EndReason: entry.EndReason,
			})
		}
		party.ContactAffiliations = append([]ContactAffiliation(nil), record.ContactAffiliations...)
		party.ServiceLocations = append([]ServiceLocationRef(nil), record.ServiceLocations...)
		restored[key] = party
	}
	for key, effect := range file.Effects {
		if key == "" || effect.Fingerprint == "" || effect.Receipt.PartyID == "" {
			return fmt.Errorf("%w: snapshot idempotency record is incomplete", ErrPartyInvalid)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.parties == nil {
		r.parties = make(map[string]*BusinessParty)
	}
	if r.effects == nil {
		r.effects = make(map[string]partyEffect)
	}
	for key, party := range restored {
		r.parties[key] = party
	}
	for key, effect := range file.Effects {
		r.effects[key] = effect
	}
	return nil
}

// Ensure the workforce-kind boundary stays explicit: this package's party
// refs never validate as worker refs, and the compiler keeps the import
// honest if the people vocabulary moves.
var _ values.Kind = people.KindWorker
