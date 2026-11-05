//go:build !(js && wasm)

// CHATBUG-014: server-side only. Planned writes belong to the intent engine,
// which the browser client must not carry.

package people

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	RecordRehireEligibilityIntentType = "hcmnext.people.record_rehire_eligibility/v1"
	RehireStatusField                 = "employment.rehire_status"
	RehireReasonField                 = "employment.rehire_reason"
	RehireEffectiveField              = "employment.rehire_effective"
)

var (
	ErrRecordRehireEligibilityInvalid = errors.New("people: record rehire eligibility is invalid")
	ErrRecordRehireEligibilityNoHead  = errors.New("people: rehire correction names no current head")
)

// RecordRehireEligibility is the People-owned
// `people.record_rehire_eligibility` capability: it records one worker's
// rehire decision (status, reason and effective date) against the separated
// employment. The first decision emits CREATE writes; a correction emits
// UPDATE writes against the current head with a higher revision, so the
// decision is retained as history and never overwritten in place.
type RecordRehireEligibility struct {
	Tenant             values.TenantId
	ProposalRevisionID string
	ProposalDigest     string
	ActorPrincipalID   string
	AuthorityDecision  string

	WorkerID         string
	EmploymentID     string
	EmploymentKey    values.ResourceKey
	Subject          intent.SubjectReference
	Effective        values.EffectiveInterval
	ExpectedRevision values.RevisionToken

	Status      RehireStatus
	Reason      RehireReason
	EffectiveAt values.LocalDate
	Revision    uint64

	CurrentStatus    string
	CurrentReason    string
	CurrentEffective string
}

// Validate proves the command records a governed decision for a live
// employment reference. It is pure and safe to rerun at commit time.
func (m RecordRehireEligibility) Validate() error {
	if err := m.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrRecordRehireEligibilityInvalid, err)
	}
	for name, value := range map[string]string{
		"proposal_revision_id": m.ProposalRevisionID, "proposal_digest": m.ProposalDigest,
		"actor_principal_id": m.ActorPrincipalID, "authority_decision": m.AuthorityDecision,
		"worker_id": m.WorkerID, "employment_id": m.EmploymentID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrRecordRehireEligibilityInvalid, name)
		}
	}
	if err := m.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrRecordRehireEligibilityInvalid, err)
	}
	if m.Subject.Kind != "worker" {
		return fmt.Errorf("%w: subject kind %q is not a worker", ErrRecordRehireEligibilityInvalid, m.Subject.Kind)
	}
	if m.Subject.SubjectID != m.WorkerID {
		return fmt.Errorf("%w: subject is not the separated worker", ErrRecordRehireEligibilityInvalid)
	}
	if m.EmploymentKey.Validate() != nil || m.EmploymentKey.Tenant != m.Tenant {
		return fmt.Errorf("%w: employment key is outside tenant", ErrRecordRehireEligibilityInvalid)
	}
	if err := m.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrRecordRehireEligibilityInvalid, err)
	}
	if m.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: effective interval must be LOCAL_DATE", ErrRecordRehireEligibilityInvalid)
	}
	if !m.ExpectedRevision.IsSpecified() {
		return fmt.Errorf("%w: baseline revision is missing", ErrRecordRehireEligibilityInvalid)
	}
	if err := m.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective date: %v", ErrRecordRehireEligibilityInvalid, err)
	}
	if !m.Status.Valid() || !m.Reason.validFor(m.Status) {
		return fmt.Errorf("%w: status %q and reason %q are not a declared combination", ErrRecordRehireEligibilityInvalid, m.Status, m.Reason)
	}
	if m.Revision == 0 {
		return fmt.Errorf("%w: revision must be positive", ErrRecordRehireEligibilityInvalid)
	}
	headed := strings.TrimSpace(m.CurrentStatus) != "" || strings.TrimSpace(m.CurrentReason) != "" || strings.TrimSpace(m.CurrentEffective) != ""
	if m.Revision == 1 {
		if headed {
			return fmt.Errorf("%w: first record names a current head", ErrRecordRehireEligibilityInvalid)
		}
		return nil
	}
	if strings.TrimSpace(m.CurrentStatus) == "" || strings.TrimSpace(m.CurrentReason) == "" || strings.TrimSpace(m.CurrentEffective) == "" {
		return ErrRecordRehireEligibilityNoHead
	}
	oldStatus := RehireStatus(strings.TrimSpace(m.CurrentStatus))
	oldReason := RehireReason(strings.TrimSpace(m.CurrentReason))
	if !oldStatus.Valid() || !oldReason.validFor(oldStatus) {
		return fmt.Errorf("%w: current head %q/%q is not a governed decision", ErrRecordRehireEligibilityInvalid, m.CurrentStatus, m.CurrentReason)
	}
	return nil
}

// PlannedWrites returns the status, reason and effective-date writes in
// stable order, fenced to the record interval. The first revision creates
// the three fields; a correction updates them against the current head.
func (m RecordRehireEligibility) PlannedWrites() ([]intent.PlannedWrite, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	operation := intent.WriteOperationUpdate
	if m.Revision == 1 {
		operation = intent.WriteOperationCreate
	}
	effective := m.EffectiveAt.String()
	return []intent.PlannedWrite{
		{Subject: m.Subject, ResourceKey: m.EmploymentKey,
			FieldPath: RehireStatusField, CurrentCanonicalText: m.CurrentStatus,
			ProposedCanonicalText: string(m.Status), SourceAuthorityDecision: m.AuthorityDecision,
			ExpectedRevision: m.ExpectedRevision, Operation: operation, EffectiveInterval: m.Effective},
		{Subject: m.Subject, ResourceKey: m.EmploymentKey,
			FieldPath: RehireReasonField, CurrentCanonicalText: m.CurrentReason,
			ProposedCanonicalText: string(m.Reason), SourceAuthorityDecision: m.AuthorityDecision,
			ExpectedRevision: m.ExpectedRevision, Operation: operation, EffectiveInterval: m.Effective},
		{Subject: m.Subject, ResourceKey: m.EmploymentKey,
			FieldPath: RehireEffectiveField, CurrentCanonicalText: m.CurrentEffective,
			ProposedCanonicalText: effective, SourceAuthorityDecision: m.AuthorityDecision,
			ExpectedRevision: m.ExpectedRevision, Operation: operation, EffectiveInterval: m.Effective},
	}, nil
}

// Explain names the rehire fields without exposing the decision values,
// making it suitable for audit telemetry.
func (m RecordRehireEligibility) Explain() string {
	return fmt.Sprintf("people record_rehire_eligibility proposal=%s worker=%s employment=%s revision=%d fields=%s,%s,%s baseline=%s",
		m.ProposalRevisionID, m.WorkerID, m.EmploymentID, m.Revision,
		RehireStatusField, RehireReasonField, RehireEffectiveField, m.ExpectedRevision.Stream())
}
