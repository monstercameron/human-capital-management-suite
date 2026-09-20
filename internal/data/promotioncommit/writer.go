// Package promotioncommit persists the bounded local portion of a Promotion.
// It is a data-plane adapter: business validation lives in
// internal/domains/promotion/commit and transaction ownership stays with the
// workflow driver or transaction coordinator that supplied the dbport.Tx.
package promotioncommit

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	promotioncommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
)

var (
	ErrBaselineChanged     = errors.New("promotion commit: an authoritative baseline changed")
	ErrReservationNotHeld  = errors.New("promotion commit: budget reservation is not held")
	ErrReservationBinding  = errors.New("promotion commit: reservation is bound to another proposal")
	ErrAggregateBinding    = errors.New("promotion commit: aggregate relationship does not match the command")
	ErrInactiveWorker      = errors.New("promotion commit: worker or employment is not active")
	ErrPositionUnavailable = errors.New("promotion commit: target position is not open")
	ErrCurrencyMismatch    = errors.New("promotion commit: compensation and budget currencies differ")
)

// Failpoint is invoked after each named local participant. It exists for
// deterministic rollback tests and may be nil in production.
type Failpoint func(stage string) error

// Receipt names every successor row and durable effect produced by Write.
type Receipt struct {
	AssignmentRowID string
	OccupancyRowID  string
	BasePayRowID    string
	BudgetRowID     string
	OutboxIDs       []string
}

// Writer appends all bounded local Promotion facts through existing aggregate
// stores. It never opens, commits, or rolls back a transaction.
type Writer struct {
	People       aggregates.PeopleStore
	Organization aggregates.OrganizationStore
	Compensation aggregates.CompensationStore
	Failpoint    Failpoint
}

// RegisterEffectSchemas registers the payload schemas a tenant's promotion
// effects are published under, so the outbox rows a commit enqueues satisfy
// their foreign key into the payload schema registry (WF-RUN-034).
//
// It is deliberately a composition-root act and not something [Writer.Write]
// does for whatever schema a command happens to name: a promotion that
// declares an effect under a schema nobody published is refused by that
// foreign key, and that refusal is the point. The composition registers the
// exact schemas its own terminal resolver renders
// (internal/platform/execution/promotionterminal.EffectSchemaRefs).
// Registration is idempotent and never overwrites an existing descriptor.
func RegisterEffectSchemas(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, schemaRefs ...string) error {
	if tx == nil || tenant == uuid.Nil {
		return fmt.Errorf("promotion commit: register effect schemas: a transaction and tenant are required")
	}
	for _, schemaRef := range schemaRefs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO payload_schema (
				tenant_id, schema_ref, schema_id, schema_version,
				message_full_name, wire_format, canonicalization_profile)
			VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')
			ON CONFLICT DO NOTHING`, tenant, schemaRef); err != nil {
			return fmt.Errorf("promotion commit: register the effect payload schema %s: %w", schemaRef, err)
		}
	}
	return nil
}

func parseID(name, text string) (uuid.UUID, error) {
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, fmt.Errorf("promotion commit: %s %q is not a UUID: %w", name, text, err)
	}
	return id, nil
}

func baseline(name, got, want string) error {
	if got != want {
		return fmt.Errorf("%w: %s digest is %q, expected %q", ErrBaselineChanged, name, got, want)
	}
	return nil
}

func (w Writer) fail(stage string) error {
	if w.Failpoint == nil {
		return nil
	}
	if err := w.Failpoint(stage); err != nil {
		return fmt.Errorf("promotion commit: failpoint after %s: %w", stage, err)
	}
	return nil
}

// Write appends assignment/manager, target occupancy, base-pay and budget
// reservation successor facts, then queues declared remote effects. The
// caller must invoke its governed terminal ledger writer through the same tx.
func (w Writer) Write(ctx context.Context, tx dbport.Tx, cmd promotioncommit.Command) (Receipt, error) {
	if tx == nil {
		return Receipt{}, fmt.Errorf("promotion commit: transaction is required")
	}
	if err := cmd.Validate(); err != nil {
		return Receipt{}, err
	}

	tenant, err := parseID("tenant_id", cmd.TenantID)
	if err != nil {
		return Receipt{}, err
	}
	workerID, err := parseID("worker_id", cmd.WorkerID)
	if err != nil {
		return Receipt{}, err
	}
	employmentID, err := parseID("employment_id", cmd.EmploymentID)
	if err != nil {
		return Receipt{}, err
	}
	assignmentID, err := parseID("assignment_id", cmd.AssignmentID)
	if err != nil {
		return Receipt{}, err
	}
	organizationID, err := parseID("target_organization_id", cmd.TargetOrganizationID)
	if err != nil {
		return Receipt{}, err
	}
	positionID, err := parseID("target_position_id", cmd.TargetPositionID)
	if err != nil {
		return Receipt{}, err
	}
	jobID, err := parseID("target_job_id", cmd.TargetJobID)
	if err != nil {
		return Receipt{}, err
	}
	occupancyID, err := parseID("position_occupancy_id", cmd.PositionOccupancyID)
	if err != nil {
		return Receipt{}, err
	}
	packageID, err := parseID("compensation_package_id", cmd.CompensationPackageID)
	if err != nil {
		return Receipt{}, err
	}
	basePayID, err := parseID("base_pay_component_id", cmd.BasePayComponentID)
	if err != nil {
		return Receipt{}, err
	}
	budgetReservationID, err := parseID("budget_reservation_id", cmd.BudgetReservationID)
	if err != nil {
		return Receipt{}, err
	}
	proposalID, err := parseID("proposal_revision_id", cmd.ProposalRevisionID)
	if err != nil {
		return Receipt{}, err
	}

	// Serialize promotions that target the same assignment. Baseline digests
	// detect an already-committed change, while this transaction-scoped lock
	// closes the interval in which two database sessions could both observe the
	// same baseline and append competing successors. The lock is released by
	// PostgreSQL on commit or rollback, including every failure path below.
	lockKey := tenant.String() + ":promotion-assignment:" + assignmentID.String()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: acquire assignment fence: %w", err)
	}

	// Re-read every mutable baseline inside the commit transaction. These are
	// the final execution-time fences after approvals and any wait.
	worker, err := w.People.CurrentWorker(ctx, tx, tenant, workerID, cmd.EffectiveAt)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: read worker: %w", err)
	}
	if err := baseline("people.worker", worker.Digest, cmd.ExpectedWorkerDigest); err != nil {
		return Receipt{}, err
	}
	if worker.LifecycleStatus != "ACTIVE" {
		return Receipt{}, ErrInactiveWorker
	}
	employment, err := w.People.CurrentEmployment(ctx, tx, tenant, employmentID, cmd.EffectiveAt)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: read employment: %w", err)
	}
	if err := baseline("people.employment", employment.Digest, cmd.ExpectedEmploymentDigest); err != nil {
		return Receipt{}, err
	}
	if employment.WorkerRef != workerID {
		return Receipt{}, fmt.Errorf("%w: employment belongs to worker %s, not %s", ErrAggregateBinding, employment.WorkerRef, workerID)
	}
	if employment.EmploymentStatus != "ACTIVE" {
		return Receipt{}, ErrInactiveWorker
	}
	assignment, err := w.People.CurrentAssignment(ctx, tx, tenant, assignmentID, cmd.EffectiveAt)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: read assignment: %w", err)
	}
	if err := baseline(promotioncommit.ParticipantAssignment, assignment.Digest, cmd.ExpectedAssignmentDigest); err != nil {
		return Receipt{}, err
	}
	if assignment.EmploymentRef != employmentID {
		return Receipt{}, fmt.Errorf("%w: assignment belongs to employment %s, not %s", ErrAggregateBinding, assignment.EmploymentRef, employmentID)
	}
	job, err := w.Organization.CurrentJob(ctx, tx, tenant, jobID, cmd.EffectiveAt)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: read target job: %w", err)
	}
	if err := baseline("organization.job", job.Digest, cmd.ExpectedJobDigest); err != nil {
		return Receipt{}, err
	}
	if job.Code != cmd.TargetJobCode || job.Grade != cmd.TargetGrade {
		return Receipt{}, fmt.Errorf("%w: target job is %s/%s, not %s/%s", ErrAggregateBinding, job.Code, job.Grade, cmd.TargetJobCode, cmd.TargetGrade)
	}
	position, err := w.Organization.CurrentJobPosition(ctx, tx, tenant, positionID, cmd.EffectiveAt)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: read position: %w", err)
	}
	if err := baseline(promotioncommit.ParticipantOccupancy, position.Digest, cmd.ExpectedPositionDigest); err != nil {
		return Receipt{}, err
	}
	if position.OrganizationRef != organizationID || position.JobRef != jobID {
		return Receipt{}, fmt.Errorf("%w: position belongs to organization/job %s/%s, not %s/%s", ErrAggregateBinding, position.OrganizationRef, position.JobRef, organizationID, jobID)
	}
	if position.LifecycleState != "OPEN" {
		return Receipt{}, ErrPositionUnavailable
	}
	compPackage, err := w.Compensation.CurrentCompensationPackage(ctx, tx, tenant, packageID, cmd.EffectiveAt)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: read compensation package: %w", err)
	}
	if err := baseline("rewards.compensation_package", compPackage.Digest, cmd.ExpectedPackageDigest); err != nil {
		return Receipt{}, err
	}
	if compPackage.WorkerRef != workerID || compPackage.EmploymentRef == nil || *compPackage.EmploymentRef != employmentID ||
		compPackage.AssignmentRef == nil || *compPackage.AssignmentRef != assignmentID {
		return Receipt{}, fmt.Errorf("%w: compensation package does not belong to the promoted worker/employment/assignment", ErrAggregateBinding)
	}
	basePay, err := w.Compensation.CurrentCompensationComponent(ctx, tx, tenant, basePayID, cmd.EffectiveAt)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: read base pay: %w", err)
	}
	if err := baseline(promotioncommit.ParticipantCompensation, basePay.Digest, cmd.ExpectedBasePayDigest); err != nil {
		return Receipt{}, err
	}
	if basePay.PackageRef != packageID || basePay.ComponentType != "BASE_PAY" {
		return Receipt{}, fmt.Errorf("%w: compensation component is not BASE_PAY in package %s", ErrAggregateBinding, packageID)
	}
	reservation, err := w.Compensation.CurrentBudgetReservation(ctx, tx, tenant, budgetReservationID, cmd.EffectiveAt)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: read budget reservation: %w", err)
	}
	if err := baseline(promotioncommit.ParticipantBudget, reservation.Digest, cmd.ExpectedBudgetDigest); err != nil {
		return Receipt{}, err
	}
	if reservation.Status != "HELD" {
		return Receipt{}, ErrReservationNotHeld
	}
	if reservation.ProposalRef == nil || *reservation.ProposalRef != proposalID {
		return Receipt{}, ErrReservationBinding
	}
	if cmd.BasePay.Currency() != compPackage.Currency || cmd.BasePay.Currency() != reservation.Currency {
		return Receipt{}, fmt.Errorf("%w: pay=%s package=%s reservation=%s", ErrCurrencyMismatch, cmd.BasePay.Currency(), compPackage.Currency, reservation.Currency)
	}
	// The hold is bounded relative to the promotion's effective business
	// instant.  Comparing it with RecordedAt makes a valid, already-effective
	// promotion impossible to execute after the hold window, even though the
	// reservation is being read at that same effective instant and is still the
	// approved baseline for this commit.
	if reservation.Expiry != nil && !reservation.Expiry.After(cmd.EffectiveAt) {
		return Receipt{}, ErrReservationNotHeld
	}

	newAssignment, err := aggregates.NewAssignment(
		tenant, assignmentID, employmentID, assignment.PrimaryFlag,
		cmd.EffectiveAt, nil, cmd.RecordedAt, cmd.TargetJobCode, cmd.TargetGrade,
		&organizationID, &positionID, cmd.Location, cmd.PayZone, cmd.FTE, cmd.ManagerRelationshipRef,
	)
	if err != nil {
		return Receipt{}, err
	}
	newOccupancy, err := aggregates.NewPositionOccupancy(
		tenant, occupancyID, positionID, &assignmentID, &workerID,
		cmd.EffectiveAt, nil, cmd.RecordedAt, cmd.FTE, assignment.PrimaryFlag,
	)
	if err != nil {
		return Receipt{}, err
	}
	newBasePay, err := aggregates.NewCompensationComponent(
		tenant, basePayID, packageID, cmd.EffectiveAt, nil, cmd.RecordedAt,
		"BASE_PAY", cmd.BasePay, cmd.PayFrequency,
	)
	if err != nil {
		return Receipt{}, err
	}
	committedBudget, err := aggregates.NewBudgetReservation(
		tenant, budgetReservationID, reservation.BudgetRef, &proposalID,
		cmd.EffectiveAt, nil, cmd.RecordedAt, reservation.Amount, reservation.Currency,
		"COMMITTED", reservation.Expiry,
	)
	if err != nil {
		return Receipt{}, err
	}

	var receipt Receipt
	rowID, err := w.People.PutAssignment(ctx, tx, newAssignment)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: append assignment successor: %w", err)
	}
	receipt.AssignmentRowID = rowID.String()
	if err := w.fail(promotioncommit.ParticipantAssignment); err != nil {
		return Receipt{}, err
	}

	rowID, err = w.Organization.PutPositionOccupancy(ctx, tx, newOccupancy)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: consume position reservation: %w", err)
	}
	receipt.OccupancyRowID = rowID.String()
	if err := w.fail(promotioncommit.ParticipantOccupancy); err != nil {
		return Receipt{}, err
	}

	rowID, err = w.Compensation.PutCompensationComponent(ctx, tx, newBasePay)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: append base-pay successor: %w", err)
	}
	receipt.BasePayRowID = rowID.String()
	if err := w.fail(promotioncommit.ParticipantCompensation); err != nil {
		return Receipt{}, err
	}

	rowID, err = w.Compensation.PutBudgetReservation(ctx, tx, committedBudget)
	if err != nil {
		return Receipt{}, fmt.Errorf("promotion commit: commit budget reservation: %w", err)
	}
	receipt.BudgetRowID = rowID.String()
	if err := w.fail(promotioncommit.ParticipantBudget); err != nil {
		return Receipt{}, err
	}

	orderingKey := "promotion." + cmd.ProposalRevisionID
	for _, effect := range cmd.Effects {
		record, enqueueErr := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
			Tenant: tenant, EffectIdentity: effect.EffectID, OrderingKey: orderingKey,
			SchemaRef: effect.SchemaRef, Payload: effect.Payload,
		})
		if enqueueErr != nil {
			return Receipt{}, fmt.Errorf("promotion commit: queue effect %s: %w", effect.EffectID, enqueueErr)
		}
		receipt.OutboxIDs = append(receipt.OutboxIDs, record.OutboxID.String())
	}
	if err := w.fail("outbox"); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}
