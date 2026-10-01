package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rulethreshold"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type promotionPhaseFixture struct {
	db          *pgtest.DB
	tenant      uuid.UUID
	instance    uuid.UUID
	worker      uuid.UUID
	employment  uuid.UUID
	assignment  uuid.UUID
	proposal    uuid.UUID
	intent      uuid.UUID
	legal       uuid.UUID
	org         uuid.UUID
	job         uuid.UUID
	position    uuid.UUID
	manager     uuid.UUID
	pkg         uuid.UUID
	base        uuid.UUID
	budget      uuid.UUID
	reservation uuid.UUID
	revision    intent.ProposalRevision
	baseline    aggregates.Assignment
	checkedAt   time.Time
	decision    rulethreshold.Decision
}

// TestPromotionPhaseGuardIntegrationUsesDurablePhaseAndExactProvenance proves
// the served RULE-004 lookup distinguishes precommit baseline allowance from
// postcommit provenance, including a same-grade row with a different identity.
func TestPromotionPhaseGuardIntegrationUsesDurablePhaseAndExactProvenance(t *testing.T) {
	for _, tc := range []struct {
		name               string
		phase              string
		withWriter         bool
		unrelatedSuccessor bool
		wantErr            string
	}{
		{name: "precommit baseline is allowed", phase: "PRECOMMIT"},
		{name: "postcommit exact writer evidence succeeds", phase: "POSTCOMMIT", withWriter: true},
		{name: "postcommit missing evidence fails closed", phase: "POSTCOMMIT", wantErr: "no exact provenance"},
		{name: "postcommit same-grade unrelated successor fails", phase: "POSTCOMMIT", withWriter: true, unrelatedSuccessor: true, wantErr: "no exact provenance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPromotionPhaseFixture(t)
			if tc.withWriter {
				commitPromotionSuccessor(t, fixture)
			} else if tc.wantErr != "" {
				appendUnprovenSuccessor(t, fixture)
			}
			if tc.unrelatedSuccessor {
				appendUnrelatedSameGradeSuccessor(t, fixture)
			}
			if tc.phase == "POSTCOMMIT" {
				recordPromotionSuccess(t, fixture)
			}

			facts := &ServedRuleFacts{
				Thresholds: &stubRuleDeriver{input: servedFactsInput(t, "15.00")},
				Approval: stubServedApprovalFacts{decisions: []runtime.ApprovalDecisionFact{{
					DecisionID: "decision:phase-proof", Outcome: runtime.ApprovalOutcomeApproved,
					ProposalDigest: fixture.revision.MaterialDigest.Digest,
				}}},
			}
			var got execute.RuleApproval
			err := withPromotionPhaseTx(t, fixture, func(tx dbport.Tx) error {
				var lookupErr error
				got, lookupErr = facts.Lookup(context.Background(), tx, fixture.tenant, fixture.revision, fixture.checkedAt)
				return lookupErr
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("served RULE-004 lookup: %v", err)
				}
				if !got.Resolved || got.Approved.Tier != rules.ApprovalTierFinanceRequired {
					t.Fatalf("lookup = %+v, want resolved finance approval", got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("served RULE-004 lookup error = %v, want substring %q", err, tc.wantErr)
			}
			if got.Resolved {
				t.Fatalf("lookup = %+v, want unresolved on provenance failure", got)
			}
		})
	}
}

func newPromotionPhaseFixture(t *testing.T) *promotionPhaseFixture {
	t.Helper()
	db := pgtest.New(t)
	at := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	recorded := at.Add(-24 * time.Hour)
	checkedAt := at.Add(2 * time.Hour)
	f := &promotionPhaseFixture{
		db: db, tenant: uuid.New(), instance: uuid.New(), worker: uuid.New(),
		employment: uuid.New(), assignment: uuid.New(), proposal: uuid.New(), intent: uuid.New(), checkedAt: checkedAt,
	}
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-rule004-phase', 'RULE-004 phase proof', 'ACTIVE', $3)`, f.tenant, "rule004-phase-"+f.tenant.String()[:8], recorded)
	seedPromotionPhaseWorld(t, f, at, recorded)
	f.revision = newPromotionPhaseRevision(t, f, at)
	f.decision = rulethreshold.Decision{
		TenantID: f.tenant, IntentID: f.intent, Revision: f.revision.Revision, Attempt: 1, InstanceID: f.instance,
		Tier: string(rules.ApprovalTierFinanceRequired), MatchedRow: "row-finance-phase", TableID: rules.PromotionApprovalTableID,
		TableVersion: rules.PromotionApprovalTableVersion, TableDigest: "sha256:table-phase", InputDigest: "sha256:input-phase",
		Input: servedFactsInput(t, "12.50"), RecordedAt: at.Add(time.Hour),
	}
	if err := withPromotionPhaseTx(t, f, func(tx dbport.Tx) error { return rulethreshold.Record(context.Background(), tx, f.decision) }); err != nil {
		t.Fatalf("record threshold decision: %v", err)
	}
	return f
}

func seedPromotionPhaseWorld(t *testing.T, f *promotionPhaseFixture, at, recorded time.Time) {
	t.Helper()
	eff := at.Add(-24 * time.Hour)
	f.legal, f.org, f.job, f.position = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.pkg, f.base, f.budget, f.reservation = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.manager = uuid.New()
	ctx := context.Background()
	if err := withPromotionPhaseTx(t, f, func(tx dbport.Tx) error {
		people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
		var err error
		if _, err = organization.PutLegalEntity(ctx, tx, mustPhase(aggregates.NewLegalEntity(f.tenant, f.legal, eff, nil, recorded, "Acme Inc", "ACTIVE"))); err != nil {
			return err
		}
		if _, err = organization.PutOrganizationUnit(ctx, tx, mustPhase(aggregates.NewOrganizationUnit(f.tenant, f.org, eff, nil, recorded, "DEPARTMENT", "ENG", "Engineering", &f.legal, nil, "ACTIVE"))); err != nil {
			return err
		}
		if _, err = organization.PutJob(ctx, tx, mustPhase(aggregates.NewJob(f.tenant, f.job, eff, nil, recorded, "ENG-MGR1", "Engineering Manager", "ENGINEERING", "M1", "EXEMPT"))); err != nil {
			return err
		}
		if _, err = organization.PutJobPosition(ctx, tx, mustPhase(aggregates.NewJobPosition(f.tenant, f.position, f.job, f.org, &f.legal, eff, nil, recorded, "POS-ENG-MGR-1", "NYC", "1.0000", "OPEN"))); err != nil {
			return err
		}
		person := uuid.New()
		if _, err = people.PutPerson(ctx, tx, mustPhase(aggregates.NewPerson(f.tenant, person, eff, nil, recorded, "ACTIVE", "Jordan Lee", "Jordan"))); err != nil {
			return err
		}
		if _, err = people.PutWorker(ctx, tx, mustPhase(aggregates.NewWorker(f.tenant, f.worker, person, eff, nil, recorded, "W-100", "EMPLOYEE", "ACTIVE"))); err != nil {
			return err
		}
		if _, err = people.PutEmployment(ctx, tx, mustPhase(aggregates.NewEmployment(f.tenant, f.employment, f.worker, f.legal, eff, nil, recorded, "EMPLOYEE", "ACTIVE", &at))); err != nil {
			return err
		}
		f.baseline = mustPhase(aggregates.NewAssignment(f.tenant, f.assignment, f.employment, true, eff, nil, recorded, "ENG-SWE3", "P3", &f.org, nil, "NYC", "US-NY", "1.0000", "manager-rel:old"))
		if _, err = people.PutAssignment(ctx, tx, f.baseline); err != nil {
			return err
		}
		if _, err = compensation.PutCompensationPackage(ctx, tx, mustPhase(aggregates.NewCompensationPackage(f.tenant, f.pkg, f.worker, &f.employment, &f.assignment, eff, nil, recorded, "USD"))); err != nil {
			return err
		}
		oldPay := mustPhase(values.NewMoney("165000.00", "USD", 2, values.RoundingExactRequired))
		if _, err = compensation.PutCompensationComponent(ctx, tx, mustPhase(aggregates.NewCompensationComponent(f.tenant, f.base, f.pkg, eff, nil, recorded, "BASE_PAY", oldPay, "ANNUAL"))); err != nil {
			return err
		}
		if _, err = compensation.PutWorkforceBudget(ctx, tx, mustPhase(aggregates.NewWorkforceBudget(f.tenant, f.budget, eff, nil, recorded, "COMPENSATION_POOL", "hcmnext.finance", "cost-center:ENG", "FY2026", "USD", "MONEY", "500000.00", "budget/1"))); err != nil {
			return err
		}
		expiry := at.AddDate(1, 0, 0)
		_, err = compensation.PutBudgetReservation(ctx, tx, mustPhase(aggregates.NewBudgetReservation(f.tenant, f.reservation, f.budget, &f.proposal, eff, nil, recorded, "15000.00", "USD", "HELD", &expiry)))
		return err
	}); err != nil {
		t.Fatalf("seed promotion aggregates: %v", err)
	}
}

func newPromotionPhaseRevision(t *testing.T, f *promotionPhaseFixture, at time.Time) intent.ProposalRevision {
	t.Helper()
	tenant := values.TenantId(f.tenant.String())
	key := mustPhase(values.NewResourceKey(tenant, "assignment", "worker", f.worker.String()))
	date := mustPhase(values.NewLocalDate(2026, 2, 1))
	interval := mustPhase(values.NewOpenLocalDateInterval(date, values.CalendarRef{Ref: "gregorian", Version: "1"}))
	revisionToken := mustPhase(values.NewSequenceRevision("assignment:"+f.assignment.String(), 1))
	subject := intent.SubjectReference{Kind: "WORKER", SubjectID: f.worker.String(), AuthorityDomain: "PEOPLE"}
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("protomap.NewDefaultDigester: %v", err)
	}
	rev, err := intent.NewProposalRevision(intent.ProposalSpec{
		IntentID: f.intent.String(), Revision: 1, Tenant: tenant, OrganizationScopeID: "org:rule004-phase",
		Subjects: []intent.SubjectReference{subject}, EffectiveTime: interval,
		CurrentState:     []intent.StateAssertion{{Subject: subject, ResourceKey: key, FieldPath: "assignment.grade", CanonicalText: "P3"}},
		ProposedState:    []intent.StateAssertion{{Subject: subject, ResourceKey: key, FieldPath: "assignment.grade", CanonicalText: "M1"}},
		Writes:           []intent.PlannedWrite{{Subject: subject, ResourceKey: key, FieldPath: "assignment.grade", CurrentCanonicalText: "P3", ProposedCanonicalText: "M1", SourceAuthorityDecision: "authority:phase-proof", ExpectedRevision: revisionToken}},
		CreatedBy:        intent.PrincipalReference{PrincipalID: "principal:phase-proof", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance:phase-proof"},
		ControlSnapshots: intent.ControlSnapshots{CapabilityRegistryDigest: "sha256:cap", PolicyBundleDigest: "sha256:policy", LegalContextDigest: "sha256:legal", EntitlementDigest: "sha256:entitlement", ReferenceDataDigest: "sha256:reference", ClassificationTaxonomyDigest: "sha256:classification", DLPDecisionDigest: "sha256:dlp"},
	}, intent.Definition{Ref: intent.Ref{TypeID: "hcmnext.phase-proof", Version: 1}, Family: intent.FamilyChangeRequest}, digester,
		func() (string, error) { return f.proposal.String(), nil }, func() values.Instant { return values.NewInstant(at.Add(-time.Hour)) })
	if err != nil {
		t.Fatalf("intent.NewProposalRevision: %v", err)
	}
	return rev
}

func commitPromotionSuccessor(t *testing.T, f *promotionPhaseFixture) {
	t.Helper()
	ctx := context.Background()
	if err := withPromotionPhaseTx(t, f, func(tx dbport.Tx) error {
		people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
		worker, err := people.CurrentWorker(ctx, tx, f.tenant, f.worker, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		employment, err := people.CurrentEmployment(ctx, tx, f.tenant, f.employment, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		assignment, err := people.CurrentAssignment(ctx, tx, f.tenant, f.assignment, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		job, err := organization.CurrentJob(ctx, tx, f.tenant, f.job, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		position, err := organization.CurrentJobPosition(ctx, tx, f.tenant, f.position, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		pkg, err := compensation.CurrentCompensationPackage(ctx, tx, f.tenant, f.pkg, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		base, err := compensation.CurrentCompensationComponent(ctx, tx, f.tenant, f.base, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		budget, err := compensation.CurrentBudgetReservation(ctx, tx, f.tenant, f.reservation, f.baseline.EffectiveFrom)
		if err != nil {
			return err
		}
		command := phaseCommand(f, worker.Digest, employment.Digest, assignment.Digest, job.Digest, position.Digest, pkg.Digest, base.Digest, budget.Digest)
		_, err = (promotioncommit.Writer{People: people, Organization: organization, Compensation: compensation}).Write(ctx, tx, command)
		return err
	}); err != nil {
		t.Fatalf("commit promotion successor: %v", err)
	}
}

func phaseCommand(f *promotionPhaseFixture, workerDigest, employmentDigest, assignmentDigest, jobDigest, positionDigest, packageDigest, baseDigest, budgetDigest string) domaincommit.Command {
	return domaincommit.Command{
		TenantID: f.tenant.String(), IntentID: f.intent.String(), ProposalRevisionID: f.revision.ProposalRevisionID, ProposalRevisionNumber: f.revision.Revision, ProposalDigest: f.revision.MaterialDigest.Digest,
		PlanID: "plan:phase-proof", PlanDigest: "sha256:plan-phase", PlanParticipants: []string{domaincommit.ParticipantAssignment, domaincommit.ParticipantOccupancy, domaincommit.ParticipantCompensation, domaincommit.ParticipantBudget}, WorkflowPlanDigest: "sha256:workflow-phase", AuthorityDigest: "sha256:authority-phase", ActorPrincipalID: "principal:phase-proof",
		EffectiveAt: f.checkedAt.Add(-2 * time.Hour), RecordedAt: f.checkedAt.Add(-time.Hour), WorkerID: f.worker.String(), EmploymentID: f.employment.String(), AssignmentID: f.assignment.String(),
		TargetJobCode: "ENG-MGR1", TargetJobID: f.job.String(), TargetGrade: "M1", TargetOrganizationID: f.org.String(), TargetPositionID: f.position.String(), ManagerRelationshipRef: "manager-rel:new", ManagerWorkerID: f.manager.String(), Location: "NYC", PayZone: "US-NY", FTE: "1.0000",
		PositionOccupancyID: uuid.NewString(), PositionReservationRef: "reservation:position-phase", CompensationPackageID: f.pkg.String(), BasePayComponentID: f.base.String(), BasePay: mustPhase(values.NewMoney("180000.00", "USD", 2, values.RoundingExactRequired)), PayFrequency: "ANNUAL", BudgetReservationID: f.reservation.String(), BudgetReservationRef: "budget:phase",
		ExpectedWorkerDigest: workerDigest, ExpectedEmploymentDigest: employmentDigest, ExpectedAssignmentDigest: assignmentDigest,
		ExpectedJobDigest: jobDigest, ExpectedPositionDigest: positionDigest, ExpectedPackageDigest: packageDigest, ExpectedBasePayDigest: baseDigest, ExpectedBudgetDigest: budgetDigest,
		AssignmentWrites:    []domaincommit.AssignmentWrite{{FieldPath: "assignment.grade", CurrentValue: "P3", ProposedValue: "M1", AuthorityDecision: "authority:phase-proof", ExpectedSource: mustPhase(values.NewSequenceRevision("assignment:"+f.assignment.String(), 1)).String()}},
		ApprovalDecisionIDs: []string{"approval:one", "approval:two"}, TaskSubmissionIDs: []string{"task:one"},
	}
}

func appendUnprovenSuccessor(t *testing.T, f *promotionPhaseFixture) {
	t.Helper()
	if err := withPromotionPhaseTx(t, f, func(tx dbport.Tx) error {
		a := mustPhase(aggregates.NewAssignment(f.tenant, f.assignment, f.employment, true, f.checkedAt.Add(-2*time.Hour), nil, f.checkedAt.Add(-90*time.Minute), "ENG-MGR1", "M1", f.baseline.OrganizationRef, nil, "NYC", "US-NY", "1.0000", "manager-rel:new"))
		_, err := (aggregates.PeopleStore{}).PutAssignment(context.Background(), tx, a)
		return err
	}); err != nil {
		t.Fatalf("append unproven successor: %v", err)
	}
}

func appendUnrelatedSameGradeSuccessor(t *testing.T, f *promotionPhaseFixture) {
	t.Helper()
	if err := withPromotionPhaseTx(t, f, func(tx dbport.Tx) error {
		a := mustPhase(aggregates.NewAssignment(f.tenant, f.assignment, f.employment, true, f.checkedAt.Add(-2*time.Hour), nil, f.checkedAt.Add(-30*time.Minute), "ENG-MGR1", "M1", f.baseline.OrganizationRef, nil, "NYC", "US-NY", "1.0000", "manager-rel:new"))
		_, err := (aggregates.PeopleStore{}).PutAssignment(context.Background(), tx, a)
		return err
	}); err != nil {
		t.Fatalf("append unrelated same-grade successor: %v", err)
	}
}

func recordPromotionSuccess(t *testing.T, f *promotionPhaseFixture) {
	t.Helper()
	store := runtime.Store{}
	if err := withPromotionPhaseTx(t, f, func(tx dbport.Tx) error {
		inst := runtime.Instance{TenantID: f.tenant, InstanceID: f.instance, CellID: "cell-rule004-phase", WorkflowID: "promotion.default/v1", WorkflowVersion: 1, CompiledPlanHash: strings.Repeat("a", 64), ExecutionMode: workflow.ModeExecute, RuntimeStatus: runtime.InstanceRunning, InputRef: "sha256:input-phase", InstanceVersion: 1, CorrelationID: "corr:phase-proof", CreatedAt: f.checkedAt.Add(-3 * time.Hour), CurrentNodeIDs: []string{promotionexec.NodeExecutePromotion}}
		if _, err := store.CreateInstance(context.Background(), tx, inst); err != nil {
			return err
		}
		node := runtime.NewNodeExecution(f.tenant, f.instance, promotionexec.NodeExecutePromotion, 1, workflow.StepCapability, runtime.NodeSucceeded)
		node.StartedAt, node.CompletedAt, node.RecordedAt = phaseTime(f.checkedAt.Add(-90*time.Minute)), phaseTime(f.checkedAt.Add(-80*time.Minute)), f.checkedAt.Add(-80*time.Minute)
		_, _, err := store.RecordNodeExecution(context.Background(), tx, node, 1)
		return err
	}); err != nil {
		t.Fatalf("record successful promotion node: %v", err)
	}
}

func phaseTime(t time.Time) *time.Time { return &t }

func withPromotionPhaseTx(t *testing.T, f *promotionPhaseFixture, fn func(dbport.Tx) error) error {
	t.Helper()
	conn := f.db.NewConn(t)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin phase proof transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tenancy.WithTenant(context.Background(), tx, f.tenant); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

func mustPhase[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
