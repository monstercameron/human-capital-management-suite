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
	EndEmploymentIntentType = "hcmnext.people.end_employment/v1"
	EmploymentStatusField   = "employment.status"
	AssignmentPositionField = "assignment.position_id"
	// EmploymentEndedStatus is the terminal employment status text. It
	// matches EmploymentPeriodEnded so the read model discloses the end
	// without translation.
	EmploymentEndedStatus = "ENDED"
)

var (
	ErrEndEmploymentInvalid      = errors.New("people: end employment is invalid")
	ErrEmploymentAlreadyEnded    = errors.New("people: employment is already ended")
	ErrEndEmploymentNothingToEnd = errors.New("people: end employment names no current status or position")
)

// EndEmployment is the People-owned `people.end_employment` capability: it
// ends one worker's employment and vacates the position the worker occupies.
// The write set is exactly two fenced writes: the employment status moves to
// ENDED, and the assignment's position link is deleted. Access revocation,
// severance and benefits continuation are separate capabilities with their
// own approvals; they cannot be smuggled into the separation itself.
type EndEmployment struct {
	Tenant             values.TenantId
	ProposalRevisionID string
	ProposalDigest     string
	ActorPrincipalID   string
	AuthorityDecision  string

	WorkerID         string
	EmploymentID     string
	AssignmentID     string
	EmploymentKey    values.ResourceKey
	AssignmentKey    values.ResourceKey
	Subject          intent.SubjectReference
	Effective        values.EffectiveInterval
	ExpectedRevision values.RevisionToken

	CurrentStatus     string
	CurrentPositionID string
}

// Validate proves the command ends a live employment with an occupied
// position. It is pure and safe to rerun at commit time.
func (m EndEmployment) Validate() error {
	if err := m.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrEndEmploymentInvalid, err)
	}
	for name, value := range map[string]string{
		"proposal_revision_id": m.ProposalRevisionID, "proposal_digest": m.ProposalDigest,
		"actor_principal_id": m.ActorPrincipalID, "authority_decision": m.AuthorityDecision,
		"worker_id": m.WorkerID, "employment_id": m.EmploymentID, "assignment_id": m.AssignmentID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrEndEmploymentInvalid, name)
		}
	}
	if err := m.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrEndEmploymentInvalid, err)
	}
	if m.Subject.Kind != "worker" {
		return fmt.Errorf("%w: subject kind %q is not a worker", ErrEndEmploymentInvalid, m.Subject.Kind)
	}
	if m.Subject.SubjectID != m.WorkerID {
		return fmt.Errorf("%w: subject is not the separated worker", ErrEndEmploymentInvalid)
	}
	if m.EmploymentKey.Validate() != nil || m.EmploymentKey.Tenant != m.Tenant {
		return fmt.Errorf("%w: employment key is outside tenant", ErrEndEmploymentInvalid)
	}
	if m.AssignmentKey.Validate() != nil || m.AssignmentKey.Tenant != m.Tenant {
		return fmt.Errorf("%w: assignment key is outside tenant", ErrEndEmploymentInvalid)
	}
	if err := m.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrEndEmploymentInvalid, err)
	}
	if m.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: effective interval must be LOCAL_DATE", ErrEndEmploymentInvalid)
	}
	if !m.ExpectedRevision.IsSpecified() {
		return fmt.Errorf("%w: baseline revision is missing", ErrEndEmploymentInvalid)
	}
	if strings.TrimSpace(m.CurrentStatus) == "" || strings.TrimSpace(m.CurrentPositionID) == "" {
		return ErrEndEmploymentNothingToEnd
	}
	if m.CurrentStatus == EmploymentEndedStatus {
		return ErrEmploymentAlreadyEnded
	}
	return nil
}

// PlannedWrites returns the employment end and the position vacate in stable
// order, both fenced to the separation interval. The vacate is a DELETE:
// the occupancy link is removed, so it carries no proposed text.
func (m EndEmployment) PlannedWrites() ([]intent.PlannedWrite, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return []intent.PlannedWrite{
		{Subject: m.Subject, ResourceKey: m.EmploymentKey,
			FieldPath: EmploymentStatusField, CurrentCanonicalText: m.CurrentStatus,
			ProposedCanonicalText: EmploymentEndedStatus, SourceAuthorityDecision: m.AuthorityDecision,
			ExpectedRevision: m.ExpectedRevision, Operation: intent.WriteOperationUpdate, EffectiveInterval: m.Effective},
		{Subject: m.Subject, ResourceKey: m.AssignmentKey,
			FieldPath: AssignmentPositionField, CurrentCanonicalText: m.CurrentPositionID,
			ProposedCanonicalText: "", SourceAuthorityDecision: m.AuthorityDecision,
			ExpectedRevision: m.ExpectedRevision, Operation: intent.WriteOperationDelete, EffectiveInterval: m.Effective},
	}, nil
}

// Explain returns identifiers without exposing the separated worker's
// position, making it suitable for audit telemetry.
func (m EndEmployment) Explain() string {
	return fmt.Sprintf("people end_employment proposal=%s worker=%s employment=%s assignment=%s baseline=%s",
		m.ProposalRevisionID, m.WorkerID, m.EmploymentID, m.AssignmentID, m.ExpectedRevision.Stream())
}
