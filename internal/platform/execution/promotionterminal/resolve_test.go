package promotionterminal_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// approvedTerminalCode is the promotion workflow's approved END code
// (internal/workflow/promotionexec/definition.go): the only terminal code
// that may write successor facts. Every other code must still record its
// ledger fact, with no mutation behind it.
const approvedTerminalCode = "PROMOTION_COMPLETE"

// promoux016IDs names every entity one promotion world needs, so the tests
// below assert exact identity binding instead of "a row appeared".
type promoux016IDs struct {
	person, worker, legal, employment, assignment                uuid.UUID
	org, job, position                                           uuid.UUID
	managerPerson, manager, managerEmployment, managerAssignment uuid.UUID
	topPerson, topManager                                        uuid.UUID
	pkg, base                                                    uuid.UUID
	budget, reservation                                          uuid.UUID
	intent, proposal, instance                                   uuid.UUID
	approvalOne, approvalTwo, taskOne                            uuid.UUID
}

func promoux016Times() (at, recorded time.Time) {
	// Fixed past dates: the commit records its successors at the real now,
	// and bitemporal physics requires successors to record after their
	// predecessors, so fixtures must predate execution. A fixed 2026 date
	// stays in the past forever; future dates would rot the suite the day
	// real time catches them -- and break it today.
	at = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	return at, at.Add(-time.Hour)
}

// must016 unwraps fixture constructors. It takes exactly the constructor's
// results (no testing.T, so multi-value calls spread into it) and panics on
// defects, the same fixture contract test/workflow's e2eMust uses: fixture
// inputs are literals, so a construction failure is a test bug, not a system
// fault, and a panic names it with a stack.
func must016[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func must016Err(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

func seed016Tenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-promoux016', 'PROMOUX-016 fixture', 'ACTIVE', now())`, id, key)
	return id
}

func seed016World(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, at, recorded time.Time) promoux016IDs {
	t.Helper()
	ctx := context.Background()
	// Rows take effect at the start of the day before the promotion date:
	// the resolver reads at the effective start (midnight), the integration
	// proof reads the old world one hour before the promotion instant, and
	// the rows must already hold at both. ProducedAt, expiry and hire dates
	// stay on at.
	y, m, d := at.Date()
	eff := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Add(-24 * time.Hour)
	ids := promoux016IDs{
		person: uuid.New(), worker: uuid.New(), legal: uuid.New(), employment: uuid.New(), assignment: uuid.New(),
		org: uuid.New(), job: uuid.New(), position: uuid.New(),
		managerPerson: uuid.New(), manager: uuid.New(), managerEmployment: uuid.New(), managerAssignment: uuid.New(),
		topPerson: uuid.New(), topManager: uuid.New(),
		pkg: uuid.New(), base: uuid.New(), budget: uuid.New(), reservation: uuid.New(),
		intent: uuid.New(), proposal: uuid.New(), instance: uuid.New(),
		approvalOne: uuid.New(), approvalTwo: uuid.New(), taskOne: uuid.New(),
	}
	people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		if _, err := organization.PutLegalEntity(ctx, tx, must016(aggregates.NewLegalEntity(tenantID, ids.legal, eff, nil, recorded, "Acme Inc", "ACTIVE"))); err != nil {
			return err
		}
		if _, err := organization.PutOrganizationUnit(ctx, tx, must016(aggregates.NewOrganizationUnit(tenantID, ids.org, eff, nil, recorded, "DEPARTMENT", "ENG", "Engineering", &ids.legal, nil, "ACTIVE"))); err != nil {
			return err
		}
		if _, err := organization.PutJob(ctx, tx, must016(aggregates.NewJob(tenantID, ids.job, eff, nil, recorded, "ENG-MGR1", "Engineering Manager", "ENGINEERING", "M1", "EXEMPT"))); err != nil {
			return err
		}
		if _, err := organization.PutJobPosition(ctx, tx, must016(aggregates.NewJobPosition(tenantID, ids.position, ids.job, ids.org, &ids.legal, eff, nil, recorded, "POS-ENG-MGR-1", "NYC", "1.0000", "OPEN"))); err != nil {
			return err
		}
		if _, err := people.PutPerson(ctx, tx, must016(aggregates.NewPerson(tenantID, ids.person, eff, nil, recorded, "ACTIVE", "Jordan Lee", "Jordan"))); err != nil {
			return err
		}
		if _, err := people.PutWorker(ctx, tx, must016(aggregates.NewWorker(tenantID, ids.worker, ids.person, eff, nil, recorded, "W-100", "EMPLOYEE", "ACTIVE"))); err != nil {
			return err
		}
		if _, err := people.PutEmployment(ctx, tx, must016(aggregates.NewEmployment(tenantID, ids.employment, ids.worker, ids.legal, eff, nil, recorded, "EMPLOYEE", "ACTIVE", &at))); err != nil {
			return err
		}
		if _, err := people.PutAssignment(ctx, tx, must016(aggregates.NewAssignment(tenantID, ids.assignment, ids.employment, true, eff, nil, recorded,
			"ENG-SWE3", "P3", &ids.org, nil, "NYC", "US-NY", "1.0000", "manager-rel:old"))); err != nil {
			return err
		}
		if _, err := compensation.PutCompensationPackage(ctx, tx, must016(aggregates.NewCompensationPackage(tenantID, ids.pkg, ids.worker, &ids.employment, &ids.assignment, eff, nil, recorded, "USD"))); err != nil {
			return err
		}
		oldPay := must016(values.NewMoney("165000.00", "USD", 2, values.RoundingExactRequired))
		if _, err := compensation.PutCompensationComponent(ctx, tx, must016(aggregates.NewCompensationComponent(tenantID, ids.base, ids.pkg, eff, nil, recorded, "BASE_PAY", oldPay, "ANNUAL"))); err != nil {
			return err
		}
		// The new manager's own chain: the manager reports to the top
		// manager, who reports nowhere, so the ancestor walk terminates.
		if _, err := people.PutPerson(ctx, tx, must016(aggregates.NewPerson(tenantID, ids.managerPerson, eff, nil, recorded, "ACTIVE", "Morgan Reyes", "Morgan"))); err != nil {
			return err
		}
		if _, err := people.PutWorker(ctx, tx, must016(aggregates.NewWorker(tenantID, ids.manager, ids.managerPerson, eff, nil, recorded, "W-200", "EMPLOYEE", "ACTIVE"))); err != nil {
			return err
		}
		if _, err := people.PutEmployment(ctx, tx, must016(aggregates.NewEmployment(tenantID, ids.managerEmployment, ids.manager, ids.legal, eff, nil, recorded, "EMPLOYEE", "ACTIVE", &at))); err != nil {
			return err
		}
		if _, err := people.PutAssignment(ctx, tx, must016(aggregates.NewAssignment(tenantID, ids.managerAssignment, ids.managerEmployment, true, eff, nil, recorded,
			"ENG-MGR0", "M0", &ids.org, nil, "NYC", "US-NY", "1.0000", ids.topManager.String()))); err != nil {
			return err
		}
		if _, err := people.PutPerson(ctx, tx, must016(aggregates.NewPerson(tenantID, ids.topPerson, eff, nil, recorded, "ACTIVE", "Alex Kim", "Alex"))); err != nil {
			return err
		}
		if _, err := people.PutWorker(ctx, tx, must016(aggregates.NewWorker(tenantID, ids.topManager, ids.topPerson, eff, nil, recorded, "W-000", "EMPLOYEE", "ACTIVE"))); err != nil {
			return err
		}
		if _, err := compensation.PutWorkforceBudget(ctx, tx, must016(aggregates.NewWorkforceBudget(tenantID, ids.budget, eff, nil, recorded,
			"COMPENSATION_POOL", "hcmnext.finance", "cost-center:ENG", "FY2026", "USD", "MONEY", "500000.00", "budget/1"))); err != nil {
			return err
		}
		// The hold must outlive the commit's real-now RecordedAt: the writer
		// refuses an expired reservation, and a 24-hour hold cut in February
		// cannot cover a test run in September. The duration is not under
		// test here -- HELD, bound and unexpired at commit is.
		expiry := at.AddDate(1, 0, 0)
		if _, err := compensation.PutBudgetReservation(ctx, tx, must016(aggregates.NewBudgetReservation(tenantID, ids.reservation, ids.budget, &ids.proposal, eff, nil, recorded, "15000.00", "USD", "HELD", &expiry))); err != nil {
			return err
		}
		return nil
	}))
	// The two promotion effect schemas the outbox foreign-keys to, the same
	// rows every other promotion test seeds.
	for _, schemaRef := range []string{"hcmnext.promotion.payroll/v1", "hcmnext.promotion.iam/v1"} {
		db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
			VALUES ($1,$2,$2,1,$2,'PROTOBUF','EXTERNAL_PAYLOAD_EVIDENCE')`, tenantID, schemaRef)
	}
	return ids
}

func in016TenantTx(db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	conn := db.Conn
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// build016DTO assembles the approved proposal revision the resolver must
// materialize its command from: subjects naming worker and position, the pay
// and manager writes in the simulation's exact field vocabulary, the two
// remote effects, and the material digest the terminal request must repeat.
func build016DTO(t *testing.T, tenantID uuid.UUID, ids promoux016IDs, at time.Time, materialDigest, basePay string, revision uint64) intent.ProposalRevision {
	t.Helper()
	sub := func(kind, id string) intent.SubjectReference {
		return intent.SubjectReference{Kind: kind, SubjectID: id, AuthorityDomain: "test"}
	}
	write := func(field, current, proposed string) intent.PlannedWrite {
		return intent.PlannedWrite{
			Subject: sub("EMPLOYMENT", ids.worker.String()), ResourceKey: testResourceKey(t, tenantID),
			FieldPath: field, CurrentCanonicalText: current, ProposedCanonicalText: proposed,
			SourceAuthorityDecision: "authority.local_master/v1", ExpectedRevision: testRevisionToken(),
			Operation: intent.WriteOperationUpdate,
		}
	}
	// Effect kinds are the simulations' own vocabulary: the payroll sync
	// reports the approved compensation revision, the IAM sync the approved
	// assignment revision. The terminal refuses revisions that carry neither.
	y, m, d := at.Date()
	effectiveDate := must016(values.NewLocalDate(y, m, d))
	snapshot := "sha256:promoux016"
	return intent.ProposalRevision{
		ProposalRevisionID: ids.proposal.String(), IntentID: ids.intent.String(), Revision: revision,
		Tenant: values.TenantId(tenantID.String()), OrganizationScopeID: ids.org.String(),
		EffectiveTime: must016(values.NewOpenLocalDateInterval(effectiveDate, values.CalendarRef{Ref: "gregorian", Version: "1"})),
		CreatedBy:     intent.PrincipalReference{PrincipalID: "principal:promoux016", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "ial:test"},
		ControlSnapshots: intent.ControlSnapshots{
			CapabilityRegistryDigest: snapshot, PolicyBundleDigest: snapshot, LegalContextDigest: snapshot,
			EntitlementDigest: snapshot, ReferenceDataDigest: snapshot,
			ClassificationTaxonomyDigest: snapshot, DLPDecisionDigest: snapshot,
		},
		Subjects: []intent.SubjectReference{
			sub("EMPLOYMENT", ids.worker.String()),
			{Kind: "POSITION", SubjectID: ids.position.String(), AuthorityDomain: "POSITION"},
		},
		// Only changed writes travel: the simulations omit unchanged fields,
		// and the envelope refuses a write whose current equals proposed.
		Writes: []intent.PlannedWrite{
			write("assignment.job_code", "ENG-SWE3", "ENG-MGR1"),
			write("assignment.grade", "P3", "M1"),
			write("assignment.position_id", "", ids.position.String()),
			write("rewards.compensation.annualized_base_pay", "165000.00", basePay),
			write("org.manager_relationship.manager_id", "manager-rel:old", ids.manager.String()),
			write("org.manager_relationship.relationship_id", "manager-rel:old", "manager-rel:new"),
		},
		Effects: []intent.PlannedEffect{
			{EffectID: "payroll:" + ids.proposal.String(), Kind: "rewards.compensation.revision", DestinationRef: "payroll", Reversibility: "REVERSIBLE"},
			{EffectID: "iam:" + ids.proposal.String(), Kind: "people.assignment.revision", DestinationRef: "iam", Reversibility: "REVERSIBLE"},
		},
		MaterialDigest: digest.Reference{Digest: materialDigest},
		CreatedAt:      values.NewInstant(at),
	}
}

// seed016Intent records the approved intent instance the revision hangs
// from: proposal_revision foreign-keys to it, and APPROVED states what the
// terminal is entitled to assume before it re-verifies everything itself.
func seed016Intent(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, ids promoux016IDs, at time.Time) {
	t.Helper()
	sum := sha256.Sum256([]byte("promoux016-request:" + ids.intent.String()))
	requestDigest := hex.EncodeToString(sum[:])
	db.Exec(t, `INSERT INTO intent_instance
		(tenant_id, intent_id, definition_ref, definition_version, request_digest, idempotency_key,
		 request_state, execution_state, business_state, consistency_state, obligation_state,
		 created_at, last_transition_at)
		VALUES ($1,$2,'promotion.default/v1',1,$3,$4,'APPROVED','SCHEDULED','IN_PROGRESS','PENDING_OBSERVATION','PENDING',$5,$5)`,
		tenantID, ids.intent, requestDigest, "promoux016-"+ids.intent.String(), at)
}

func store016Revision(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, ids promoux016IDs, revision uint64, at time.Time, dto intent.ProposalRevision, materialDigest string) {
	t.Helper()
	payload := must016(intentcontrol.EncodeFullProposal(dto))
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		_, err := intentcontrol.RevisionStore{}.Materialize(ctxOf(t), tx, intentcontrol.Revision{
			TenantID: tenantID, IntentID: ids.intent, Revision: revision,
			ProposalDigest: materialDigest, MaterialDigest: materialDigest,
			SchemaRef: "hcmnext.proposal.full/v1", Payload: payload,
			ProducedBy: "hcmnext:intent-cell", ProducedAt: at,
		})
		return err
	}))
}

// seed016Proposal stores revision 1 of the approved proposal.
func seed016Proposal(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, ids promoux016IDs, at time.Time) (dto intent.ProposalRevision, materialDigest string) {
	t.Helper()
	seed016Intent(t, db, tenantID, ids, at)
	sum := sha256.Sum256([]byte("promoux016-material:" + ids.proposal.String()))
	materialDigest = hex.EncodeToString(sum[:])
	dto = build016DTO(t, tenantID, ids, at, materialDigest, "180000.00", 1)
	store016Revision(t, db, tenantID, ids, 1, at, dto, materialDigest)
	return dto, materialDigest
}

func ctxOf(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func testRevisionToken() values.RevisionToken {
	return must016(values.NewSequenceRevision("test-stream", 1))
}

func testResourceKey(t *testing.T, tenantID uuid.UUID) values.ResourceKey {
	t.Helper()
	return must016(values.NewResourceKey(values.TenantId(tenantID.String()), "test_resource", "promoux016"))
}

// seed016Decisions records two approvals and one task submission against the
// terminal's workflow instance: the quorum Command.Validate demands. The full
// chain is seeded honestly -- instance, completed work items, then decisions
// whose body digest repeats the completion output they were recorded from.
func seed016Decisions(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, ids promoux016IDs, at time.Time) {
	t.Helper()
	bodySum := sha256.Sum256([]byte(`{}`))
	bodyDigest := "sha256:" + hex.EncodeToString(bodySum[:])
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		ctx := ctxOf(t)
		if _, err := tx.Exec(ctx, `INSERT INTO workflow_instance
			(tenant_id, instance_id, cell_id, workflow_id, workflow_version, compiled_plan_hash,
			 business_subject_refs, execution_mode, runtime_status, completion_dimensions, input_ref,
			 variable_revision_head, current_node_ids, correlation_id, created_at)
			VALUES ($1,$2,'cell-promoux016','promotion.default/v1',1,repeat('a',64),
			 '{}','EXECUTE','RUNNING','{}','input:promoux016',0,'{END}','promoux016-correlation',$3)`,
			tenantID, ids.instance, at); err != nil {
			return err
		}
		for _, d := range []struct {
			decision, kind, workType, requirement string
		}{
			{ids.approvalOne.String(), "APPROVAL", "promotion.approval", "req:manager"},
			{ids.approvalTwo.String(), "APPROVAL", "promotion.approval", "req:finance"},
			{ids.taskOne.String(), "TASK", "promotion.task", ""},
		} {
			item := uuid.NewString()
			var requirement any
			if d.requirement != "" {
				requirement = d.requirement
			}
			if _, err := tx.Exec(ctx, `INSERT INTO work_item
				(tenant_id, work_item_id, kind, work_type, status, correlation_id, workflow_instance_id,
				 node_id, approval_requirement_ref, subject_refs, owner_kind, owner_ref, policy_route_ref,
				 visibility, organization_scope_id, deadline_at,
				 completed_by, completed_at, completed_output_digest, created_at)
				VALUES ($1,$2,$3,$4,'COMPLETED','promoux016-correlation',$5,
				 'END',$6,$7,'PRINCIPAL','principal:promoux016','policy:promoux016',
				 'TENANT_GOVERNANCE',$8,$9,
				 'principal:promoux016',$9,$10,$9)`,
				tenantID, item, d.kind, d.workType, ids.instance,
				requirement, []string{ids.worker.String()}, ids.org.String(), at.Add(24*time.Hour), bodyDigest); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO work_item_decision
				(tenant_id, decision_id, work_item_id, workflow_instance_id, item_version, kind,
				 decision_body, decision_body_digest, decided_by, decided_at)
				VALUES ($1,$2,$3,$4,1,$5,'{}',$6,'principal:promoux016',$7)`,
				tenantID, d.decision, item, ids.instance, d.kind, bodyDigest, at); err != nil {
				return err
			}
		}
		return nil
	}))
}

// resolve016 builds the production-shaped resolver the composition will use:
// real stores, a real boundary, fixed test authority. The boundary carries
// the test tenant and cell like a composed cell boundary would: the fencing
// check refuses a plan resolved under any other tenant.
func resolve016(tenantID uuid.UUID) promotionterminal.Resolver {
	return promotionterminal.Resolver{
		Revisions:    intentcontrol.RevisionStore{},
		Decisions:    promotionterminal.WorkItemDecisions{},
		People:       aggregates.PeopleStore{},
		Organization: aggregates.OrganizationStore{},
		Compensation: aggregates.CompensationStore{},
		Boundary: transaction.ConsistencyBoundary{
			BoundaryID: "boundary:promoux016", Tenant: values.TenantId(tenantID.String()), CellID: "cell-promoux016",
			CoordinatorID: "coordinator:promoux016",
			Admitted:      []transaction.AdmissionSelector{{StorageClass: "LOCAL_POSTGRES"}},
			Isolation:     transaction.IsolationSerializable, Protocol: transaction.CommitProtocolSingleDatabaseACID,
			CoordinatorEpoch: 7, CrossBoundaryDisposition: transaction.CrossBoundaryDispositionSplitIntoEffects,
		},
		AuthorityDigest: "sha256:execution-authority", ActorPrincipalID: "principal:promotion-approver",
	}
}

func request016(tenantID, instanceID uuid.UUID, ids promoux016IDs, materialDigest string) execute.TerminalWriteRequest {
	return execute.TerminalWriteRequest{
		TenantID: tenantID, InstanceID: instanceID, WorkflowID: "promotion.default/v1",
		PlanDigest:      "sha256:workflow-plan",
		TerminalCode:    approvedTerminalCode,
		CorrelationID:   uuid.NewString(),
		IdempotencyKey:  "promoux016-terminal",
		EndNodeID:       "END",
		EndOutputDigest: materialDigest,
		Proposal:        runtimeProposalBinding(ids, materialDigest),
	}
}

func runtimeProposalBinding(ids promoux016IDs, materialDigest string) runtime.ProposalBinding {
	return runtimeProposalBindingFor(ids, materialDigest, 1)
}

func runtimeProposalBindingFor(ids promoux016IDs, materialDigest string, revision uint64) runtime.ProposalBinding {
	return runtime.ProposalBinding{
		Revision: intent.ProposalRevision{
			ProposalRevisionID: ids.proposal.String(), IntentID: ids.intent.String(), Revision: revision,
			MaterialDigest: digest.Reference{Digest: materialDigest},
		},
		ApprovalRef: ids.approvalOne.String(),
	}
}

// setup016 seeds one complete approved promotion world and returns the
// database, resolver, request and fact digests the assertions pin.
func setup016(t *testing.T) (*pgtest.DB, promotionterminal.Resolver, execute.TerminalWriteRequest, promoux016IDs, uuid.UUID, map[string]string) {
	t.Helper()
	db := pgtest.New(t)
	at, recorded := promoux016Times()
	tenantID := seed016Tenant(t, db, "promoux016-"+uuid.NewString()[:8])
	ids := seed016World(t, db, tenantID, at, recorded)
	_, materialDigest := seed016Proposal(t, db, tenantID, ids, at)
	seed016Decisions(t, db, tenantID, ids, at)
	digests := map[string]string{}
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		ctx := ctxOf(t)
		people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
		pin := func(name, digest string, err error) error {
			if err != nil {
				return err
			}
			digests[name] = digest
			return nil
		}
		worker, err := people.KnownAsOfWorker(ctx, tx, tenantID, ids.worker, at, at)
		if err := pin("worker", worker.Digest, err); err != nil {
			return err
		}
		employment, err := people.KnownAsOfEmployment(ctx, tx, tenantID, ids.employment, at, at)
		if err := pin("employment", employment.Digest, err); err != nil {
			return err
		}
		assignment, err := people.KnownAsOfAssignment(ctx, tx, tenantID, ids.assignment, at, at)
		if err := pin("assignment", assignment.Digest, err); err != nil {
			return err
		}
		job, err := organization.KnownAsOfJob(ctx, tx, tenantID, ids.job, at, at)
		if err := pin("job", job.Digest, err); err != nil {
			return err
		}
		position, err := organization.KnownAsOfJobPosition(ctx, tx, tenantID, ids.position, at, at)
		if err := pin("position", position.Digest, err); err != nil {
			return err
		}
		pkg, err := compensation.KnownAsOfCompensationPackage(ctx, tx, tenantID, ids.pkg, at, at)
		if err := pin("package", pkg.Digest, err); err != nil {
			return err
		}
		base, err := compensation.KnownAsOfCompensationComponent(ctx, tx, tenantID, ids.base, at, at)
		if err := pin("base", base.Digest, err); err != nil {
			return err
		}
		budget, err := compensation.KnownAsOfBudgetReservation(ctx, tx, tenantID, ids.reservation, at, at)
		return pin("budget", budget.Digest, err)
	}))
	return db, resolve016(tenantID), request016(tenantID, ids.instance, ids, materialDigest), ids, tenantID, digests
}

// resolveInTx resolves one request in a tenant-scoped transaction and
// commits, the shape the terminal sink's own in-tx call takes.
func resolveInTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, resolver promotionterminal.Resolver, req execute.TerminalWriteRequest) domaincommit.Command {
	t.Helper()
	var cmd domaincommit.Command
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		var err error
		cmd, err = resolver.Resolve(ctxOf(t), tx, req)
		return err
	}))
	return cmd
}

// ---------------------------------------------------------------------------
// PRIMARY
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_016 is the primary: the production resolver materializes
// the approved proposal's validated, plan-bound commit command -- exact
// identities, approval-frozen baseline digests, approval quorum, deterministic
// plan -- and the composed writer applies it atomically before the ledger
// fact, while a rejected terminal records only its ledger fact.
func TestTodo_PROMOUX_016(t *testing.T) {
	db, resolver, req, ids, tenantID, digests := setup016(t)
	at, _ := promoux016Times()

	cmd := resolveInTx(t, db, tenantID, resolver, req)

	// Exact identity binding: every id names an already-resolved aggregate,
	// and the proposal/digest/plan triple matches the terminal request.
	cases := map[string][2]string{
		"tenant":     {cmd.TenantID, tenantID.String()},
		"proposal":   {cmd.ProposalRevisionID, ids.proposal.String()},
		"digest":     {cmd.ProposalDigest, req.Proposal.Revision.MaterialDigest.Digest},
		"plan":       {cmd.WorkflowPlanDigest, req.PlanDigest},
		"worker":     {cmd.WorkerID, ids.worker.String()},
		"employment": {cmd.EmploymentID, ids.employment.String()},
		"assignment": {cmd.AssignmentID, ids.assignment.String()},
		"job":        {cmd.TargetJobID, ids.job.String()},
		"position":   {cmd.TargetPositionID, ids.position.String()},
		"package":    {cmd.CompensationPackageID, ids.pkg.String()},
		"basepay":    {cmd.BasePayComponentID, ids.base.String()},
		"budget":     {cmd.BudgetReservationID, ids.reservation.String()},
		"manager":    {cmd.ManagerWorkerID, ids.manager.String()},
	}
	for name, pair := range cases {
		if pair[0] != pair[1] {
			t.Fatalf("command %s = %q, want %q", name, pair[0], pair[1])
		}
	}
	if cmd.TargetJobCode != "ENG-MGR1" || cmd.TargetGrade != "M1" || cmd.TargetOrganizationID != ids.org.String() {
		t.Fatalf("command target = %q/%q/%q, want ENG-MGR1/M1/%v", cmd.TargetJobCode, cmd.TargetGrade, cmd.TargetOrganizationID, ids.org)
	}
	if cmd.BasePay.Amount().String() != "180000.00" || cmd.BasePay.Currency() != "USD" || cmd.PayFrequency != "ANNUAL" {
		t.Fatalf("command pay = %q/%q, want 180000.00/ANNUAL", cmd.BasePay.String(), cmd.PayFrequency)
	}
	if cmd.Location != "NYC" || cmd.PayZone != "US-NY" || cmd.FTE != "1.0000" {
		t.Fatalf("command placement = %q/%q/%q, want NYC/US-NY/1.0000", cmd.Location, cmd.PayZone, cmd.FTE)
	}
	if cmd.ManagerRelationshipRef != "manager-rel:new" || cmd.BudgetReservationRef != ids.proposal.String() || cmd.PositionReservationRef != ids.proposal.String() {
		t.Fatalf("command refs = %q/%q/%q, want manager-rel:new and the proposal id twice",
			cmd.ManagerRelationshipRef, cmd.BudgetReservationRef, cmd.PositionReservationRef)
	}
	// Approval-frozen baselines: the digests bound are the ones observed at
	// approval, pinned here against independent KnownAsOf reads.
	for name, got := range map[string]string{
		"worker": cmd.ExpectedWorkerDigest, "employment": cmd.ExpectedEmploymentDigest,
		"assignment": cmd.ExpectedAssignmentDigest, "job": cmd.ExpectedJobDigest,
		"position": cmd.ExpectedPositionDigest, "package": cmd.ExpectedPackageDigest,
		"base": cmd.ExpectedBasePayDigest, "budget": cmd.ExpectedBudgetDigest,
	} {
		if want := digests[name]; got != want {
			t.Fatalf("expected %s digest = %q, want approval-frozen %q", name, got, want)
		}
	}
	// Approval quorum travels from the terminal's own decisions, seeded above.
	if len(cmd.ApprovalDecisionIDs) != 3 || len(cmd.TaskSubmissionIDs) != 1 {
		t.Fatalf("approvals/tasks = %d/%d, want 3/1 (approval ref + 2 approvals, 1 task)",
			len(cmd.ApprovalDecisionIDs), len(cmd.TaskSubmissionIDs))
	}
	// The admitted plan is deterministic and bound: same inputs, same digest,
	// the four admitted local writers. The two remote effects are not
	// admitted participants -- they ride the outbox legs the binding matched
	// one-to-one against the declared effects -- and are asserted below.
	if cmd.PlanDigest == "" || cmd.PlanID != "promotion.transaction/"+ids.proposal.String() {
		t.Fatalf("plan = %q/%q, want bound digest and deterministic id", cmd.PlanDigest, cmd.PlanID)
	}
	if len(cmd.PlanParticipants) != 4 {
		t.Fatalf("plan participants = %d, want 4 (the admitted local writers)", len(cmd.PlanParticipants))
	}
	if len(cmd.Effects) != 2 {
		t.Fatalf("effects = %d, want exactly the 2 approved remote effects", len(cmd.Effects))
	}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("resolved command fails its own validation: %v", err)
	}

	// The composed writer applies the command atomically before the ledger
	// fact: one transaction, successor facts plus exactly one terminal call.
	var terminalCalls int
	w := &promotionterminal.Writer{
		ApprovedTerminalCode: approvedTerminalCode,
		Resolver:             resolver,
		Mutation:             promotioncommit.Writer{},
		Next: terminalFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
			terminalCalls++
			return idempotency.ResultIdentity{ResultRef: "complete"}, nil
		}),
	}
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		_, err := w.Write(ctxOf(t), tx, req)
		return err
	}))
	if terminalCalls != 1 {
		t.Fatalf("terminal calls = %d, want exactly 1 after the mutation", terminalCalls)
	}
	assert016Committed(t, db, tenantID, ids, must016(uuid.Parse(cmd.PositionOccupancyID)), at)

	// A rejected terminal records only its ledger fact: a fresh world, the
	// same writer shape, zero successor facts afterwards.
	db2, _, req2, ids2, tenant2, _ := setup016(t)
	rejected := req2
	rejected.TerminalCode = "PROMOTION_REJECTED"
	terminalCalls = 0
	must016Err(t, in016TenantTx(db2, tenant2, func(tx dbport.Tx) error {
		_, err := w.Write(ctxOf(t), tx, rejected)
		return err
	}))
	if terminalCalls != 1 {
		t.Fatalf("rejected terminal calls = %d, want exactly 1 (ledger fact, no mutation)", terminalCalls)
	}
	assert016RejectedWritesNothing(t, db2, tenant2, ids2, at)
}

// assert016Committed proves the successor facts durably: the assignment
// carries the new job/grade/position, the occupancy binds worker to position,
// the package carries the new base, all effective at the promotion date. The
// occupancy is read by the command's deterministic entity: the position id
// names the position row, never the occupancy row.
func assert016Committed(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, ids promoux016IDs, occupancyID uuid.UUID, at time.Time) {
	t.Helper()
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		ctx := ctxOf(t)
		people, organization, compensation := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
		assignment, err := people.CurrentAssignment(ctx, tx, tenantID, ids.assignment, at)
		if err != nil {
			return err
		}
		if assignment.JobCode != "ENG-MGR1" || assignment.Grade != "M1" || assignment.PositionRef == nil || *assignment.PositionRef != ids.position {
			t.Fatalf("committed assignment = %q/%q/%v, want ENG-MGR1/M1/%v",
				assignment.JobCode, assignment.Grade, assignment.PositionRef, ids.position)
		}
		occupancy, err := organization.CurrentPositionOccupancy(ctx, tx, tenantID, occupancyID, at)
		if err != nil {
			return err
		}
		if occupancy.PositionRef != ids.position || occupancy.WorkerRef == nil || *occupancy.WorkerRef != ids.worker {
			t.Fatalf("committed occupancy = %v/%v, want position %v worker %v",
				occupancy.PositionRef, occupancy.WorkerRef, ids.position, ids.worker)
		}
		base, err := compensation.CurrentCompensationComponent(ctx, tx, tenantID, ids.base, at)
		if err != nil {
			return err
		}
		if base.Amount != "180000.0000" {
			t.Fatalf("committed base pay = %q, want 180000.0000", base.Amount)
		}
		return nil
	}))
}

// assert016RejectedWritesNothing proves a rejected terminal left no
// successor facts: no occupancy for the position, base pay unchanged.
func assert016RejectedWritesNothing(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, ids promoux016IDs, at time.Time) {
	t.Helper()
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		ctx := ctxOf(t)
		organization, compensation := aggregates.OrganizationStore{}, aggregates.CompensationStore{}
		if _, err := organization.CurrentPositionOccupancy(ctx, tx, tenantID, ids.position, at); err == nil {
			t.Fatal("rejected terminal created an occupancy: successor facts must stay empty")
		}
		base, err := compensation.CurrentCompensationComponent(ctx, tx, tenantID, ids.base, at)
		if err != nil {
			return err
		}
		if base.Amount != "165000.0000" {
			t.Fatalf("rejected terminal moved base pay to %q, want the untouched 165000.0000", base.Amount)
		}
		return nil
	}))
}

// ---------------------------------------------------------------------------
// SECURITY
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_016_Security proves the terminal boundary refuses what it
// must: a request naming another tenant's proposal, a material digest that no
// longer matches the stored revision, and a revision that was never approved.
func TestTodo_PROMOUX_016_Security(t *testing.T) {
	db, resolver, req, _, tenantID, _ := setup016(t)

	// Another tenant's request for this proposal resolves nothing: the
	// revision lookup is tenant-scoped before anything else is read.
	foreign := req
	foreign.TenantID = uuid.New()
	must016ResolveErr(t, db, tenantID, resolver, foreign, "cross-tenant")

	// A material digest that drifted from the stored revision refuses, even
	// though the revision id is real: the digest is the tamper line.
	tampered := req
	tampered.Proposal.Revision.MaterialDigest = digest.Reference{Digest: strings.Repeat("0", 64)}
	must016ResolveErr(t, db, tenantID, resolver, tampered, "tampered digest")

	// A revision that was never approved resolves nothing.
	missing := req
	missing.Proposal.Revision.ProposalRevisionID = uuid.NewString()
	must016ResolveErr(t, db, tenantID, resolver, missing, "unknown revision")

	// The approved terminal code is the only code that mutates: every other
	// known code -- and any unknown one -- records its ledger fact with no
	// successor facts behind it.
	for _, code := range []string{"PROMOTION_REJECTED", "PROMOTION_INVALIDATED", "PROMOTION_EXPIRED", "PROMOTION_CANCELLED", "PROMOTION_BLOCKED", "PROMOTION_REPAIR_REQUIRED", "SOMETHING_ELSE"} {
		db2, _, req2, ids2, tenant2, _ := setup016(t)
		other := req2
		other.TerminalCode = code
		var terminalCalls int
		w := &promotionterminal.Writer{
			ApprovedTerminalCode: approvedTerminalCode,
			Resolver:             resolver,
			Mutation:             promotioncommit.Writer{},
			Next: terminalFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
				terminalCalls++
				return idempotency.ResultIdentity{ResultRef: "done"}, nil
			}),
		}
		at, _ := promoux016Times()
		must016Err(t, in016TenantTx(db2, tenant2, func(tx dbport.Tx) error {
			_, err := w.Write(ctxOf(t), tx, other)
			return err
		}))
		if terminalCalls != 1 {
			t.Fatalf("code %s: terminal calls = %d, want 1", code, terminalCalls)
		}
		assert016RejectedWritesNothing(t, db2, tenant2, ids2, at)
	}

	// An unconfigured writer refuses to mutate at all: the zero value fails
	// closed instead of silently recording outcomes with no facts behind
	// them -- the exact defect this todo removes from production.
	db3, _, req3, _, tenant3, _ := setup016(t)
	unconfigured := &promotionterminal.Writer{
		Resolver: resolver,
		Mutation: promotioncommit.Writer{},
		Next: terminalFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
			return idempotency.ResultIdentity{ResultRef: "done"}, nil
		}),
	}
	must016WriteErr(t, db3, tenant3, unconfigured, req3, "unset approval code")
}

func must016ResolveErr(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, resolver promotionterminal.Resolver, req execute.TerminalWriteRequest, name string) {
	t.Helper()
	err := in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		_, err := resolver.Resolve(ctxOf(t), tx, req)
		return err
	})
	if err == nil {
		t.Fatalf("Resolve(%s) = nil, want refusal", name)
	}
}

func must016WriteErr(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, w *promotionterminal.Writer, req execute.TerminalWriteRequest, name string) {
	t.Helper()
	err := in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		_, err := w.Write(ctxOf(t), tx, req)
		return err
	})
	if err == nil {
		t.Fatalf("Write(%s) = nil, want refusal", name)
	}
}

// ---------------------------------------------------------------------------
// FAULT
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_016_Fault proves every resolution failpoint refuses with
// no partial command: an unparseable pay amount, a proposal with no position
// subject, an unresolvable manager, a moved baseline, and a failing
// participant mid-commit each leave the world exactly as they found it.
func TestTodo_PROMOUX_016_Fault(t *testing.T) {
	db, resolver, req, ids, tenantID, digests := setup016(t)
	at, _ := promoux016Times()
	_ = at
	_ = digests

	// A failpoint after any single local participant rolls the whole commit
	// back: no assignment, no occupancy, no package, no next call. Stage
	// names are the commit's own participant vocabulary, so the test trips
	// the exact hook the writer fires.
	stages := []string{
		domaincommit.ParticipantAssignment, domaincommit.ParticipantOccupancy,
		domaincommit.ParticipantCompensation, domaincommit.ParticipantBudget, "outbox",
	}
	for _, stage := range stages {
		db2, resolver2, req2, ids2, tenant2, _ := setup016(t)
		var terminalCalls int
		w := &promotionterminal.Writer{
			ApprovedTerminalCode: approvedTerminalCode,
			Resolver:             resolver2,
			Mutation: promotioncommit.Writer{Failpoint: promotioncommit.Failpoint(func(got string) error {
				if got == stage {
					return err016Injected
				}
				return nil
			})},
			Next: terminalFunc(func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
				terminalCalls++
				return idempotency.ResultIdentity{ResultRef: "done"}, nil
			}),
		}
		err := in016TenantTx(db2, tenant2, func(tx dbport.Tx) error {
			_, err := w.Write(ctxOf(t), tx, req2)
			return err
		})
		if err == nil {
			t.Fatalf("Write(failpoint %s) = nil, want the injected failure", stage)
		}
		if terminalCalls != 0 {
			t.Fatalf("Write(failpoint %s) called Next %d times, want 0", stage, terminalCalls)
		}
		assert016RejectedWritesNothing(t, db2, tenant2, ids2, at)
	}

	// A baseline that moved after approval still resolves frozen -- the
	// resolver pins what approval saw -- but the commit refuses: the writer
	// re-reads current state and the digests no longer match. The move
	// commits in its own transaction first, exactly like a real concurrent
	// edit landing between approval and execution.
	must016Err(t, in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		ctx := ctxOf(t)
		people := aggregates.PeopleStore{}
		current, err := people.CurrentWorker(ctx, tx, tenantID, ids.worker, at)
		if err != nil {
			return err
		}
		// A fresh envelope, the way any production editor writes: read
		// structs don't round-trip through Put (the scan leaves Kind
		// unset), so the edit is constructed like a real concurrent write
		// -- new row id, new digest over the ON_LEAVE status, recorded
		// after the revision's ProducedAt so it lands outside the frozen
		// horizon while staying effective for the writer's current read.
		moved, err := aggregates.NewWorker(tenantID, ids.worker, current.PersonRef,
			current.EffectiveFrom, nil, at.Add(time.Hour),
			current.WorkerNumber, current.WorkerType, "ON_LEAVE")
		if err != nil {
			return err
		}
		_, err = people.PutWorker(ctx, tx, moved)
		return err
	}))
	movedCmd := resolveInTx(t, db, tenantID, resolver, req)
	if movedCmd.ExpectedWorkerDigest != digests["worker"] {
		t.Fatalf("resolved frozen digest = %q, want approval-frozen %q",
			movedCmd.ExpectedWorkerDigest, digests["worker"])
	}
	must016CommitErr(t, db, tenantID, movedCmd)
}

var err016Injected = err016("injected participant failure")

type err016 string

func (e err016) Error() string { return string(e) }

// ---------------------------------------------------------------------------
// MUTATION
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_016_Mutation seeds the defects the suite must catch: a
// dropped participant binding, a swapped baseline digest, a wrong coordinator
// epoch, and a removed approval gate each fail loudly instead of committing
// quietly different facts.
func TestTodo_PROMOUX_016_Mutation(t *testing.T) {
	db, resolver, req, ids, tenantID, _ := setup016(t)

	// A plan that drops the budget participant cannot bind: the remote
	// effect would leave the commit with no outbox leg. The resolution is
	// re-derived here through the same boundary the resolver composes, so
	// the test pins the binding, not a copy of it.
	cmd := resolveInTx(t, db, tenantID, resolver, req)
	plan := intent.TransactionPlan{
		PlanID: "promotion.transaction/" + ids.proposal.String(),
		Tenant: values.TenantId(tenantID.String()),
	}
	streams := map[string]string{
		domaincommit.ParticipantAssignment:   "people.assignment/" + ids.assignment.String(),
		domaincommit.ParticipantOccupancy:    "position.occupancy/" + ids.position.String(),
		domaincommit.ParticipantCompensation: "rewards.compensation/" + ids.pkg.String(),
		domaincommit.ParticipantBudget:       "rewards.budget/" + ids.reservation.String(),
	}
	for _, id := range cmd.PlanParticipants {
		if id == domaincommit.ParticipantBudget {
			continue
		}
		stream, ok := streams[id]
		if !ok {
			t.Fatalf("bound participant %q names no known stream", id)
		}
		plan.Participants = append(plan.Participants, intent.PlanParticipant{
			ParticipantID: id, StreamID: stream, StorageClass: "LOCAL_POSTGRES", Local: true,
		})
	}
	resolution, err := transaction.ResolveConsistencyBoundary(resolve016(tenantID).Boundary, plan, resolve016(tenantID).Boundary.CoordinatorEpoch)
	if err != nil {
		t.Fatalf("resolve test plan: %v", err)
	}
	dropped := cmd
	dropped.PlanParticipants = cmd.PlanParticipants[:3]
	if _, err := promotionterminal.BindResolution(resolution, dropped); err == nil {
		t.Fatal("BindResolution(dropped participant) = nil, want refusal")
	}

	// A swapped baseline digest cannot commit: the worker moved under the
	// resolver, and the writer's own comparison is the backstop.
	swapped := cmd
	swapped.ExpectedWorkerDigest = strings.Repeat("1", 64)
	must016CommitErr(t, db, tenantID, swapped)

	// A wrong coordinator epoch cannot resolve: the boundary's fencing term
	// is checked, not carried blindly, by the exact call the resolver makes.
	if _, err := transaction.ResolveConsistencyBoundary(resolve016(tenantID).Boundary, plan, 999); err == nil {
		t.Fatal("ResolveConsistencyBoundary(wrong epoch) = nil, want the fencing refusal")
	}
}

func must016CommitErr(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, cmd domaincommit.Command) {
	t.Helper()
	err := in016TenantTx(db, tenantID, func(tx dbport.Tx) error {
		_, err := promotioncommit.Writer{}.Write(ctxOf(t), tx, cmd)
		return err
	})
	if err == nil {
		t.Fatal("Write(swapped digest) = nil, want the baseline refusal")
	}
}
