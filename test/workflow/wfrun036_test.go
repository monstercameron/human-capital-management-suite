package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestTodo_WF_RUN_036 proves the served driver composition fences every
// caller-driven run against PostgreSQL: Execute takes the WORKFLOW_INSTANCE
// lease through the production leaser, advances under its fence and releases
// it; while another replica holds the instance a run is refused before any
// advancement; and a caller presenting a fence that is no longer current is
// refused in the advance transaction, leaving the instance untouched.
func TestTodo_WF_RUN_036(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	fireAt := at.Add(48 * time.Hour)

	db := pgtest.New(t)
	beginner := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun036", at)
	versions, plan, activated := publishActiveWaitPlan(t, fireAt, at)
	proposal := newDemoProposal(t, values.TenantId("wfrun036"), "intent:wfrun036", at)
	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: activated.CompiledPlanDigest}, Plan: plan,
	}}}
	terminal := &effects.LedgerTerminalWriter{Appender: newLedgerAppender(t), ProjectionName: "workflow_wfrun036_outcome_test", SourceRef: "hcmnext:test:workflow"}
	scheduler := timer.Scheduler{}
	var manager lease.Manager
	holder := lease.Identity{WorkloadRef: "workload:hcmnext-execution", InstanceRef: "replica:wfrun036-a"}

	driver := func(clock time.Time) *execute.Driver {
		d, err := execute.New(execute.Options{
			DB: beginner, Steps: endOnlySteps{}, Terminal: terminal,
			Timers: &waitTimerFactory{scheduler: scheduler, dataset: waitDataset}, TimerReader: waitTimerReader{scheduler: scheduler},
			Guard:     idempotency.PostgresStore{},
			Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
			Clock:     func() time.Time { return clock },
			Leases:    platformexecution.NewInstanceLeaser(holder, time.Hour), FenceVerifier: lease.Fenced{Manager: manager},
		})
		if err != nil {
			t.Fatalf("execute.New: %v", err)
		}
		return d
	}
	start := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:wfrun036",
		Resolver: resolver, Versions: versions, Proposal: runtime.ProposalBinding{Revision: proposal},
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(proposal),
		ExpectedIntentID: proposal.IntentID, ExpectedTenant: proposal.Tenant,
		BusinessSubjectRefs: []string{"employment:promotion-execute-demo-1"},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "corr:wfrun036", CreatedAt: at,
	}

	parked, err := driver(at).Execute(ctx, execute.ExecuteRequest{Start: start})
	if err != nil || parked.Status != execute.StatusParked || len(parked.Timers) != 1 {
		t.Fatalf("leased Execute = %+v, %v", parked, err)
	}
	instance := lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: parked.Start.InstanceID.String()}
	var history []lease.Evidence
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var herr error
		history, herr = manager.History(ctx, tx, tenantID, instance)
		return herr
	})
	if !hasTransition(history, lease.TransitionAcquired, holder) || !hasTransition(history, lease.TransitionReleased, holder) {
		t.Fatalf("instance lease history = %+v, want the served holder to acquire and release", history)
	}

	// Another replica takes the instance: a run that must lease it is refused
	// before it advances anything.
	holderB := lease.Identity{WorkloadRef: "workload:hcmnext-execution", InstanceRef: "replica:wfrun036-b"}
	var grantB lease.Grant
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var aerr error
		grantB, aerr = manager.Acquire(ctx, tx, lease.AcquireRequest{TenantID: tenantID, Resource: instance, Holder: holderB, Now: at.Add(time.Minute), TTL: 720 * time.Hour})
		return aerr
	})
	resume := execute.ResumeTimerRequest{
		Start: start, InstanceID: parked.Start.InstanceID, ExpectedInstanceVersion: parked.InstanceVersion,
		TimerID: parked.Timers[0].TimerID, RecordedAt: fireAt.Add(time.Minute),
	}
	if _, err := driver(at.Add(2*time.Minute)).ResumeTimer(ctx, resume); !errors.Is(err, execute.ErrFenceRefused) || !errors.Is(err, lease.ErrHeld) {
		t.Fatalf("resume while another replica holds the instance = %v, want ErrFenceRefused carrying LEASE_HELD", err)
	}

	// A caller presenting the first holder's released fence is refused in
	// the advance transaction.
	stale := grantB.Fence.RuntimeFence(at)
	stale.Token--
	if _, err := driver(at.Add(3*time.Minute)).ResumeTimer(execute.WithFence(ctx, stale), resume); !errors.Is(err, execute.ErrFenceRefused) {
		t.Fatalf("resume under a stale caller fence = %v, want ErrFenceRefused", err)
	}
	var version int64
	if err := db.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`, tenantID, parked.Start.InstanceID).Scan(&version); err != nil || version != parked.InstanceVersion {
		t.Fatalf("refused runs moved the instance to version %d (%v), want %d", version, err, parked.InstanceVersion)
	}
}

func hasTransition(history []lease.Evidence, kind lease.TransitionKind, holder lease.Identity) bool {
	for _, e := range history {
		if e.Kind == kind && e.HolderID == holder.HolderID() {
			return true
		}
	}
	return false
}
