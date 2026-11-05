// Package org external participants for SMB-PORTAL-001.
//
// ExternalParticipant is a non-workforce identity admitted for business
// representation: customer and supplier contacts who act under explicit
// PartyRepresentationGrants evaluated at request time. The registry below
// owns participants, verification challenges, grants, session tokens, and
// the authorization oracle; party existence resolves through the
// business-party registry, and workforce identity, project membership,
// and entitlements stay entirely outside this vocabulary: this package
// cannot create, link, or union them.
//
// The registry is hermetic: no storage, clocks, or network. Restarts
// restore through Export/Import snapshots; durable rows, RLS, and served
// transport stay with the owning stores and edges.
package org

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrGrantInvalid             = errors.New("org: grant request is invalid")
	ErrGrantDenied              = errors.New("org: portal access is not permitted")
	ErrGrantRevoked             = errors.New("org: portal access is not available")
	ErrGrantContextUnion        = errors.New("org: workforce and external contexts never union")
	ErrGrantRevisionConflict    = errors.New("org: grant revision conflict")
	ErrGrantIdempotencyConflict = errors.New("org: grant key was reused for another request")
	ErrGrantCeiling             = errors.New("org: grant ceiling names an undeclared action")
	ErrGrantChallenge           = errors.New("org: verification challenge is not valid")
	ErrGrantIssuer              = errors.New("org: issuer is not trusted")
	ErrGrantResponseLost        = errors.New("org: grant receipt recorded but response lost")
	ErrGrantSnapshot            = errors.New("org: grant snapshot cannot be restored")
	ErrParticipantExists        = errors.New("org: issuer and subject are already admitted")
	ErrParticipantNotFound      = errors.New("org: participant is not visible")
)

// ParticipantKind names the non-workforce subject vocabulary. There is
// exactly one kind: external participants are never workforce identities,
// and no fake worker target exists.
type ParticipantKind string

const (
	ParticipantKindExternal ParticipantKind = "EXTERNAL_PARTICIPANT"
)

// ParticipantState is the external-participant lifecycle.
type ParticipantState string

const (
	ParticipantActive    ParticipantState = "ACTIVE"
	ParticipantSuspended ParticipantState = "SUSPENDED"
	ParticipantRevoked   ParticipantState = "REVOKED"
)

// RepresentationRole declares one commercial representation capacity.
type RepresentationRole string

const (
	RepresentationCustomerContact  RepresentationRole = "CUSTOMER_CONTACT"
	RepresentationCustomerApprover RepresentationRole = "CUSTOMER_APPROVER"
	RepresentationSupplierContact  RepresentationRole = "SUPPLIER_CONTACT"
	RepresentationSupplierApprover RepresentationRole = "SUPPLIER_APPROVER"
)

func (r RepresentationRole) valid() bool {
	switch r {
	case RepresentationCustomerContact, RepresentationCustomerApprover, RepresentationSupplierContact, RepresentationSupplierApprover:
		return true
	default:
		return false
	}
}

// GrantLifecycle is the representation lifecycle.
type GrantLifecycle string

const (
	GrantInvited   GrantLifecycle = "INVITED"
	GrantActive    GrantLifecycle = "ACTIVE"
	GrantSuspended GrantLifecycle = "SUSPENDED"
	GrantEnded     GrantLifecycle = "ENDED"
	GrantExpired   GrantLifecycle = "EXPIRED"
)

// Closed portal vocabularies. Actions, resources, and fields outside
// these sets cannot be granted; project membership is not expressible.
var (
	portalActions = map[string]bool{
		"read": true, "comment": true, "approve": true, "sign": true, "upload": true,
	}
	portalResources = map[string]bool{
		"party": true, "request": true, "document": true, "attachment": true,
		"subscription": true, "booking": true, "job": true,
	}
	portalFields = map[string]bool{
		"profile": true, "status": true, "content": true, "events": true, "signature": true,
	}
)

// ExternalParticipant is one admitted non-workforce identity. The subject
// stores as a digest only; raw federated identifiers never persist.
type ExternalParticipant struct {
	Tenant          values.TenantId
	ParticipantID   string
	Kind            ParticipantKind
	Issuer          string
	SubjectDigest   string
	ContactEndpoint string
	State           ParticipantState
	RevocationEpoch uint64
	Revision        uint64
	VerifiedAt      time.Time
	InvitedBy       string
}

// PartyRepresentationGrant authorizes one participant to act for one
// party within resource, action, and field ceilings. Epoch pins the
// participant revocation epoch the grant was issued under: any later
// revocation retires the grant without touching its record.
type PartyRepresentationGrant struct {
	GrantID         string
	ParticipantID   string
	Tenant          values.TenantId
	CompanyScope    string
	PartyID         string
	Role            RepresentationRole
	ResourceScope   []string
	ActionCeiling   []string
	FieldCeiling    []string
	EffectiveFrom   time.Time
	EffectiveTo     *time.Time
	ExpiresAt       time.Time
	GrantedBy       string
	Revision        uint64
	Epoch           uint64
	SourceAuthority string
	Lifecycle       GrantLifecycle
}

// Challenge is one single-use verification challenge for an endpoint.
type Challenge struct {
	ChallengeID string
	Tenant      values.TenantId
	Issuer      string
	Endpoint    string
	IssuedAt    time.Time
	ExpiresAt   time.Time
	Consumed    bool
}

// ParticipantToken is one bearer session token. Epoch pins the
// revocation epoch at issue: any later revocation voids the token.
type ParticipantToken struct {
	TokenID       string
	ParticipantID string
	Tenant        values.TenantId
	Epoch         uint64
	IssuedAt      time.Time
	ExpiresAt     time.Time
}

// ExternalContext carries exactly one principal: a workforce identity or
// an external participant token, never both and never neither.
type ExternalContext struct {
	WorkforceID      string
	ParticipantToken *ParticipantToken
}

// GrantResource names the object an authorization check guards.
type GrantResource struct {
	Kind    string
	PartyID string
	Fields  []string
}

// AuthRequest asks whether one context may perform one action.
type AuthRequest struct {
	Context  ExternalContext
	Action   string
	Resource GrantResource
	At       time.Time
}

// AllowedProjection is the authorized outcome: the granting record, the
// covered fields, and the watermark the consumer pins.
type AllowedProjection struct {
	GrantID   string
	PartyID   string
	Fields    []string
	Watermark uint64
	ExpiresAt time.Time
}

// ListScopeResult bounds list, count, and search to granted parties.
type ListScopeResult struct {
	PartyIDs []string
	Total    int
}

// DenialAudit is one sanitized refusal record: reason and opaque
// references only, never names, endpoints, or subjects.
type DenialAudit struct {
	At     time.Time
	Tenant values.TenantId
	Reason string
	Detail string
}

// AuditSink receives denial audits. A nil sink drops them.
type AuditSink interface {
	Denial(DenialAudit)
}

// InviteRequest admits one verified identity. The challenge must be
// fresh, unexpired, and unused; email text alone admits nothing.
type InviteRequest struct {
	Tenant          values.TenantId
	Issuer          string
	SubjectDigest   string
	ContactEndpoint string
	ChallengeID     string
	InvitedBy       string
	IdempotencyKey  string
	At              time.Time
}

// GrantRequest issues one representation grant in INVITED lifecycle.
type GrantRequest struct {
	ParticipantID               string
	Tenant                      values.TenantId
	CompanyScope                string
	PartyID                     string
	Role                        RepresentationRole
	ResourceScope               []string
	ActionCeiling               []string
	FieldCeiling                []string
	ExpiresAt                   time.Time
	SourceAuthority             string
	ExpectedParticipantRevision uint64
	GrantedBy                   string
	IdempotencyKey              string
	At                          time.Time
}

// RebindRequest moves one participant to a new issuer+subject. Rebinding
// is deny-by-default: it needs an explicit allow policy, an approver
// distinct from the requestor, and a provider-independent proof ref.
// Success retires prior authority by bumping the revocation epoch.
type RebindRequest struct {
	ParticipantID    string
	Tenant           values.TenantId
	NewIssuer        string
	NewSubjectDigest string
	ProofRef         string
	Approver         string
	Requestor        string
	IdempotencyKey   string
	At               time.Time
}

// AuthResult carries the resolved participant of a fresh authentication.
type AuthResult struct {
	Participant ExternalParticipant
	Fresh       bool
}

// ParticipantConfig wires the registry: trusted issuers, the denial
// audit sink, and the rebind allow policy (default deny).
type ParticipantConfig struct {
	TrustedIssuers map[string]bool
	RebindAllowed  bool
	Audit          AuditSink
}

type grantEffect struct {
	Fingerprint string `json:"fingerprint"`
	ReceiptID   string `json:"receipt_id"`
}

// ParticipantRegistry is the concurrency-safe in-memory portal port.
// AfterCommit is a fault-injection seam: when non-nil it runs after a
// receipt commits but before the response, so tests prove a lost
// response still replays without a duplicate record.
type ParticipantRegistry struct {
	mu            sync.Mutex
	parties       *PartyRegistry
	issuers       map[string]bool
	rebindAllow   bool
	audit         AuditSink
	participants  map[string]*ExternalParticipant
	bySubject     map[string]string
	grants        map[string]*PartyRepresentationGrant
	byParticipant map[string][]string
	challenges    map[string]*Challenge
	effects       map[string]grantEffect
	AfterCommit   func() error
}

// NewParticipantRegistry builds the portal port over the business-party
// registry, which validates every grant's party reference.
func NewParticipantRegistry(parties *PartyRegistry, config ParticipantConfig) *ParticipantRegistry {
	return &ParticipantRegistry{
		parties:       parties,
		issuers:       config.TrustedIssuers,
		rebindAllow:   config.RebindAllowed,
		audit:         config.Audit,
		participants:  make(map[string]*ExternalParticipant),
		bySubject:     make(map[string]string),
		grants:        make(map[string]*PartyRepresentationGrant),
		byParticipant: make(map[string][]string),
		challenges:    make(map[string]*Challenge),
		effects:       make(map[string]grantEffect),
	}
}

func participantKey(tenant values.TenantId, participantID string) string {
	return string(tenant) + "\x00" + participantID
}

func subjectKey(tenant values.TenantId, issuer, digest string) string {
	return string(tenant) + "\x00" + issuer + "\x00" + digest
}

func cloneParticipant(in ExternalParticipant) ExternalParticipant {
	return in
}

func cloneGrant(in PartyRepresentationGrant) PartyRepresentationGrant {
	in.ResourceScope = append([]string(nil), in.ResourceScope...)
	in.ActionCeiling = append([]string(nil), in.ActionCeiling...)
	in.FieldCeiling = append([]string(nil), in.FieldCeiling...)
	if in.EffectiveTo != nil {
		stamp := *in.EffectiveTo
		in.EffectiveTo = &stamp
	}
	return in
}

func (r *ParticipantRegistry) deny(tenant values.TenantId, reason, detail string, at time.Time) {
	if r.audit == nil {
		return
	}
	sum := sha256.Sum256([]byte(detail))
	r.audit.Denial(DenialAudit{At: at, Tenant: tenant, Reason: reason, Detail: hex.EncodeToString(sum[:8])})
}

// IssueChallenge issues one single-use endpoint challenge valid for ten
// minutes. Untrusted issuers get no challenge at all.
func (r *ParticipantRegistry) IssueChallenge(_ context.Context, tenant values.TenantId, issuer, endpoint string, at time.Time) (Challenge, error) {
	if err := tenant.Validate(); err != nil {
		return Challenge{}, fmt.Errorf("%w: tenant: %v", ErrGrantInvalid, err)
	}
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(endpoint) == "" || at.IsZero() {
		return Challenge{}, fmt.Errorf("%w: issuer, endpoint, and time are required", ErrGrantInvalid)
	}
	if !r.issuers[issuer] {
		r.deny(tenant, "issuer", issuer, at)
		return Challenge{}, fmt.Errorf("%w: issuer %q is not trusted", ErrGrantIssuer, issuer)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	challenge := Challenge{
		ChallengeID: uuid.NewString(), Tenant: tenant, Issuer: issuer, Endpoint: endpoint,
		IssuedAt: at, ExpiresAt: at.Add(10 * time.Minute),
	}
	r.challenges[challenge.ChallengeID] = &challenge
	return challenge, nil
}

// InviteParticipant admits one verified identity as an external
// participant. The challenge must match the endpoint and be fresh and
// unused; a duplicate issuer+subject never invites twice.
func (r *ParticipantRegistry) InviteParticipant(_ context.Context, req InviteRequest) (ExternalParticipant, error) {
	if err := req.Tenant.Validate(); err != nil {
		return ExternalParticipant{}, fmt.Errorf("%w: tenant: %v", ErrGrantInvalid, err)
	}
	if strings.TrimSpace(req.Issuer) == "" || strings.TrimSpace(req.SubjectDigest) == "" || strings.TrimSpace(req.InvitedBy) == "" {
		return ExternalParticipant{}, fmt.Errorf("%w: issuer, subject digest, and inviter are required", ErrGrantInvalid)
	}
	if strings.TrimSpace(req.ChallengeID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		r.deny(req.Tenant, "challenge", "missing", req.At)
		return ExternalParticipant{}, fmt.Errorf("%w: challenge, key, and time are required", ErrGrantChallenge)
	}
	if !r.issuers[req.Issuer] {
		r.deny(req.Tenant, "issuer", req.Issuer, req.At)
		return ExternalParticipant{}, fmt.Errorf("%w: issuer %q is not trusted", ErrGrantIssuer, req.Issuer)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fingerprint := strings.Join([]string{"invite", string(req.Tenant), req.Issuer, req.SubjectDigest, req.ChallengeID}, "\x00")
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return ExternalParticipant{}, fmt.Errorf("%w: key was reused", ErrGrantIdempotencyConflict)
		}
		for _, participant := range r.participants {
			if participant.ParticipantID == prior.ReceiptID {
				return cloneParticipant(*participant), nil
			}
		}
		return ExternalParticipant{}, ErrParticipantNotFound
	}
	challenge, ok := r.challenges[req.ChallengeID]
	if !ok || challenge.Tenant != req.Tenant || challenge.Issuer != req.Issuer || challenge.Endpoint != req.ContactEndpoint {
		r.deny(req.Tenant, "challenge", req.ChallengeID, req.At)
		return ExternalParticipant{}, fmt.Errorf("%w: challenge does not verify", ErrGrantChallenge)
	}
	if challenge.Consumed || !req.At.Before(challenge.ExpiresAt) {
		r.deny(req.Tenant, "challenge", req.ChallengeID, req.At)
		return ExternalParticipant{}, fmt.Errorf("%w: challenge is spent or expired", ErrGrantChallenge)
	}
	if _, dup := r.bySubject[subjectKey(req.Tenant, req.Issuer, req.SubjectDigest)]; dup {
		return ExternalParticipant{}, fmt.Errorf("%w: subject is already admitted", ErrParticipantExists)
	}
	participant := ExternalParticipant{
		Tenant: req.Tenant, ParticipantID: uuid.NewString(), Kind: ParticipantKindExternal,
		Issuer: req.Issuer, SubjectDigest: req.SubjectDigest, ContactEndpoint: req.ContactEndpoint,
		State: ParticipantActive, Revision: 1, VerifiedAt: req.At, InvitedBy: req.InvitedBy,
	}
	challenge.Consumed = true
	r.participants[participantKey(req.Tenant, participant.ParticipantID)] = &participant
	r.bySubject[subjectKey(req.Tenant, req.Issuer, req.SubjectDigest)] = participant.ParticipantID
	r.effects[req.IdempotencyKey] = grantEffect{Fingerprint: fingerprint, ReceiptID: participant.ParticipantID}
	out := cloneParticipant(participant)
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return ExternalParticipant{}, fmt.Errorf("%w: %w", ErrGrantResponseLost, crash)
		}
	}
	return out, nil
}

// Authenticate resolves a fresh authentication to its participant. Same
// issuer+subject resolves the same record; unknown subjects need a fresh
// invitation and untrusted issuers fail closed.
func (r *ParticipantRegistry) Authenticate(_ context.Context, tenant values.TenantId, issuer, subjectDigest string, at time.Time) (AuthResult, error) {
	if !r.issuers[issuer] {
		r.deny(tenant, "issuer", issuer, at)
		return AuthResult{}, fmt.Errorf("%w: issuer %q is not trusted", ErrGrantIssuer, issuer)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.bySubject[subjectKey(tenant, issuer, subjectDigest)]
	if !ok {
		r.deny(tenant, "unknown", issuer, at)
		return AuthResult{}, ErrParticipantNotFound
	}
	participant, ok := r.participants[participantKey(tenant, id)]
	if !ok {
		r.deny(tenant, "unknown", issuer, at)
		return AuthResult{}, ErrParticipantNotFound
	}
	switch participant.State {
	case ParticipantActive:
		return AuthResult{Participant: cloneParticipant(*participant), Fresh: true}, nil
	case ParticipantRevoked:
		r.deny(tenant, "revoked", id, at)
		return AuthResult{}, ErrGrantRevoked
	default:
		r.deny(tenant, "suspended", id, at)
		return AuthResult{}, ErrGrantDenied
	}
}

// IssueToken mints one bearer token pinned to the current revocation
// epoch. Tokens for inactive participants never issue.
func (r *ParticipantRegistry) IssueToken(participantID string, at, expiresAt time.Time) (ParticipantToken, error) {
	if at.IsZero() || !expiresAt.After(at) {
		return ParticipantToken{}, fmt.Errorf("%w: token needs a valid window", ErrGrantInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, participant := range r.participants {
		if participant.ParticipantID != participantID {
			continue
		}
		if participant.State != ParticipantActive {
			r.deny(participant.Tenant, "inactive", participantID, at)
			return ParticipantToken{}, ErrGrantRevoked
		}
		return ParticipantToken{
			TokenID: uuid.NewString(), ParticipantID: participantID, Tenant: participant.Tenant,
			Epoch: participant.RevocationEpoch, IssuedAt: at, ExpiresAt: expiresAt,
		}, nil
	}
	return ParticipantToken{}, ErrParticipantNotFound
}

// SuspendParticipant suspends one participant without retiring its epoch.
func (r *ParticipantRegistry) SuspendParticipant(_ context.Context, tenant values.TenantId, participantID, actor, key string, at time.Time) (ExternalParticipant, error) {
	return r.restateParticipant(tenant, participantID, actor, key, at, ParticipantSuspended)
}

// RevokeParticipant revokes one participant and bumps its revocation
// epoch, voiding every outstanding token and retiring every grant issued
// under an older epoch. Revocation is never reversed by replay.
func (r *ParticipantRegistry) RevokeParticipant(_ context.Context, tenant values.TenantId, participantID, actor, reason, key string, at time.Time) (ExternalParticipant, error) {
	if strings.TrimSpace(reason) == "" {
		return ExternalParticipant{}, fmt.Errorf("%w: reason is required", ErrGrantInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	participant, ok := r.participants[participantKey(tenant, participantID)]
	if !ok {
		return ExternalParticipant{}, ErrParticipantNotFound
	}
	fingerprint := strings.Join([]string{"revoke", string(tenant), participantID, reason}, "\x00")
	if prior, ok := r.effects[key]; ok {
		if prior.Fingerprint != fingerprint {
			return ExternalParticipant{}, fmt.Errorf("%w: key was reused", ErrGrantIdempotencyConflict)
		}
		return cloneParticipant(*participant), nil
	}
	participant.State = ParticipantRevoked
	participant.RevocationEpoch++
	participant.Revision++
	r.effects[key] = grantEffect{Fingerprint: fingerprint, ReceiptID: participantID}
	out := cloneParticipant(*participant)
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return ExternalParticipant{}, fmt.Errorf("%w: %w", ErrGrantResponseLost, crash)
		}
	}
	return out, nil
}

func (r *ParticipantRegistry) restateParticipant(tenant values.TenantId, participantID, actor, key string, at time.Time, state ParticipantState) (ExternalParticipant, error) {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(key) == "" || at.IsZero() {
		return ExternalParticipant{}, fmt.Errorf("%w: actor, key, and time are required", ErrGrantInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	participant, ok := r.participants[participantKey(tenant, participantID)]
	if !ok {
		return ExternalParticipant{}, ErrParticipantNotFound
	}
	if participant.State == ParticipantRevoked {
		return ExternalParticipant{}, ErrGrantRevoked
	}
	participant.State = state
	participant.Revision++
	return cloneParticipant(*participant), nil
}

// GrantRepresentation issues one INVITED grant. The party must exist in
// the grant company, ceilings must stay inside the closed vocabularies,
// and the participant revision must be current.
func (r *ParticipantRegistry) GrantRepresentation(_ context.Context, req GrantRequest) (PartyRepresentationGrant, error) {
	if err := req.Tenant.Validate(); err != nil {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: tenant: %v", ErrGrantInvalid, err)
	}
	if !req.Role.valid() {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: role %q is not declared", ErrGrantInvalid, req.Role)
	}
	if len(req.ResourceScope) == 0 || len(req.ActionCeiling) == 0 || len(req.FieldCeiling) == 0 {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: resource, action, and field ceilings are required", ErrGrantInvalid)
	}
	for _, action := range req.ActionCeiling {
		if !portalActions[action] {
			return PartyRepresentationGrant{}, fmt.Errorf("%w: action %q is not grantable", ErrGrantCeiling, action)
		}
	}
	for _, resource := range req.ResourceScope {
		if !portalResources[resource] {
			return PartyRepresentationGrant{}, fmt.Errorf("%w: resource %q is not grantable", ErrGrantCeiling, resource)
		}
	}
	for _, field := range req.FieldCeiling {
		if !portalFields[field] {
			return PartyRepresentationGrant{}, fmt.Errorf("%w: field %q is not grantable", ErrGrantCeiling, field)
		}
	}
	if strings.TrimSpace(req.CompanyScope) == "" || strings.TrimSpace(req.GrantedBy) == "" || strings.TrimSpace(req.SourceAuthority) == "" {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: company, grantor, and source authority are required", ErrGrantInvalid)
	}
	if !req.ExpiresAt.After(req.At) || req.At.IsZero() || strings.TrimSpace(req.IdempotencyKey) == "" {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: future expiry, key, and time are required", ErrGrantInvalid)
	}
	if r.parties == nil {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: no party registry is configured", ErrGrantInvalid)
	}
	if _, ok := r.parties.GetParty(context.Background(), req.Tenant, req.CompanyScope, req.PartyID, req.GrantedBy); !ok {
		r.deny(req.Tenant, "party", req.PartyID, req.At)
		return PartyRepresentationGrant{}, ErrGrantDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	participant, ok := r.participants[participantKey(req.Tenant, req.ParticipantID)]
	if !ok {
		return PartyRepresentationGrant{}, ErrParticipantNotFound
	}
	if participant.State != ParticipantActive {
		return PartyRepresentationGrant{}, ErrGrantRevoked
	}
	if participant.Revision != req.ExpectedParticipantRevision {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: writer holds %d, participant is at %d", ErrGrantRevisionConflict, req.ExpectedParticipantRevision, participant.Revision)
	}
	fingerprint := strings.Join([]string{"grant", string(req.Tenant), req.ParticipantID, req.CompanyScope, req.PartyID,
		string(req.Role), strings.Join(req.ResourceScope, ","), strings.Join(req.ActionCeiling, ","),
		strings.Join(req.FieldCeiling, ","), req.ExpiresAt.UTC().Format(time.RFC3339Nano)}, "\x00")
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.Fingerprint != fingerprint {
			return PartyRepresentationGrant{}, fmt.Errorf("%w: key was reused for another grant", ErrGrantIdempotencyConflict)
		}
		return cloneGrant(*r.grants[prior.ReceiptID]), nil
	}
	grant := PartyRepresentationGrant{
		GrantID: uuid.NewString(), ParticipantID: req.ParticipantID, Tenant: req.Tenant,
		CompanyScope: req.CompanyScope, PartyID: req.PartyID, Role: req.Role,
		ResourceScope: append([]string(nil), req.ResourceScope...),
		ActionCeiling: append([]string(nil), req.ActionCeiling...),
		FieldCeiling:  append([]string(nil), req.FieldCeiling...),
		ExpiresAt:     req.ExpiresAt, GrantedBy: req.GrantedBy, Revision: 1,
		Epoch: participant.RevocationEpoch, SourceAuthority: req.SourceAuthority, Lifecycle: GrantInvited,
	}
	r.grants[grant.GrantID] = &grant
	r.byParticipant[participantKey(req.Tenant, req.ParticipantID)] = append(r.byParticipant[participantKey(req.Tenant, req.ParticipantID)], grant.GrantID)
	r.effects[req.IdempotencyKey] = grantEffect{Fingerprint: fingerprint, ReceiptID: grant.GrantID}
	out := cloneGrant(grant)
	if r.AfterCommit != nil {
		if crash := r.AfterCommit(); crash != nil {
			return PartyRepresentationGrant{}, fmt.Errorf("%w: %w", ErrGrantResponseLost, crash)
		}
	}
	return out, nil
}

// ActivateGrant moves one INVITED grant to ACTIVE after fresh
// authentication. Activation converges: replaying it returns the grant.
func (r *ParticipantRegistry) ActivateGrant(_ context.Context, tenant values.TenantId, grantID, participantID, key string, at time.Time) (PartyRepresentationGrant, error) {
	if strings.TrimSpace(key) == "" || at.IsZero() {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: key and time are required", ErrGrantInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	grant, ok := r.grants[grantID]
	if !ok || grant.Tenant != tenant || grant.ParticipantID != participantID {
		return PartyRepresentationGrant{}, ErrGrantDenied
	}
	if grant.Lifecycle == GrantActive {
		return cloneGrant(*grant), nil
	}
	if grant.Lifecycle != GrantInvited {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: only an invited grant activates", ErrGrantInvalid)
	}
	participant, ok := r.participants[participantKey(tenant, participantID)]
	if !ok || participant.State != ParticipantActive {
		return PartyRepresentationGrant{}, ErrGrantRevoked
	}
	grant.Lifecycle = GrantActive
	grant.EffectiveFrom = at
	grant.Revision++
	return cloneGrant(*grant), nil
}

// SuspendGrant suspends one ACTIVE grant under compare-and-swap.
func (r *ParticipantRegistry) SuspendGrant(_ context.Context, tenant values.TenantId, grantID, participantID string, expectedRevision uint64, actor, key string, at time.Time) (PartyRepresentationGrant, error) {
	return r.restateGrant(tenant, grantID, participantID, expectedRevision, actor, key, at, GrantSuspended, GrantActive)
}

// EndGrant ends one grant under compare-and-swap. Ended grants never
// return: representation resumes only through a new grant revision.
func (r *ParticipantRegistry) EndGrant(_ context.Context, tenant values.TenantId, grantID, participantID string, expectedRevision uint64, actor, key string, at time.Time) (PartyRepresentationGrant, error) {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(key) == "" || at.IsZero() {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: actor, key, and time are required", ErrGrantInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	grant, ok := r.grants[grantID]
	if !ok || grant.Tenant != tenant || grant.ParticipantID != participantID {
		return PartyRepresentationGrant{}, ErrGrantDenied
	}
	if grant.Revision != expectedRevision {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: writer holds %d, grant is at %d", ErrGrantRevisionConflict, expectedRevision, grant.Revision)
	}
	if grant.Lifecycle == GrantEnded {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: grant is already ended", ErrGrantInvalid)
	}
	grant.Lifecycle = GrantEnded
	stamp := at
	grant.EffectiveTo = &stamp
	grant.Revision++
	return cloneGrant(*grant), nil
}

func (r *ParticipantRegistry) restateGrant(tenant values.TenantId, grantID, participantID string, expectedRevision uint64, actor, key string, at time.Time, to, from GrantLifecycle) (PartyRepresentationGrant, error) {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(key) == "" || at.IsZero() {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: actor, key, and time are required", ErrGrantInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	grant, ok := r.grants[grantID]
	if !ok || grant.Tenant != tenant || grant.ParticipantID != participantID {
		return PartyRepresentationGrant{}, ErrGrantDenied
	}
	if grant.Revision != expectedRevision {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: writer holds %d, grant is at %d", ErrGrantRevisionConflict, expectedRevision, grant.Revision)
	}
	if grant.Lifecycle != from {
		return PartyRepresentationGrant{}, fmt.Errorf("%w: grant is %s", ErrGrantInvalid, grant.Lifecycle)
	}
	grant.Lifecycle = to
	grant.Revision++
	return cloneGrant(*grant), nil
}

// Rebind moves one participant to a new issuer+subject under an explicit
// allow policy with a distinct approver and proof ref. Success bumps the
// revocation epoch, retiring all prior authority; nothing is ever
// silently mapped.
func (r *ParticipantRegistry) Rebind(_ context.Context, req RebindRequest) (ExternalParticipant, error) {
	if err := req.Tenant.Validate(); err != nil {
		return ExternalParticipant{}, fmt.Errorf("%w: tenant: %v", ErrGrantInvalid, err)
	}
	if strings.TrimSpace(req.NewIssuer) == "" || strings.TrimSpace(req.NewSubjectDigest) == "" || strings.TrimSpace(req.ProofRef) == "" {
		return ExternalParticipant{}, fmt.Errorf("%w: new issuer, subject, and proof are required", ErrGrantInvalid)
	}
	if strings.TrimSpace(req.Approver) == "" || req.Approver == req.Requestor {
		r.deny(req.Tenant, "rebind", req.ParticipantID, req.At)
		return ExternalParticipant{}, fmt.Errorf("%w: rebind needs an approver distinct from the requestor", ErrGrantDenied)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return ExternalParticipant{}, fmt.Errorf("%w: key and time are required", ErrGrantInvalid)
	}
	if !r.issuers[req.NewIssuer] {
		r.deny(req.Tenant, "issuer", req.NewIssuer, req.At)
		return ExternalParticipant{}, fmt.Errorf("%w: issuer %q is not trusted", ErrGrantIssuer, req.NewIssuer)
	}
	if !r.rebindAllow {
		r.deny(req.Tenant, "rebind", req.ParticipantID, req.At)
		return ExternalParticipant{}, fmt.Errorf("%w: rebind policy does not allow this move", ErrGrantDenied)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	participant, ok := r.participants[participantKey(req.Tenant, req.ParticipantID)]
	if !ok {
		r.deny(req.Tenant, "rebind", req.ParticipantID, req.At)
		return ExternalParticipant{}, ErrGrantDenied
	}
	if _, dup := r.bySubject[subjectKey(req.Tenant, req.NewIssuer, req.NewSubjectDigest)]; dup {
		return ExternalParticipant{}, fmt.Errorf("%w: subject is already admitted", ErrParticipantExists)
	}
	delete(r.bySubject, subjectKey(req.Tenant, participant.Issuer, participant.SubjectDigest))
	participant.Issuer = req.NewIssuer
	participant.SubjectDigest = req.NewSubjectDigest
	participant.RevocationEpoch++
	participant.Revision++
	r.bySubject[subjectKey(req.Tenant, req.NewIssuer, req.NewSubjectDigest)] = req.ParticipantID
	return cloneParticipant(*participant), nil
}

// Authorize evaluates one action against live participant, token, and
// grant state. Every refusal is sanitized and audited; zero domain
// effects accompany any denial.
func (r *ParticipantRegistry) Authorize(_ context.Context, req AuthRequest) (AllowedProjection, error) {
	if req.At.IsZero() {
		return AllowedProjection{}, fmt.Errorf("%w: time is required", ErrGrantInvalid)
	}
	token := req.Context.ParticipantToken
	workforce := strings.TrimSpace(req.Context.WorkforceID) != ""
	switch {
	case token != nil && workforce:
		return AllowedProjection{}, ErrGrantContextUnion
	case token == nil:
		return AllowedProjection{}, ErrGrantDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	participant, ok := r.participants[participantKey(token.Tenant, token.ParticipantID)]
	if !ok {
		r.deny(token.Tenant, "unknown", token.ParticipantID, req.At)
		return AllowedProjection{}, ErrGrantDenied
	}
	if participant.State == ParticipantRevoked {
		r.deny(token.Tenant, "revoked", token.ParticipantID, req.At)
		return AllowedProjection{}, ErrGrantRevoked
	}
	if participant.State != ParticipantActive {
		r.deny(token.Tenant, "suspended", token.ParticipantID, req.At)
		return AllowedProjection{}, ErrGrantDenied
	}
	if token.Epoch != participant.RevocationEpoch || !req.At.Before(token.ExpiresAt) {
		r.deny(token.Tenant, "stale", token.ParticipantID, req.At)
		return AllowedProjection{}, ErrGrantRevoked
	}
	for _, id := range r.byParticipant[participantKey(token.Tenant, token.ParticipantID)] {
		grant := r.grants[id]
		if grant.Lifecycle != GrantActive || grant.Epoch != participant.RevocationEpoch {
			continue
		}
		if !req.At.Before(grant.ExpiresAt) || req.At.Before(grant.EffectiveFrom) {
			continue
		}
		if grant.EffectiveTo != nil && !req.At.Before(*grant.EffectiveTo) {
			continue
		}
		if grant.PartyID != req.Resource.PartyID || !containsString(grant.ResourceScope, req.Resource.Kind) {
			continue
		}
		if !containsString(grant.ActionCeiling, req.Action) {
			continue
		}
		covered := true
		for _, field := range req.Resource.Fields {
			if !containsString(grant.FieldCeiling, field) {
				covered = false
			}
		}
		if !covered {
			continue
		}
		return AllowedProjection{
			GrantID: grant.GrantID, PartyID: grant.PartyID,
			Fields:    append([]string(nil), req.Resource.Fields...),
			Watermark: grant.Revision, ExpiresAt: grant.ExpiresAt,
		}, nil
	}
	r.deny(token.Tenant, "denied", token.ParticipantID, req.At)
	return AllowedProjection{}, ErrGrantDenied
}

// ListScope bounds list, count, and search to the caller's live grants
// in one company. Counts never include ungranted parties.
func (r *ParticipantRegistry) ListScope(_ context.Context, ctx ExternalContext, tenant values.TenantId, company string, at time.Time) (ListScopeResult, error) {
	token := ctx.ParticipantToken
	if token == nil || strings.TrimSpace(ctx.WorkforceID) != "" {
		return ListScopeResult{}, ErrGrantDenied
	}
	if at.IsZero() {
		return ListScopeResult{}, fmt.Errorf("%w: time is required", ErrGrantInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	participant, ok := r.participants[participantKey(token.Tenant, token.ParticipantID)]
	if !ok || token.Tenant != tenant || participant.State != ParticipantActive {
		return ListScopeResult{}, ErrGrantDenied
	}
	if token.Epoch != participant.RevocationEpoch || !at.Before(token.ExpiresAt) {
		return ListScopeResult{}, ErrGrantRevoked
	}
	seen := make(map[string]bool)
	var ids []string
	for _, id := range r.byParticipant[participantKey(token.Tenant, token.ParticipantID)] {
		grant := r.grants[id]
		if grant.Lifecycle != GrantActive || grant.Epoch != participant.RevocationEpoch || grant.CompanyScope != company {
			continue
		}
		if !at.Before(grant.ExpiresAt) || at.Before(grant.EffectiveFrom) {
			continue
		}
		if grant.EffectiveTo != nil && !at.Before(*grant.EffectiveTo) {
			continue
		}
		if !seen[grant.PartyID] {
			seen[grant.PartyID] = true
			ids = append(ids, grant.PartyID)
		}
	}
	sort.Strings(ids)
	return ListScopeResult{PartyIDs: ids, Total: len(ids)}, nil
}

// GrantsForTesting lists one participant's grants for assertions.
func (r *ParticipantRegistry) GrantsForTesting(tenant values.TenantId, participantID string) []PartyRepresentationGrant {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []PartyRepresentationGrant
	for _, id := range r.byParticipant[participantKey(tenant, participantID)] {
		out = append(out, cloneGrant(*r.grants[id]))
	}
	return out
}

func containsString(set []string, want string) bool {
	for _, item := range set {
		if item == want {
			return true
		}
	}
	return false
}

// AuthorizeGrantProjection renders the authorized projection descriptor:
// grant id, party id, covered fields, and watermark. It carries no
// identity: names and endpoints never enter a projection.
func AuthorizeGrantProjection(allowed AllowedProjection, grant PartyRepresentationGrant) string {
	return fmt.Sprintf("grant %s permits party %s fields [%s] at revision %d",
		allowed.GrantID, allowed.PartyID, strings.Join(allowed.Fields, ","), grant.Revision)
}

// GrantRefusalTitle maps a refusal to UI-safe guidance. Revocation and
// absence read identically neutral: no caller learns which applied.
func GrantRefusalTitle(err error) string {
	switch {
	case errors.Is(err, ErrGrantContextUnion):
		return "Sign in with one identity at a time."
	case errors.Is(err, ErrGrantRevisionConflict):
		return "Record changed under you; refetch and retry."
	case errors.Is(err, ErrGrantIdempotencyConflict):
		return "Request key was reused for another change."
	case errors.Is(err, ErrGrantCeiling):
		return "Requested permission is not grantable."
	case errors.Is(err, ErrGrantChallenge), errors.Is(err, ErrGrantIssuer):
		return "Verification did not complete; try a fresh sign-in."
	case errors.Is(err, ErrParticipantExists):
		return "Identity is already admitted."
	case errors.Is(err, ErrGrantDenied), errors.Is(err, ErrGrantRevoked), errors.Is(err, ErrParticipantNotFound):
		return "Access is not permitted."
	default:
		return "Request was refused."
	}
}

type grantSnapshot struct {
	Participants  map[string]ExternalParticipant      `json:"participants"`
	BySubject     map[string]string                   `json:"by_subject"`
	Grants        map[string]PartyRepresentationGrant `json:"grants"`
	ByParticipant map[string][]string                 `json:"by_participant"`
	Challenges    map[string]Challenge                `json:"challenges"`
	Effects       map[string]grantEffect              `json:"effects"`
}

// Export captures the registry for restart recovery.
func (r *ParticipantRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := grantSnapshot{
		Participants:  make(map[string]ExternalParticipant, len(r.participants)),
		BySubject:     make(map[string]string, len(r.bySubject)),
		Grants:        make(map[string]PartyRepresentationGrant, len(r.grants)),
		ByParticipant: make(map[string][]string, len(r.byParticipant)),
		Challenges:    make(map[string]Challenge, len(r.challenges)),
		Effects:       make(map[string]grantEffect, len(r.effects)),
	}
	for key, participant := range r.participants {
		snapshot.Participants[key] = cloneParticipant(*participant)
	}
	for key, id := range r.bySubject {
		snapshot.BySubject[key] = id
	}
	for id, grant := range r.grants {
		snapshot.Grants[id] = cloneGrant(*grant)
	}
	for key, ids := range r.byParticipant {
		snapshot.ByParticipant[key] = append([]string(nil), ids...)
	}
	for id, challenge := range r.challenges {
		snapshot.Challenges[id] = *challenge
	}
	for key, effect := range r.effects {
		snapshot.Effects[key] = effect
	}
	return json.Marshal(snapshot)
}

// Import restores a snapshot previously captured by Export. Keys,
// revisions, and epochs revalidate so corrupt snapshots fail closed.
func (r *ParticipantRegistry) Import(data []byte) error {
	var snapshot grantSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("%w: snapshot is not valid grant state: %v", ErrGrantSnapshot, err)
	}
	for key, participant := range snapshot.Participants {
		if participant.ParticipantID == "" || participant.Revision == 0 {
			return fmt.Errorf("%w: snapshot participant is incomplete", ErrGrantSnapshot)
		}
		if key != participantKey(participant.Tenant, participant.ParticipantID) {
			return fmt.Errorf("%w: snapshot key does not match its participant", ErrGrantSnapshot)
		}
		switch participant.State {
		case ParticipantActive, ParticipantSuspended, ParticipantRevoked:
		default:
			return fmt.Errorf("%w: snapshot state %q is not declared", ErrGrantSnapshot, participant.State)
		}
	}
	for id, grant := range snapshot.Grants {
		if id == "" || grant.GrantID == "" || grant.Revision == 0 {
			return fmt.Errorf("%w: snapshot grant is incomplete", ErrGrantSnapshot)
		}
		switch grant.Lifecycle {
		case GrantInvited, GrantActive, GrantSuspended, GrantEnded, GrantExpired:
		default:
			return fmt.Errorf("%w: snapshot lifecycle %q is not declared", ErrGrantSnapshot, grant.Lifecycle)
		}
	}
	for key, effect := range snapshot.Effects {
		if key == "" || effect.Fingerprint == "" || effect.ReceiptID == "" {
			return fmt.Errorf("%w: snapshot idempotency record is incomplete", ErrGrantSnapshot)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.participants == nil {
		r.participants = make(map[string]*ExternalParticipant)
	}
	if r.bySubject == nil {
		r.bySubject = make(map[string]string)
	}
	if r.grants == nil {
		r.grants = make(map[string]*PartyRepresentationGrant)
	}
	if r.byParticipant == nil {
		r.byParticipant = make(map[string][]string)
	}
	if r.challenges == nil {
		r.challenges = make(map[string]*Challenge)
	}
	if r.effects == nil {
		r.effects = make(map[string]grantEffect)
	}
	for key, participant := range snapshot.Participants {
		stored := participant
		r.participants[key] = &stored
	}
	for key, id := range snapshot.BySubject {
		r.bySubject[key] = id
	}
	for id, grant := range snapshot.Grants {
		stored := grant
		r.grants[id] = &stored
	}
	for key, ids := range snapshot.ByParticipant {
		r.byParticipant[key] = append([]string(nil), ids...)
	}
	for id, challenge := range snapshot.Challenges {
		stored := challenge
		r.challenges[id] = &stored
	}
	for key, effect := range snapshot.Effects {
		r.effects[key] = effect
	}
	return nil
}
