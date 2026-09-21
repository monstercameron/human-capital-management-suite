package promotioncommit_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type fixture struct {
	db                                                    *pgtest.DB
	tenant, person, worker, legal, employment, assignment uuid.UUID
	org, job, position, occupancy, packageID, baseID      uuid.UUID
	budget, reservation, proposal                         uuid.UUID
	at, recorded                                          time.Time
	workerFact                                            aggregates.Worker
	employmentFact                                        aggregates.Employment
	assignmentFact                                        aggregates.Assignment
	jobFact                                               aggregates.Job
	positionFact                                          aggregates.JobPosition
	packageFact                                           aggregates.CompensationPackage
	baseFact                                              aggregates.CompensationComponent
	budgetFact                                            aggregates.BudgetReservation
}

func mustValue[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func put(t *testing.T, fn func(dbport.Tx) error, db *pgtest.DB) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{
		db: pgtest.New(t), tenant: uuid.New(), person: uuid.New(), worker: uuid.New(), legal: uuid.New(),
		employment: uuid.New(), assignment: uuid.New(), org: uuid.New(), job: uuid.New(), position: uuid.New(),
		occupancy: uuid.New(), packageID: uuid.New(), baseID: uuid.New(), budget: uuid.New(), reservation: uuid.New(), proposal: uuid.New(),
		at: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), recorded: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
	f.db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local','Promotion commit','ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, f.tenant, "promotion-"+f.tenant.String()[:8])
	for _, schemaRef := range []string{"hcmnext.promotion.payroll/v1", "hcmnext.promotion.iam/v1"} {
		f.db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
			VALUES ($1,$2,$2,1,$2,'PROTOBUF','EXTERNAL_PAYLOAD_EVIDENCE')`, f.tenant, schemaRef)
	}
	people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
	put(t, func(tx dbport.Tx) error {
		legal := mustValue(aggregates.NewLegalEntity(f.tenant, f.legal, f.at, nil, f.recorded, "Acme Inc", "ACTIVE"))
		if _, err := organization.PutLegalEntity(context.Background(), tx, legal); err != nil {
			return err
		}
		org := mustValue(aggregates.NewOrganizationUnit(f.tenant, f.org, f.at, nil, f.recorded, "DEPARTMENT", "ENG", "Engineering", &f.legal, nil, "ACTIVE"))
		if _, err := organization.PutOrganizationUnit(context.Background(), tx, org); err != nil {
			return err
		}
		f.jobFact = mustValue(aggregates.NewJob(f.tenant, f.job, f.at, nil, f.recorded, "ENG-MGR1", "Engineering Manager", "ENGINEERING", "M1", "EXEMPT"))
		if _, err := organization.PutJob(context.Background(), tx, f.jobFact); err != nil {
			return err
		}
		f.positionFact = mustValue(aggregates.NewJobPosition(f.tenant, f.position, f.job, f.org, &f.legal, f.at, nil, f.recorded, "POS-ENG-MGR-1", "NYC", "1.0000", "OPEN"))
		if _, err := organization.PutJobPosition(context.Background(), tx, f.positionFact); err != nil {
			return err
		}

		person := mustValue(aggregates.NewPerson(f.tenant, f.person, f.at, nil, f.recorded, "ACTIVE", "Jordan Lee", "Jordan"))
		if _, err := people.PutPerson(context.Background(), tx, person); err != nil {
			return err
		}
		f.workerFact = mustValue(aggregates.NewWorker(f.tenant, f.worker, f.person, f.at, nil, f.recorded, "W-100", "EMPLOYEE", "ACTIVE"))
		if _, err := people.PutWorker(context.Background(), tx, f.workerFact); err != nil {
			return err
		}
		f.employmentFact = mustValue(aggregates.NewEmployment(f.tenant, f.employment, f.worker, f.legal, f.at, nil, f.recorded, "EMPLOYEE", "ACTIVE", &f.at))
		if _, err := people.PutEmployment(context.Background(), tx, f.employmentFact); err != nil {
			return err
		}
		f.assignmentFact = mustValue(aggregates.NewAssignment(f.tenant, f.assignment, f.employment, true, f.at, nil, f.recorded,
			"ENG-SWE3", "P3", &f.org, nil, "NYC", "US-NY", "1.0000", "manager-rel:old"))
		if _, err := people.PutAssignment(context.Background(), tx, f.assignmentFact); err != nil {
			return err
		}

		f.packageFact = mustValue(aggregates.NewCompensationPackage(f.tenant, f.packageID, f.worker, &f.employment, &f.assignment, f.at, nil, f.recorded, "USD"))
		if _, err := compensation.PutCompensationPackage(context.Background(), tx, f.packageFact); err != nil {
			return err
		}
		oldPay := mustValue(values.NewMoney("165000.00", "USD", 2, values.RoundingExactRequired))
		f.baseFact = mustValue(aggregates.NewCompensationComponent(f.tenant, f.baseID, f.packageID, f.at, nil, f.recorded, "BASE_PAY", oldPay, "ANNUAL"))
		if _, err := compensation.PutCompensationComponent(context.Background(), tx, f.baseFact); err != nil {
			return err
		}
		budget := mustValue(aggregates.NewWorkforceBudget(f.tenant, f.budget, f.at, nil, f.recorded,
			"COMPENSATION_POOL", "hcmnext.finance", "cost-center:ENG", "FY2026", "USD", "MONEY", "500000.00", "budget/1"))
		if _, err := compensation.PutWorkforceBudget(context.Background(), tx, budget); err != nil {
			return err
		}
		expiry := f.recorded.Add(30 * 24 * time.Hour)
		f.budgetFact = mustValue(aggregates.NewBudgetReservation(f.tenant, f.reservation, f.budget, &f.proposal,
			f.at, nil, f.recorded, "15000.00", "USD", "HELD", &expiry))
		_, err := compensation.PutBudgetReservation(context.Background(), tx, f.budgetFact)
		return err
	}, f.db)
	return f
}

func (f fixture) command(t *testing.T) domaincommit.Command {
	t.Helper()
	pay := mustValue(values.NewMoney("180000.00", "USD", 2, values.RoundingExactRequired))
	cmd := domaincommit.Command{
		TenantID: f.tenant.String(), ProposalRevisionID: f.proposal.String(), ProposalDigest: "sha256:proposal",
		PlanID: "plan:promotion", PlanDigest: "sha256:transaction-resolution", WorkflowPlanDigest: "sha256:workflow-plan",
		AuthorityDigest: "sha256:authority", ActorPrincipalID: "principal:hrbp",
		EffectiveAt: f.at, RecordedAt: f.recorded.Add(time.Hour), WorkerID: f.worker.String(), EmploymentID: f.employment.String(),
		AssignmentID: f.assignment.String(), TargetJobCode: "ENG-MGR1", TargetJobID: f.job.String(), TargetGrade: "M1", TargetOrganizationID: f.org.String(),
		TargetPositionID: f.position.String(), ManagerRelationshipRef: "manager-rel:new", ManagerWorkerID: uuid.New().String(),
		ManagerAncestorWorkerIDs: []string{uuid.New().String()}, Location: "NYC", PayZone: "US-NY", FTE: "1.0000",
		PositionOccupancyID: f.occupancy.String(), PositionReservationRef: "position-hold:1", CompensationPackageID: f.packageID.String(),
		BasePayComponentID: f.baseID.String(), BasePay: pay, PayFrequency: "ANNUAL", BudgetReservationID: f.reservation.String(),
		BudgetReservationRef: "budget-hold:1", ExpectedWorkerDigest: f.workerFact.Digest, ExpectedEmploymentDigest: f.employmentFact.Digest,
		ExpectedAssignmentDigest: f.assignmentFact.Digest, ExpectedJobDigest: f.jobFact.Digest, ExpectedPositionDigest: f.positionFact.Digest,
		ExpectedPackageDigest: f.packageFact.Digest, ExpectedBasePayDigest: f.baseFact.Digest, ExpectedBudgetDigest: f.budgetFact.Digest,
		ApprovalDecisionIDs: []string{"decision:manager", "decision:finance"}, TaskSubmissionIDs: []string{"submission:ack"},
		Effects: []domaincommit.ExternalEffect{
			{EffectID: "payroll:" + f.proposal.String(), DestinationRef: "payroll", SchemaRef: "hcmnext.promotion.payroll/v1", Payload: []byte(`{"kind":"PAYROLL_SYNC"}`)},
			{EffectID: "iam:" + f.proposal.String(), DestinationRef: "iam", SchemaRef: "hcmnext.promotion.iam/v1", Payload: []byte(`{"kind":"IAM_SYNC"}`)},
		},
	}
	cmd.PlanParticipants = cmd.Participants()
	return cmd
}

func commitCommand(t *testing.T, f fixture, writer promotioncommit.Writer, cmd domaincommit.Command) (promotioncommit.Receipt, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	receipt, err := writer.Write(ctx, tx, cmd)
	if err != nil {
		return receipt, err
	}
	if err := tx.Commit(ctx); err != nil {
		return receipt, err
	}
	return receipt, nil
}

func TestTodo_PROMO_005(t *testing.T) {
	f := newFixture(t)
	receipt, err := commitCommand(t, f, promotioncommit.Writer{}, f.command(t))
	if err != nil {
		t.Fatalf("commit promotion: %v", err)
	}
	if receipt.AssignmentRowID == "" || receipt.OccupancyRowID == "" || receipt.BasePayRowID == "" || receipt.BudgetRowID == "" || len(receipt.OutboxIDs) != 2 {
		t.Fatalf("incomplete receipt: %+v", receipt)
	}
	ctx := context.Background()
	var job, grade, manager, amount, budgetStatus string
	if err := f.db.Conn.QueryRow(ctx, `SELECT job_code, grade, manager_relationship_ref FROM assignment WHERE tenant_id=$1 AND entity_id=$2 AND superseded_at IS NULL`, f.tenant, f.assignment).Scan(&job, &grade, &manager); err != nil {
		t.Fatal(err)
	}
	if job != "ENG-MGR1" || grade != "M1" || manager != "manager-rel:new" {
		t.Fatalf("assignment = %s/%s/%s", job, grade, manager)
	}
	if err := f.db.Conn.QueryRow(ctx, `SELECT amount::text FROM compensation_component WHERE tenant_id=$1 AND entity_id=$2 AND superseded_at IS NULL`, f.tenant, f.baseID).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != "180000.0000" {
		t.Fatalf("base pay = %s", amount)
	}
	if err := f.db.Conn.QueryRow(ctx, `SELECT status FROM budget_reservation WHERE tenant_id=$1 AND entity_id=$2 AND superseded_at IS NULL`, f.tenant, f.reservation).Scan(&budgetStatus); err != nil {
		t.Fatal(err)
	}
	if budgetStatus != "COMMITTED" {
		t.Fatalf("budget status = %s", budgetStatus)
	}
	for table, want := range map[string]int{"assignment": 2, "position_occupancy": 1, "compensation_component": 2, "budget_reservation": 2, "outbox": 2} {
		var got int
		if err := f.db.Conn.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id=$1", table), f.tenant).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
}

func TestTodo_PROMO_005_Fault(t *testing.T) {
	stages := []string{domaincommit.ParticipantAssignment, domaincommit.ParticipantOccupancy, domaincommit.ParticipantCompensation, domaincommit.ParticipantBudget, "outbox"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t)
			injected := errors.New("injected")
			writer := promotioncommit.Writer{Failpoint: func(got string) error {
				if got == stage {
					return injected
				}
				return nil
			}}
			_, err := commitCommand(t, f, writer, f.command(t))
			if !errors.Is(err, injected) {
				t.Fatalf("error = %v", err)
			}
			ctx := context.Background()
			for table, want := range map[string]int{"assignment": 1, "position_occupancy": 0, "compensation_component": 1, "budget_reservation": 1, "outbox": 0} {
				var got int
				if err := f.db.Conn.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id=$1", table), f.tenant).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Errorf("%s rows after %s fault = %d, want %d", table, stage, got, want)
				}
			}
		})
	}
}

func TestTodo_PROMO_005_Mutation(t *testing.T) {
	f := newFixture(t)
	euroPay := mustValue(values.NewMoney("180000.00", "EUR", 2, values.RoundingExactRequired))
	tests := []struct {
		name   string
		mutate func(*domaincommit.Command)
		want   error
	}{
		{"stale assignment baseline", func(cmd *domaincommit.Command) { cmd.ExpectedAssignmentDigest = "sha256:stale" }, promotioncommit.ErrBaselineChanged},
		{"job substitution", func(cmd *domaincommit.Command) { cmd.TargetJobCode = "OTHER-JOB" }, promotioncommit.ErrAggregateBinding},
		{"currency substitution", func(cmd *domaincommit.Command) { cmd.BasePay = euroPay }, promotioncommit.ErrCurrencyMismatch},
		{"dropped plan participant", func(cmd *domaincommit.Command) { cmd.PlanParticipants = cmd.PlanParticipants[:3] }, domaincommit.ErrParticipantSet},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := f.command(t)
			test.mutate(&cmd)
			_, err := commitCommand(t, f, promotioncommit.Writer{}, cmd)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	cmd := f.command(t)
	cmd.Effects[0].SchemaRef = "hcmnext.promotion.unregistered/v1"
	if _, err := commitCommand(t, f, promotioncommit.Writer{}, cmd); err == nil {
		t.Fatal("an outbox effect with an unregistered payload schema committed")
	}
	for table, want := range map[string]int{"assignment": 1, "position_occupancy": 0, "compensation_component": 1, "budget_reservation": 1, "outbox": 0} {
		var got int
		if err := f.db.Conn.QueryRow(context.Background(), fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id=$1", table), f.tenant).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s rows after mutation refusals = %d, want %d", table, got, want)
		}
	}
}

func TestTodo_PROMO_005_Race(t *testing.T) {
	f := newFixture(t)
	connections := []*pgxadapter.Conn{f.db.NewConn(t), f.db.NewConn(t)}
	commands := []domaincommit.Command{f.command(t), f.command(t)}
	start := make(chan struct{})
	results := make(chan error, len(connections))

	for index, conn := range connections {
		conn := conn
		cmd := commands[index]
		go func() {
			<-start
			ctx := context.Background()
			tx, err := conn.Begin(ctx)
			if err == nil {
				_, err = (promotioncommit.Writer{}).Write(ctx, tx, cmd)
			}
			if err == nil {
				err = tx.Commit(ctx)
			} else if tx != nil {
				_ = tx.Rollback(ctx)
			}
			results <- err
		}()
	}
	close(start)

	var successes, baselineConflicts int
	for range connections {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, promotioncommit.ErrBaselineChanged):
			baselineConflicts++
		default:
			t.Fatalf("unexpected racing commit error: %v", err)
		}
	}
	if successes != 1 || baselineConflicts != 1 {
		t.Fatalf("race results: successes=%d baseline conflicts=%d", successes, baselineConflicts)
	}

	for table, want := range map[string]int{"assignment": 2, "position_occupancy": 1, "compensation_component": 2, "budget_reservation": 2, "outbox": 2} {
		var got int
		if err := f.db.Conn.QueryRow(context.Background(), fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id=$1", table), f.tenant).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s rows after race = %d, want %d", table, got, want)
		}
	}
}

type failingTerminal struct{ err error }

func (f failingTerminal) Write(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	return idempotency.ResultIdentity{}, f.err
}

func TestTodo_PROMO_005_TerminalFailureRollsBackDomainChanges(t *testing.T) {
	f := newFixture(t)
	cmd := f.command(t)
	req := execute.TerminalWriteRequest{
		TenantID:   f.tenant,
		PlanDigest: cmd.WorkflowPlanDigest,
	}
	req.Proposal.Revision.ProposalRevisionID = cmd.ProposalRevisionID
	req.Proposal.Revision.MaterialDigest.Digest = cmd.ProposalDigest
	injected := errors.New("terminal ledger failed")
	writer := promotionterminal.Writer{
		Resolver: promotionterminal.ResolverFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (domaincommit.Command, error) {
			return cmd, nil
		}),
		Mutation: promotioncommit.Writer{},
		Next:     failingTerminal{err: injected},
	}

	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := writer.Write(ctx, tx, req); !errors.Is(err, injected) {
		t.Fatalf("error = %v, want terminal failure", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	for table, want := range map[string]int{"assignment": 1, "position_occupancy": 0, "compensation_component": 1, "budget_reservation": 1, "outbox": 0} {
		var got int
		if err := f.db.Conn.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id=$1", table), f.tenant).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s rows after terminal failure = %d, want %d", table, got, want)
		}
	}
}

// TestTodo_PROMO_EXEC_007_CommitComplete is the commit-side proof for the
// executable promotion path: one transaction produces every local successor
// and both declared outbox legs.
func TestTodo_PROMO_EXEC_007_CommitComplete(t *testing.T) {
	f := newFixture(t)
	receipt, err := commitCommand(t, f, promotioncommit.Writer{}, f.command(t))
	if err != nil {
		t.Fatalf("commit promotion: %v", err)
	}
	if receipt.AssignmentRowID == "" || receipt.OccupancyRowID == "" || receipt.BasePayRowID == "" || receipt.BudgetRowID == "" || len(receipt.OutboxIDs) != 2 {
		t.Fatalf("incomplete commit receipt: %+v", receipt)
	}
	ctx := context.Background()
	checks := []struct {
		table, column, want string
		id                  uuid.UUID
	}{
		{"assignment", "job_code", "ENG-MGR1", f.assignment},
		{"position_occupancy", "worker_ref::text", f.worker.String(), f.occupancy},
		{"compensation_component", "amount::text", "180000.0000", f.baseID},
		{"budget_reservation", "status", "COMMITTED", f.reservation},
	}
	for _, check := range checks {
		var got string
		query := fmt.Sprintf("SELECT %s FROM %s WHERE tenant_id=$1 AND entity_id=$2 AND superseded_at IS NULL", check.column, check.table)
		if err := f.db.Conn.QueryRow(ctx, query, f.tenant, check.id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", check.table, err)
		}
		if got != check.want {
			t.Errorf("%s.%s = %q, want %q", check.table, check.column, got, check.want)
		}
	}
}

// TestTodo_PROMO_EXEC_007_CommitComplete_Recovery proves a participant
// failure rolls back the whole local transaction and a resumed attempt can
// commit the same command without duplicate successor rows.
func TestTodo_PROMO_EXEC_007_CommitComplete_Recovery(t *testing.T) {
	f := newFixture(t)
	fail := promotioncommit.Writer{Failpoint: func(stage string) error {
		if stage == domaincommit.ParticipantOccupancy {
			return errors.New("injected occupancy failure")
		}
		return nil
	}}
	if _, err := commitCommand(t, f, fail, f.command(t)); err == nil {
		t.Fatal("failed commit returned nil")
	}
	for table, want := range map[string]int{"assignment": 1, "position_occupancy": 0, "compensation_component": 1, "budget_reservation": 1, "outbox": 0} {
		var got int
		if err := f.db.Conn.QueryRow(context.Background(), fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id=$1", table), f.tenant).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s rows after rollback = %d, want %d", table, got, want)
		}
	}
	if _, err := commitCommand(t, f, promotioncommit.Writer{}, f.command(t)); err != nil {
		t.Fatalf("recovered commit: %v", err)
	}
}
