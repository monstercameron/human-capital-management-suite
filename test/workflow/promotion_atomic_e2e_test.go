package workflow_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	datacommit "github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
)

type atomicPromotionFixture struct {
	worker, assignment, position, base, reservation uuid.UUID
}

func e2eMust[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func seedAtomicPromotion(t *testing.T, db *pgtest.DB, tenant, proposalID uuid.UUID, at time.Time) (atomicPromotionFixture, domaincommit.Command) {
	t.Helper()
	for _, schemaRef := range []string{"hcmnext.promotion.payroll/v1", "hcmnext.promotion.iam/v1"} {
		db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
			VALUES ($1,$2,$2,1,$2,'PROTOBUF','EXTERNAL_PAYLOAD_EVIDENCE')`, tenant, schemaRef)
	}
	ids := struct {
		person, worker, legal, employment, assignment, org, job, position, occupancy uuid.UUID
		pkg, base, budget, reservation                                               uuid.UUID
	}{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	recorded := at.Add(-time.Hour)
	people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
	var assignmentFact aggregates.Assignment
	var workerFact aggregates.Worker
	var employmentFact aggregates.Employment
	var jobFact aggregates.Job
	var positionFact aggregates.JobPosition
	var packageFact aggregates.CompensationPackage
	var baseFact aggregates.CompensationComponent
	var reservationFact aggregates.BudgetReservation
	inTenantTx(t, db, tenant, func(tx dbport.Tx) error {
		ctx := context.Background()
		legal := e2eMust(aggregates.NewLegalEntity(tenant, ids.legal, at, nil, recorded, "Acme Inc", "ACTIVE"))
		if _, err := organization.PutLegalEntity(ctx, tx, legal); err != nil {
			return err
		}
		org := e2eMust(aggregates.NewOrganizationUnit(tenant, ids.org, at, nil, recorded, "DEPARTMENT", "ENG", "Engineering", &ids.legal, nil, "ACTIVE"))
		if _, err := organization.PutOrganizationUnit(ctx, tx, org); err != nil {
			return err
		}
		jobFact = e2eMust(aggregates.NewJob(tenant, ids.job, at, nil, recorded, "ENG-MGR1", "Engineering Manager", "ENGINEERING", "M1", "EXEMPT"))
		if _, err := organization.PutJob(ctx, tx, jobFact); err != nil {
			return err
		}
		positionFact = e2eMust(aggregates.NewJobPosition(tenant, ids.position, ids.job, ids.org, &ids.legal, at, nil, recorded, "POS-ENG-MGR-1", "NYC", "1.0000", "OPEN"))
		if _, err := organization.PutJobPosition(ctx, tx, positionFact); err != nil {
			return err
		}

		person := e2eMust(aggregates.NewPerson(tenant, ids.person, at, nil, recorded, "ACTIVE", "Jordan Lee", "Jordan"))
		if _, err := people.PutPerson(ctx, tx, person); err != nil {
			return err
		}
		workerFact = e2eMust(aggregates.NewWorker(tenant, ids.worker, ids.person, at, nil, recorded, "W-100", "EMPLOYEE", "ACTIVE"))
		if _, err := people.PutWorker(ctx, tx, workerFact); err != nil {
			return err
		}
		employmentFact = e2eMust(aggregates.NewEmployment(tenant, ids.employment, ids.worker, ids.legal, at, nil, recorded, "EMPLOYEE", "ACTIVE", &at))
		if _, err := people.PutEmployment(ctx, tx, employmentFact); err != nil {
			return err
		}
		assignmentFact = e2eMust(aggregates.NewAssignment(tenant, ids.assignment, ids.employment, true, at, nil, recorded,
			"ENG-SWE3", "P3", &ids.org, nil, "NYC", "US-NY", "1.0000", "manager-rel:old"))
		if _, err := people.PutAssignment(ctx, tx, assignmentFact); err != nil {
			return err
		}

		packageFact = e2eMust(aggregates.NewCompensationPackage(tenant, ids.pkg, ids.worker, &ids.employment, &ids.assignment, at, nil, recorded, "USD"))
		if _, err := compensation.PutCompensationPackage(ctx, tx, packageFact); err != nil {
			return err
		}
		oldPay := e2eMust(values.NewMoney("165000.00", "USD", 2, values.RoundingExactRequired))
		baseFact = e2eMust(aggregates.NewCompensationComponent(tenant, ids.base, ids.pkg, at, nil, recorded, "BASE_PAY", oldPay, "ANNUAL"))
		if _, err := compensation.PutCompensationComponent(ctx, tx, baseFact); err != nil {
			return err
		}
		budget := e2eMust(aggregates.NewWorkforceBudget(tenant, ids.budget, at, nil, recorded,
			"COMPENSATION_POOL", "hcmnext.finance", "cost-center:ENG", "FY2026", "USD", "MONEY", "500000.00", "budget/1"))
		if _, err := compensation.PutWorkforceBudget(ctx, tx, budget); err != nil {
			return err
		}
		expiry := at.Add(24 * time.Hour)
		reservationFact = e2eMust(aggregates.NewBudgetReservation(tenant, ids.reservation, ids.budget, &proposalID,
			at, nil, recorded, "15000.00", "USD", "HELD", &expiry))
		_, err := compensation.PutBudgetReservation(ctx, tx, reservationFact)
		return err
	})
	newPay := e2eMust(values.NewMoney("180000.00", "USD", 2, values.RoundingExactRequired))
	cmd := domaincommit.Command{
		TenantID: tenant.String(), ProposalRevisionID: proposalID.String(),
		AuthorityDigest: "sha256:execution-authority", ActorPrincipalID: humanworkPrincipalHRBP,
		EffectiveAt: at, RecordedAt: at.Add(2 * time.Hour), WorkerID: ids.worker.String(), EmploymentID: ids.employment.String(),
		AssignmentID: ids.assignment.String(), TargetJobCode: "ENG-MGR1", TargetJobID: ids.job.String(), TargetGrade: "M1", TargetOrganizationID: ids.org.String(),
		TargetPositionID: ids.position.String(), ManagerRelationshipRef: "manager-rel:new", ManagerWorkerID: uuid.New().String(),
		ManagerAncestorWorkerIDs: []string{uuid.New().String()}, Location: "NYC", PayZone: "US-NY", FTE: "1.0000",
		PositionOccupancyID: ids.occupancy.String(), PositionReservationRef: "position-hold:approved", CompensationPackageID: ids.pkg.String(),
		BasePayComponentID: ids.base.String(), BasePay: newPay, PayFrequency: "ANNUAL", BudgetReservationID: ids.reservation.String(),
		BudgetReservationRef: "budget-hold:approved", ExpectedWorkerDigest: workerFact.Digest, ExpectedEmploymentDigest: employmentFact.Digest,
		ExpectedAssignmentDigest: assignmentFact.Digest, ExpectedJobDigest: jobFact.Digest, ExpectedPositionDigest: positionFact.Digest,
		ExpectedPackageDigest: packageFact.Digest, ExpectedBasePayDigest: baseFact.Digest, ExpectedBudgetDigest: reservationFact.Digest,
		Effects: []domaincommit.ExternalEffect{
			{EffectID: "payroll:" + proposalID.String(), DestinationRef: "payroll", SchemaRef: "hcmnext.promotion.payroll/v1", Payload: []byte(`{"kind":"PAYROLL_SYNC"}`)},
			{EffectID: "iam:" + proposalID.String(), DestinationRef: "iam", SchemaRef: "hcmnext.promotion.iam/v1", Payload: []byte(`{"kind":"IAM_SYNC"}`)},
		},
	}
	cmd.PlanParticipants = cmd.Participants()
	return atomicPromotionFixture{worker: ids.worker, assignment: ids.assignment, position: ids.position, base: ids.base, reservation: ids.reservation}, cmd
}

const humanworkPrincipalHRBP = "principal:hrbp"

func TestPromotionBackendCommitsTheWorkflowTransactionEndToEnd(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	var seeded atomicPromotionFixture
	f := runPromotionToCompleteWithTerminal(t, "promotion-atomic-e2e", at,
		func(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, proposal intent.ProposalRevision, plan *workflow.CompiledWorkflow, base *effects.LedgerTerminalWriter) execute.TerminalWriter {
			proposalID := uuid.MustParse(proposal.ProposalRevisionID)
			var cmd domaincommit.Command
			seeded, cmd = seedAtomicPromotion(t, db, tenantID, proposalID, at)
			cmd.ProposalDigest = proposal.MaterialDigest.Digest
			cmd.WorkflowPlanDigest = plan.Digest()
			participants := []intent.PlanParticipant{
				{ParticipantID: domaincommit.ParticipantAssignment, StreamID: "people.assignment/" + cmd.AssignmentID, StorageClass: "LOCAL_POSTGRES", Local: true},
				{ParticipantID: domaincommit.ParticipantOccupancy, StreamID: "position.occupancy/" + cmd.TargetPositionID, StorageClass: "LOCAL_POSTGRES", Local: true},
				{ParticipantID: domaincommit.ParticipantCompensation, StreamID: "rewards.compensation/" + cmd.CompensationPackageID, StorageClass: "LOCAL_POSTGRES", Local: true},
				{ParticipantID: domaincommit.ParticipantBudget, StreamID: "rewards.budget/" + cmd.BudgetReservationID, StorageClass: "LOCAL_POSTGRES", Local: true},
			}
			for _, effect := range cmd.Effects {
				participants = append(participants, intent.PlanParticipant{ParticipantID: effect.EffectID, StreamID: "external/" + effect.DestinationRef, StorageClass: "REMOTE", Local: false})
			}
			tenantSlug := values.TenantId(tenantID.String())
			resolution, err := transaction.ResolveConsistencyBoundary(transaction.ConsistencyBoundary{
				BoundaryID: "boundary:promotion-e2e", Tenant: tenantSlug, CellID: "cell-e2e", CoordinatorID: "coordinator:promotion-e2e",
				Admitted: []transaction.AdmissionSelector{{StorageClass: "LOCAL_POSTGRES"}}, Isolation: transaction.IsolationSerializable,
				Protocol: transaction.CommitProtocolSingleDatabaseACID, CoordinatorEpoch: 1,
				CrossBoundaryDisposition: transaction.CrossBoundaryDispositionSplitIntoEffects,
			}, intent.TransactionPlan{PlanID: "promotion.transaction/" + proposalID.String(), Tenant: tenantSlug, Participants: participants}, 1)
			if err != nil {
				t.Fatalf("resolve promotion transaction plan: %v", err)
			}
			cmd, err = promotionterminal.BindResolution(resolution, cmd)
			if err != nil {
				t.Fatalf("bind promotion transaction plan: %v", err)
			}
			return &promotionterminal.Writer{
				// PROMOUX-016: only the approved END code writes successor facts.
				ApprovedTerminalCode: demoTerminalCode,
				Resolver: promotionterminal.ResolverFunc(func(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (domaincommit.Command, error) {
					rows, err := tx.Query(ctx, `SELECT decision_id::text, kind FROM work_item_decision WHERE tenant_id=$1 AND workflow_instance_id=$2 ORDER BY kind`, req.TenantID, req.InstanceID)
					if err != nil {
						return domaincommit.Command{}, err
					}
					defer rows.Close()
					cmd.ApprovalDecisionIDs = []string{req.Proposal.ApprovalRef}
					cmd.TaskSubmissionIDs = nil
					for rows.Next() {
						var id, kind string
						if err := rows.Scan(&id, &kind); err != nil {
							return domaincommit.Command{}, err
						}
						if kind == "APPROVAL" {
							cmd.ApprovalDecisionIDs = append(cmd.ApprovalDecisionIDs, id)
						} else if kind == "TASK" {
							cmd.TaskSubmissionIDs = append(cmd.TaskSubmissionIDs, id)
						}
					}
					return cmd, rows.Err()
				}),
				Mutation: datacommit.Writer{}, Next: base,
			}
		})

	ctx := context.Background()
	checks := []struct {
		query string
		args  []any
		want  string
	}{
		{`SELECT job_code FROM assignment WHERE tenant_id=$1 AND entity_id=$2 AND superseded_at IS NULL`, []any{f.tenantID, seeded.assignment}, "ENG-MGR1"},
		{`SELECT manager_relationship_ref FROM assignment WHERE tenant_id=$1 AND entity_id=$2 AND superseded_at IS NULL`, []any{f.tenantID, seeded.assignment}, "manager-rel:new"},
		{`SELECT amount::text FROM compensation_component WHERE tenant_id=$1 AND entity_id=$2 AND superseded_at IS NULL`, []any{f.tenantID, seeded.base}, "180000.0000"},
		{`SELECT status FROM budget_reservation WHERE tenant_id=$1 AND entity_id=$2 AND superseded_at IS NULL`, []any{f.tenantID, seeded.reservation}, "COMMITTED"},
	}
	for _, check := range checks {
		var got string
		if err := f.db.Conn.QueryRow(ctx, check.query, check.args...).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != check.want {
			t.Errorf("query %q = %q, want %q", check.query, got, check.want)
		}
	}
	for table, want := range map[string]int{"position_occupancy": 1, "ledger_event": 1, "outbox": 3} {
		var got int
		if err := f.db.Conn.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id=$1", table), f.tenantID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
}
