package diagnosticsession

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	RegistryID      = "ADMIN-006"
	ContractVersion = "hcmnext.admincenter.diagnostic-session/1"
	DefaultTTL      = 10 * time.Minute
	MaxTTL          = 15 * time.Minute
	PurposeSupport  = "support_diagnostics"
)

var (
	ErrInvalidScope    = errors.New("diagnostic session: invalid JIT scope")
	ErrExpired         = errors.New("diagnostic session: expired")
	ErrActionDenied    = errors.New("diagnostic session: UI action is not allowlisted")
	ErrInvalidApproval = errors.New("diagnostic session: customer approval is invalid")
)

// Scope is the complete JIT grant. An empty resource or action set is denied
// by New, preventing accidental conversion of a support session into a
// broad operator credential.
type Scope struct {
	TenantID  string
	SubjectID string
	CaseID    string
	Purpose   string
	Resources []string
	Actions   []UIAction
	IssuedAt  time.Time
	ExpiresAt time.Time
	Approval  CustomerApproval
}

// CustomerApproval is the customer consent bound to one scope. The digest
// binds the approval to the exact tenant, subject, case, purpose, resources,
// actions and window it was granted for: transplanting an approval onto a
// different scope fails validation, so consent cannot be forged by copying
// an approval reference.
type CustomerApproval struct {
	Reference   string
	ScopeDigest string
}

// ScopeDigest computes the hex SHA-256 over the canonical scope fields. The
// approval itself is excluded: it is what the digest authenticates.
func ScopeDigest(scope Scope) string {
	var b strings.Builder
	b.WriteString(scope.TenantID)
	b.WriteByte(0)
	b.WriteString(scope.SubjectID)
	b.WriteByte(0)
	b.WriteString(scope.CaseID)
	b.WriteByte(0)
	b.WriteString(scope.Purpose)
	b.WriteByte(0)
	for _, resource := range scope.Resources {
		b.WriteString(resource)
		b.WriteByte(0)
	}
	for _, action := range scope.Actions {
		b.WriteString(string(action))
		b.WriteByte(0)
	}
	b.WriteString(scope.IssuedAt.UTC().Format(time.RFC3339Nano))
	b.WriteByte(0)
	b.WriteString(scope.ExpiresAt.UTC().Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// MintApproval binds a customer reference to a scope.
// bindConsent seals the customer reference to the exact grant: the stored
// digest covers the canonical scope digest plus the reference, so a consent
// minted for one ticket cannot be retargeted to another.
func bindConsent(scope Scope, reference string) string {
	sum := sha256.Sum256([]byte(ScopeDigest(scope) + "\x00" + reference))
	return hex.EncodeToString(sum[:])
}

// MintApproval binds a customer reference to a scope.
func MintApproval(scope Scope, reference string) CustomerApproval {
	return CustomerApproval{Reference: reference, ScopeDigest: bindConsent(scope, reference)}
}

// ValidFor reports whether the approval binds this exact scope.
func (a CustomerApproval) ValidFor(scope Scope) bool {
	if a.Reference == "" || a.ScopeDigest == "" {
		return false
	}
	want := bindConsent(scope, a.Reference)
	if len(a.ScopeDigest) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a.ScopeDigest), []byte(want)) == 1
}

// Session is an immutable-at-the-boundary diagnostic session handle.
type Session struct{ scope Scope }

func New(scope Scope) (Session, error) {
	if strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.SubjectID) == "" || strings.TrimSpace(scope.CaseID) == "" || scope.Purpose != PurposeSupport || len(scope.Resources) == 0 || len(scope.Actions) == 0 {
		return Session{}, ErrInvalidScope
	}
	// Consent is checked before the server fills in defaults: the approval
	// binds exactly the grant the customer signed, with an unspecified
	// window meaning server defaults apply.
	if strings.TrimSpace(scope.Approval.Reference) == "" {
		return Session{}, fmt.Errorf("%w: customer approval reference is required", ErrInvalidApproval)
	}
	if !scope.Approval.ValidFor(scope) {
		return Session{}, fmt.Errorf("%w: approval does not bind this scope", ErrInvalidApproval)
	}
	if scope.IssuedAt.IsZero() {
		scope.IssuedAt = time.Now().UTC()
	}
	if scope.ExpiresAt.IsZero() {
		scope.ExpiresAt = scope.IssuedAt.Add(DefaultTTL)
	}
	if !scope.ExpiresAt.After(scope.IssuedAt) || scope.ExpiresAt.Sub(scope.IssuedAt) > MaxTTL {
		return Session{}, fmt.Errorf("%w: TTL exceeds %s", ErrInvalidScope, MaxTTL)
	}
	resources := append([]string(nil), scope.Resources...)
	actions := append([]UIAction(nil), scope.Actions...)
	for _, r := range resources {
		if strings.TrimSpace(r) == "" {
			return Session{}, ErrInvalidScope
		}
	}
	for _, a := range actions {
		if !IsSafeUIAction(a) {
			return Session{}, fmt.Errorf("%w: %q", ErrActionDenied, a)
		}
	}
	scope.Resources, scope.Actions = resources, actions
	return Session{scope: scope}, nil
}

func (s Session) Scope() Scope {
	c := s.scope
	c.Resources = append([]string(nil), c.Resources...)
	c.Actions = append([]UIAction(nil), c.Actions...)
	return c
}
func (s Session) Expired(now time.Time) bool {
	return s.scope.ExpiresAt.IsZero() || !now.Before(s.scope.ExpiresAt)
}
func (s Session) Validate(now time.Time) error {
	if s.Expired(now) {
		return ErrExpired
	}
	return nil
}
func (s Session) AllowsResource(resource string) bool {
	for _, r := range s.scope.Resources {
		if r == resource {
			return true
		}
	}
	return false
}
func (s Session) AllowsAction(action UIAction) bool {
	for _, a := range s.scope.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// UIAction is intentionally a closed set of read-only support affordances.
type UIAction string

const (
	ActionViewSummary           UIAction = "view_summary"
	ActionViewTimeline          UIAction = "view_timeline"
	ActionViewEvidence          UIAction = "view_evidence"
	ActionCopyRedactedReference UIAction = "copy_redacted_reference"
)

func IsSafeUIAction(a UIAction) bool {
	switch a {
	case ActionViewSummary, ActionViewTimeline, ActionViewEvidence, ActionCopyRedactedReference:
		return true
	}
	return false
}
