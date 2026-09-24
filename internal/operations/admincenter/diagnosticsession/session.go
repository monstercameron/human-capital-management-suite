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
	RegistryID       = "ADMIN-006"
	ContractVersion  = "hcmnext.admincenter.diagnostic-session/1"
	DefaultTTL       = 10 * time.Minute
	MaxTTL           = 15 * time.Minute
	EmergencyTTL     = 5 * time.Minute
	PurposeSupport   = "support_diagnostics"
	PurposeEmergency = "incident_declared_emergency"
)

var (
	ErrInvalidScope    = errors.New("diagnostic session: invalid JIT scope")
	ErrExpired         = errors.New("diagnostic session: expired")
	ErrNotYetValid     = errors.New("diagnostic session: not active yet")
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
	Emergency EmergencyJustification
}

// CustomerApproval is the customer consent bound to one scope. The digest
// binds the approval to the exact tenant, subject, case, purpose, resources,
// actions and window it was granted for: transplanting an approval onto a
// different scope fails validation, so consent cannot be forged by copying
// an approval reference.
type CustomerApproval struct {
	Reference   string
	ApproverID  string
	ScopeDigest string
}

// EmergencyJustification is a reference to an incident-declared emergency
// record. Its scope digest prevents using a recorded declaration for a
// different support grant; the injected verifier must confirm the record.
type EmergencyJustification struct {
	IncidentID      string
	RecordReference string
	DeclarerID      string
	Reason          string
	ScopeDigest     string
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
	sum := sha256.Sum256([]byte(ScopeDigest(scope) + "\x00" + reference + "\x00" + scope.Approval.ApproverID))
	return hex.EncodeToString(sum[:])
}

// MintCustomerApproval binds a customer approver identity to the same exact
// scope as the approval reference. It creates only a scope-bound candidate;
// New and Ledger.Open still require the customer service to verify its record.
func MintCustomerApproval(scope Scope, reference, approverID string) CustomerApproval {
	scope.Approval.ApproverID = approverID
	return CustomerApproval{Reference: reference, ApproverID: approverID, ScopeDigest: bindConsent(scope, reference)}
}

// ValidFor reports whether the approval binds this exact scope.
func (a CustomerApproval) ValidFor(scope Scope) bool {
	if a.Reference == "" || a.ScopeDigest == "" {
		return false
	}
	scope.Approval.ApproverID = a.ApproverID
	want := bindConsent(scope, a.Reference)
	if len(a.ScopeDigest) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a.ScopeDigest), []byte(want)) == 1
}

func emergencyDigest(scope Scope, justification EmergencyJustification) string {
	material := ScopeDigest(scope) + "\x00" + justification.IncidentID + "\x00" + justification.RecordReference + "\x00" + justification.DeclarerID + "\x00" + justification.Reason
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

// BindEmergencyJustification binds a recorded incident declaration to a
// scope. This does not authorize it; New and Ledger.Open require an external
// IncidentEmergencyVerifier to confirm the referenced record.
func BindEmergencyJustification(scope Scope, incidentID, recordReference, declarerID, reason string) EmergencyJustification {
	justification := EmergencyJustification{IncidentID: incidentID, RecordReference: recordReference, DeclarerID: declarerID, Reason: reason}
	justification.ScopeDigest = emergencyDigest(scope, justification)
	return justification
}

func (j EmergencyJustification) ValidFor(scope Scope) bool {
	if j.ScopeDigest == "" {
		return false
	}
	want := emergencyDigest(scope, j)
	if len(j.ScopeDigest) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(j.ScopeDigest), []byte(want)) == 1
}

// Session is an immutable-at-the-boundary diagnostic session handle.
type Session struct{ scope Scope }

// New opens a session only after the configured customer-approval or incident
// record service confirms the applicable authorization. A digest alone is not
// proof of either authority: callers must supply trusted composition-root
// verifiers.
func New(scope Scope, verifier ApprovalVerifier, emergencyVerifier ...IncidentEmergencyVerifier) (Session, error) {
	session, err := newSession(scope)
	if err != nil {
		return Session{}, err
	}
	switch scope.Purpose {
	case PurposeSupport:
		if verifier == nil {
			return Session{}, ErrApprovalUnverified
		}
		if err := verifier.VerifyCustomerApproval(scope, scope.Approval); err != nil {
			return Session{}, fmt.Errorf("%w: %v", ErrApprovalUnverified, err)
		}
	case PurposeEmergency:
		if len(emergencyVerifier) == 0 || emergencyVerifier[0] == nil {
			return Session{}, ErrEmergencyUnverified
		}
		if err := emergencyVerifier[0].VerifyIncidentEmergency(scope, scope.Emergency); err != nil {
			return Session{}, fmt.Errorf("%w: %v", ErrEmergencyUnverified, err)
		}
	}
	return session, nil
}

// newSession validates and materializes the bounded grant. Callers must first
// verify the customer approval or incident declaration that authorizes it.
func newSession(scope Scope) (Session, error) {
	if strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.SubjectID) == "" || strings.TrimSpace(scope.CaseID) == "" || len(scope.Resources) == 0 || len(scope.Actions) == 0 {
		return Session{}, ErrInvalidScope
	}
	switch scope.Purpose {
	case PurposeSupport:
		if scope.Emergency != (EmergencyJustification{}) {
			return Session{}, fmt.Errorf("%w: emergency record on customer-consent scope", ErrInvalidScope)
		}
		if strings.TrimSpace(scope.Approval.Reference) == "" || strings.TrimSpace(scope.Approval.ApproverID) == "" {
			return Session{}, fmt.Errorf("%w: customer approval reference and approver are required", ErrInvalidApproval)
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
	case PurposeEmergency:
		if scope.Approval != (CustomerApproval{}) || strings.TrimSpace(scope.Emergency.IncidentID) == "" || strings.TrimSpace(scope.Emergency.RecordReference) == "" || strings.TrimSpace(scope.Emergency.DeclarerID) == "" || strings.TrimSpace(scope.Emergency.Reason) == "" || !scope.Emergency.ValidFor(scope) {
			return Session{}, fmt.Errorf("%w: emergency scope requires a bound incident record, declarer, and reason without customer approval", ErrInvalidScope)
		}
		if scope.IssuedAt.IsZero() || scope.ExpiresAt.IsZero() || !scope.ExpiresAt.After(scope.IssuedAt) || scope.ExpiresAt.Sub(scope.IssuedAt) > EmergencyTTL {
			return Session{}, fmt.Errorf("%w: incident emergency requires explicit times and TTL at most %s", ErrInvalidScope, EmergencyTTL)
		}
	default:
		return Session{}, ErrInvalidScope
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
	if now.Before(s.scope.IssuedAt) {
		return ErrNotYetValid
	}
	return nil
}
func (s Session) AllowsResource(resource string, now time.Time) bool {
	if s.Validate(now) != nil {
		return false
	}
	for _, r := range s.scope.Resources {
		if r == resource {
			return true
		}
	}
	return false
}
func (s Session) AllowsAction(action UIAction, now time.Time) bool {
	if s.Validate(now) != nil {
		return false
	}
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
