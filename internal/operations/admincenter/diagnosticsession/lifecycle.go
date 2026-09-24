package diagnosticsession

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrApprovalUnverified  = errors.New("diagnostic session: customer approval was not verified")
	ErrEmergencyUnverified = errors.New("diagnostic session: incident emergency was not verified")
	ErrSessionExists       = errors.New("diagnostic session: session ID already exists")
	ErrSessionNotFound     = errors.New("diagnostic session: session not found")
	ErrRevocationDenied    = errors.New("diagnostic session: revocation is not authorized")
	ErrSessionRevoked      = errors.New("diagnostic session: revoked")
)

// ApprovalVerifier validates the approval reference against the customer
// support-access system of record. A digest alone binds consent to a scope,
// but cannot prove who issued it.
type ApprovalVerifier interface {
	VerifyCustomerApproval(scope Scope, approval CustomerApproval) error
}

// ApprovalVerifierFunc adapts a function to ApprovalVerifier.
type ApprovalVerifierFunc func(Scope, CustomerApproval) error

func (f ApprovalVerifierFunc) VerifyCustomerApproval(scope Scope, approval CustomerApproval) error {
	return f(scope, approval)
}

// IncidentEmergencyVerifier confirms that the incident declaration and
// justification exist in the authoritative incident system.
type IncidentEmergencyVerifier interface {
	VerifyIncidentEmergency(scope Scope, justification EmergencyJustification) error
}

// IncidentEmergencyVerifierFunc adapts a function to IncidentEmergencyVerifier.
type IncidentEmergencyVerifierFunc func(Scope, EmergencyJustification) error

func (f IncidentEmergencyVerifierFunc) VerifyIncidentEmergency(scope Scope, justification EmergencyJustification) error {
	return f(scope, justification)
}

type AuditKind string

const (
	AuditSessionOpened  AuditKind = "session_opened"
	AuditSessionRevoked AuditKind = "session_revoked"
)

// AuditRecord captures the actors and consent reference for lifecycle changes.
// It contains no diagnostic payload.
type AuditRecord struct {
	SessionID        string
	Kind             AuditKind
	TenantID         string
	SubjectID        string
	ActorID          string
	CaseID           string
	ApprovalRef      string
	EmergencyRef     string
	IncidentID       string
	EmergencyReason  string
	OccurredAt       time.Time
	RevocationReason string
}

type ledgerEntry struct {
	session   Session
	revoked   bool
	revokedAt time.Time
}

// Ledger is the authorization boundary for opening and using support
// diagnostic sessions. It verifies customer consent before opening and only
// the named customer approver may revoke that grant.
type Ledger struct {
	mu                sync.RWMutex
	verifier          ApprovalVerifier
	emergencyVerifier IncidentEmergencyVerifier
	entries           map[string]ledgerEntry
	audit             []AuditRecord
}

func NewLedger(verifier ApprovalVerifier, emergencyVerifier ...IncidentEmergencyVerifier) *Ledger {
	ledger := &Ledger{verifier: verifier, entries: make(map[string]ledgerEntry)}
	if len(emergencyVerifier) > 0 {
		ledger.emergencyVerifier = emergencyVerifier[0]
	}
	return ledger
}

// Open verifies the external approval, creates the bounded session and records
// the open event atomically. Session IDs are supplied by the caller so they
// can be correlated with the support-access service.
func (l *Ledger) Open(id string, scope Scope, now time.Time) (Session, error) {
	if l == nil || strings.TrimSpace(id) == "" {
		return Session{}, ErrApprovalUnverified
	}
	switch scope.Purpose {
	case PurposeSupport:
		if l.verifier == nil {
			return Session{}, ErrApprovalUnverified
		}
		if strings.TrimSpace(scope.SubjectID) == "" || strings.TrimSpace(scope.Approval.ApproverID) == "" || !scope.Approval.ValidFor(scope) {
			return Session{}, ErrInvalidApproval
		}
		if err := l.verifier.VerifyCustomerApproval(scope, scope.Approval); err != nil {
			return Session{}, fmt.Errorf("%w: %v", ErrApprovalUnverified, err)
		}
	case PurposeEmergency:
		if l.emergencyVerifier == nil {
			return Session{}, ErrEmergencyUnverified
		}
		if strings.TrimSpace(scope.Emergency.DeclarerID) == "" || !scope.Emergency.ValidFor(scope) {
			return Session{}, ErrInvalidScope
		}
		if err := l.emergencyVerifier.VerifyIncidentEmergency(scope, scope.Emergency); err != nil {
			return Session{}, fmt.Errorf("%w: %v", ErrEmergencyUnverified, err)
		}
	default:
		return Session{}, ErrInvalidScope
	}
	session, err := newSession(scope)
	if err != nil {
		return Session{}, err
	}
	if err := session.Validate(now); err != nil {
		return Session{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.entries[id]; exists {
		return Session{}, ErrSessionExists
	}
	l.entries[id] = ledgerEntry{session: session}
	record := AuditRecord{SessionID: id, Kind: AuditSessionOpened, TenantID: scope.TenantID, SubjectID: scope.SubjectID, ActorID: scope.SubjectID, CaseID: scope.CaseID, ApprovalRef: scope.Approval.Reference, OccurredAt: now.UTC()}
	if scope.Purpose == PurposeEmergency {
		record.EmergencyRef = scope.Emergency.RecordReference
		record.IncidentID = scope.Emergency.IncidentID
		record.EmergencyReason = scope.Emergency.Reason
		record.ActorID = scope.Emergency.DeclarerID
	}
	l.audit = append(l.audit, record)
	return session, nil
}

// Inspect returns an active session only while its TTL remains valid and it
// has not been revoked.
func (l *Ledger) Inspect(id string, now time.Time) (Session, error) {
	if l == nil {
		return Session{}, ErrSessionNotFound
	}
	l.mu.RLock()
	entry, ok := l.entries[id]
	l.mu.RUnlock()
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	if entry.revoked {
		return Session{}, ErrSessionRevoked
	}
	if err := entry.session.Validate(now); err != nil {
		return Session{}, err
	}
	return entry.session, nil
}

// Revoke is authorized only for the customer approver recorded in the
// verified consent. Repeated revocation is idempotent and does not duplicate
// the audit record.
func (l *Ledger) Revoke(id, actorID, reason string, now time.Time) error {
	if l == nil {
		return ErrSessionNotFound
	}
	if strings.TrimSpace(reason) == "" {
		return ErrRevocationDenied
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[id]
	if !ok {
		return ErrSessionNotFound
	}
	scope := entry.session.scope
	authorizedActor := scope.Approval.ApproverID
	if scope.Purpose == PurposeEmergency {
		authorizedActor = scope.Emergency.DeclarerID
	}
	if strings.TrimSpace(actorID) == "" || actorID != authorizedActor {
		return ErrRevocationDenied
	}
	if entry.revoked {
		return nil
	}
	entry.revoked, entry.revokedAt = true, now.UTC()
	l.entries[id] = entry
	approvalRef, emergencyRef, incidentID := scope.Approval.Reference, scope.Emergency.RecordReference, scope.Emergency.IncidentID
	l.audit = append(l.audit, AuditRecord{SessionID: id, Kind: AuditSessionRevoked, TenantID: scope.TenantID, SubjectID: scope.SubjectID, ActorID: actorID, CaseID: scope.CaseID, ApprovalRef: approvalRef, EmergencyRef: emergencyRef, IncidentID: incidentID, EmergencyReason: scope.Emergency.Reason, OccurredAt: now.UTC(), RevocationReason: strings.TrimSpace(reason)})
	return nil
}

// Audit returns a detached snapshot of lifecycle records.
func (l *Ledger) Audit() []AuditRecord {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]AuditRecord(nil), l.audit...)
}
