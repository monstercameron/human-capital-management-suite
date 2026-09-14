// Package jit implements time-bounded, just-in-time operator access grants:
// the only door through which an operator can act with elevated capability
// outside their ordinary role. There is no standing grant path -- every
// grant is bound to a ticket or incident reference, a role drawn from a
// closed vocabulary, an approver distinct from the requester, and a
// time-to-live that can never exceed that role's hard maximum. A grant
// expires on its own once its TTL elapses, can be revoked early, and every
// grant, use, expiry and revocation is captured as evidence on the [Grant]
// itself.
package jit

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Role is the closed vocabulary of operator roles a JIT grant can carry. A
// caller cannot mint a new role at request time: [Request.Role] is checked
// against this fixed set, and each role in the set carries its own hard TTL
// ceiling in [maxTTLByRole].
type Role string

// The closed set of JIT operator roles.
const (
	RoleSupportReadOnly   Role = "SUPPORT_READ_ONLY"
	RoleIncidentResponder Role = "INCIDENT_RESPONDER"
	RoleIntegrityRepair   Role = "INTEGRITY_REPAIR"
	RolePayrollEmergency  Role = "PAYROLL_EMERGENCY"
	RoleAccessRevocation  Role = "ACCESS_REVOCATION"
)

// maxTTLByRole is the hard, per-role TTL ceiling. A request can ask for less
// but never more; there is deliberately no role whose ceiling is unbounded,
// because an unbounded ceiling is a standing grant in disguise.
var maxTTLByRole = map[Role]time.Duration{
	RoleSupportReadOnly:   4 * time.Hour,
	RoleIncidentResponder: 8 * time.Hour,
	RoleIntegrityRepair:   4 * time.Hour,
	RolePayrollEmergency:  2 * time.Hour,
	RoleAccessRevocation:  2 * time.Hour,
}

// MaxTTL returns the hard TTL ceiling for r and whether r is a recognized
// role at all.
func (r Role) MaxTTL() (time.Duration, bool) {
	d, ok := maxTTLByRole[r]
	return d, ok
}

func (r Role) valid() bool {
	_, ok := maxTTLByRole[r]
	return ok
}

// Errors returned while requesting or using a grant. All are matchable with
// errors.Is.
var (
	ErrInvalidRequest      = errors.New("jit: invalid grant request")
	ErrUnknownRole         = errors.New("jit: role is not in the closed vocabulary")
	ErrApproverIsRequester = errors.New("jit: approver must be distinct from the requester")
	ErrTTLRequired         = errors.New("jit: a positive, bounded ttl is required; there is no standing grant path")
	ErrTTLExceedsMax       = errors.New("jit: requested ttl exceeds the role's hard maximum")
	ErrGrantExpired        = errors.New("jit: grant has expired")
	ErrGrantRevoked        = errors.New("jit: grant has been revoked")
)

// Request is a request for a time-bounded operator grant.
type Request struct {
	// Principal is the requesting operator. It must be distinct from the
	// approver that later signs off on the grant.
	Principal string
	// Tenant is the tenant the grant is scoped to.
	Tenant values.TenantId
	// Role is drawn from the closed [Role] vocabulary.
	Role Role
	// TicketRef names the incident or change ticket that justifies the
	// grant. Never empty.
	TicketRef string
	// Justification is the human-readable reason for the grant. Never empty.
	Justification string
	// Capabilities are the specific capabilities the grant authorizes. Never
	// empty: a grant authorizes named capabilities, not "whatever the role
	// can do".
	Capabilities []string
	// Fields optionally narrows the grant to specific data fields. May be
	// empty when the role's capabilities are not field-scoped.
	Fields []string
	// Purpose is the purpose of processing under this grant. Never empty.
	Purpose string
	// TTL is the requested lifetime. It must be positive and at most the
	// role's [Role.MaxTTL]; there is no default and no way to omit it.
	TTL time.Duration
}

func (r Request) validate() error {
	if strings.TrimSpace(r.Principal) == "" {
		return fmt.Errorf("%w: principal is required", ErrInvalidRequest)
	}
	if r.Tenant.Validate() != nil {
		return fmt.Errorf("%w: tenant is required", ErrInvalidRequest)
	}
	if !r.Role.valid() {
		return fmt.Errorf("%w: %q", ErrUnknownRole, r.Role)
	}
	if strings.TrimSpace(r.TicketRef) == "" {
		return fmt.Errorf("%w: ticket/incident reference is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(r.Justification) == "" {
		return fmt.Errorf("%w: justification is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(r.Purpose) == "" {
		return fmt.Errorf("%w: purpose is required", ErrInvalidRequest)
	}
	if len(r.Capabilities) == 0 {
		return fmt.Errorf("%w: at least one capability is required", ErrInvalidRequest)
	}
	for _, c := range r.Capabilities {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("%w: empty capability", ErrInvalidRequest)
		}
	}
	return nil
}

// Approval is the distinct approver's sign-off on a [Request].
type Approval struct {
	// Approver is the approving principal. It must differ from
	// [Request.Principal] -- self-approval is never accepted.
	Approver string
	// At is when the approval was recorded.
	At time.Time
}

// EvidenceKind is the closed set of evidence events recorded on a [Grant].
type EvidenceKind string

// Evidence kinds.
const (
	EvidenceGranted EvidenceKind = "GRANTED"
	EvidenceUsed    EvidenceKind = "USED"
	EvidenceExpired EvidenceKind = "EXPIRED"
	EvidenceRevoked EvidenceKind = "REVOKED"
)

// EvidenceRecord is one immutable entry in a [Grant]'s evidence trail.
type EvidenceRecord struct {
	Kind   EvidenceKind
	At     time.Time
	Actor  string
	Detail string
}

// Grant is a time-bounded JIT operator access grant. Every field describing
// what was requested and approved is immutable after [New] returns; only
// revocation state and the evidence trail change over the grant's life, and
// both are guarded by mu so [Grant.Use] and [Grant.Revoke] are safe to call
// from concurrent callers.
type Grant struct {
	ID            string
	Principal     string
	Tenant        values.TenantId
	Role          Role
	TicketRef     string
	Justification string
	Capabilities  []string
	Fields        []string
	Purpose       string
	Approver      string
	ApprovedAt    time.Time
	IssuedAt      time.Time
	ExpiresAt     time.Time

	mu            sync.Mutex
	revokedAt     *time.Time
	revokedBy     string
	revokedReason string
	evidence      []EvidenceRecord
}

// New requests and, if every condition is met, immediately issues a grant.
// There is no separate pending state: a [Request] and its [Approval] are
// evaluated together, and either a fully-formed, evidenced grant is returned
// or nothing is.
//
// New refuses: an unrecognized role, a missing ticket/justification/purpose,
// no named capabilities, an approver equal to the requester, and a TTL that
// is zero, negative, or beyond the role's hard maximum. There is no
// parameter that produces a standing (non-expiring) grant.
func New(id string, req Request, approval Approval, now time.Time) (*Grant, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: grant id is required", ErrInvalidRequest)
	}
	if err := req.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(approval.Approver) == "" || approval.At.IsZero() {
		return nil, fmt.Errorf("%w: approval requires a named approver and timestamp", ErrInvalidRequest)
	}
	if strings.EqualFold(strings.TrimSpace(approval.Approver), strings.TrimSpace(req.Principal)) {
		return nil, ErrApproverIsRequester
	}
	maxTTL, _ := req.Role.MaxTTL()
	if req.TTL <= 0 {
		return nil, ErrTTLRequired
	}
	if req.TTL > maxTTL {
		return nil, fmt.Errorf("%w: requested %s exceeds max %s for role %s", ErrTTLExceedsMax, req.TTL, maxTTL, req.Role)
	}

	issuedAt := now.UTC()
	g := &Grant{
		ID:            id,
		Principal:     req.Principal,
		Tenant:        req.Tenant,
		Role:          req.Role,
		TicketRef:     req.TicketRef,
		Justification: req.Justification,
		Capabilities:  append([]string(nil), req.Capabilities...),
		Fields:        append([]string(nil), req.Fields...),
		Purpose:       req.Purpose,
		Approver:      approval.Approver,
		ApprovedAt:    approval.At.UTC(),
		IssuedAt:      issuedAt,
		ExpiresAt:     issuedAt.Add(req.TTL),
	}
	g.evidence = append(g.evidence, EvidenceRecord{
		Kind: EvidenceGranted, At: issuedAt, Actor: approval.Approver,
		Detail: fmt.Sprintf("role=%s ticket=%s ttl=%s", req.Role, req.TicketRef, req.TTL),
	})
	return g, nil
}

// Stored is a grant's durable projection, as the trust store records it.
type Stored struct {
	ID            string
	Principal     string
	Tenant        values.TenantId
	Role          Role
	TicketRef     string
	Justification string
	Capabilities  []string
	Fields        []string
	Purpose       string
	Approver      string
	IssuedAt      time.Time
	ExpiresAt     time.Time
	Revoked       bool
}

// Restore rebuilds a grant from its durable projection. Unlike [New] it keeps
// the recorded issue and expiry instants, and it re-applies every rule New
// enforces -- closed role, named ticket, justification, purpose and
// capabilities, a distinct approver, and a lifetime within the role's hard
// ceiling -- so a tampered or widened record cannot come back as authority. A
// revoked record restores revoked.
func Restore(s Stored) (*Grant, error) {
	if strings.TrimSpace(s.ID) == "" {
		return nil, fmt.Errorf("%w: grant id is required", ErrInvalidRequest)
	}
	req := Request{Principal: s.Principal, Tenant: s.Tenant, Role: s.Role, TicketRef: s.TicketRef,
		Justification: s.Justification, Capabilities: s.Capabilities, Fields: s.Fields, Purpose: s.Purpose, TTL: s.ExpiresAt.Sub(s.IssuedAt)}
	if err := req.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.Approver) == "" {
		return nil, fmt.Errorf("%w: stored grant names no approver", ErrInvalidRequest)
	}
	if strings.EqualFold(strings.TrimSpace(s.Approver), strings.TrimSpace(s.Principal)) {
		return nil, ErrApproverIsRequester
	}
	maxTTL, _ := s.Role.MaxTTL()
	if s.IssuedAt.IsZero() || req.TTL <= 0 {
		return nil, ErrTTLRequired
	}
	if req.TTL > maxTTL {
		return nil, fmt.Errorf("%w: stored lifetime %s exceeds max %s for role %s", ErrTTLExceedsMax, req.TTL, maxTTL, s.Role)
	}
	g := &Grant{
		ID: s.ID, Principal: s.Principal, Tenant: s.Tenant, Role: s.Role, TicketRef: s.TicketRef,
		Justification: s.Justification, Capabilities: append([]string(nil), s.Capabilities...),
		Fields: append([]string(nil), s.Fields...), Purpose: s.Purpose, Approver: s.Approver,
		ApprovedAt: s.IssuedAt.UTC(), IssuedAt: s.IssuedAt.UTC(), ExpiresAt: s.ExpiresAt.UTC(),
	}
	g.evidence = append(g.evidence, EvidenceRecord{Kind: EvidenceGranted, At: g.IssuedAt, Actor: s.Approver,
		Detail: fmt.Sprintf("restored role=%s ticket=%s", s.Role, s.TicketRef)})
	if s.Revoked {
		at := g.IssuedAt
		g.revokedAt, g.revokedBy, g.revokedReason = &at, "store", "revoked in durable record"
		g.evidence = append(g.evidence, EvidenceRecord{Kind: EvidenceRevoked, At: at, Actor: "store", Detail: "revoked in durable record"})
	}
	return g, nil
}

// activeLocked reports whether g is usable at now, and lazily records an
// [EvidenceExpired] entry the first time an expiry is observed. mu must be
// held.
func (g *Grant) activeLocked(now time.Time) bool {
	if g.revokedAt != nil {
		return false
	}
	if !now.UTC().Before(g.ExpiresAt) {
		g.noteExpiryLocked(now)
		return false
	}
	return true
}

func (g *Grant) noteExpiryLocked(now time.Time) {
	for _, e := range g.evidence {
		if e.Kind == EvidenceExpired {
			return
		}
	}
	g.evidence = append(g.evidence, EvidenceRecord{
		Kind: EvidenceExpired, At: now.UTC(), Actor: "system",
		Detail: fmt.Sprintf("grant reached ttl expiry at %s", g.ExpiresAt.Format(time.RFC3339)),
	})
}

// IsActive reports whether the grant can be used at now: not revoked and not
// past its expiry.
func (g *Grant) IsActive(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.activeLocked(now)
}

// Use records one action taken under the grant. It refuses -- recording
// nothing beyond the refusal reason already implied by the grant's own
// state -- when the grant is revoked or has expired; there is no path from
// an inactive grant to a recorded use.
func (g *Grant) Use(action string, now time.Time) error {
	if strings.TrimSpace(action) == "" {
		return fmt.Errorf("%w: action is required", ErrInvalidRequest)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revokedAt != nil {
		return fmt.Errorf("%w: revoked at %s (%s)", ErrGrantRevoked, g.revokedAt.Format(time.RFC3339), g.revokedReason)
	}
	if !g.activeLocked(now) {
		return fmt.Errorf("%w: expired at %s", ErrGrantExpired, g.ExpiresAt.Format(time.RFC3339))
	}
	g.evidence = append(g.evidence, EvidenceRecord{Kind: EvidenceUsed, At: now.UTC(), Actor: g.Principal, Detail: action})
	return nil
}

// Revoke ends the grant immediately, regardless of remaining TTL. Revocation
// is permanent and idempotent: revoking an already-revoked grant returns
// [ErrGrantRevoked] rather than overwriting the original revocation.
func (g *Grant) Revoke(by, reason string, now time.Time) error {
	if strings.TrimSpace(by) == "" {
		return fmt.Errorf("%w: revoking actor is required", ErrInvalidRequest)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revokedAt != nil {
		return fmt.Errorf("%w: already revoked at %s", ErrGrantRevoked, g.revokedAt.Format(time.RFC3339))
	}
	t := now.UTC()
	g.revokedAt, g.revokedBy, g.revokedReason = &t, by, reason
	g.evidence = append(g.evidence, EvidenceRecord{Kind: EvidenceRevoked, At: t, Actor: by, Detail: reason})
	return nil
}

// Revocation returns the revocation instant and reason, and whether the
// grant has been revoked at all.
func (g *Grant) Revocation() (at time.Time, by, reason string, revoked bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revokedAt == nil {
		return time.Time{}, "", "", false
	}
	return *g.revokedAt, g.revokedBy, g.revokedReason, true
}

// Evidence returns a copy of the grant's full evidence trail in the order
// events were recorded.
func (g *Grant) Evidence() []EvidenceRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]EvidenceRecord(nil), g.evidence...)
}
