package execute

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	operationrepair "github.com/monstercameron/human-capital-management-suite/internal/operations/repair"
)

type repairEffectDouble struct {
	mu    sync.Mutex
	calls []RepairEffectRequest
	err   error
}

func (d *repairEffectDouble) ExecuteRepairEffect(_ context.Context, req RepairEffectRequest) (RepairEffectResult, error) {
	d.mu.Lock()
	d.calls = append(d.calls, req)
	failure := d.err
	d.mu.Unlock()
	if failure != nil {
		return RepairEffectResult{}, failure
	}
	return RepairEffectResult{EffectKey: req.Step.EffectKey, EffectRef: req.Step.EffectRef, Accepted: true, ResultRef: "effect-result:1"}, nil
}

func (d *repairEffectDouble) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

type repairObservationDouble struct{}

func (repairObservationDouble) ObserveRepair(context.Context, operationrepair.RepairPlan, RepairEffectResult) (RepairObservation, error) {
	return RepairObservation{Observed: true, Complete: true, Digest: "sha256:observed-repair", State: "IAM_EXPECTED"}, nil
}

type repairReconciliationDouble struct{ decision reconcile.CompletionDecision }

func (d repairReconciliationDouble) ReconcileRepair(context.Context, RepairReconciliationRequest) (reconcile.CompletionDecision, error) {
	return d.decision, nil
}

type repairApprovalDouble struct{ calls int }

func (d *repairApprovalDouble) ApproveRepair(context.Context, RepairApprovalRequest) (RepairApprovalResult, error) {
	d.calls++
	return RepairApprovalResult{Approved: true, Digest: "approval:repair-1"}, nil
}

// executeRepairTenant is the one tenant every repair in this file is scoped
// to; the durable record is meaningless without one.
var executeRepairTenant = uuid.MustParse("6f1a6f68-1f2f-4f5d-9d4a-1f9c0c0a0016")

func executeRepairPlan() operationrepair.RepairPlan {
	return operationrepair.RepairPlan{
		ID: "repair-promotion-1", Digest: "sha256:plan-1", FindingDigest: "sha256:finding-1",
		ObservationDigest: "sha256:observation-1", AuthorityPolicy: "authority.promotion/1",
		MappingVersion: "mapping.iam/1", CredentialRef: "credential:iam-1", TargetVersion: "iam:worker-1@7",
		OriginalSemanticKey: "promotion:worker-1:proposal-1", FailedEffectKey: "effect:iam-provision",
		Steps: []operationrepair.Step{
			{Ordinal: 1, EffectKey: "effect:unrelated", EffectRef: "operation:unrelated", Target: "iam:worker-1", ExpectedVersion: "iam:worker-1@7", MaxAttempts: 2},
			{Ordinal: 2, EffectKey: "effect:iam-provision", EffectRef: "operation:iam-1", Target: "iam:worker-1", ExpectedVersion: "iam:worker-1@7", MaxAttempts: 2},
		},
	}
}

func executeRepairEvidence() operationrepair.CurrentEvidence {
	return operationrepair.CurrentEvidence{
		PlanDigest: "sha256:plan-1", FindingDigest: "sha256:finding-1", ObservationDigest: "sha256:observation-1",
		AuthorityPolicy: "authority.promotion/1", MappingVersion: "mapping.iam/1", CredentialRef: "credential:iam-1",
		TargetVersion: "iam:worker-1@7", Facts: map[string]string{"worker": "worker-1"},
	}
}

// executeRepairRequest is the standard well-formed request: the shared tenant,
// the shipped plan and a distinct author and actor.
func executeRepairRequest(at time.Time) RepairExecutionRequest {
	return RepairExecutionRequest{
		TenantID: executeRepairTenant, Plan: executeRepairPlan(), Current: executeRepairEvidence(),
		Now: at, Actor: "operator:repair", Author: "analyst:diagnosis",
	}
}

func newRepairExecutor(t *testing.T, effect *repairEffectDouble, approval RepairApprovalPort, decision reconcile.CompletionDecision) *RepairExecutor {
	t.Helper()
	return newRepairExecutorOver(t, NewMemoryRepairRecords(), effect, approval, decision)
}

// newRepairExecutorOver composes an executor over an existing record store, so
// a test can compose a second executor -- a restarted cell -- over exactly the
// record the first one left behind.
func newRepairExecutorOver(t *testing.T, records RepairIdempotencyStore, effect RepairEffectPort, approval RepairApprovalPort, decision reconcile.CompletionDecision) *RepairExecutor {
	t.Helper()
	executor, err := NewRepairExecutor(RepairExecutionOptions{
		Admission: operationrepair.NewMemoryStore(), Approval: approval, Effect: effect,
		Observation: repairObservationDouble{}, Reconciliation: repairReconciliationDouble{decision: decision},
		Records: records,
	})
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func passingRepairDecision() reconcile.CompletionDecision {
	return reconcile.CompletionDecision{Status: reconcile.CompletionPass, Terminal: true, Route: reconcile.RouteConsistent, Reason: "fresh complete observation matches intended and canonical values"}
}

// recordStages lists the stages the durable record holds for the plan's fence.
func recordStages(t *testing.T, records RepairIdempotencyStore) []RepairRecordStage {
	t.Helper()
	stored, err := records.LoadRepairRecords(context.Background(), executeRepairTenant, "repair:repair-promotion-1:effect:iam-provision")
	if err != nil {
		t.Fatalf("load repair records: %v", err)
	}
	stages := make([]RepairRecordStage, 0, len(stored))
	for _, record := range stored {
		stages = append(stages, record.Stage)
	}
	return stages
}

// TestTodo_WF_RUN_016 proves the distinct REPAIR mode lifecycle and its
// one-effect invariant. The parent Promotion semantic identity reaches the
// effect port unchanged, while the repair itself is fenced separately, records
// its claim before the effect and closes only after RECON-002 returns
// CONSISTENT.
func TestTodo_WF_RUN_016(t *testing.T) {
	effects := &repairEffectDouble{}
	records := NewMemoryRepairRecords()
	executor := newRepairExecutorOver(t, records, effects, nil, passingRepairDecision())
	result, err := executor.Execute(context.Background(), executeRepairRequest(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != RepairExecutionMode || result.Status != RepairCompleted || !result.Executed || result.ConsistencyState != "CONSISTENT" {
		t.Fatalf("repair result = %+v, want REPAIR/COMPLETED/executed/CONSISTENT", result)
	}
	if len(effects.calls) != 1 {
		t.Fatalf("effect calls = %d, want exactly one failed effect", len(effects.calls))
	}
	call := effects.calls[0]
	if call.Step.EffectKey != "effect:iam-provision" || call.OriginalSemanticKey != "promotion:worker-1:proposal-1" || call.Mode != RepairExecutionMode {
		t.Fatalf("effect request = %+v, want failed effect, original key and REPAIR mode", call)
	}
	if result.Fence.FenceID == "" || result.Fence.FenceID == call.OriginalSemanticKey {
		t.Fatalf("repair fence = %+v, want distinct fenced identity", result.Fence)
	}
	if len(result.Evidence) != 6 || result.Evidence[len(result.Evidence)-1].Stage != "VERIFIED" {
		t.Fatalf("repair evidence = %+v, want six stages ending VERIFIED", result.Evidence)
	}
	stages := recordStages(t, records)
	if len(stages) != 3 || stages[0] != RepairStageClaimed || stages[1] != RepairStageExecuted || stages[2] != RepairStageSettled {
		t.Fatalf("durable record stages = %v, want CLAIMED, EXECUTED, SETTLED", stages)
	}
}

// TestTodo_WF_RUN_016_Restart proves the record, not process memory, is what
// stops a second redrive: a freshly composed executor sharing only the durable
// record replays the settled decision and never reaches the effect port.
func TestTodo_WF_RUN_016_Restart(t *testing.T) {
	effects := &repairEffectDouble{}
	records := NewMemoryRepairRecords()
	first := newRepairExecutorOver(t, records, effects, nil, passingRepairDecision())
	if _, err := first.Execute(context.Background(), executeRepairRequest(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	// A restarted cell: new executor, new admission fence store, same record.
	restarted := newRepairExecutorOver(t, records, effects, nil, passingRepairDecision())
	result, err := restarted.Execute(context.Background(), executeRepairRequest(time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RepairCompleted || !result.Executed || result.ConsistencyState != "CONSISTENT" {
		t.Fatalf("replayed repair = %+v, want the original COMPLETED/CONSISTENT decision", result)
	}
	if result.Reconciliation.Route != reconcile.RouteConsistent || result.Observation.State != "IAM_EXPECTED" {
		t.Fatalf("replayed evidence = %+v / %+v, want the recorded observation and route", result.Observation, result.Reconciliation)
	}
	if result.Evidence[len(result.Evidence)-1].Stage != "REPLAYED" {
		t.Fatalf("replay evidence = %+v, want a REPLAYED stage", result.Evidence)
	}
	if effects.count() != 1 {
		t.Fatalf("effect calls across the restart = %d, want the one original redrive", effects.count())
	}
}

// TestTodo_WF_RUN_016_Race releases eight concurrent operators at one repair
// fence. Exactly one claim wins and exactly one redrive reaches the provider;
// every other attempt is refused, reports the in-flight claim as unknown, or
// replays the settled decision; none reports an effect it did not execute.
func TestTodo_WF_RUN_016_Race(t *testing.T) {
	const attempts = 8
	effects := &repairEffectDouble{}
	records := NewMemoryRepairRecords()
	executor := newRepairExecutorOver(t, records, effects, nil, passingRepairDecision())

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	results := make([]RepairExecutionResult, attempts)
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			results[i], errs[i] = executor.Execute(context.Background(), executeRepairRequest(time.Date(2026, 9, 5, 12, 0, i, 0, time.UTC)))
		}(i)
	}
	start.Done()
	done.Wait()

	completed := 0
	for i, result := range results {
		if errs[i] != nil {
			t.Fatalf("attempt %d: %v", i, errs[i])
		}
		switch result.Status {
		case RepairCompleted:
			completed++
			if !result.Executed {
				t.Fatalf("attempt %d reported COMPLETED without a redrive: %+v", i, result)
			}
		case RepairBlocked, RepairIndeterminate:
			if result.Executed {
				t.Fatalf("attempt %d lost the claim yet reported a redrive: %+v", i, result)
			}
			if result.Status == RepairIndeterminate && result.ConsistencyState != "UNKNOWN" {
				t.Fatalf("attempt %d observed an unsettled claim without UNKNOWN consistency: %+v", i, result)
			}
		default:
			t.Fatalf("attempt %d status = %s, want COMPLETED, BLOCKED or INDETERMINATE", i, result.Status)
		}
	}
	if completed == 0 {
		t.Fatal("no concurrent attempt completed the repair")
	}
	if effects.count() != 1 {
		t.Fatalf("effect calls = %d under %d concurrent attempts, want exactly one redrive", effects.count(), attempts)
	}
}

// TestTodo_WF_RUN_016_Fault proves the failure path never clears the repair and
// never repeats an effect whose outcome nobody observed: the claim written
// before the provider call survives, and the next attempt is told
// INDETERMINATE instead of redriving.
func TestTodo_WF_RUN_016_Fault(t *testing.T) {
	effects := &repairEffectDouble{err: errors.New("provider unavailable")}
	records := NewMemoryRepairRecords()
	executor := newRepairExecutorOver(t, records, effects, nil, passingRepairDecision())
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	result, err := executor.Execute(context.Background(), executeRepairRequest(at))
	if err == nil || result.Status != RepairFailed {
		t.Fatalf("failed repair result=%+v err=%v, want FAILED", result, err)
	}
	if result.ConsistencyState == "CONSISTENT" {
		t.Fatalf("failed repair claimed consistency: %+v", result)
	}
	if stages := recordStages(t, records); len(stages) != 1 || stages[0] != RepairStageClaimed {
		t.Fatalf("record after a failed redrive = %v, want only the claim", stages)
	}

	// The provider is healthy again, but the earlier attempt's outcome is
	// still unknown, so the effect must not be repeated.
	effects.mu.Lock()
	effects.err = nil
	effects.mu.Unlock()
	retry, err := executor.Execute(context.Background(), executeRepairRequest(at.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if retry.Status != RepairIndeterminate || retry.ConsistencyState != "UNKNOWN" || retry.Executed {
		t.Fatalf("retry after an unobserved redrive = %+v, want INDETERMINATE/UNKNOWN", retry)
	}
	if effects.count() != 1 {
		t.Fatalf("effect calls = %d, want the single unobserved attempt and no repeat", effects.count())
	}
}

// TestTodo_WF_RUN_016_Mutation kills two mutations at once. Closing on
// anything but a passed reconciliation would flip the first assertion, and
// resuming a recorded redrive by calling the provider again -- rather than
// from the EXECUTED record -- would flip the second.
func TestTodo_WF_RUN_016_Mutation(t *testing.T) {
	effects := &repairEffectDouble{}
	records := NewMemoryRepairRecords()
	degraded := newRepairExecutorOver(t, records, effects,
		nil, reconcile.CompletionDecision{Status: reconcile.CompletionMismatch, Terminal: true, Route: reconcile.RouteDegraded})
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	result, err := degraded.Execute(context.Background(), executeRepairRequest(at))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RepairReconciliationWait || result.ConsistencyState == "CONSISTENT" {
		t.Fatalf("unverified repair result=%+v, want RECONCILIATION_REQUIRED and no consistency claim", result)
	}
	stages := recordStages(t, records)
	if len(stages) != 2 || stages[1] != RepairStageExecuted {
		t.Fatalf("record after an unverified redrive = %v, want CLAIMED and EXECUTED but no SETTLED", stages)
	}

	// Reconciliation now passes. The repair closes without a second redrive.
	verified := newRepairExecutorOver(t, records, effects, nil, passingRepairDecision())
	closed, err := verified.Execute(context.Background(), executeRepairRequest(at.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != RepairCompleted || !closed.Executed || closed.ConsistencyState != "CONSISTENT" {
		t.Fatalf("resumed repair = %+v, want COMPLETED/CONSISTENT", closed)
	}
	if effects.count() != 1 {
		t.Fatalf("effect calls = %d, want the recorded redrive reused, not repeated", effects.count())
	}
	if last := closed.Evidence[len(closed.Evidence)-1]; last.Stage != "VERIFIED" {
		t.Fatalf("resumed evidence = %+v, want VERIFIED", closed.Evidence)
	}
}

// TestTodo_WF_RUN_016_SeparationOfDuties proves a plan's own author cannot
// execute it where approval -- and therefore separation of duties -- applies,
// and that an unrecorded author fails closed rather than open.
func TestTodo_WF_RUN_016_SeparationOfDuties(t *testing.T) {
	plan := executeRepairPlan()
	plan.RequiresApproval = true
	plan.ApprovalDigest = "approval:repair-1"
	current := executeRepairEvidence()
	current.ApprovalValid = true
	current.ApprovalDigest = "approval:repair-1"

	for name, actor := range map[string]struct{ author, operator string }{
		"author executes their own plan": {author: "analyst:diagnosis", operator: "analyst:diagnosis"},
		"author differs only in case":    {author: "Analyst:Diagnosis", operator: "analyst:diagnosis"},
		"author is not recorded":         {author: "", operator: "operator:repair"},
	} {
		t.Run(name, func(t *testing.T) {
			effects := &repairEffectDouble{}
			approval := &repairApprovalDouble{}
			records := NewMemoryRepairRecords()
			executor := newRepairExecutorOver(t, records, effects, approval, passingRepairDecision())
			result, err := executor.Execute(context.Background(), RepairExecutionRequest{
				TenantID: executeRepairTenant, Plan: plan, Current: current,
				Now: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), Actor: actor.operator, Author: actor.author,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != RepairSeparationRequired || result.Executed {
				t.Fatalf("self-executed repair = %+v, want SEPARATION_OF_DUTIES_REQUIRED", result)
			}
			if effects.count() != 0 || approval.calls != 0 {
				t.Fatalf("effect calls = %d, approval calls = %d, want neither reached", effects.count(), approval.calls)
			}
			if stages := recordStages(t, records); len(stages) != 0 {
				t.Fatalf("refused repair claimed the fence: %v", stages)
			}
		})
	}
}

func TestTodo_WF_RUN_016_Reapproval(t *testing.T) {
	plan := executeRepairPlan()
	plan.RequiresApproval = true
	plan.ApprovalDigest = "approval:repair-1"
	effects := &repairEffectDouble{}
	approval := &repairApprovalDouble{}
	executor := newRepairExecutor(t, effects, approval, passingRepairDecision())
	req := executeRepairRequest(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	req.Plan = plan
	req.Current.ApprovalValid = true
	req.Current.ApprovalDigest = "approval:repair-1"
	result, err := executor.Execute(context.Background(), req)
	if err != nil || result.Status != RepairCompleted || approval.calls != 1 {
		t.Fatalf("approval result=%+v err=%v calls=%d", result, err, approval.calls)
	}
}

// TestRepairExecutorRefusesAnUnscopedRequest proves the durable record's
// tenant scope is a precondition, not an optional field.
func TestRepairExecutorRefusesAnUnscopedRequest(t *testing.T) {
	effects := &repairEffectDouble{}
	executor := newRepairExecutor(t, effects, nil, passingRepairDecision())
	req := executeRepairRequest(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	req.TenantID = uuid.Nil
	if _, err := executor.Execute(context.Background(), req); err == nil {
		t.Fatal("an unscoped repair was accepted")
	}
	if effects.count() != 0 {
		t.Fatalf("effect calls = %d on an unscoped repair", effects.count())
	}
}

// TestNewRepairExecutorRequiresADurableRecord proves the composition refuses
// to build an executor whose idempotency would only live in memory.
func TestNewRepairExecutorRequiresADurableRecord(t *testing.T) {
	_, err := NewRepairExecutor(RepairExecutionOptions{
		Admission: operationrepair.NewMemoryStore(), Effect: &repairEffectDouble{},
		Observation: repairObservationDouble{}, Reconciliation: repairReconciliationDouble{decision: passingRepairDecision()},
	})
	if err == nil {
		t.Fatal("an executor without a repair record store was composed")
	}
}

// TestExplainRepairIsTelemetrySafe pins the payload-free summary line.
func TestExplainRepairIsTelemetrySafe(t *testing.T) {
	summary := ExplainRepair(RepairExecutionResult{
		Mode: RepairExecutionMode, Status: RepairCompleted, PlanDigest: "sha256:plan-1",
		FailedEffectKey: "effect:iam-provision", Executed: true,
		Reconciliation: passingRepairDecision(), Evidence: make([]RepairEvidence, 6),
	})
	want := "workflow repair mode=REPAIR status=COMPLETED plan=sha256:plan-1 effect=effect:iam-provision executed=true consistency=CONSISTENT evidence=6"
	if summary != want {
		t.Fatalf("ExplainRepair = %q, want %q", summary, want)
	}
}
