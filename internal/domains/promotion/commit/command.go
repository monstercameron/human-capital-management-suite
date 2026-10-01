// Package commit owns the bounded, authoritative mutation command for the
// promote-into-management workflow. It is deliberately storage-neutral: the
// command says exactly what the approved proposal permits, while the data
// adapter decides how those successor facts are persisted.
package commit

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	ParticipantAssignment   = "people.assignment"
	ParticipantOccupancy    = "position.occupancy"
	ParticipantCompensation = "rewards.compensation"
	ParticipantBudget       = "rewards.budget_reservation"
)

var (
	ErrInvalidCommand   = errors.New("promotion commit: invalid command")
	ErrApprovalEvidence = errors.New("promotion commit: approval evidence is incomplete")
	ErrManagerCycle     = errors.New("promotion commit: manager relationship would create a cycle")
	ErrParticipantSet   = errors.New("promotion commit: transaction participant set does not match the command")
)

// ExternalEffect is a post-commit obligation. The adapter persists it to the
// transactional outbox; it must never call the destination while the local
// transaction is open.
type ExternalEffect struct {
	EffectID       string
	DestinationRef string
	SchemaRef      string
	Payload        []byte
}

// Command is the immutable materialization of one approved promotion. Every
// identifier names an already-resolved local aggregate; callers cannot ask
// this command to create employment or merge identity records.
type Command struct {
	TenantID               string
	IntentID               string
	ProposalRevisionID     string
	ProposalRevisionNumber uint64
	ProposalDigest         string
	PlanID                 string
	PlanDigest             string
	PlanParticipants       []string
	WorkflowPlanDigest     string
	AuthorityDigest        string
	ActorPrincipalID       string
	EffectiveAt            time.Time
	RecordedAt             time.Time

	WorkerID     string
	EmploymentID string
	AssignmentID string

	TargetJobCode            string
	TargetJobID              string
	TargetGrade              string
	TargetOrganizationID     string
	TargetPositionID         string
	ManagerRelationshipRef   string
	ManagerWorkerID          string
	ManagerAncestorWorkerIDs []string
	Location                 string
	PayZone                  string
	FTE                      string

	PositionOccupancyID    string
	PositionReservationRef string

	CompensationPackageID string
	BasePayComponentID    string
	BasePay               values.Money
	PayFrequency          string

	BudgetReservationID  string
	BudgetReservationRef string

	ExpectedWorkerDigest     string
	ExpectedEmploymentDigest string
	ExpectedAssignmentDigest string
	AssignmentWrites         []AssignmentWrite
	ExpectedJobDigest        string
	ExpectedPositionDigest   string
	ExpectedPackageDigest    string
	ExpectedBasePayDigest    string
	ExpectedBudgetDigest     string

	ApprovalDecisionIDs []string
	TaskSubmissionIDs   []string
	Effects             []ExternalEffect
}

// AssignmentWrite is the exact approved assignment field transition recorded
// with the successor row produced by the local commit.
type AssignmentWrite struct {
	FieldPath         string
	CurrentValue      string
	ProposedValue     string
	AuthorityDecision string
	ExpectedSource    string
}

// Participants is the exact local ACID set. Manager placement is part of the
// Assignment aggregate in the current physical model, so it is intentionally
// not presented as a fictitious fifth table participant.
func (c Command) Participants() []string {
	return []string{ParticipantAssignment, ParticipantOccupancy, ParticipantCompensation, ParticipantBudget}
}

// Validate fails closed before any mutation is attempted.
func (c Command) Validate() error {
	required := []struct{ name, value string }{
		{"tenant_id", c.TenantID}, {"intent_id", c.IntentID}, {"proposal_revision_id", c.ProposalRevisionID},
		{"proposal_digest", c.ProposalDigest}, {"plan_id", c.PlanID}, {"plan_digest", c.PlanDigest},
		{"workflow_plan_digest", c.WorkflowPlanDigest},
		{"authority_digest", c.AuthorityDigest}, {"actor_principal_id", c.ActorPrincipalID},
		{"worker_id", c.WorkerID}, {"employment_id", c.EmploymentID}, {"assignment_id", c.AssignmentID},
		{"target_job_code", c.TargetJobCode}, {"target_job_id", c.TargetJobID}, {"target_grade", c.TargetGrade},
		{"target_organization_id", c.TargetOrganizationID}, {"target_position_id", c.TargetPositionID},
		{"manager_relationship_ref", c.ManagerRelationshipRef}, {"manager_worker_id", c.ManagerWorkerID},
		{"fte", c.FTE}, {"position_occupancy_id", c.PositionOccupancyID},
		{"position_reservation_ref", c.PositionReservationRef},
		{"compensation_package_id", c.CompensationPackageID}, {"base_pay_component_id", c.BasePayComponentID},
		{"pay_frequency", c.PayFrequency}, {"budget_reservation_id", c.BudgetReservationID},
		{"budget_reservation_ref", c.BudgetReservationRef},
		{"expected_worker_digest", c.ExpectedWorkerDigest},
		{"expected_employment_digest", c.ExpectedEmploymentDigest},
		{"expected_assignment_digest", c.ExpectedAssignmentDigest},
		{"expected_job_digest", c.ExpectedJobDigest},
		{"expected_position_digest", c.ExpectedPositionDigest},
		{"expected_package_digest", c.ExpectedPackageDigest},
		{"expected_base_pay_digest", c.ExpectedBasePayDigest},
		{"expected_budget_digest", c.ExpectedBudgetDigest},
	}
	if c.ProposalRevisionNumber == 0 {
		return fmt.Errorf("%w: proposal_revision_number must be positive", ErrInvalidCommand)
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidCommand, field.name)
		}
	}
	if len(c.AssignmentWrites) == 0 {
		return fmt.Errorf("%w: assignment write evidence is required", ErrInvalidCommand)
	}
	seenFields := map[string]bool{}
	for _, write := range c.AssignmentWrites {
		if (write.FieldPath != "assignment.job_code" && write.FieldPath != "assignment.grade") ||
			strings.TrimSpace(write.CurrentValue) == "" || strings.TrimSpace(write.ProposedValue) == "" ||
			strings.TrimSpace(write.AuthorityDecision) == "" || strings.TrimSpace(write.ExpectedSource) == "" || seenFields[write.FieldPath] {
			return fmt.Errorf("%w: invalid or duplicate assignment write evidence for %q", ErrInvalidCommand, write.FieldPath)
		}
		seenFields[write.FieldPath] = true
	}
	if c.EffectiveAt.IsZero() || c.RecordedAt.IsZero() {
		return fmt.Errorf("%w: effective_at and recorded_at are required", ErrInvalidCommand)
	}
	if err := c.BasePay.Validate(); err != nil {
		return fmt.Errorf("%w: base_pay: %v", ErrInvalidCommand, err)
	}
	if err := c.ValidateParticipants(c.PlanParticipants); err != nil {
		return err
	}
	if distinctNonEmpty(c.ApprovalDecisionIDs) < 2 || distinctNonEmpty(c.TaskSubmissionIDs) == 0 {
		return ErrApprovalEvidence
	}
	if c.ManagerWorkerID == c.WorkerID || slices.Contains(c.ManagerAncestorWorkerIDs, c.WorkerID) {
		return ErrManagerCycle
	}
	seen := map[string]bool{}
	for i, effect := range c.Effects {
		if strings.TrimSpace(effect.EffectID) == "" || strings.TrimSpace(effect.DestinationRef) == "" ||
			strings.TrimSpace(effect.SchemaRef) == "" || len(effect.Payload) == 0 {
			return fmt.Errorf("%w: effect %d is incomplete", ErrInvalidCommand, i)
		}
		if seen[effect.EffectID] {
			return fmt.Errorf("%w: duplicate effect %q", ErrInvalidCommand, effect.EffectID)
		}
		seen[effect.EffectID] = true
	}
	return nil
}

// ValidateParticipants proves that the plan participant ids materialized by
// the caller select every local writer and no undeclared local writer.
func (c Command) ValidateParticipants(actual []string) error {
	want := c.Participants()
	if len(actual) != len(want) {
		return fmt.Errorf("%w: got %v, want %v", ErrParticipantSet, actual, want)
	}
	for _, id := range want {
		if !slices.Contains(actual, id) {
			return fmt.Errorf("%w: missing %s", ErrParticipantSet, id)
		}
	}
	return nil
}

func distinctNonEmpty(ids []string) int {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			seen[id] = struct{}{}
		}
	}
	return len(seen)
}
