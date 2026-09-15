package application

// WF-RUN-034 on the served path: the composition PROMOUX-015 drives
// (embedded PostgreSQL, ComposeServe with the executable plan, the real gRPC
// JourneyService and the production timer scheduler) now runs
// internal/platform/execution/promotionsteps for every node.

import (
	"context"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
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
// the effective date runs revalidation for real: no durable GOVERN-002
// historical decision exists, so still_valid routes BLOCKED and the promotion
// closes PROMOTION_BLOCKED with no domain commit, instead of the fabricated
// VALID -> commit -> PASS -> COMPLETE it used to report.
func TestTodo_WF_RUN_034_Integration(t *testing.T) {
	h := promoux015Compose(t)
	evidence := h.composed.Cell().Evidence
	before := evidence.Len()

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
	for _, rec := range evidence.Records()[before:] {
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
	if !wfrun034Took(routes, promotionexec.NodeRevalidate, promotionexec.NodeStillValid) ||
		!wfrun034Took(routes, promotionexec.NodeStillValid, promotionexec.NodeEndBlocked) {
		t.Fatalf("after the effective date routes = %+v, want revalidate -> still_valid -> end_blocked", routes)
	}
	if wfrun034Took(routes, promotionexec.NodeStillValid, promotionexec.NodeExecutePromotion) {
		t.Fatal("still_valid confirmed a revalidation no durable governance record supports")
	}
	var blocked bool
	for _, r := range routes {
		blocked = blocked || (r.kind == "COMPLETE" && r.terminal == "PROMOTION_BLOCKED")
	}
	if !blocked {
		t.Fatalf("no PROMOTION_BLOCKED terminal: %+v", routes)
	}
	after := h.effects()
	if after.commitReceipts != approved.commitReceipts || h.wfrun034Count(`SELECT count(*) FROM outbox WHERE effect_identity LIKE 'payroll:%' OR effect_identity LIKE 'iam:%'`) != 0 {
		t.Fatalf("a blocked promotion committed domain effects: %+v -> %+v", approved, after)
	}
	// The frontier may record the untaken branch (SKIPPED); what must not
	// exist is an attempt that ran.
	if n := h.wfrun034Count(`SELECT count(*) FROM workflow_node_execution WHERE node_id = $1 AND status IN ('RUNNING','SUCCEEDED','FAILED','RETRYING')`, promotionexec.NodeExecutePromotion); n != 0 {
		t.Fatalf("execute_promotion ran %d times behind an unconfirmed revalidation", n)
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
	evidence := h.composed.Cell().Evidence
	before := evidence.Len()

	req, _ := h.discoverPromotion("hiring-manager")
	proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), req)
	if err != nil {
		t.Fatalf("ProposeJourney: %v", err)
	}
	_, execErr := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: proposed.GetJourney().GetIntentId()})
	t.Logf("ExecuteJourney after revocation: %v", execErr)

	refused := false
	for _, rec := range evidence.Records()[before:] {
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
