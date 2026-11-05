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
	PromotionIntentType  = "hcmnext.people.promote_worker/v1"
	AssignmentJobField   = "assignment.job_code"
	AssignmentLevelField = "assignment.grade"
)

var (
	ErrPromotionMutationInvalid = errors.New("people: promotion mutation is invalid")
	ErrPromotionFieldNotAllowed = errors.New("people: promotion field is not allowed")
	ErrPromotionNoChange        = errors.New("people: promotion does not change job or level")
	ErrPromotionBaseline        = errors.New("people: promotion baseline is missing")
)

// PromotionMutation is the People-owned, bounded write set for Promotion.
// The closed shape intentionally contains job and level only: employment
// creation/end, identity merge, and unrelated worker fields cannot be smuggled
// into the promotion capability.
type PromotionMutation struct {
	Tenant             values.TenantId
	ProposalRevisionID string
	ProposalDigest     string
	ActorPrincipalID   string
	AuthorityDecision  string

	WorkerID         string
	AssignmentID     string
	ResourceKey      values.ResourceKey
	Subject          intent.SubjectReference
	Effective        values.EffectiveInterval
	ExpectedRevision values.RevisionToken

	CurrentJobCode string
	TargetJobCode  string
	CurrentLevel   string
	TargetLevel    string
}

// Validate proves that this command is a promotion write, rather than a
// general People mutation. It is pure and safe to rerun at commit time.
func (m PromotionMutation) Validate() error {
	if err := m.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrPromotionMutationInvalid, err)
	}
	for name, value := range map[string]string{
		"proposal_revision_id": m.ProposalRevisionID, "proposal_digest": m.ProposalDigest,
		"actor_principal_id": m.ActorPrincipalID, "authority_decision": m.AuthorityDecision,
		"worker_id": m.WorkerID, "assignment_id": m.AssignmentID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrPromotionMutationInvalid, name)
		}
	}
	if err := m.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrPromotionMutationInvalid, err)
	}
	if m.Subject.Kind != "worker" {
		return fmt.Errorf("%w: subject kind %q is not a worker", ErrPromotionMutationInvalid, m.Subject.Kind)
	}
	if m.Subject.SubjectID != m.WorkerID {
		return fmt.Errorf("%w: subject is not the promoted worker", ErrPromotionMutationInvalid)
	}
	if m.ResourceKey.Validate() != nil || m.ResourceKey.Tenant != m.Tenant {
		return fmt.Errorf("%w: resource key is outside tenant", ErrPromotionMutationInvalid)
	}
	if err := m.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrPromotionMutationInvalid, err)
	}
	if !m.ExpectedRevision.IsSpecified() {
		return ErrPromotionBaseline
	}
	if m.TargetJobCode == m.CurrentJobCode && m.TargetLevel == m.CurrentLevel {
		return ErrPromotionNoChange
	}
	if strings.TrimSpace(m.TargetJobCode) == "" || strings.TrimSpace(m.TargetLevel) == "" {
		return fmt.Errorf("%w: target job and level are required", ErrPromotionMutationInvalid)
	}
	return nil
}

// assignmentRevision projects this promotion onto the generic assignment
// revision: the revision owns the closed assignment field set and its
// stable order, so the promotion mutation cannot drift from it. Only job
// and grade are ever set; the promotion struct has no other assignment
// field to smuggle through.
func (m PromotionMutation) assignmentRevision() AssignmentRevision {
	return AssignmentRevision{
		Tenant: m.Tenant, ProposalRevisionID: m.ProposalRevisionID, ProposalDigest: m.ProposalDigest,
		ActorPrincipalID: m.ActorPrincipalID, AuthorityDecision: m.AuthorityDecision,
		WorkerID: m.WorkerID, AssignmentID: m.AssignmentID,
		ResourceKey: m.ResourceKey, Subject: m.Subject,
		Effective: m.Effective, ExpectedRevision: m.ExpectedRevision,
		CurrentJobCode: m.CurrentJobCode, TargetJobCode: m.TargetJobCode,
		CurrentLevel: m.CurrentLevel, TargetLevel: m.TargetLevel,
	}
}

// PlannedWrites returns exactly the changed job and level fields in stable
// order. The expected revision is attached to both writes so a stale stream
// cannot partially advance.
//
// The write set comes from the generic revision's field projection; this
// method only encodes it in the legacy unfenced shape the promotion
// execution path consumes. Promotion keeps its own Validate contract, so
// its refusal errors are unchanged.
func (m PromotionMutation) PlannedWrites() ([]intent.PlannedWrite, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	changes := m.assignmentRevision().changedPairs()
	writes := make([]intent.PlannedWrite, 0, len(changes))
	for _, change := range changes {
		writes = append(writes, intent.PlannedWrite{Subject: m.Subject, ResourceKey: m.ResourceKey,
			FieldPath: change.FieldPath, CurrentCanonicalText: change.CurrentCanonicalText,
			ProposedCanonicalText: change.ProposedCanonicalText, SourceAuthorityDecision: m.AuthorityDecision,
			ExpectedRevision: m.ExpectedRevision})
	}
	return writes, nil
}

// Explain returns identifiers and dimensions without exposing the proposed
// values, making it suitable for audit telemetry.
func (m PromotionMutation) Explain() string {
	return fmt.Sprintf("people promotion proposal=%s worker=%s assignment=%s fields=job,level baseline=%s",
		m.ProposalRevisionID, m.WorkerID, m.AssignmentID, m.ExpectedRevision.Stream())
}

func ExplainPromotion(m PromotionMutation) string { return m.Explain() }
