package workflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestTodo_WF_RUN_019_DurableInspection is WF-RUN-019's GREEN clause over a
// really started instance: the served driver starts a promotion against the
// durable version registry, runs it to a durable WAIT timer under the
// instance lease, and inspect.Load -- given nothing but the tenant, the
// instance id and an authorization decision -- traverses the pinned compiled
// version and its record digest, the pinned execution context and its digest,
// the node executions and attempts, the advancement receipts, the timer, the
// lease history and the trace, from PostgreSQL. Protected content stays out,
// and another tenant cannot disclose the instance.
func TestTodo_WF_RUN_019_DurableInspection(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	fireAt := at.Add(48 * time.Hour)

	db := pgtest.New(t)
	beginner := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun019", at)
	otherTenant := insertTenant(t, db, "wfrun019-other", at)

	_, plan, activated := publishActiveWaitPlan(t, fireAt, at)
	versions := workflowversionstore.Store{DB: beginner}
	if err := versions.Put(activated); err != nil {
		t.Fatalf("publish the activated version into the durable registry: %v", err)
	}

	proposal := newDemoProposal(t, values.TenantId("wfrun019"), "intent:wfrun019", at)
	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: activated.CompiledPlanDigest}, Plan: plan,
	}}}
	scheduler := timer.Scheduler{}
	holder := lease.Identity{WorkloadRef: "workload:hcmnext-execution", InstanceRef: "replica:wfrun019"}
	driver, err := execute.New(execute.Options{
		DB: beginner, Steps: endOnlySteps{},
		Terminal: &effects.LedgerTerminalWriter{Appender: newLedgerAppender(t), ProjectionName: "workflow_wfrun019_outcome_test", SourceRef: "hcmnext:test:workflow"},
		Timers:   &waitTimerFactory{scheduler: scheduler, dataset: waitDataset}, TimerReader: waitTimerReader{scheduler: scheduler},
		Guard:     idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:     func() time.Time { return at },
		Leases:    platformexecution.NewInstanceLeaser(holder, time.Hour), FenceVerifier: lease.Fenced{Manager: lease.Manager{}},
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}
	parked, err := driver.Execute(ctx, execute.ExecuteRequest{Start: runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:wfrun019",
		Resolver: resolver, Versions: versions, Proposal: runtime.ProposalBinding{Revision: proposal},
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(proposal),
		ExpectedIntentID: proposal.IntentID, ExpectedTenant: proposal.Tenant,
		BusinessSubjectRefs: []string{"employment:promotion-execute-demo-1"},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "corr:wfrun019", CreatedAt: at,
	}})
	if err != nil || parked.Status != execute.StatusParked || len(parked.Timers) != 1 {
		t.Fatalf("Execute = %+v, %v; want an instance parked on one timer", parked, err)
	}
	instanceID := parked.Start.InstanceID

	load := func(tenant uuid.UUID, auth inspect.Authorization) (inspect.DurableView, error) {
		t.Helper()
		tx, err := beginner.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
			t.Fatalf("scope tenant: %v", err)
		}
		return inspect.Load(ctx, tx, inspect.LoadRequest{
			TenantID: tenant, InstanceID: instanceID, Authorization: auth,
			WorkItems: inspect.WorkItemAuthorization{Disclosed: true}, Versions: versions,
		})
	}

	operator := inspect.AllowAll("policy.test/v1", "OPERATIONS", "principal:test-operator")
	dv, err := load(tenantID, operator)
	if err != nil {
		t.Fatalf("inspect.Load: %v", err)
	}

	var stored runtime.Instance
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var lerr error
		stored, lerr = (runtime.Store{}).LoadInstance(ctx, tx, tenantID, instanceID)
		return lerr
	})

	// definition: the pinned version, resolved from the durable registry with
	// its record digest and lifecycle status.
	if dv.Version.State != inspect.RecordLoaded || dv.Version.CompiledPlanDigest != stored.CompiledPlanHash ||
		dv.Version.RecordDigest != activated.Digest() || dv.Version.Status != string(version.StatusActive) ||
		!dv.Version.PinMatches || len(dv.Version.Approvals) == 0 {
		t.Errorf("version record = %+v, want the ACTIVE registry record %s with digest %s",
			dv.Version, stored.CompiledPlanHash, activated.Digest())
	}
	// instance: the pinned execution context, verified against its digest.
	if dv.ExecutionContext.State != inspect.RecordLoaded || !dv.ExecutionContext.Verified ||
		!dv.ExecutionContext.PlanDigestMatches || dv.ExecutionContext.RuntimeVersion != runtime.RuntimeVersion {
		t.Errorf("execution context = %+v", dv.ExecutionContext)
	}
	if got, _ := dv.ExecutionContext.ContextDigest.Get(); got == "" || got != stored.EffectiveContextRef {
		t.Errorf("context digest = %q, want the instance pin %q", got, stored.EffectiveContextRef)
	}
	// node: every attempt, and the frontier node the timer parks.
	if len(dv.View.Nodes) == 0 || len(dv.Attempts) == 0 {
		t.Fatalf("no node executions or attempts traversed: %+v", dv.Attempts)
	}
	if len(dv.Timers) != 1 || dv.Timers[0].TimerID != parked.Timers[0].TimerID.String() || dv.Timers[0].State != "PENDING" {
		t.Errorf("timers = %+v, want the one pending durable timer %s", dv.Timers, parked.Timers[0].TimerID)
	}
	// advancement receipts, each re-verified against its digest.
	if len(dv.Receipts) != len(parked.Advances) || len(dv.Receipts) == 0 {
		t.Fatalf("receipts = %d, want the %d committed advancements", len(dv.Receipts), len(parked.Advances))
	}
	for i, r := range dv.Receipts {
		if !r.DigestVerified || r.ReceiptDigest != parked.Advances[i].Digest() {
			t.Errorf("receipt %d = %+v, want verified digest %s", i, r, parked.Advances[i].Digest())
		}
	}
	// lease: the served holder acquired and released the instance.
	if dv.Lease.State != inspect.RecordLoaded || dv.Lease.Held || len(dv.Lease.Transitions) < 2 {
		t.Errorf("lease = %+v, want an acquired-then-released instance lease", dv.Lease)
	}
	for _, family := range []string{inspect.FamilyBusinessTransaction, inspect.FamilyConnectorOperation} {
		if rec, ok := dv.Record(family); !ok || rec.State != inspect.RecordUnavailable || rec.Reason == "" {
			t.Errorf("family %s = %+v, want UNAVAILABLE with a reason", family, rec)
		}
	}
	if !dv.Completeness.Complete {
		t.Errorf("a fully authorized view of a clean instance is incomplete: redactions %v, gaps %v",
			dv.Completeness.Redactions, dv.Completeness.Gaps)
	}

	// Protected content: the context's principal is never rendered, and the
	// protected refs are redacted when their fields are denied.
	rendered, err := dv.JSON()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(string(rendered), humanwork.PrincipalRequester) {
		t.Errorf("the durable view renders the execution context principal %q", humanwork.PrincipalRequester)
	}
	denied := operator
	denied.Fields = map[string]inspect.Ruling{}
	for _, f := range []string{inspect.FieldInstanceInput, inspect.FieldInstanceContext, inspect.FieldNodeOutput} {
		denied.Fields[f] = inspect.Ruling{Effect: inspect.EffectDeny, Reason: "CLASSIFICATION_CONFIDENTIAL_HR"}
	}
	redacted, err := load(tenantID, denied)
	if err != nil {
		t.Fatalf("inspect.Load with protected fields denied: %v", err)
	}
	out, err := redacted.JSON()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	secrets := []string{stored.InputRef, stored.EffectiveContextRef}
	for _, a := range parked.Advances {
		if a.OutputDigest != "" {
			secrets = append(secrets, a.OutputDigest)
		}
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(string(out), secret) {
			t.Errorf("the redacted durable view leaks %q", secret)
		}
	}

	// Tenant isolation, on the least-privilege role.
	_, foreign := load(otherTenant, operator)
	if !errors.Is(foreign, inspect.ErrNotDisclosable) {
		t.Fatalf("another tenant's inspect.Load = %v, want ErrNotDisclosable", foreign)
	}
}
