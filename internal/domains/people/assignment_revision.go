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
	ReviseAssignmentIntentType = "hcmnext.people.revise_assignment/v1"
	AssignmentOrgUnitField     = "assignment.org_unit"
	AssignmentLocationField    = "assignment.location"
	AssignmentCostCenterField  = "assignment.cost_center"
	AssignmentFTEField         = "assignment.fte"
)

var (
	ErrAssignmentRevisionInvalid  = errors.New("people: assignment revision is invalid")
	ErrAssignmentRevisionNoChange = errors.New("people: assignment revision changes no field")
	ErrAssignmentRevisionOverlaps = errors.New("people: assignment revision overlaps an earlier revision")
)

// AssignmentRevision is the People-owned `people.revise_assignment`
// capability: one effective-dated revision of the closed assignment field
// set. The shape is closed by construction -- organization, location, cost
// center, job, grade and FTE -- so unrelated worker fields cannot be
// smuggled into a transfer or correction. Grade rides along only so the
// promotion mutation can delegate its whole write set here; a revision that
// changes grade alone is still a revision, not a promotion.
//
// Each field pair is optional: an empty target leaves the field unrevised.
// Clearing a field is not a revision and needs its own mutation.
type AssignmentRevision struct {
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

	CurrentOrgUnit    string
	TargetOrgUnit     string
	CurrentLocation   string
	TargetLocation    string
	CurrentCostCenter string
	TargetCostCenter  string
	CurrentJobCode    string
	TargetJobCode     string
	CurrentLevel      string
	TargetLevel       string
	CurrentFTE        string
	TargetFTE         string
}

// Validate proves the command is one effective-dated assignment revision.
// It is pure and safe to rerun at commit time.
func (m AssignmentRevision) Validate() error {
	if err := m.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrAssignmentRevisionInvalid, err)
	}
	for name, value := range map[string]string{
		"proposal_revision_id": m.ProposalRevisionID, "proposal_digest": m.ProposalDigest,
		"actor_principal_id": m.ActorPrincipalID, "authority_decision": m.AuthorityDecision,
		"worker_id": m.WorkerID, "assignment_id": m.AssignmentID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrAssignmentRevisionInvalid, name)
		}
	}
	if err := m.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrAssignmentRevisionInvalid, err)
	}
	if m.Subject.Kind != "worker" {
		return fmt.Errorf("%w: subject kind %q is not a worker", ErrAssignmentRevisionInvalid, m.Subject.Kind)
	}
	if m.Subject.SubjectID != m.WorkerID {
		return fmt.Errorf("%w: subject is not the revised worker", ErrAssignmentRevisionInvalid)
	}
	if m.ResourceKey.Validate() != nil || m.ResourceKey.Tenant != m.Tenant {
		return fmt.Errorf("%w: resource key is outside tenant", ErrAssignmentRevisionInvalid)
	}
	if err := m.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrAssignmentRevisionInvalid, err)
	}
	if m.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: effective interval must be LOCAL_DATE", ErrAssignmentRevisionInvalid)
	}
	if !m.ExpectedRevision.IsSpecified() {
		return fmt.Errorf("%w: baseline revision is missing", ErrAssignmentRevisionInvalid)
	}
	if len(m.changedPairs()) == 0 {
		return ErrAssignmentRevisionNoChange
	}
	return nil
}

// AssignmentFieldChange is one revised field with its before and after
// canonical text.
type AssignmentFieldChange struct {
	FieldPath             string
	CurrentCanonicalText  string
	ProposedCanonicalText string
}

// changedPairs projects the revised fields in stable capability order:
// organization, location, cost center, job, grade, FTE.
func (m AssignmentRevision) changedPairs() []AssignmentFieldChange {
	pairs := []struct {
		field   string
		current string
		target  string
	}{
		{AssignmentOrgUnitField, m.CurrentOrgUnit, m.TargetOrgUnit},
		{AssignmentLocationField, m.CurrentLocation, m.TargetLocation},
		{AssignmentCostCenterField, m.CurrentCostCenter, m.TargetCostCenter},
		{AssignmentJobField, m.CurrentJobCode, m.TargetJobCode},
		{AssignmentLevelField, m.CurrentLevel, m.TargetLevel},
		{AssignmentFTEField, m.CurrentFTE, m.TargetFTE},
	}
	changes := make([]AssignmentFieldChange, 0, len(pairs))
	for _, pair := range pairs {
		if pair.target == "" || pair.target == pair.current {
			continue
		}
		changes = append(changes, AssignmentFieldChange{
			FieldPath: pair.field, CurrentCanonicalText: pair.current, ProposedCanonicalText: pair.target,
		})
	}
	return changes
}

// ChangedFields validates the revision and projects its revised fields in
// stable order. It is the one projection both the revision's own fenced
// writes and the promotion mutation's legacy writes are built from.
func (m AssignmentRevision) ChangedFields() ([]AssignmentFieldChange, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m.changedPairs(), nil
}

// PlannedWrites returns one fenced UPDATE per revised field in stable order.
// Every write carries the revision interval and the baseline, so a stale
// stream cannot partially advance and two revisions can never fence the
// same effective time twice.
func (m AssignmentRevision) PlannedWrites() ([]intent.PlannedWrite, error) {
	changes, err := m.ChangedFields()
	if err != nil {
		return nil, err
	}
	writes := make([]intent.PlannedWrite, 0, len(changes))
	for _, change := range changes {
		writes = append(writes, intent.PlannedWrite{Subject: m.Subject, ResourceKey: m.ResourceKey,
			FieldPath: change.FieldPath, CurrentCanonicalText: change.CurrentCanonicalText,
			ProposedCanonicalText: change.ProposedCanonicalText, SourceAuthorityDecision: m.AuthorityDecision,
			ExpectedRevision: m.ExpectedRevision, Operation: intent.WriteOperationUpdate, EffectiveInterval: m.Effective})
	}
	return writes, nil
}

// Explain returns identifiers and revised field names without exposing the
// proposed values, making it suitable for audit telemetry.
func (m AssignmentRevision) Explain() string {
	changes := m.changedPairs()
	fields := make([]string, 0, len(changes))
	for _, change := range changes {
		fields = append(fields, strings.TrimPrefix(change.FieldPath, "assignment."))
	}
	return fmt.Sprintf("people revise_assignment proposal=%s worker=%s assignment=%s fields=%s baseline=%s",
		m.ProposalRevisionID, m.WorkerID, m.AssignmentID, strings.Join(fields, ","), m.ExpectedRevision.Stream())
}

// AssignmentRevisionChain is the accepted effective intervals of one
// assignment's revision history, oldest first. Revisions fence effective
// time: a new revision must abut or follow the history, never overlap it.
type AssignmentRevisionChain []values.EffectiveInterval

// Append accepts next into the chain, refusing any interval that overlaps a
// revision already accepted. An interval that starts exactly where an
// earlier one ends abuts it and is accepted: half-open boundaries never
// overlap.
func (c AssignmentRevisionChain) Append(next values.EffectiveInterval) (AssignmentRevisionChain, error) {
	if err := next.Validate(); err != nil {
		return c, fmt.Errorf("%w: %v", ErrAssignmentRevisionInvalid, err)
	}
	for _, have := range c {
		overlap, err := have.Overlaps(next)
		if err != nil {
			return c, fmt.Errorf("%w: %v", ErrAssignmentRevisionInvalid, err)
		}
		if overlap {
			return c, fmt.Errorf("%w: %s overlaps %s", ErrAssignmentRevisionOverlaps, next, have)
		}
	}
	return append(c, next), nil
}
