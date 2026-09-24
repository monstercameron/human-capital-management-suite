package bootstrap_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/evidencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/positionpicker"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// rev05701CommitLossDB delegates to the live PostgreSQL connection and drops
// the acknowledgement only after the ordinary promotion writer has inserted
// its compensation-component successor. The transaction has committed when
// the injected error is returned.
type rev05701CommitLossDB struct {
	inner dbport.Beginner
	mu    sync.Mutex
	lost  int
	armed bool
}

func (d *rev05701CommitLossDB) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := d.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &rev05701CommitLossTx{Tx: tx, db: d}, nil
}

type rev05701CommitLossTx struct {
	dbport.Tx
	db                *rev05701CommitLossDB
	wroteCompensation bool
}

func (t *rev05701CommitLossTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	n, err := t.Tx.Exec(ctx, query, args...)
	if err == nil && strings.Contains(strings.ToLower(query), "insert into compensation_component") {
		t.wroteCompensation = true
	}
	return n, err
}

func (t *rev05701CommitLossTx) Commit(ctx context.Context) error {
	if err := t.Tx.Commit(ctx); err != nil {
		return err
	}
	if t.wroteCompensation {
		t.db.mu.Lock()
		defer t.db.mu.Unlock()
		if t.db.armed && t.db.lost == 0 {
			t.db.lost++
			return errors.New("REV-057-01 injected commit acknowledgement loss after PostgreSQL commit")
		}
	}
	return nil
}

func (d *rev05701CommitLossDB) losses() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lost
}

func (d *rev05701CommitLossDB) arm() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.armed = true
}

type rev05701DiagnosticGrant struct{}

const rev05701EffectiveDate = "2026-10-01"

func (rev05701DiagnosticGrant) AllowsInternalDiagnostics() bool { return true }

func composeREV05701Journey(t *testing.T, h *journeyHarness, db dbport.Beginner, managerApprover string, now func() time.Time) {
	t.Helper()
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	terminal := &effects.LedgerTerminalWriter{
		Appender: ledgerport.NewAppender(registry), ProjectionName: "workflow_promotion_outcome_rev05701",
		SourceRef: "hcmnext:test:rev05701",
	}
	evidence := evidencestore.New(db, func(tenant values.TenantId) uuid.UUID {
		return pgstore.TenantID(string(tenant))
	})
	execution, err := platformexecution.NewPromotionExecution(platformexecution.PromotionExecutionConfig{
		DB: db, Terminal: terminal, Plan: platformexecution.PLAN_EXECUTE, Clock: now,
		ApproverPrincipalID: journeyApprover, ManagerApproverPrincipalID: managerApprover,
		AuthorityDigest: "sha256:test-p1b-authority-amendment", RequiredRole: executionAuthorityTestRole,
		Evidence: evidence, TimerDataset: values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"},
	})
	if err != nil {
		t.Fatalf("NewPromotionExecution after restart: %v", err)
	}
	jc := h.cell
	composed, err := app.NewCell(app.CellConfig{
		Store: jc.store, RoleAccess: bootstrapTestRoleAccess(t, jc.pool, testTenant), Verifier: h.verifier,
		Audience: testAudience, MaxDeadline: 30 * time.Second, Logger: transport.LoggerFunc(jc.appendRecord),
		Now: now, Evidence: evidence,
		Executor: execution.Executor, ExecutionAuthority: execution.Authority, ExecutionResolver: execution.Resolver,
		ExecutionVersions: execution.Versions, ExecutionCellID: testCellID,
		TenantUUID:  func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) },
		ExecutionDB: db, ExecutionApprover: journeyApprover,
		ApprovalAuthority: platformexecution.NewPromotionApprovalAuthority(platformexecution.PromotionExecutionConfig{
			Plan: platformexecution.PLAN_EXECUTE, ApproverPrincipalID: journeyApprover,
			ManagerApproverPrincipalID: managerApprover,
		}),
		BindPromotionSteps: func(services *app.PromotionStepServices) error { return execution.BindStepServices(services) },
	})
	if err != nil {
		t.Fatalf("app.NewCell after restart: %v", err)
	}
	jc.app = composed
	h.engine = composed.Journey
}

func journeyWorkItemApprover(t *testing.T, item workitem.WorkItem) string {
	t.Helper()
	if item.OwnerRef != "" && item.Assignment.IsCandidate(item.OwnerRef) {
		return item.OwnerRef
	}
	for _, candidate := range item.Assignment.Resolution.Candidates {
		if candidate.PrincipalID != "" {
			return candidate.PrincipalID
		}
	}
	t.Fatalf("work item %s/%s has no routed approver candidate: owner=%q assignment=%+v", item.NodeID, item.WorkItemID, item.OwnerRef, item.Assignment)
	return ""
}

// seedREV05701GovernedInputs supplies the durable inputs the ordinary
// promotion gates consume. CreateWorker projects its real workforce row into
// employment, assignment, position occupancy and compensation aggregates;
// the catalog call records the budget pool; seedTargetPosition gets its
// reference from the real picker over a persisted OPEN position.
func seedREV05701GovernedInputs(t *testing.T, h *journeyHarness) (workspace.WorkerSummary, string, string, string) {
	t.Helper()
	ctx := context.Background()
	tx, err := h.cell.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin governed promotion catalog seed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenant := pgstore.TenantID(testTenant)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope governed promotion catalog seed: %v", err)
	}
	if _, err := demoworkforce.SeedOrganization(ctx, tx, tenant); err != nil {
		t.Fatalf("seed governed organization: %v", err)
	}
	catalog, err := app.PromotionAggregateCatalog(time.Date(2026, 9, 1, 13, 45, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("derive published promotion catalog: %v", err)
	}
	if _, err := demoworkforce.SeedAggregateCatalog(ctx, tx, tenant, catalog); err != nil {
		t.Fatalf("seed published promotion jobs, positions, and budget pools: %v", err)
	}
	if err := promotioncommit.RegisterEffectSchemas(ctx, tx, tenant, promotionterminal.EffectSchemaRefs()...); err != nil {
		t.Fatalf("publish promotion outbox payload schemas: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit governed promotion catalog seed: %v", err)
	}
	managerInput := journeyWorkerInput()
	managerInput.LegalName, managerInput.PreferredName = "Nora Manager", "Nora"
	manager, err := h.engine.CreateWorker(h.operatorCtx(t), managerInput)
	if err != nil {
		t.Fatalf("CreateWorker for governed manager: %v", err)
	}
	subjectInput := journeyWorkerInput()
	subjectInput.LegalName, subjectInput.PreferredName = "Avery Stone", "Avery"
	subjectInput.PositionID = "POS-HRBP-205"
	subjectInput.ManagerRef = manager.WorkerID
	worker, err := h.engine.CreateWorker(h.operatorCtx(t), subjectInput)
	if err != nil {
		t.Fatalf("CreateWorker for governed promotion subject: %v", err)
	}
	assertREV05701ManagerAssignment(t, h, worker.WorkerID, manager.WorkerID)
	positionRef, selectedPositionID := rev05701PickerPosition(t, h)
	// Promotion routing resolves the manager row's persisted WorkerKey, which
	// is also the trusted SubjectID for a created worker. WorkerRef is only
	// the display slug and is not the routed principal.
	return worker, positionRef, selectedPositionID, manager.SubjectID
}

func assertREV05701ManagerAssignment(t *testing.T, h *journeyHarness, workerID, managerID string) {
	t.Helper()
	ctx := context.Background()
	tx, err := h.cell.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin governed manager assignment read: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenant := pgstore.TenantID(testTenant)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope governed manager assignment read: %v", err)
	}
	row, found, err := (workforce.Store{}).Get(ctx, tx, tenant, workerID)
	if err != nil || !found {
		t.Fatalf("read governed worker row %s: found=%t error=%v", workerID, found, err)
	}
	ids := demoworkforce.AggregateIDsFor(row)
	assignment, err := (aggregates.PeopleStore{}).CurrentAssignment(ctx, tx, tenant, ids.Assignment, time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("read governed current assignment: %v", err)
	}
	if assignment.ManagerRelationshipRef != managerID {
		t.Fatalf("governed assignment manager = %q, want direct manager worker %q", assignment.ManagerRelationshipRef, managerID)
	}
}

func rev05701PickerPosition(t *testing.T, h *journeyHarness) (string, string) {
	t.Helper()
	tenant := values.TenantId(testTenant)
	effective, err := values.NewLocalDate(2026, time.October, 1)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(values.NewInstant(baseTime))
	if err != nil {
		t.Fatal(err)
	}
	positionID := demoworkforce.VacancyPositionID("people-ops", "OPS-HRBP3", "P3", 1)
	ref := values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: positionID.String()}
	candidates, err := positionpicker.ResolveCandidates(context.Background(),
		positionfacts.Reader{DB: h.cell.pool, TenantUUID: func(t values.TenantId) uuid.UUID { return pgstore.TenantID(string(t)) }},
		positionpicker.Request{
			Tenant: tenant, AsOf: position.AsOf{EffectiveOn: effective, KnownAt: known},
			Directory: []positionpicker.DirectoryEntry{{
				Position: ref, Title: "Senior HR Business Partner", Organization: "people-ops",
			}},
			DesiredJobCode: "OPS-HRBP3", DesiredOrgUnit: "people-ops",
		})
	if err != nil {
		t.Fatalf("picker resolve governed target position: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("picker disclosed %d target positions, want one", len(candidates))
	}
	decodedPosition, _, err := candidates[0].Reference.Decode()
	if err != nil {
		t.Fatalf("decode picker-issued position revision: %v", err)
	}
	if decodedPosition != ref {
		t.Fatalf("picker-issued reference position = %v, want resolved candidate %v", decodedPosition, ref)
	}
	return candidates[0].Reference.String(), decodedPosition.Id
}

func fireREV05701EffectiveDateTimer(t *testing.T, h *journeyHarness, instanceID string) time.Time {
	t.Helper()
	ctx := context.Background()
	tenantID := pgstore.TenantID(testTenant)
	parsedInstanceID, err := uuid.Parse(instanceID)
	if err != nil {
		t.Fatalf("parse timer instance id: %v", err)
	}
	tx, err := h.cell.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin scheduler timer fire: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope scheduler timer fire: %v", err)
	}
	var timerID uuid.UUID
	var fireAt time.Time
	if err := tx.QueryRow(ctx, `SELECT timer_id, fires_at FROM workflow_timer
		WHERE tenant_id=$1 AND instance_id=$2 AND node_id='wait_effective_date' AND timer_state='PENDING'
		ORDER BY timer_id LIMIT 1`, tenantID, parsedInstanceID).Scan(&timerID, &fireAt); err != nil {
		t.Fatalf("load pending effective-date timer: %v", err)
	}
	grant, err := (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
		TenantID: tenantID,
		Resource: lease.Resource{Kind: lease.ResourceQueue, ID: "queue:workflow-runtime"},
		Holder:   lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:rev05701-test"},
		Now:      fireAt, TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("acquire timer queue lease: %v", err)
	}
	result, err := (timer.Scheduler{}).Fire(ctx, tx, timer.FireRequest{
		TenantID: tenantID, Now: fireAt, Fence: grant.Fence,
		Misfire: schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour},
		Only:    []uuid.UUID{timerID},
	})
	if err != nil {
		t.Fatalf("fire effective-date timer under queue lease: %v", err)
	}
	if len(result.Fired) != 1 || result.Fired[0].Timer.TimerID != timerID || result.Fired[0].ReadyWorkID == uuid.Nil {
		t.Fatalf("lease-fenced timer fire result = %+v, want one fired timer with ready work", result)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit lease-fenced timer fire: %v", err)
	}
	return fireAt
}

func TestTodo_REV_057_01_Integration(t *testing.T) {
	var lostDB *rev05701CommitLossDB
	currentTime := baseTime
	now := func() time.Time { return currentTime }
	h := newJourneyHarnessWithPlan(t, platformexecution.PLAN_EXECUTE, func(inner dbport.Beginner) dbport.Beginner {
		lostDB = &rev05701CommitLossDB{inner: inner}
		return lostDB
	}, func(cfg *app.CellConfig) {
		// UUIDv7's first eight characters encode its millisecond timestamp, and
		// the ordinary worker flow derives employment/assignment ids from that
		// prefix. Keep these two governed fixture workers in separate millis so
		// their projected aggregate ids remain unique under the frozen test clock.
		next := uint64(0)
		cfg.IDs = func() (string, error) {
			id := fmt.Sprintf("%08x-0000-7000-8000-%012x", uint32(0x0192f3c4+next), next+1)
			next++
			return id, nil
		}
	})
	ctx := h.operatorCtx(t)
	worker, positionRef, selectedPositionID, managerPrincipal := seedREV05701GovernedInputs(t, h)
	composeREV05701Journey(t, h, lostDB, managerPrincipal, now)
	input := journeyProposalFor(worker.WorkerRef)
	input.EffectiveDate = rev05701EffectiveDate
	input.TargetPositionID = positionRef
	proposal, err := h.engine.Propose(ctx, input)
	if err != nil {
		var diagnostic error
		var owned *envelope.Error
		if errors.As(err, &owned) {
			diagnostic, _ = owned.Diagnostic(rev05701DiagnosticGrant{})
		}
		t.Fatalf("Propose: %v; diagnostic: %v", err, diagnostic)
	}
	if proposal.Target.PositionID != selectedPositionID {
		t.Fatalf("persisted proposal target position = %q, want picker-issued position ID %q", proposal.Target.PositionID, selectedPositionID)
	}
	started, err := h.engine.Execute(ctx, proposal.IntentID)
	if err != nil {
		detail, simulateErr := h.cell.app.Service.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: proposal.IntentID})
		if simulateErr != nil {
			t.Fatalf("ExecuteJourney: %v; diagnostic resimulation also failed: %v", err, simulateErr)
		}
		artifact := detail.GetSimulation()
		_, rawErr := h.cell.app.Service.ExecuteIntent(ctx, &intentsv1.ExecuteIntentRequest{
			IdempotencyKey: "journey:execute:" + proposal.IntentID, IntentId: proposal.IntentID,
			Approval: &intentsv1.ProposalApproval{ProposalRevisionId: artifact.GetProposalRevisionId(),
				MaterialProposalDigest: artifact.GetMaterialProposalDigest(), Approved: true,
				ApprovalRef: "approval:journey:" + proposal.IntentID},
		})
		var diagnostic error
		if owned, ok := rawErr.(*envelope.Error); ok {
			diagnostic, _ = owned.Diagnostic(rev05701DiagnosticGrant{})
		}
		states := queryOne[string](t, h.cell, `SELECT COALESCE(string_agg(node_id || ':' || status, ',' ORDER BY node_id), '')
			FROM workflow_node_execution WHERE tenant_id=$1`, pgstore.TenantID(testTenant))
		instances := queryOne[int64](t, h.cell, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1`, pgstore.TenantID(testTenant))
		t.Fatalf("ExecuteJourney: %v; raw ExecuteIntent error: %v; diagnostic: %v; instances=%d node_states=%q", err, rawErr, diagnostic, instances, states)
	}
	if len(started.WorkItems) != 1 {
		t.Fatalf("initial work items = %d, want finance approval", len(started.WorkItems))
	}
	financeApprover := journeyWorkItemApprover(t, started.WorkItems[0])
	financeDetail, err := h.engine.Decide(h.ctxAs(t, financeApprover, testRole), proposal.IntentID, workspace.Decision{Approve: true, Reason: "reason.promotion_supported/v1"})
	if err != nil {
		t.Fatalf("finance approval: %v", err)
	}
	var managerWorkItems []workitem.WorkItem
	for _, item := range financeDetail.WorkItems {
		if item.NodeID == promotionexec.NodeApproveManager {
			managerWorkItems = append(managerWorkItems, item)
		}
	}
	if len(managerWorkItems) != 1 {
		t.Fatalf("after finance approval manager-node work items = %d (total %d), want one routed direct-manager approval", len(managerWorkItems), len(financeDetail.WorkItems))
	}
	managerApprover := journeyWorkItemApprover(t, managerWorkItems[0])
	if managerApprover != managerPrincipal {
		t.Fatalf("routed manager approver = %q, want persisted direct manager principal %q", managerApprover, managerPrincipal)
	}
	managerCtx := h.ctxAs(t, managerApprover, testRole)
	managerDetail, err := h.engine.Decide(managerCtx, proposal.IntentID, workspace.Decision{Approve: true, Reason: "reason.promotion_supported/v1"})
	if err != nil {
		t.Fatalf("manager approval: %v", err)
	}
	if managerDetail.Summary.Stage != workspace.JourneyStageWaitingEffectiveDate || managerDetail.Instance == nil {
		t.Fatalf("after manager approval detail = %+v, want a workflow parked at its effective-date wait", managerDetail)
	}
	approvedGovernance := queryOne[int64](t, h.cell, `SELECT count(*) FROM promotion_approval_governance
		WHERE tenant_id=$1 AND proposal_revision_id=$2::uuid AND decision_state='ALLOW'`, pgstore.TenantID(testTenant), proposal.ProposalRevisionID)
	if approvedGovernance != 2 {
		t.Fatalf("durable approval governance ALLOW records = %d, want finance and manager approvals", approvedGovernance)
	}
	heldBudget := queryOne[int64](t, h.cell, `SELECT count(*) FROM budget_reservation
		WHERE tenant_id=$1 AND proposal_ref=$2::uuid AND status='HELD'`, pgstore.TenantID(testTenant), proposal.ProposalRevisionID)
	if heldBudget != 1 {
		t.Fatalf("durable budget reservations HELD = %d, want one for proposal revision %s", heldBudget, proposal.ProposalRevisionID)
	}
	instanceID := managerDetail.Instance.InstanceID
	// The scheduler settles the durable promise under its queue lease and
	// enqueues ready work before the ordinary app ResumeFiredTimer boundary.
	currentTime = fireREV05701EffectiveDateTimer(t, h, instanceID)
	before := queryOne[int64](t, h.cell, `SELECT count(*) FROM compensation_component WHERE tenant_id=$1`, pgstore.TenantID(testTenant))
	lostDB.arm()
	_, err = h.cell.app.ResumeFiredTimer(app.WithResumeTenant(context.Background(), testTenant), instanceID, "wait_effective_date", 1)
	if err == nil || !strings.Contains(err.Error(), "REV-057-01 injected commit acknowledgement loss") {
		states := queryOne[string](t, h.cell, `SELECT COALESCE(string_agg(node_id || ':' || status, ',' ORDER BY node_id), '')
			FROM workflow_node_execution WHERE tenant_id=$1`, pgstore.TenantID(testTenant))
		executeFailure := queryOne[string](t, h.cell, `SELECT COALESCE(error_class, '') || ':' || COALESCE(output_artifact_ref, '')
			FROM workflow_node_execution WHERE tenant_id=$1 AND instance_id=$2::uuid AND node_id='execute_promotion'
			ORDER BY attempt DESC LIMIT 1`, pgstore.TenantID(testTenant), instanceID)
		rows := queryOne[int64](t, h.cell, `SELECT count(*) FROM compensation_component WHERE tenant_id=$1`, pgstore.TenantID(testTenant))
		governance := queryOne[string](t, h.cell, `SELECT COALESCE(string_agg(node_id || '=' || decision_state || ':' || record::text, E'\n' ORDER BY recorded_at), '')
			FROM promotion_approval_governance WHERE tenant_id=$1`, pgstore.TenantID(testTenant))
		detail, inspectErr := h.engine.Inspect(h.operatorCtx(t), proposal.IntentID)
		t.Fatalf("effective-date timer resume error = %v, want injected lost acknowledgement after domain commit; losses=%d rows_before=%d rows_after=%d node_states=%q execute_failure=%q inspect_error=%v stage=%s findings=%+v governance=%s",
			err, lostDB.losses(), before, rows, states, executeFailure, inspectErr, detail.Summary.Stage, detail.Findings, governance)
	}
	if lostDB.losses() != 1 {
		t.Fatalf("lost commit acknowledgements = %d, want one", lostDB.losses())
	}
	afterCommit := queryOne[int64](t, h.cell, `SELECT count(*) FROM compensation_component WHERE tenant_id=$1`, pgstore.TenantID(testTenant))
	if afterCommit != before+1 {
		t.Fatalf("base-pay component rows after ambiguous commit = %d, before %d; want one committed successor", afterCommit, before)
	}
	committedNodes := queryOne[int64](t, h.cell, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id=$1 AND instance_id=$2::uuid AND node_id='execute_promotion' AND status='SUCCEEDED'`, pgstore.TenantID(testTenant), instanceID)
	if committedNodes != 1 {
		t.Fatalf("durable execute_promotion outcomes after lost acknowledgement = %d, want one", committedNodes)
	}

	// Rebuild both driver and journey service against the same database. The
	// committed ordinary drain is now parked on its durable payroll signal wait,
	// so recovery observes that frontier rather than trying to replay a timer or
	// invoke compensation again.
	composeREV05701Journey(t, h, lostDB, managerPrincipal, now)
	afterRestart, err := h.engine.Inspect(h.operatorCtx(t), proposal.IntentID)
	if err != nil {
		t.Fatalf("restart inspection of committed promotion: %v", err)
	}
	if afterRestart.Instance == nil || afterRestart.Instance.InstanceID != instanceID || afterRestart.Summary.Stage != workspace.JourneyStageObservingEffects {
		t.Fatalf("restart inspection = stage %s instance %+v, want OBSERVING_EFFECTS on %s", afterRestart.Summary.Stage, afterRestart.Instance, instanceID)
	}
	frontier := queryOne[string](t, h.cell, `SELECT runtime_status || '@' || instance_version::text || ':' || array_to_string(current_node_ids, ',')
		FROM workflow_instance WHERE tenant_id=$1 AND instance_id=$2::uuid`, pgstore.TenantID(testTenant), instanceID)
	if !strings.HasSuffix(frontier, ":await_payroll_confirmation") || !strings.HasPrefix(frontier, "RUNNING@") {
		t.Fatalf("durable instance after restart = %q, want RUNNING on await_payroll_confirmation", frontier)
	}
	openPayrollWaits := queryOne[int64](t, h.cell, `SELECT count(*) FROM workflow_signal_subscription
		WHERE tenant_id=$1 AND instance_id=$2::uuid AND node_id='await_payroll_confirmation' AND subscription_state='OPEN'`, pgstore.TenantID(testTenant), instanceID)
	if openPayrollWaits != 1 {
		t.Fatalf("durable open payroll signal waits after restart = %d, want one", openPayrollWaits)
	}
	afterRestartRead := queryOne[int64](t, h.cell, `SELECT count(*) FROM compensation_component WHERE tenant_id=$1`, pgstore.TenantID(testTenant))
	if afterRestartRead != afterCommit {
		t.Fatalf("compensation rows after restart inspection = %d, after commit %d; duplicate domain write", afterRestartRead, afterCommit)
	}
	committedNodes = queryOne[int64](t, h.cell, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id=$1 AND instance_id=$2::uuid AND node_id='execute_promotion' AND status='SUCCEEDED'`, pgstore.TenantID(testTenant), instanceID)
	if committedNodes != 1 {
		t.Fatalf("durable execute_promotion outcomes after restart inspection = %d, want one", committedNodes)
	}
	if lostDB.losses() != 1 {
		t.Fatalf("compensation commit acknowledgement loss count changed after restart: %d", lostDB.losses())
	}
}
