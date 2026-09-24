package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncancel "github.com/monstercameron/human-capital-management-suite/internal/transaction/cancel"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/compensate"
)

var rev002At = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func rev002LedgerEvents(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) []compensate.Event {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin compensation event read: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope compensation event read: %v", err)
	}
	rows, err := tx.Query(ctx, `SELECT payload FROM workflow_compensation_event WHERE tenant_id=$1 ORDER BY recorded_at,event_ref`, tenantID)
	if err != nil {
		t.Fatalf("query compensation events: %v", err)
	}
	defer rows.Close()
	events := make([]compensate.Event, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan compensation event: %v", err)
		}
		var event compensate.Event
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("decode compensation event: %v", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read compensation events: %v", err)
	}
	return events
}

func rev002Clock() func() time.Time { return func() time.Time { return rev002At } }

// rev002Composition builds the served cell under test: the real
// NewPromotionExecution composition against a real database.
func rev002Composition(t *testing.T, db *pgtest.DB) *PromotionExecution {
	t.Helper()
	composition, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: db.Conn, Terminal: stubTerminal{}, Clock: rev002Clock(), Plan: PLAN_EXECUTE,
	})
	if err != nil {
		t.Fatalf("NewPromotionExecution: %v", err)
	}
	return composition
}

// TestTodo_WF_REV_002 proves the PRIMARY contract: the served cell composes
// the compensate executor for COMPENSATE nodes and for cancellation-driven
// compensation, and passes it as the governed cancel compensator. One
// executor instance serves all three paths: the COMPENSATE node runs
// through it, a discharge item runs through it, and the served
// CancelGoverned resolves through it -- before the commit point with no
// writes and no compensator launch, after the commit point by releasing
// the hold through the executor.
func TestTodo_WF_REV_002(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := ext001SeedTenant(t, db)
	composition := rev002Composition(t, db)

	served := composition.Compensation
	if served == nil || served.Executor() == nil {
		t.Fatal("the served cell composes no compensate executor")
	}
	if composition.steps.servedComp != served {
		t.Fatal("the COMPENSATE node path is not wired to the shared executor instance")
	}

	// The COMPENSATE node runs through the shared executor.
	intentID, proposal, instanceID := uuid.New(), uuid.New(), uuid.New()
	materialDigest := strings.Repeat("d", 64)
	ext001SeedHold(t, db, tenantID, intentID, proposal, materialDigest)
	ext001RecordDelegation(t, db, tenantID, instanceID)
	req := ext001CompensateRequest(t, tenantID, instanceID, intentID.String(), proposal.String(), materialDigest)
	released := ext001ReleaseInTx(t, db, tenantID, composition.steps, req, true)
	if released.Status != "COMPENSATED" {
		t.Fatalf("served COMPENSATE node status = %q, want COMPENSATED", released.Status)
	}
	events := rev002LedgerEvents(t, db, tenantID)
	if len(events) != 1 || events[0].Status != "COMPENSATED" ||
		events[0].OriginalHistoryRef != "proposal-hold:"+materialDigest {
		t.Fatalf("ledger after the COMPENSATE node = %+v, want one COMPENSATED event naming the countered hold", events)
	}
	if served.CapabilityCalls() != 1 {
		t.Fatalf("capability calls = %d, want 1", served.CapabilityCalls())
	}

	// Cancellation-driven compensation runs through the same instance.
	// First the negative half: an unknown compensation ref is not
	// presented at all -- no event, no invented undo -- so the effect
	// stays remaining.
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		gated := withCompensationIdentity(withStepTx(ctx, tx), tenantID, intentID)
		item := cancellation.CompensationItem{
			ObligationID: uuid.New(), EffectID: "execute_promotion#1",
			NodeID: "execute_promotion", Attempt: 1, Compensation: "compensation.unknown.reverse@1",
		}
		res, err := served.Compensate(gated, tx, item)
		if err != nil {
			return err
		}
		if res.Compensated {
			t.Fatal("an unknown compensation ref reported compensated")
		}
		return nil
	})
	if served.CapabilityCalls() != 1 {
		t.Fatalf("capability calls = %d, want 1: an unserved inverse reached the capability", served.CapabilityCalls())
	}
	if got := len(rev002LedgerEvents(t, db, tenantID)); got != 1 {
		t.Fatalf("ledger after the unserved discharge holds %d events, want 1: an unserved inverse minted an event", got)
	}
	// Then the positive half: the served hold ref discharges through the
	// shared executor, sharing its ledger and capability counter.
	dischargeScope := idempotency.Scope{Tenant: tenantID, Capability: "promotion.flow", EffectScope: instanceID.String(), Key: "execute-promotion-original"}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		store := idempotency.PostgresStore{}
		if _, created, err := store.Reserve(ctx, tx, dischargeScope, strings.Repeat("b", 64),
			idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: time.Hour}, rev002At); err != nil || !created {
			return fmt.Errorf("reserve source effect scope: created=%v err=%w", created, err)
		}
		if _, err := store.Complete(ctx, tx, dischargeScope, idempotency.ResultIdentity{ResultRef: "execute-promotion-result", EventRef: "execute-promotion-event"}, rev002At); err != nil {
			return err
		}
		gated := withCompensationIdentity(withStepTx(ctx, tx), tenantID, intentID)
		item := cancellation.CompensationItem{
			ObligationID: uuid.New(), EffectID: "execute_promotion#1",
			NodeID: "execute_promotion", Attempt: 1, Compensation: ServedHoldReleaseCapability + "@1",
			TenantID: tenantID, WorkflowID: dischargeScope.Capability, InstanceID: instanceID,
			PlanDigest: strings.Repeat("c", 64), CapabilityExecutionID: "execute-promotion-run-1",
			OriginalScope: dischargeScope, OriginalEffectRefs: []string{"execute_promotion#1"},
		}
		res, err := served.Compensate(gated, tx, item)
		if err != nil {
			return err
		}
		if !res.Compensated || strings.TrimSpace(res.EvidenceRef) == "" {
			t.Fatalf("served discharge = %+v, want compensated with observation evidence", res)
		}
		return nil
	})
	if served.CapabilityCalls() != 2 {
		t.Fatalf("capability calls = %d, want 2: the discharge path bypassed the shared executor", served.CapabilityCalls())
	}
	events = rev002LedgerEvents(t, db, tenantID)
	var discharged bool
	for _, event := range events {
		if event.OriginalHistoryRef == "workflow-effect:promotion.flow/"+instanceID.String()+"/execute_promotion#1" && event.Status == compensate.StatusCompensated {
			discharged = true
		}
	}
	if len(events) != 2 || !discharged {
		t.Fatalf("ledger after the discharge = %+v, want the compensation event naming the countered effect", events)
	}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		closed, found, err := (idempotency.PostgresStore{}).Lookup(ctx, tx, dischargeScope)
		if err != nil {
			return err
		}
		if !found || closed.Status != idempotency.StatusCompensated {
			return fmt.Errorf("discharged source scope=%+v found=%v, want COMPENSATED", closed, found)
		}
		return nil
	})

	// A cancel before the commit point returns CANCELLED with no writes and
	// never launches the served compensator.
	prePlan := rev002TxPlan(t, tenantID, "wfr002-pre")
	preReq := transactioncancel.NewRequest(prePlan, "principal:promotion", "PROMOTION_WITHDRAWN", rev002At)
	preRes, err := composition.CancelGoverned(ctx, preReq)
	if err != nil {
		t.Fatalf("governed cancel before commit: %v", err)
	}
	if preRes.Decision.Outcome != transactioncancel.OutcomeCancelled ||
		preRes.Decision.Boundary != transactioncancel.BoundaryBeforeCommit ||
		preRes.Decision.CompensationLaunched {
		t.Fatalf("pre-commit governed cancel = %+v, want CANCELLED/BEFORE_COMMIT with no launch", preRes.Decision)
	}
	if got := len(rev002LedgerEvents(t, db, tenantID)); got != 2 {
		t.Fatalf("ledger after the pre-commit cancel holds %d events, want 2: the compensator ran before the commit point", got)
	}

	// A cancel after the commit point launches the served compensator,
	// which releases the hold through the same executor.
	postPlan := rev002TxPlan(t, tenantID, "wfr002-post")
	committer := transactioncommit.New(db.Conn, transactioncommit.Options{Clock: rev002Clock()})
	postReq := transactioncancel.NewRequest(postPlan, "principal:promotion", "PROMOTION_WITHDRAWN", rev002At)
	committed, err := (transactioncancel.Coordinator{DB: db.Conn}).Commit(ctx, postReq,
		func(ctx context.Context, tx dbport.Tx) (transactioncancel.CommitRecord, error) {
			receipt, err := committer.CommitInTx(ctx, tx, postPlan)
			if err != nil {
				return transactioncancel.CommitRecord{}, err
			}
			return transactioncancel.CommitRecord{Identity: receipt.ReceiptID.String()}, nil
		})
	if err != nil || committed.Outcome != transactioncancel.OutcomeCommitted {
		t.Fatalf("commit = %+v, %v", committed, err)
	}
	govIntent, govProposal := uuid.New(), uuid.New()
	ext001SeedHold(t, db, tenantID, govIntent, govProposal, strings.Repeat("e", 64))
	govRes, err := composition.CancelGoverned(WithCompensationIntent(ctx, govIntent), postReq)
	if err != nil {
		t.Fatalf("governed cancel after commit: %v", err)
	}
	if govRes.Decision.Outcome != transactioncancel.OutcomeCommitted ||
		govRes.Decision.Boundary != transactioncancel.BoundaryAfterCommit ||
		!govRes.Decision.CompensationLaunched {
		t.Fatalf("post-commit governed cancel = %+v, want COMMITTED/AFTER_COMMIT with a launch", govRes.Decision)
	}
	events = rev002LedgerEvents(t, db, tenantID)
	var governed bool
	var governedEventRef string
	for _, ev := range events {
		if ev.OriginalHistoryRef == "commit:"+govRes.Decision.CommitIdentity {
			governed = true
			governedEventRef = ev.EventRef
			if ev.Status != "COMPENSATED" {
				t.Fatalf("governed compensation event = %+v, want COMPENSATED", ev)
			}
		}
	}
	if !governed {
		t.Fatalf("ledger after the governed cancel = %+v, want one event naming the countered commit", events)
	}
	commitScope := idempotency.Scope{Tenant: tenantID, Capability: "transaction.commit", EffectScope: postPlan.PlanID, Key: postPlan.IdempotencyKey}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		closed, found, err := (idempotency.PostgresStore{}).Lookup(ctx, tx, commitScope)
		if err != nil {
			return err
		}
		if !found || closed.Status != idempotency.StatusCompensated || closed.CompensatedByRef != governedEventRef {
			return fmt.Errorf("production commit scope closure=%+v found=%v, want COMPENSATED by event %q", closed, found, governedEventRef)
		}
		return nil
	})
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		reservation, err := aggregates.CompensationStore{}.ReservationForProposal(ctx, tx, tenantID, govProposal, ext001EffectiveStart)
		if err != nil {
			return err
		}
		if reservation.Status != promotionbudget.ReservationReleased {
			t.Fatalf("reservation after the governed cancel = %q, want RELEASED", reservation.Status)
		}
		return nil
	})
}

// rev002TxPlan builds a committable promotion-shaped transaction plan in
// the TX-008 fixture shape.
func rev002TxPlan(t *testing.T, tenantID uuid.UUID, suffix string) plan.TransactionPlan {
	t.Helper()
	p := plan.TransactionPlan{
		PlanID: uuid.New().String(), Tenant: values.TenantId(tenantID.String()), ProposalRevisionID: "promotion:" + suffix,
		ProposalDigest: "sha256:" + strings.Repeat("a", 64), IdempotencyKey: "wfr002:" + suffix + ":" + tenantID.String(),
		ExpiresAt:     values.NewInstant(rev002At.Add(time.Hour)),
		Streams:       []plan.StreamPlan{{StreamKey: "promotion:worker", ExpectedSequence: 0}},
		Events:        []plan.PlannedEvent{{StreamKey: "promotion:worker", Sequence: 1, EventType: "PROMOTION_COMMITTED", SchemaRef: "hcmnext.promotion/v1", Digest: strings.Repeat("b", 64)}},
		OutboxEffects: []plan.OutboxEffect{{EffectID: "promotion:payroll", DestinationRef: "payroll", SchemaRef: "hcmnext.payroll/v1", PayloadDigest: "sha256:" + strings.Repeat("c", 64), IdempotencyKey: "effect:" + tenantID.String()}},
	}
	digest := sha256.Sum256(p.CanonicalBytes())
	p.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return p
}

// rev002BindHold binds the served hold-release compensation onto the named
// compiled nodes. The binding stands in for the published compensation
// registry (WF-REV-006, Gate C): the contract under test is one event per
// reversed effect through the shared executor, not the registry itself.
func rev002BindHold(p *workflow.CompiledWorkflow, compensationID string, nodes ...string) {
	want := map[string]bool{}
	for _, id := range nodes {
		want[id] = true
	}
	for i := range p.Nodes {
		if want[p.Nodes[i].ID] {
			p.Nodes[i].CompensationRef = &workflow.ResolvedReference{
				Kind: workflow.RefCompensation, ID: compensationID, Version: "1",
			}
		}
	}
}

// rev002SeedInstance records a RUNNING served-promotion instance with the
// named nodes succeeded, in plan order, at the given frontier.
func rev002SeedInstance(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, p *workflow.CompiledWorkflow, frontier []string, succeeded []string, at time.Time) uuid.UUID {
	t.Helper()
	instanceID := uuid.New()
	inst, err := runtime.NewInstance(tenantID, instanceID, "cell-local", p, workflow.ModeExecute,
		"sha256:"+strings.Repeat("f", 64), "corr-rev002-"+instanceID.String()[:8], at)
	if err != nil {
		t.Fatalf("runtime.NewInstance: %v", err)
	}
	want := map[string]bool{}
	for _, id := range succeeded {
		want[id] = true
	}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		ctx := context.Background()
		store := runtime.Store{}
		stored, err := store.CreateInstance(ctx, tx, inst)
		if err != nil {
			return err
		}
		running, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
			TenantID: tenantID, InstanceID: instanceID, ExpectedVersion: stored.InstanceVersion,
			Status: runtime.InstanceRunning, CurrentNodeIDs: frontier,
		})
		if err != nil {
			return err
		}
		version := running.InstanceVersion
		for _, node := range p.Nodes {
			if !want[node.ID] {
				continue
			}
			cn, ok := p.Node(node.ID)
			if !ok {
				return fmt.Errorf("plan carries no node %q", node.ID)
			}
			if cn.CompensationRef != nil {
				scope := idempotency.Scope{Tenant: tenantID, Capability: p.WorkflowID,
					EffectScope: workflow.NodeEffectScope(node.ID), Key: workflow.StepActivationKey(instanceID, node.ID, 1)}
				store := idempotency.PostgresStore{}
				if _, created, err := store.Reserve(ctx, tx, scope, strings.Repeat("a", 64),
					idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: time.Hour}, at); err != nil || !created {
					return fmt.Errorf("reserve source effect %s: created=%v err=%w", node.ID, created, err)
				}
				if _, err := store.Complete(ctx, tx, scope, idempotency.ResultIdentity{
					ResultRef: "effect:" + node.ID, EventRef: "event:" + node.ID,
				}, at); err != nil {
					return err
				}
			}
			exec := runtime.NewNodeExecution(tenantID, instanceID, node.ID, 1, cn.Type, runtime.NodeReady)
			exec.RecordedAt = at
			var v int64
			if _, v, err = store.RecordNodeExecution(ctx, tx, exec, version); err != nil {
				return err
			}
			version = v
			for _, status := range []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded} {
				if _, v, err = store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{
					TenantID: tenantID, InstanceID: instanceID, NodeID: node.ID, Attempt: 1,
					ExpectedInstanceVersion: version, Status: status,
				}); err != nil {
					return err
				}
				version = v
			}
		}
		return nil
	})
	return instanceID
}

// rev002Decide cancels a seeded instance through the real judge.
func rev002Decide(t *testing.T, db *pgtest.DB, tenantID, instanceID uuid.UUID, p *workflow.CompiledWorkflow) cancellation.Outcome {
	t.Helper()
	var out cancellation.Outcome
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		out, err = cancellation.Decide(context.Background(), tx, cancellation.Request{
			TenantID: tenantID, InstanceID: instanceID, Plan: p,
			Reason: "operator-cancel", RequestedBy: "principal:rev002", RecordedAt: rev002At,
		})
		return err
	})
	return out
}

// rev002Discharge drains an instance's recorded obligation through the
// served compensation.
func rev002Discharge(t *testing.T, served *ServedCompensation, db *pgtest.DB, tenantID, holdIntent, instanceID uuid.UUID) cancellation.Outcome {
	t.Helper()
	var out cancellation.Outcome
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		out, err = served.Discharge(context.Background(), tx, tenantID, holdIntent, cancellation.DischargeRequest{
			TenantID: tenantID, InstanceID: instanceID,
			Reason: "operator-cancel", RequestedBy: "principal:rev002", RecordedAt: rev002At,
		})
		return err
	})
	return out
}

// rev002InstanceStatus reads an instance's current status.
func rev002InstanceStatus(t *testing.T, db *pgtest.DB, tenantID, instanceID uuid.UUID) runtime.InstanceStatus {
	t.Helper()
	var status runtime.InstanceStatus
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		inst, err := (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, instanceID)
		if err != nil {
			return err
		}
		status = inst.RuntimeStatus
		return nil
	})
	return status
}

// rev002CancelAt cancels a served promotion whose frontier sits at the
// named safe point, discharges it through the served compensation, and
// asserts the ledger carries one compensation event per reversed effect,
// each naming the event it counters.
func rev002CancelAt(t *testing.T, served *ServedCompensation, db *pgtest.DB, tenantID, holdIntent uuid.UUID, plan *workflow.CompiledWorkflow, frontier string, succeeded []string, wantDecision workflow.CancellationDecision, wantHistory []string) {
	t.Helper()
	instanceID := rev002SeedInstance(t, db, tenantID, plan, []string{frontier}, succeeded, rev002At)
	decided := rev002Decide(t, db, tenantID, instanceID, plan)
	if decided.Decision != wantDecision {
		t.Fatalf("cancel at %s: decision = %s, want %s (refs %+v)", frontier, decided.Decision, wantDecision, decided.CompensationRefs)
	}
	before := len(rev002LedgerEvents(t, db, tenantID))
	if wantDecision != workflow.CompensationRequired {
		if got := len(rev002LedgerEvents(t, db, tenantID)) - before; got != 0 {
			t.Fatalf("cancel at %s recorded %d compensation events, want none", frontier, got)
		}
		if status := rev002InstanceStatus(t, db, tenantID, instanceID); status != runtime.InstanceCancelled {
			t.Fatalf("cancel at %s: instance = %s, want CANCELLED", frontier, status)
		}
		return
	}
	drained := rev002Discharge(t, served, db, tenantID, holdIntent, instanceID)
	if drained.Decision != workflow.Cancelled {
		t.Fatalf("discharge at %s: decision = %s, want CANCELLED", frontier, drained.Decision)
	}
	fresh := rev002LedgerEvents(t, db, tenantID)[before:]
	if len(fresh) != len(wantHistory) {
		t.Fatalf("cancel at %s recorded %d compensation events, want %d: %+v", frontier, len(fresh), len(wantHistory), fresh)
	}
	seen := map[string]string{}
	for _, ev := range fresh {
		seen[ev.OriginalHistoryRef] = string(ev.Status)
	}
	for _, history := range wantHistory {
		historyRef := history
		if strings.HasPrefix(history, "effect:") {
			historyRef = "workflow-effect:" + plan.WorkflowID + "/" + instanceID.String() + "/" + strings.TrimPrefix(history, "effect:")
		}
		status, ok := seen[historyRef]
		if !ok {
			t.Fatalf("cancel at %s: no compensation event names %q: %+v", frontier, historyRef, fresh)
		}
		if status != "COMPENSATED" {
			t.Fatalf("cancel at %s: event for %q has status %s, want COMPENSATED", frontier, historyRef, status)
		}
	}
	if status := rev002InstanceStatus(t, db, tenantID, instanceID); status != runtime.InstanceCancelled {
		t.Fatalf("cancel at %s: instance = %s, want CANCELLED", frontier, status)
	}
}

// TestTodo_WF_REV_002_Integration cancels a served promotion at each safe
// point and asserts the ledger carries one compensation event per reversed
// effect, each naming the event it counters. The plan is the real served
// promotion plan; the judge, the discharge driver and the compensate
// executor are all real; only the compensation binding stands in for the
// published registry (WF-REV-006, Gate C) and the budget hold stands in
// for the domain side of the reversal.
func TestTodo_WF_REV_002_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := ext001SeedTenant(t, db)
	composition := rev002Composition(t, db)
	served := composition.Compensation

	compile := func(t *testing.T) *workflow.CompiledWorkflow {
		t.Helper()
		plan, err := promotionexec.Compile()
		if err != nil {
			t.Fatalf("promotionexec.Compile: %v", err)
		}
		for _, id := range []string{promotionexec.NodeWaitEffectiveDate, promotionexec.NodeExecutePromotion, promotionexec.NodeCompensateHold} {
			node, ok := plan.Node(id)
			if !ok || !node.SafePoint {
				t.Fatalf("served plan node %q safe point = %v, want a declared safe point", id, ok && node.SafePoint)
			}
		}
		return plan
	}

	holdIntent, holdProposal := uuid.New(), uuid.New()
	ext001SeedHold(t, db, tenantID, holdIntent, holdProposal, strings.Repeat("e", 64))
	snapshot := promotionexec.NodeSnapshotWorker

	// Before any write: the cancel lands CANCELLED with no compensation.
	rev002CancelAt(t, served, db, tenantID, holdIntent, compile(t),
		promotionexec.NodeWaitEffectiveDate, []string{snapshot}, workflow.Cancelled, nil)

	// After the core commit: one reversed effect, one compensation event
	// naming it.
	postWrite := compile(t)
	rev002BindHold(postWrite, ServedHoldReleaseCapability, promotionexec.NodeExecutePromotion)
	rev002CancelAt(t, served, db, tenantID, holdIntent, postWrite,
		promotionexec.NodeExecutePromotion,
		[]string{snapshot, promotionexec.NodeExecutePromotion},
		workflow.CompensationRequired, []string{"effect:execute_promotion#1"})

	// After the hold release: two reversed effects, one event each, each
	// naming its own countered effect.
	postHold := compile(t)
	rev002BindHold(postHold, ServedHoldReleaseCapability,
		promotionexec.NodeExecutePromotion, promotionexec.NodeCompensateHold)
	rev002CancelAt(t, served, db, tenantID, holdIntent, postHold,
		promotionexec.NodeCompensateHold,
		[]string{snapshot, promotionexec.NodeExecutePromotion, promotionexec.NodeCompensateHold},
		workflow.CompensationRequired,
		[]string{"effect:execute_promotion#1", "effect:compensate_budget_hold#1"})

	// An effect whose compensation names no served inverse fails closed:
	// the discharge presents nothing, mints no event and invents no undo,
	// so the effect stays remaining and the instance parks
	// REPAIR_REQUIRED beside the compensations.
	unknown := compile(t)
	rev002BindHold(unknown, "compensation.unknown.reverse", promotionexec.NodeExecutePromotion)
	instanceID := rev002SeedInstance(t, db, tenantID, unknown,
		[]string{promotionexec.NodeExecutePromotion},
		[]string{snapshot, promotionexec.NodeExecutePromotion}, rev002At)
	decided := rev002Decide(t, db, tenantID, instanceID, unknown)
	if decided.Decision != workflow.CompensationRequired {
		t.Fatalf("unknown-compensation cancel: decision = %s, want COMPENSATION_REQUIRED", decided.Decision)
	}
	before := len(rev002LedgerEvents(t, db, tenantID))
	drained := rev002Discharge(t, served, db, tenantID, holdIntent, instanceID)
	if drained.Decision != workflow.RepairRequired {
		t.Fatalf("unknown-compensation discharge: decision = %s, want REPAIR_REQUIRED", drained.Decision)
	}
	if got := len(rev002LedgerEvents(t, db, tenantID)) - before; got != 0 {
		t.Fatalf("unknown-compensation discharge minted %d events, want none", got)
	}
	if status := rev002InstanceStatus(t, db, tenantID, instanceID); status != runtime.InstanceRepairRequired {
		t.Fatalf("unknown-compensation instance = %s, want REPAIR_REQUIRED", status)
	}

	// The hold the compensations released is released exactly once, no
	// matter how many effects pointed at it.
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		reservation, err := aggregates.CompensationStore{}.ReservationForProposal(ctx, tx, tenantID, holdProposal, ext001EffectiveStart)
		if err != nil {
			return err
		}
		if reservation.Status != promotionbudget.ReservationReleased {
			t.Fatalf("reservation after the safe-point cancels = %q, want RELEASED", reservation.Status)
		}
		return nil
	})
}

// rev002FakeCapability is the GOLDEN test's deterministic inverse: one
// fixed receipt for the expected ref, a refusal for anything else.
type rev002FakeCapability struct{ ref string }

func (f *rev002FakeCapability) Manifest(context.Context) (compensate.CapabilityManifest, error) {
	return compensate.CapabilityManifest{CapabilityRef: f.ref, Digest: "sha256:rev002-manifest", Idempotent: true}, nil
}

func (f *rev002FakeCapability) Compensate(_ context.Context, req compensate.CapabilityRequest) (compensate.CapabilityReceipt, error) {
	if req.Request.CompensationCapabilityRef != f.ref {
		return compensate.CapabilityReceipt{}, fmt.Errorf("rev002 fake capability: unexpected ref %q", req.Request.CompensationCapabilityRef)
	}
	return compensate.CapabilityReceipt{Accepted: true, Applied: true, EvidenceRef: "cap:rev002-fixed"}, nil
}

// rev002FakeObserver is the GOLDEN test's deterministic witness: it always
// confirms the fixed correction evidence at the fixed clock.
type rev002FakeObserver struct{ now time.Time }

func (o *rev002FakeObserver) ObserveCompensation(_ context.Context, req compensate.ObservationRequest) (compensate.Observation, error) {
	return compensate.Observation{
		Match: true, ObservedAt: o.now,
		TenantID: req.Request.TenantID, TargetEffectRef: req.Request.TargetEffectRef,
		CorrectionEvidenceRef: req.CorrectionEvidenceRef, EvidenceRef: "obs:rev002-fixed",
	}, nil
}

type rev002CloseableOperations struct{ *servedOperations }

func (rev002CloseableOperations) CloseCompensated(context.Context, idempotency.Scope, string) error {
	return nil
}

// TestTodo_WF_REV_002_Golden pins the served compensation's wire contract:
// the discharge request the served cell builds for a recorded obligation
// item, the compensation event the shared executor mints for it, and the
// executor-status to COMPENSATE-route map. Any drift in the mapping fails
// here first.
func TestTodo_WF_REV_002_Golden(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	obligation := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	instanceID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	workflowID := "promotion.flow"
	item := cancellation.CompensationItem{
		ObligationID: obligation, EffectID: "execute_promotion#1",
		NodeID: "execute_promotion", Attempt: 1, Compensation: "test.compensation.reverse@3",
		TenantID: tenant, WorkflowID: workflowID, InstanceID: instanceID, PlanDigest: strings.Repeat("a", 64),
		CapabilityExecutionID: "cap-exec:golden", OriginalScope: idempotency.Scope{
			Tenant: tenant, Capability: workflowID, EffectScope: "workflow-node:execute_promotion", Key: "execute-promotion-key",
		}, OriginalEffectRefs: []string{"execute_promotion#1"},
	}
	fingerprint := servedAuthorityFingerprint("sha256:rev002-authority")
	req := dischargeCompensateRequest(tenant, item, "sha256:rev002-manifest", fingerprint)
	sum := sha256.Sum256([]byte(strings.Join([]string{
		req.TenantID, req.ActorID, req.TargetExecutionRef, req.TargetEffectRef,
		req.CompensationCapabilityRef, req.VerificationObservationRef, req.Reason,
		req.ApprovalPolicy, req.ApprovalRef, req.AuthorityPolicyFingerprint,
		req.CapabilityManifestDigest, req.PayloadDigest, req.IdempotencyKey,
		req.OriginalHistoryRef, string(req.Strategy), req.ObservationMaxAge.String(),
	}, "\x00")))
	if got := hex.EncodeToString(sum[:]); got != rev002GoldenRequestDigest {
		t.Fatalf("served discharge request digest = %s, want %s", got, rev002GoldenRequestDigest)
	}

	served, err := ComposeServedCompensation(ServedCompensationOptions{
		Capability: &rev002FakeCapability{ref: "test.compensation.reverse"},
		Observer:   &rev002FakeObserver{now: rev002At},
		Clock:      rev002Clock(), AuthorityDigest: "sha256:rev002-authority",
		Operations: &rev002CloseableOperations{servedOperations: &servedOperations{}},
	})
	if err != nil {
		t.Fatalf("ComposeServedCompensation: %v", err)
	}
	result, err := served.Executor().Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("executor.Execute: %v", err)
	}
	if result.Status != compensate.StatusCompensated {
		t.Fatalf("executor status = %s, want COMPENSATED", result.Status)
	}
	if result.Event.Digest != rev002GoldenEventDigest {
		t.Fatalf("compensation event digest = %s, want %s", result.Event.Digest, rev002GoldenEventDigest)
	}
	if result.Event.OriginalHistoryRef != "workflow-effect:"+workflowID+"/"+instanceID.String()+"/execute_promotion#1" {
		t.Fatalf("event history ref = %q, want the countered effect", result.Event.OriginalHistoryRef)
	}

	for status, want := range map[compensate.Status]string{
		compensate.StatusCompensated: "COMPENSATED", compensate.StatusPartial: "PARTIAL",
		compensate.StatusFailed: "FAILED", compensate.StatusRepairRequired: "REPAIR_REQUIRED",
	} {
		if got := servedStatusRoute(status); got != want {
			t.Fatalf("route(%s) = %q, want %q", status, got, want)
		}
	}
}

const (
	rev002GoldenRequestDigest = "017ed32f7cf2a454a13bcc1b607fbf067641ea08548cb98d0e11c9f93853612e"
	rev002GoldenEventDigest   = "db3bdd20afce25c7342dbc58624e97127b1a36c56aac803e403412d0660f260e"
)
