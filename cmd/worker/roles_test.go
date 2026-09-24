package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

type activityEvidenceRecorder struct {
	mu      sync.Mutex
	entries []ActivityAttemptEvidence
}

func (r *activityEvidenceRecorder) RecordActivityAttempt(_ context.Context, e ActivityAttemptEvidence) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	return "evidence-" + e.Outcome, nil
}

type activityExecutorFixture struct {
	calls int
	err   error
}

func (f *activityExecutorFixture) ExecuteCapability(_ context.Context, _ CapabilityActivityExecution) (any, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return "activity-result", nil
}

type ambiguityRouterFixture struct{ calls int }

func (f *ambiguityRouterFixture) RouteActivityAmbiguity(context.Context, ActivityAmbiguity) error {
	f.calls++
	return nil
}

type leaseVerifierFixture struct {
	calls int
	err   error
}

func (f *leaseVerifierFixture) VerifyActivityLease(context.Context, uuid.UUID, string, ActivityLease, time.Time) error {
	f.calls++
	return f.err
}

func activityFixture(t *testing.T) (CapabilityActivity, CapabilityActivityRequest, *activityExecutorFixture, *activityEvidenceRecorder) {
	t.Helper()
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	key := capability.Key{ID: "hcmnext.people.explain_worker_state", Version: 1}
	rec, ok := registry.Lookup(key)
	if !ok {
		t.Fatal("bootstrap capability is not registered")
	}
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	executor := &activityExecutorFixture{}
	evidence := &activityEvidenceRecorder{}
	activity := CapabilityActivity{
		Resolver: application.NewRegistryCapabilityResolver(registry), Executor: executor, Evidence: evidence,
		Now: func() time.Time { return now },
	}
	req := CapabilityActivityRequest{
		TenantID: uuid.New(), ActivityID: "activity-1", Capability: key, DescriptorDigest: rec.Digest,
		IdempotencyKey: "intent-1/activity-1", Attempt: 1,
		Lease: ActivityLease{ID: "lease-1", Fence: 1, ExpiresAt: now.Add(time.Minute)}, Payload: "payload",
	}
	return activity, req, executor, evidence
}

// TestTodo_SVC_006 proves that the capability role is a thin, guarded
// execution boundary: it resolves one immutable descriptor version, checks
// its digest and fence, records an attempt, and routes ambiguity without a
// blind retry.
func TestTodo_SVC_006(t *testing.T) {
	activity, req, executor, evidence := activityFixture(t)
	lease := &leaseVerifierFixture{}
	activity.Fence = lease
	result, evidenceID, err := activity.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "activity-result" || evidenceID != "evidence-SUCCEEDED" {
		t.Fatalf("result/evidence = %v/%q", result, evidenceID)
	}
	if executor.calls != 1 || lease.calls != 1 || len(evidence.entries) != 1 {
		t.Fatalf("calls executor=%d lease=%d evidence=%d, want 1/1/1", executor.calls, lease.calls, len(evidence.entries))
	}
	if evidence.entries[0].DescriptorDigest != req.DescriptorDigest || evidence.entries[0].Fence != req.Lease.Fence {
		t.Fatalf("attempt evidence lost descriptor/fence: %+v", evidence.entries[0])
	}
}

// TestTodo_SVC_006_Golden pins stable role parsing, authorization explain
// text, and immutable repair-plan digest generation.
func TestTodo_SVC_006_Golden(t *testing.T) {
	roles, err := ParseWorkerRoles("repair, capability-activity")
	if err != nil {
		t.Fatalf("ParseWorkerRoles: %v", err)
	}
	if got := roles[0]; got != WorkerRoleCapabilityActivity {
		t.Fatalf("sorted roles[0] = %q", got)
	}
	workloads := workerRoleWorkloads(discardLogger(), roles)
	if len(workloads) != 2 || workloads[0].Name != "role-capability-activity" || workloads[1].Name != "role-repair" {
		t.Fatalf("selected workloads = %+v", workloads)
	}
	decision, err := AuthorizeWorkerRole(WorkerRoleRepair, WorkerActionExecuteRepair)
	if err != nil || decision.Explain() != "worker role=repair action=execute-repair allowed=true rule=worker.repair.execute reason=" {
		t.Fatalf("decision = %+v (%v), explain=%q", decision, err, decision.Explain())
	}
	when := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	observation := RepairObservation{TenantID: uuid.New(), ResourceKey: "worker:1", Digest: "observed", ObservedAt: when, FreshUntil: when.Add(time.Hour)}
	role := ReconciliationRole{Now: func() time.Time { return when }}
	first, err := role.CreateRepairPlan(context.Background(), observation, "expected", "requester", "idem-1")
	if err != nil {
		t.Fatalf("CreateRepairPlan: %v", err)
	}
	second, err := role.CreateRepairPlan(context.Background(), observation, "expected", "requester", "idem-1")
	if err != nil || first.Digest != second.Digest || first.PlanID != second.PlanID {
		t.Fatalf("plan is not deterministic: first=%+v second=%+v err=%v", first, second, err)
	}
}

// TestTodo_SVC_006_Race exercises the immutable resolution and role matrix
// concurrently; no call may observe a partially selected descriptor.
func TestTodo_SVC_006_Race(t *testing.T) {
	type invocation struct {
		activity CapabilityActivity
		req      CapabilityActivityRequest
	}
	const workers = 16
	invocations := make([]invocation, workers)
	for i := range invocations {
		activity, req, _, _ := activityFixture(t)
		invocations[i] = invocation{activity: activity, req: req}
	}
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for _, call := range invocations {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, evidenceID, err := call.activity.Execute(context.Background(), call.req)
			if err != nil {
				errs <- err
				return
			}
			if result != "activity-result" || evidenceID != "evidence-SUCCEEDED" {
				errs <- fmt.Errorf("concurrent activity result/evidence = %v/%q", result, evidenceID)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestTodo_SVC_006_Integration proves the ambiguity path reaches the
// observation/repair port and does not call the executor a second time.
func TestTodo_SVC_006_Integration(t *testing.T) {
	activity, req, executor, evidence := activityFixture(t)
	executor.err = ErrActivityAmbiguous
	router := &ambiguityRouterFixture{}
	activity.Ambiguity = router
	if _, _, err := activity.Execute(context.Background(), req); !errors.Is(err, ErrActivityAmbiguous) {
		t.Fatalf("Execute error = %v, want ErrActivityAmbiguous", err)
	}
	if executor.calls != 1 || router.calls != 1 || len(evidence.entries) != 1 || evidence.entries[0].Outcome != "AMBIGUOUS" {
		t.Fatalf("ambiguous path calls=%d routes=%d evidence=%+v", executor.calls, router.calls, evidence.entries)
	}
}

// TestTodo_SVC_006_Mutation proves the two guards that mutation testing most
// often removes: exact descriptor digest and non-expired lease fencing.
func TestTodo_SVC_006_Mutation(t *testing.T) {
	activity, req, executor, evidence := activityFixture(t)
	req.DescriptorDigest = "sha256:mutated"
	if _, _, err := activity.Execute(context.Background(), req); !errors.Is(err, ErrDescriptorStale) {
		t.Fatalf("digest mutation error = %v", err)
	}
	if executor.calls != 0 || len(evidence.entries) != 1 {
		t.Fatalf("stale descriptor reached executor/evidence = %d/%d", executor.calls, len(evidence.entries))
	}

	activity, req, executor, _ = activityFixture(t)
	req.Lease.ExpiresAt = req.Lease.ExpiresAt.Add(-2 * time.Minute)
	if _, _, err := activity.Execute(context.Background(), req); !errors.Is(err, ErrStaleActivity) {
		t.Fatalf("expired lease error = %v", err)
	}
	if executor.calls != 0 {
		t.Fatal("expired lease reached executor")
	}
}

type repairVerifierFixture struct {
	values []RepairVerification
	calls  int
}

type reconciliationSourceFixture struct {
	observation RepairObservation
	calls       int
}

func (f *reconciliationSourceFixture) ObserveReconciliation(context.Context, ReconciliationRequest) (RepairObservation, error) {
	f.calls++
	return f.observation, nil
}

func (f *repairVerifierFixture) VerifyRepairFresh(context.Context, RepairPlan) (RepairVerification, error) {
	f.calls++
	if len(f.values) == 0 {
		return RepairVerification{}, errors.New("no verification scripted")
	}
	v := f.values[0]
	f.values = f.values[1:]
	return v, nil
}

type repairExecutorFixture struct {
	calls int
	err   error
}

func (f *repairExecutorFixture) ExecuteRepair(context.Context, RepairExecution) (string, error) {
	f.calls++
	return "effect-1", f.err
}

type repairReceiptStoreFixture struct {
	receipt RepairReceipt
	found   bool
	stored  int
}

func (f *repairReceiptStoreFixture) FindRepairReceipt(context.Context, string, string) (RepairReceipt, bool, error) {
	return f.receipt, f.found, nil
}
func (f *repairReceiptStoreFixture) StoreRepairReceipt(_ context.Context, receipt RepairReceipt) error {
	f.receipt, f.found, f.stored = receipt, true, f.stored+1
	return nil
}

func repairPlanFixture(t *testing.T) (RepairPlan, time.Time) {
	t.Helper()
	when := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	plan, err := (ReconciliationRole{Now: func() time.Time { return when }}).CreateRepairPlan(
		context.Background(),
		RepairObservation{TenantID: uuid.New(), ResourceKey: "worker:1", Digest: "observed", ObservedAt: when, FreshUntil: when.Add(time.Hour)},
		"expected", "requester", "repair-idem-1")
	if err != nil {
		t.Fatalf("CreateRepairPlan: %v", err)
	}
	return plan, when
}

// TestTodo_SVC_009 proves reconciliation and repair are separate authorized
// roles and that repair requires dual control plus fresh closure evidence.
func TestTodo_SVC_009(t *testing.T) {
	plan, when := repairPlanFixture(t)
	verifier := &repairVerifierFixture{values: []RepairVerification{
		{Digest: plan.ObservationDigest, VerifiedAt: when, Fresh: true},
		{Digest: plan.ExpectedDigest, VerifiedAt: when.Add(time.Minute), Fresh: true},
	}}
	executor := &repairExecutorFixture{}
	store := &repairReceiptStoreFixture{}
	role := RepairRole{Verifier: verifier, Executor: executor, Receipts: store, ExecutorID: "repair-worker", Now: func() time.Time { return when.Add(2 * time.Minute) }}
	receipt, err := role.ExecuteRepair(context.Background(), plan, RepairApproval{ApprovedBy: "approver", ApprovedAt: when.Add(time.Minute), PlanDigest: plan.Digest})
	if err != nil {
		t.Fatalf("ExecuteRepair: %v", err)
	}
	if receipt.EffectID != "effect-1" || executor.calls != 1 || verifier.calls != 2 || store.stored != 1 {
		t.Fatalf("receipt/calls = %+v/%d/%d/%d", receipt, executor.calls, verifier.calls, store.stored)
	}
	if _, err := AuthorizeWorkerRole(WorkerRoleRepair, WorkerActionCreateRepairPlan); !errors.Is(err, ErrRoleDenied) {
		t.Fatalf("repair role gained plan creation: %v", err)
	}
}

// TestTodo_SVC_009_Race repeats idempotent repair delivery after the receipt
// exists; the stored receipt is returned and the corrective executor is not
// invoked again.
func TestTodo_SVC_009_Race(t *testing.T) {
	plan, when := repairPlanFixture(t)
	store := &repairReceiptStoreFixture{receipt: RepairReceipt{PlanID: plan.PlanID, PlanDigest: plan.Digest, EffectID: "effect-existing"}, found: true}
	executor := &repairExecutorFixture{}
	role := RepairRole{Receipts: store, Executor: executor, ExecutorID: "repair-worker", Now: func() time.Time { return when.Add(time.Minute) }}
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := role.ExecuteRepair(context.Background(), plan, RepairApproval{ApprovedBy: "approver", ApprovedAt: when, PlanDigest: plan.Digest})
			if err != nil {
				errs <- err
				return
			}
			if receipt.EffectID != "effect-existing" {
				errs <- fmt.Errorf("replay receipt = %+v", receipt)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if executor.calls != 0 {
		t.Fatalf("idempotent replay invoked executor %d times", executor.calls)
	}
}

// TestTodo_SVC_009_Integration proves the plan store and receipt store ports
// are the only durable boundaries required by the role orchestration.
func TestTodo_SVC_009_Integration(t *testing.T) {
	plan, when := repairPlanFixture(t)
	source := &reconciliationSourceFixture{observation: RepairObservation{TenantID: plan.TenantID, ResourceKey: plan.ResourceKey, Digest: plan.ObservationDigest, ObservedAt: plan.ObservationAt, FreshUntil: when.Add(time.Hour)}}
	diagnosed, err := (ReconciliationRole{Source: source, Now: func() time.Time { return when }}).Diagnose(context.Background(), ReconciliationRequest{
		TenantID: plan.TenantID, ResourceKey: plan.ResourceKey, ExpectedDigest: plan.ExpectedDigest, RequestedBy: plan.CreatedBy, IdempotencyKey: plan.IdempotencyKey,
	})
	if err != nil || source.calls != 1 || diagnosed.Digest != plan.Digest {
		t.Fatalf("diagnosis plan/source = %+v/%d err=%v", diagnosed, source.calls, err)
	}
	verifier := &repairVerifierFixture{values: []RepairVerification{{Digest: plan.ObservationDigest, VerifiedAt: when, Fresh: true}, {Digest: plan.ExpectedDigest, VerifiedAt: when, Fresh: true}}}
	store := &repairReceiptStoreFixture{}
	role := RepairRole{Verifier: verifier, Executor: &repairExecutorFixture{}, Receipts: store, ExecutorID: "repair-worker", Now: func() time.Time { return when.Add(time.Minute) }}
	if _, err := role.ExecuteRepair(context.Background(), plan, RepairApproval{ApprovedBy: "approver", ApprovedAt: when, PlanDigest: plan.Digest}); err != nil {
		t.Fatalf("ExecuteRepair: %v", err)
	}
	store.found = true
	if got, err := role.ExecuteRepair(context.Background(), plan, RepairApproval{ApprovedBy: "approver", ApprovedAt: when, PlanDigest: plan.Digest}); err != nil || got.ReceiptDigest == "" {
		t.Fatalf("receipt replay = %+v err=%v", got, err)
	}
}

// TestTodo_SVC_009_Security pins stale observation refusal and self-approval
// refusal before any repair executor call.
func TestTodo_SVC_009_Security(t *testing.T) {
	plan, when := repairPlanFixture(t)
	executor := &repairExecutorFixture{}
	role := RepairRole{Verifier: &repairVerifierFixture{values: []RepairVerification{{Digest: plan.ObservationDigest, VerifiedAt: when, Fresh: false}}}, Executor: executor, ExecutorID: "repair-worker", Now: func() time.Time { return when.Add(time.Minute) }}
	_, err := role.ExecuteRepair(context.Background(), plan, RepairApproval{ApprovedBy: plan.CreatedBy, ApprovedAt: when, PlanDigest: plan.Digest})
	if !errors.Is(err, ErrInvalidRepairPlan) || executor.calls != 0 {
		t.Fatalf("self approval result/calls = %v/%d", err, executor.calls)
	}
	_, err = (RepairRole{Verifier: role.Verifier, Executor: executor, ExecutorID: "repair-worker", Now: role.Now}).ExecuteRepair(context.Background(), plan, RepairApproval{ApprovedBy: "approver", ApprovedAt: when, PlanDigest: plan.Digest})
	if !errors.Is(err, ErrStaleObservation) || executor.calls != 0 {
		t.Fatalf("stale observation result/calls = %v/%d", err, executor.calls)
	}
}

// TestTodo_SVC_009_Mutation pins immutable plan and post-effect closure
// checks.
func TestTodo_SVC_009_Mutation(t *testing.T) {
	plan, when := repairPlanFixture(t)
	mutated := plan
	mutated.ExpectedDigest = "changed"
	if err := mutated.Validate(when.Add(time.Minute)); !errors.Is(err, ErrInvalidRepairPlan) {
		t.Fatalf("mutated plan accepted: %v", err)
	}
	role := RepairRole{
		Verifier: &repairVerifierFixture{values: []RepairVerification{{Digest: plan.ObservationDigest, VerifiedAt: when, Fresh: true}, {Digest: "wrong", VerifiedAt: when, Fresh: true}}},
		Executor: &repairExecutorFixture{}, ExecutorID: "repair-worker", Now: func() time.Time { return when.Add(time.Minute) },
	}
	if _, err := role.ExecuteRepair(context.Background(), plan, RepairApproval{ApprovedBy: "approver", ApprovedAt: when, PlanDigest: plan.Digest}); !errors.Is(err, ErrRepairNotFresh) {
		t.Fatalf("post-effect stale closure error = %v", err)
	}
}
