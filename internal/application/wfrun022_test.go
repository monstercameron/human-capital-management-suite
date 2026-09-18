package application

// WF-RUN-022: workflow recovery after a database and cell interruption, on
// the production composition promoux015Compose builds. A promotion is driven
// to its effective-date wait with a scheduler replica holding the queue lease;
// every database backend is then terminated and the whole composed server is
// stopped and recomposed from PostgreSQL alone. The restored runtime state
// must be identical, the wait must resume under a new fence, the dead replica
// must be refused, and the promotion must commit exactly once.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// wfrun022State is the durable runtime state recovery must preserve for one
// instance: the instance row's status, frontier, completion dimensions and
// version, every timer, frontier entry, signal subscription, node execution,
// continuation and work item, and the outbox and idempotency records. Leases
// are deliberately excluded: recovery is supposed to change them.
func wfrun022StateQueries() []string {
	return []string{
		`SELECT coalesce(json_agg(json_build_array(runtime_status, current_node_ids, completion_dimensions, instance_version, variable_revision_head))::text, '') FROM workflow_instance WHERE instance_id::text = $1`,
		`SELECT coalesce(json_agg(json_build_array(timer_id, node_id, timer_kind, timer_state, fires_at, timer_version) ORDER BY timer_id)::text, '') FROM workflow_timer WHERE instance_id::text = $1`,
		`SELECT coalesce(json_agg(json_build_array(node_id, entry_state, entry_sequence, admitted_at_version) ORDER BY entry_sequence, node_id)::text, '') FROM workflow_frontier_entry WHERE instance_id::text = $1`,
		`SELECT coalesce(json_agg(json_build_array(subscription_id, node_id, subscription_state, subscription_version) ORDER BY subscription_id)::text, '') FROM workflow_signal_subscription WHERE instance_id::text = $1`,
		`SELECT coalesce(json_agg(json_build_array(node_id, attempt, status, effect_refs) ORDER BY node_id, attempt)::text, '') FROM workflow_node_execution WHERE instance_id::text = $1`,
		`SELECT coalesce(json_agg(json_build_array(source_node_id, source_attempt, kind, target_node_id) ORDER BY source_node_id, source_attempt, kind)::text, '') FROM workflow_continuation WHERE instance_id::text = $1`,
		`SELECT coalesce(json_agg(json_build_array(node_id, status, item_version, completed_by) ORDER BY node_id)::text, '') FROM work_item WHERE workflow_instance_id::text = $1`,
		`SELECT coalesce(json_agg(json_build_array(effect_identity, status, attempts) ORDER BY effect_identity)::text, '') FROM outbox WHERE $1 <> ''`,
		`SELECT count(*)::text FROM idempotency_record WHERE $1 <> ''`,
	}
}

func (h *promoux015Harness) wfrun022State(instanceID string) []string {
	h.t.Helper()
	var parts []string
	for _, q := range wfrun022StateQueries() {
		var s string
		if err := h.pool.QueryRow(context.Background(), q, instanceID).Scan(&s); err != nil {
			h.t.Fatalf("read runtime state (%s): %v", q, err)
		}
		parts = append(parts, s)
	}
	return parts
}

// wfrun022Diff is the recovery oracle: the tables whose durable state differs.
func wfrun022Diff(before, after []string) []string {
	names := []string{"workflow_instance", "workflow_timer", "workflow_frontier_entry", "workflow_signal_subscription",
		"workflow_node_execution", "workflow_continuation", "work_item", "outbox", "idempotency_record"}
	var changed []string
	for i := range before {
		if i >= len(after) || before[i] != after[i] {
			changed = append(changed, names[i])
		}
	}
	return changed
}

// replica is one scheduler replica with its own workload identity and clock.
// The same *Scheduler value is reused across ticks, so it keeps the queue
// grant it holds in memory exactly as a long-running process would.
func (h *promoux015Harness) replica(name string, clock func() time.Time) *scheduler.Scheduler {
	h.t.Helper()
	dispatcher := scheduler.DispatcherFunc(func(ctx context.Context, work scheduler.Work) (scheduler.Disposition, error) {
		if _, err := h.composed.Cell().ResumeFiredTimer(app.WithResumeTenant(ctx, demoworkforce.CompanyKey), work.Row.InstanceID.String(), work.Row.NodeID, work.Row.Attempt); err != nil {
			return scheduler.DispositionRetry, err
		}
		return scheduler.DispositionCompleted, nil
	})
	s, err := scheduler.New(scheduler.Config{
		DB: h.pool,
		Claims: []lease.AcquireRequest{{
			TenantID: pgstore.TenantID(demoworkforce.CompanyKey), Resource: lease.Resource{Kind: lease.ResourceQueue, ID: scheduler.DefaultQueueKey},
			Holder: lease.Identity{WorkloadRef: "workload:wfrun022", InstanceRef: name},
		}},
		Leases: lease.Manager{}, Timers: timer.Scheduler{Attempts: runtime.Store{}},
		Misfire:    schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: 30 * 24 * time.Hour, MaxCatchUp: 1},
		Dispatcher: dispatcher, Clock: clock, QueueTTL: 30 * time.Second,
	})
	if err != nil {
		h.t.Fatalf("compose replica %s: %v", name, err)
	}
	return s
}

// terminateBackends kills every other client backend connected to the test
// database, the pool's live connections included, and reports how many.
func (h *promoux015Harness) terminateBackends() int {
	h.t.Helper()
	var killed int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity
		WHERE datname = current_database() AND pid <> pg_backend_pid() AND backend_type = 'client backend'`).Scan(&killed); err != nil {
		h.t.Fatalf("terminate database backends: %v", err)
	}
	return killed
}

// interrupt is a database interruption followed by a cell interruption: every
// connection is killed, then the composed server is stopped and recomposed, so
// nothing survives but what PostgreSQL recorded.
func (h *promoux015Harness) interrupt() {
	h.t.Helper()
	h.terminateBackends()
	h.restart()
}

// queueLease reads the held queue lease's fence token and holder.
func (h *promoux015Harness) queueLease() (int64, string) {
	h.t.Helper()
	var token int64
	var holder string
	if err := h.pool.QueryRow(context.Background(), `SELECT fence_token, holder_id FROM workflow_lease
		WHERE resource_kind = 'QUEUE' AND lease_state = 'HELD' ORDER BY fence_token DESC LIMIT 1`).Scan(&token, &holder); err != nil {
		h.t.Fatalf("read queue lease: %v", err)
	}
	return token, holder
}

// decisionRows counts recorded human approval decisions.
func (h *promoux015Harness) decisionRows() int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM intent_decision WHERE decision_kind = 'HUMAN_APPROVAL'`).Scan(&n); err != nil {
		h.t.Fatalf("count decisions: %v", err)
	}
	return n
}

// latestInstance is the workflow instance the most recent execution created.
func (h *promoux015Harness) latestInstance() string {
	h.t.Helper()
	var id string
	if err := h.pool.QueryRow(context.Background(), `SELECT instance_id::text FROM workflow_instance ORDER BY created_at DESC LIMIT 1`).Scan(&id); err != nil {
		h.t.Fatalf("latest instance: %v", err)
	}
	return id
}

// waitingWithLease drives a promotion to its effective-date wait and has
// replica A take the queue lease before the effective date.
func (h *promoux015Harness) waitingWithLease() (intentID, instance string, replicaA *scheduler.Scheduler, fenceA int64) {
	h.t.Helper()
	intentID = h.runSeparatedPromotion()
	instance = h.latestInstance()
	early := time.Now().UTC()
	replicaA = h.replica("replica-a", func() time.Time { return early })
	if tick, err := replicaA.Tick(context.Background()); err != nil || tick.Leased != 1 || tick.Fired != 0 {
		h.t.Fatalf("replica A before the effective date = %+v, %v; want the queue leased and nothing fired", tick, err)
	}
	fenceA, _ = h.queueLease()
	return intentID, instance, replicaA, fenceA
}

// TestTodo_WF_RUN_022 is the PRIMARY. RED: a restore loses a timer, signal,
// outbox or frontier row, a stale worker resumes, or an external effect
// duplicates. GREEN: the restored state resumes under a new fence with the
// same completion dimensions and evidence, and the effect happens once.
func TestTodo_WF_RUN_022(t *testing.T) {
	h := promoux015Compose(t)
	id, instance, replicaA, fenceA := h.waitingWithLease()
	stateBefore := h.wfrun022State(instance)
	effectsBefore := h.effects()

	h.interrupt()

	if changed := wfrun022Diff(stateBefore, h.wfrun022State(instance)); len(changed) != 0 {
		t.Fatalf("the interruption changed durable runtime state in %v", changed)
	}

	late := h.afterEffectiveDate()()
	replicaB := h.replica("replica-b", func() time.Time { return late })
	tickB, err := replicaB.Tick(context.Background())
	if err != nil {
		t.Fatalf("replica B after restore: %v", err)
	}
	if tickB.Leased != 1 || tickB.Fired != 1 {
		t.Fatalf("replica B after restore = %+v, want the queue taken over and the one due timer fired", tickB)
	}
	fenceB, holderB := h.queueLease()
	if fenceB <= fenceA || !strings.Contains(holderB, "replica-b") {
		t.Fatalf("queue lease after restore = fence %d holder %s, want replica B above fence %d", fenceB, holderB, fenceA)
	}
	h.acknowledgeParkedPromotion(id)
	if err := promoux015CommittedOnce(effectsBefore, h.effects()); err != nil {
		t.Fatalf("the restored wait's commit: %v", err)
	}

	// Replica A is the worker that was running when the database went away:
	// it still holds its old grant in memory and its clock never advanced.
	committed := h.effects()
	staleTick, _ := replicaA.Tick(context.Background())
	if staleTick.Leased != 0 || staleTick.Fired != 0 || staleTick.Claimed != 0 {
		t.Fatalf("the stale replica after takeover = %+v, want it refused the queue and doing nothing", staleTick)
	}
	if again, err := replicaB.Tick(context.Background()); err != nil || again.Fired != 0 {
		t.Fatalf("replica B's next tick = %+v, %v; want nothing left to fire", again, err)
	}
	if h.effects() != committed {
		t.Fatalf("a stale or repeated tick wrote effects: %+v -> %+v", committed, h.effects())
	}

	detail, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("InspectJourney after recovery: %v", err)
	}
	if detail.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED || detail.GetDetail().GetLedger() == nil {
		t.Fatalf("recovered promotion = stage %s ledger %v, want RECORDED with its ledger fact",
			detail.GetDetail().GetJourney().GetStage(), detail.GetDetail().GetLedger())
	}
}

// TestTodo_WF_RUN_022_Recovery interrupts twice -- once while waiting and once
// after the commit -- and proves neither restore loses or repeats anything.
func TestTodo_WF_RUN_022_Recovery(t *testing.T) {
	h := promoux015Compose(t)
	id, instance, _, _ := h.waitingWithLease()
	effectsBefore := h.effects()
	h.interrupt()
	late := h.afterEffectiveDate()()
	if tick, err := h.replica("replica-b", func() time.Time { return late }).Tick(context.Background()); err != nil || tick.Fired != 1 {
		t.Fatalf("resume after the first restore = %+v, %v", tick, err)
	}
	h.acknowledgeParkedPromotion(id)
	committedState := h.wfrun022State(instance)
	committed := h.effects()
	h.interrupt()
	if changed := wfrun022Diff(committedState, h.wfrun022State(instance)); len(changed) != 0 {
		t.Fatalf("restoring a committed promotion changed %v", changed)
	}
	if tick, err := h.replica("replica-c", func() time.Time { return late.Add(time.Minute) }).Tick(context.Background()); err != nil || tick.Fired != 0 || tick.Claimed != 0 {
		t.Fatalf("a third replica after the second restore = %+v, %v; want nothing to do", tick, err)
	}
	if err := promoux015CommittedOnce(effectsBefore, h.effects()); err != nil || h.effects() != committed {
		t.Fatalf("across two restores: %v (effects %+v -> %+v)", err, committed, h.effects())
	}
}

// TestTodo_WF_RUN_022_Race runs two fresh replicas against the restored state
// at the same instant and proves exactly one takes the queue and fires the
// timer: the restored wait cannot be woken twice.
func TestTodo_WF_RUN_022_Race(t *testing.T) {
	h := promoux015Compose(t)
	id, _, _, _ := h.waitingWithLease()
	effectsBefore := h.effects()
	h.interrupt()
	late := h.afterEffectiveDate()()
	replicas := []*scheduler.Scheduler{
		h.replica("replica-x", func() time.Time { return late }),
		h.replica("replica-y", func() time.Time { return late }),
	}
	results := make([]scheduler.TickResult, len(replicas))
	errs := make([]error, len(replicas))
	var wg sync.WaitGroup
	for i, r := range replicas {
		wg.Go(func() { results[i], errs[i] = r.Tick(context.Background()) })
	}
	wg.Wait()
	leased, fired := 0, 0
	for i := range replicas {
		if errs[i] != nil {
			t.Logf("replica %d tick error (acceptable contention): %v", i, errs[i])
		}
		leased += results[i].Leased
		fired += results[i].Fired
	}
	if leased != 1 || fired != 1 {
		t.Fatalf("concurrent replicas after restore leased %d and fired %d, want exactly 1 each", leased, fired)
	}
	h.acknowledgeParkedPromotion(id)
	if err := promoux015CommittedOnce(effectsBefore, h.effects()); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_WF_RUN_022_Fault kills every connection while the composed server
// is still running and proves the next operation recovers on fresh
// connections without corrupting state: the in-flight pool reconnects, the
// journey still reads back, and no durable row moved.
func TestTodo_WF_RUN_022_Fault(t *testing.T) {
	h := promoux015Compose(t)
	id, instance, _, _ := h.waitingWithLease()
	stateBefore := h.wfrun022State(instance)
	if killed := h.terminateBackends(); killed == 0 {
		t.Fatal("no backend was terminated, so the fault was not injected")
	}
	var detail *journeyv1.InspectJourneyResponse
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		detail, err = h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("the running server did not recover from killed connections: %v", err)
	}
	if detail.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE {
		t.Fatalf("stage after the connection fault = %s, want WAITING_EFFECTIVE_DATE", detail.GetDetail().GetJourney().GetStage())
	}
	if changed := wfrun022Diff(stateBefore, h.wfrun022State(instance)); len(changed) != 0 {
		t.Fatalf("the connection fault changed durable state in %v", changed)
	}
}

// TestTodo_WF_RUN_022_Integration exercises the recovery path end to end over
// gRPC: after a full interruption the journey is readable at the same stage,
// its approvals still refuse a repeated decision, and the promotion completes
// once through the production scheduler.
func TestTodo_WF_RUN_022_Integration(t *testing.T) {
	h := promoux015Compose(t)
	id, _, _, _ := h.waitingWithLease()
	h.interrupt()
	detail, err := h.client.InspectJourney(h.rpc("hiring-manager"), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil || detail.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE {
		t.Fatalf("journey after interruption = %v, %v; want WAITING_EFFECTIVE_DATE", detail.GetDetail().GetJourney().GetStage(), err)
	}
	// A repeated decision after the restore may be refused or answered as an
	// idempotent read of the current journey; either way it must record no
	// second decision and must not move the journey.
	decisionsBefore := h.decisionRows()
	repeat, err := h.client.DecideJourney(h.rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "decide again after restore"})
	if after := h.decisionRows(); after != decisionsBefore {
		t.Fatalf("a repeated decision after the restore recorded %d more decision rows", after-decisionsBefore)
	}
	if err == nil && repeat.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE {
		t.Fatalf("a repeated decision after the restore moved the journey to %s", repeat.GetDetail().GetJourney().GetStage())
	}
	late := h.afterEffectiveDate()()
	before := h.effects()
	if tick, err := h.replica("replica-b", func() time.Time { return late }).Tick(context.Background()); err != nil || tick.Fired != 1 {
		t.Fatalf("resume after interruption = %+v, %v", tick, err)
	}
	h.acknowledgeParkedPromotion(id)
	if err := promoux015CommittedOnce(before, h.effects()); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_WF_RUN_022_Mutation proves the recovery oracle is not vacuous: a
// planted change to any preserved table is reported by name, and an identical
// state reports nothing.
func TestTodo_WF_RUN_022_Mutation(t *testing.T) {
	before := []string{"inst", "timer", "frontier", "subs", "nodes", "cont", "items", "outbox", "3"}
	if changed := wfrun022Diff(before, append([]string(nil), before...)); len(changed) != 0 {
		t.Fatalf("identical state reported changes %v", changed)
	}
	names := []string{"workflow_instance", "workflow_timer", "workflow_frontier_entry", "workflow_signal_subscription",
		"workflow_node_execution", "workflow_continuation", "work_item", "outbox", "idempotency_record"}
	for i, name := range names {
		after := append([]string(nil), before...)
		after[i] += "-lost"
		if changed := wfrun022Diff(before, after); len(changed) != 1 || changed[0] != name {
			t.Errorf("a planted change to %s reported %v", name, changed)
		}
	}
	if changed := wfrun022Diff(before, before[:3]); len(changed) == 0 {
		t.Error("a truncated state was reported as unchanged")
	}
	if len(wfrun022StateQueries()) != len(names) {
		t.Fatalf("the oracle reads %d tables but names %d", len(wfrun022StateQueries()), len(names))
	}
}
