package application

// WF-RUN-034 on the served path: the composition PROMOUX-015 drives
// (embedded PostgreSQL, ComposeServe with the executable plan, the real gRPC
// JourneyService and the production timer scheduler) now runs
// internal/platform/execution/promotionsteps for every node.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/committedfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/evidencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// wfrun034Route is one recorded advancement: the node, the route its
// outcome took, and the successor it derived.
type wfrun034Route struct{ source, route, target, kind, terminal string }

func (h *promoux015Harness) wfrun034Routes() []wfrun034Route {
	h.t.Helper()
	rows, err := h.pool.Query(context.Background(), `SELECT source_node_id, coalesce(route_key,''), target_node_id, kind, coalesce(terminal_code,'')
		FROM workflow_continuation ORDER BY recorded_at, source_node_id, target_node_id`)
	if err != nil {
		h.t.Fatalf("read continuations: %v", err)
	}
	defer rows.Close()
	var out []wfrun034Route
	for rows.Next() {
		var r wfrun034Route
		if err := rows.Scan(&r.source, &r.route, &r.target, &r.kind, &r.terminal); err != nil {
			h.t.Fatalf("scan continuation: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func wfrun034Took(routes []wfrun034Route, source, target string) bool {
	for _, r := range routes {
		if r.source == source && r.target == target {
			return true
		}
	}
	return false
}

func (h *promoux015Harness) wfrun034Count(sql string, args ...any) int64 {
	h.t.Helper()
	var n int64
	if err := h.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		h.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// TestTodo_WF_RUN_034_Integration proves a served promotion invokes each
// capability node through the gateway as the delegated executing principal
// (evidence per invocation carries the subject, purpose, idempotency key,
// deadline and effect class), routes the real threshold decision, and after
// the effective date revalidates for real against the GOVERN-002 decision its
// approvals recorded: an unchanged world confirms, execute_promotion commits
// the bounded domain change inside the advance transaction, the three
// observations read it back and the run closes PROMOTION_COMPLETE with its
// one ledger fact. Nothing here is fabricated: every route is the answer a
// governed read produced.
func TestTodo_WF_RUN_034_Integration(t *testing.T) {
	h := promoux015Compose(t)
	evidence := func() []evidencestore.Record {
		return servedEvidence(t, h.composed.Cell(), values.TenantId(demoworkforce.CompanyKey))
	}
	before := len(evidence())

	id := h.runSeparatedPromotion()
	if n := h.wfrun034Count(`SELECT count(*) FROM workflow_execution_delegation WHERE subject = $1 AND 'promotion_operator' = ANY(roles)`, promoux015Manager); n != 1 {
		t.Fatalf("pinned delegations for the executing operator = %d, want 1", n)
	}
	routes := h.wfrun034Routes()
	for _, edge := range [][2]string{
		{promotionexec.NodeSnapshotWorker, promotionexec.NodeSimulateCompensation},
		{promotionexec.NodeSimulateCompensation, promotionexec.NodeEvaluateBand},
		{promotionexec.NodeEvaluateBand, promotionexec.NodeRaiseThreshold},
		// The subject moves P2 -> P3: RULE-003's grade-change row requires
		// finance, so the real decision routes ABOVE_THRESHOLD.
		{promotionexec.NodeRaiseThreshold, promotionexec.NodeApproveFinance},
	} {
		if !wfrun034Took(routes, edge[0], edge[1]) {
			t.Fatalf("served run did not advance %s -> %s; routes %+v", edge[0], edge[1], routes)
		}
	}
	for _, r := range routes {
		if r.source == promotionexec.NodeRaiseThreshold && r.route != "ABOVE_THRESHOLD" {
			t.Fatalf("raise_threshold routed %q, want ABOVE_THRESHOLD", r.route)
		}
	}

	invoked := map[string]int{}
	for _, rec := range evidence()[before:] {
		if rec.IdempotencyKey == "" {
			continue // interactive calls (proposal simulation) carry no envelope
		}
		if rec.Decision != "INVOKED" || rec.SubjectRef != promoux015Manager || rec.Purpose != authz.PurposeCompensationReview ||
			!strings.HasPrefix(rec.IdempotencyKey, "workflow:") || rec.Deadline.IsZero() || rec.EffectClass != "READ_ONLY" {
			t.Fatalf("step evidence = %+v, want an INVOKED READ_ONLY call as %s", rec, promoux015Manager)
		}
		invoked[rec.CapabilityID]++
	}
	for _, capabilityID := range []string{
		promotionexec.CapabilitySnapshotWorker, promotionexec.CapabilitySimulateCompensation,
		promotionexec.CapabilityEvaluateBand, promotionexec.CapabilityExecutePromotion,
	} {
		if invoked[capabilityID] == 0 {
			t.Fatalf("no governed invocation of %s was recorded: %v", capabilityID, invoked)
		}
	}
	var withEvidence int64
	if withEvidence = h.wfrun034Count(`SELECT count(*) FROM workflow_node_execution WHERE node_id IN ($1,$2,$3) AND capability_execution_id LIKE 'ev:capability:%' AND authorization_decision_id = $4`,
		promotionexec.NodeSnapshotWorker, promotionexec.NodeSimulateCompensation, promotionexec.NodeEvaluateBand, promoux015Manager); withEvidence < 3 {
		t.Fatalf("capability node executions citing gateway evidence and the delegated subject = %d, want 3", withEvidence)
	}

	approved := h.effects()
	fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background())
	if err != nil || fired.Fired != 1 {
		t.Fatalf("Tick after the effective date = %+v, %v; want one timer fired", fired, err)
	}
	routes = h.wfrun034Routes()
	for _, edge := range [][2]string{
		{promotionexec.NodeRevalidate, promotionexec.NodeStillValid},
		{promotionexec.NodeStillValid, promotionexec.NodeExecutePromotion},
		{promotionexec.NodeExecutePromotion, promotionexec.NodeObservePayroll},
		{promotionexec.NodeObservePayroll, promotionexec.NodeObserveAccess},
		{promotionexec.NodeObserveAccess, promotionexec.NodeObserveReconciliation},
		{promotionexec.NodeObserveReconciliation, promotionexec.NodeEndComplete},
	} {
		if !wfrun034Took(routes, edge[0], edge[1]) {
			t.Fatalf("after the effective date the run did not advance %s -> %s; routes %+v", edge[0], edge[1], routes)
		}
	}
	if wfrun034Took(routes, promotionexec.NodeStillValid, promotionexec.NodeEndBlocked) {
		t.Fatal("still_valid blocked a revalidation the recorded governance confirms")
	}
	var completed bool
	for _, r := range routes {
		completed = completed || (r.kind == "COMPLETE" && r.terminal == "PROMOTION_COMPLETE")
	}
	if !completed {
		t.Fatalf("no PROMOTION_COMPLETE terminal: %+v", routes)
	}

	// The promotion committed its bounded domain change exactly once: the
	// approved placement, the approved pay, the target occupancy, the budget
	// hold transitioned to COMMITTED and one outbox leg per declared effect.
	after := h.effects()
	if err := promoux015CommittedOnce(approved, after); err != nil {
		t.Fatalf("committed effects: %v", err)
	}
	for _, check := range []struct {
		what string
		sql  string
		want int64
	}{
		{"payroll sync legs", `SELECT count(*) FROM outbox WHERE effect_identity LIKE 'payroll:%'`, 1},
		{"IAM sync legs", `SELECT count(*) FROM outbox WHERE effect_identity LIKE 'iam:%'`, 1},
		{"committed budget reservations", `SELECT count(*) FROM budget_reservation WHERE status = 'COMMITTED' AND superseded_at IS NULL`, 1},
		{"recorded approval governance", `SELECT count(*) FROM promotion_approval_governance`, 2},
	} {
		if got := h.wfrun034Count(check.sql); got != check.want {
			t.Fatalf("%s = %d, want %d", check.what, got, check.want)
		}
	}
	if n := h.wfrun034Count(`SELECT count(*) FROM assignment WHERE job_code = $1 AND grade = $2 AND superseded_at IS NULL`, "OPS-HRBP3", "P3"); n != 1 {
		t.Fatalf("committed assignments at the target placement = %d, want 1", n)
	}
	if n := h.wfrun034Count(`SELECT count(*) FROM workflow_node_execution WHERE node_id = $1 AND status = 'SUCCEEDED'`, promotionexec.NodeExecutePromotion); n != 1 {
		t.Fatalf("succeeded execute_promotion attempts = %d, want exactly one", n)
	}
	if got := h.journeyFor("hiring-manager", id).GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED {
		t.Fatalf("proposer sees stage %s, want RECORDED", got)
	}
	detail, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil || detail.GetDetail().GetLedger() == nil {
		t.Fatalf("InspectJourney after the commit = %v, ledger %v; want the terminal ledger fact", err, detail.GetDetail().GetLedger())
	}
	// The promoted worker reads as promoted from the effective date on: the
	// same committed-placement read the worker listing and the governed
	// worker read are composed over reports the target job, grade and pay.
	// Before that date the recorded placement still stands, which is what an
	// effective-dated promotion means.
	promoted := h.committedPlacement(h.afterEffectiveDate()())
	if promoted.JobCode != "OPS-HRBP3" || promoted.Grade != "P3" || promoted.BasePay == "" {
		t.Fatalf("committed placement at the effective date = %+v, want the target placement and pay", promoted)
	}
	listed, err := h.client.ListWorkers(h.rpc("hiring-manager"), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatalf("ListWorkers after the commit: %v", err)
	}
	var row *journeyv1.Worker
	for _, worker := range listed.GetWorkers() {
		if worker.GetWorkerRef() == h.subject {
			row = worker
		}
	}
	if row == nil || row.GetJobCode() != "OPS-HRBP2" {
		t.Fatalf("before the effective date the listing = %+v, want the pre-promotion placement", row)
	}
}

// committedPlacement is the committed placement read the journey surfaces are
// composed over, at one business instant.
func (h *promoux015Harness) committedPlacement(at time.Time) committedfacts.Placement {
	h.t.Helper()
	ctx := context.Background()
	tenantID := pgstore.TenantID(demoworkforce.CompanyKey)
	var workerID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT worker_id FROM journey_worker WHERE tenant_id = $1 AND worker_key = $2`, tenantID, h.subject).Scan(&workerID); err != nil {
		h.t.Fatalf("read the promoted worker: %v", err)
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		h.t.Fatalf("scope tenant: %v", err)
	}
	placement, found, err := committedfacts.CurrentPlacement(ctx, tx, tenantID, workerID, at)
	if err != nil || !found {
		h.t.Fatalf("committed placement = %+v, %t, %v", placement, found, err)
	}
	return placement
}

// movePayBaseline records an intervening pay change for the promoted worker,
// exactly as any other governed pay write would: a new compensation component
// revision that supersedes the one the approval was computed against.
func (h *promoux015Harness) movePayBaseline() {
	h.t.Helper()
	ctx := context.Background()
	tenantID := pgstore.TenantID(demoworkforce.CompanyKey)
	var componentID, packageID uuid.UUID
	var amount, currency, frequency string
	if err := h.pool.QueryRow(ctx, `
		SELECT entity_id, package_ref, amount::text, currency, frequency FROM compensation_component
		WHERE tenant_id = $1 AND component_type = 'BASE_PAY' AND superseded_at IS NULL
		ORDER BY recorded_at DESC LIMIT 1`, tenantID).Scan(&componentID, &packageID, &amount, &currency, &frequency); err != nil {
		h.t.Fatalf("read the approved pay baseline: %v", err)
	}
	moved, err := values.NewMoney(amount, currency, 4, values.RoundingExactRequired)
	if err != nil {
		h.t.Fatalf("parse the approved pay: %v", err)
	}
	raised, err := moved.Add(mustMoney(h.t, "1000.0000", currency))
	if err != nil {
		h.t.Fatalf("raise the approved pay: %v", err)
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		h.t.Fatalf("scope tenant: %v", err)
	}
	component, err := aggregates.NewCompensationComponent(tenantID, componentID, packageID,
		time.Now().UTC().AddDate(-1, 0, 0), nil, time.Now().UTC(), "BASE_PAY", raised, frequency)
	if err != nil {
		h.t.Fatalf("build the intervening pay revision: %v", err)
	}
	if _, err := (aggregates.CompensationStore{}).PutCompensationComponent(ctx, tx, component); err != nil {
		h.t.Fatalf("record the intervening pay revision: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		h.t.Fatalf("commit the intervening pay revision: %v", err)
	}
}

func mustMoney(t *testing.T, amount, currency string) values.Money {
	t.Helper()
	money, err := values.NewMoney(amount, currency, 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("money %s %s: %v", amount, currency, err)
	}
	return money
}

// TestTodo_WF_RUN_034_Fault proves a world that moved after approval is never
// committed on the recorded approval alone: an intervening write to the
// promoted worker's own pay baseline between the approvals and the effective
// date routes still_valid to BLOCKED, nothing is committed, and the budget
// hold the proposal took is released with the terminal fact.
func TestTodo_WF_RUN_034_Fault(t *testing.T) {
	h := promoux015Compose(t)
	ctx := context.Background()
	id := h.runSeparatedPromotion()
	before := h.effects()

	// The world moves between the approvals and the effective date.
	h.movePayBaseline()

	fired, err := h.scheduler(h.afterEffectiveDate()).Tick(ctx)
	if err != nil || fired.Fired != 1 {
		t.Fatalf("Tick after the effective date = %+v, %v", fired, err)
	}
	routes := h.wfrun034Routes()
	if !wfrun034Took(routes, promotionexec.NodeStillValid, promotionexec.NodeEndBlocked) {
		t.Fatalf("a moved baseline did not block the commit; routes %+v", routes)
	}
	if wfrun034Took(routes, promotionexec.NodeStillValid, promotionexec.NodeExecutePromotion) {
		t.Fatal("still_valid confirmed a world that moved after approval")
	}
	// The blocked run records exactly one terminal outcome fact (its ledger
	// event and its outbox record) and nothing else: no commit
	// receipt, no effect leg, no committed reservation and no promoted
	// assignment.
	after := h.effects()
	if after.outcomeEvents-before.outcomeEvents != 1 || after.outcomeOutbox-before.outcomeOutbox != 1 || after.commitReceipts != before.commitReceipts {
		t.Fatalf("a blocked promotion = %+v -> %+v, want exactly its one terminal outcome fact and no commit", before, after)
	}
	for _, check := range []struct {
		what string
		sql  string
	}{
		{"effect legs", `SELECT count(*) FROM outbox WHERE effect_identity LIKE 'payroll:%' OR effect_identity LIKE 'iam:%'`},
		{"committed budget reservations", `SELECT count(*) FROM budget_reservation WHERE status = 'COMMITTED'`},
		{"promoted assignments", `SELECT count(*) FROM assignment WHERE job_code = 'OPS-HRBP3' AND superseded_at IS NULL`},
		{"succeeded execute_promotion attempts", `SELECT count(*) FROM workflow_node_execution WHERE node_id = 'execute_promotion' AND status = 'SUCCEEDED'`},
	} {
		if n := h.wfrun034Count(check.sql); n != 0 {
			t.Fatalf("a blocked promotion left %d %s", n, check.what)
		}
	}
	if n := h.wfrun034Count(`SELECT count(*) FROM budget_reservation WHERE status = 'HELD' AND superseded_at IS NULL`); n != 0 {
		t.Fatalf("a blocked promotion still holds %d budget reservations", n)
	}
	if got := h.journeyFor("hiring-manager", id).GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		t.Fatalf("proposer sees stage %s, want BLOCKED", got)
	}
}

// TestTodo_WF_RUN_034_Security proves a step re-authorizes the pinned
// delegation against the current role assignment: once the operator's
// promotion_operator role is revoked, the first step refuses at the gateway,
// no capability answers as that operator, and the run raises no approval.
func TestTodo_WF_RUN_034_Security(t *testing.T) {
	h := promoux015Compose(t)
	ctx := context.Background()
	if _, err := h.composed.Cell().RoleAccess.SaveAssignment(ctx, values.TenantId(demoworkforce.CompanyKey), "system:wfrun034", roleaccess.Assignment{
		WorkerRef: promoux015Manager, RoleIDs: []string{"hcm_admin", "comp_admin", "intent_author"},
	}); err != nil {
		t.Fatalf("revoke promotion_operator: %v", err)
	}
	evidence := func() []evidencestore.Record {
		return servedEvidence(t, h.composed.Cell(), values.TenantId(demoworkforce.CompanyKey))
	}
	before := len(evidence())

	req, _ := h.discoverPromotion("hiring-manager")
	proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), req)
	if err != nil {
		t.Fatalf("ProposeJourney: %v", err)
	}
	_, execErr := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: proposed.GetJourney().GetIntentId()})
	t.Logf("ExecuteJourney after revocation: %v", execErr)

	refused := false
	for _, rec := range evidence()[before:] {
		if rec.IdempotencyKey == "" {
			continue
		}
		if rec.Decision == "INVOKED" {
			t.Fatalf("a step invoked %s as the revoked operator: %+v", rec.CapabilityID, rec)
		}
		refused = refused || (rec.Decision == "REFUSED" && rec.ReasonCode == "CAPABILITY_UNAUTHORIZED" && rec.SubjectRef == promoux015Manager)
	}
	if !refused {
		t.Fatal("no step recorded a gateway refusal of the revoked delegation")
	}
	if n := h.wfrun034Count(`SELECT count(*) FROM workflow_execution_delegation WHERE subject = $1`, promoux015Manager); n != 1 {
		t.Fatalf("delegations pinned for the run = %d, want 1: the refusal must come from a started run's step", n)
	}
	if n := h.wfrun034Count(`SELECT count(*) FROM work_item`); n != 0 {
		t.Fatalf("a revoked delegation raised %d work items", n)
	}
	if n := h.wfrun034Count(`SELECT count(*) FROM workflow_continuation WHERE source_node_id = $1`, promotionexec.NodeSnapshotWorker); n != 0 {
		if routes := h.wfrun034Routes(); wfrun034Took(routes, promotionexec.NodeSnapshotWorker, promotionexec.NodeSimulateCompensation) {
			t.Fatalf("snapshot_worker succeeded under a revoked delegation: %+v", routes)
		}
	}
}
