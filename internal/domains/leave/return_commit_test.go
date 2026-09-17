package leave

import (
	"fmt"
	"sync"
	"testing"
)

// LEAVE-013 RED: committing ReturnFromLeave does not exist yet. The trace
// below must fail to compile until the return committer lands.

func readyReadiness() ReturnToWorkResult {
	res, err := EvaluateReturnToWorkReadiness(ReturnToWorkInput{
		LeaveRevision: "rev:leave:7", EvidenceRevision: "rev:evidence:7",
		JobRevision: "rev:job:3", ScheduleRevision: "rev:schedule:3", AccessRevision: "rev:access:2",
		Clearance: ClearanceValid, RestrictionState: RestrictionConditional,
		Restrictions:   []StructuredRestriction{{ID: "r:lift", Code: "LIFT-10KG", Source: "MEDICAL"}},
		JobRequirement: JobAvailable,
		Schedule:       ScheduleConfirmed,
		Access:         AccessRestored,
		Qualification:  QualificationQualified,
	})
	if err != nil {
		panic(err)
	}
	return res
}

func returnInput() ReturnCommitInput {
	return ReturnCommitInput{
		IdempotencyKey:   "commit:leave:w1:return",
		EmploymentState:  "ACTIVE",
		Readiness:        readyReadiness(),
		Restrictions:     []StructuredRestriction{{ID: "r:lift", Code: "LIFT-10KG", Source: "MEDICAL"}},
		QueuedEffects:    []string{"payroll", "benefits", "schedule", "access"},
		ObligationPolicy: "policy:v3",
		StepReceipts: map[string]string{
			"leave-ended-revision": "receipt:ended", "availability-restoration": "receipt:avail",
			"restriction-carryover": "receipt:restr", "ledger-events": "receipt:ledger",
			"projections": "receipt:proj", "effect-outbox": "receipt:outbox",
			"obligation-policy": "receipt:policy",
		},
		ReturnEffects: []ExternalEffect{
			{System: "payroll", Mandatory: true, FreshnessTick: 300, DeadlineTick: 400, State: EffectPass, Observation: "obs:payroll-r1", Owner: "payroll-ops"},
			{System: "benefits", Mandatory: true, FreshnessTick: 300, DeadlineTick: 400, State: EffectPass, Observation: "obs:benefits-r1", Owner: "benefits-ops"},
			{System: "schedule", Mandatory: true, FreshnessTick: 300, DeadlineTick: 400, State: EffectPass, Observation: "obs:schedule-r1", Owner: "wfm-ops"},
			{System: "access", Mandatory: false, FreshnessTick: 300, DeadlineTick: 400, State: EffectPass, Observation: "obs:access-r1", Owner: "iam-ops"},
		},
	}
}

func TestTodo_LEAVE_013(t *testing.T) {
	committer := NewReturnCommitter()
	record, err := committer.Commit(returnInput(), nil)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if record.BusinessState != BusinessLeaveEnded || len(record.Steps) != 7 {
		t.Fatalf("record=%+v", record)
	}
	if len(record.PreservedRestrictions) != 1 || record.PreservedRestrictions[0] != "r:lift" {
		t.Fatalf("restrictions=%+v", record.PreservedRestrictions)
	}
	if err := record.Verify(returnInput()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Duplicate returns return the identical record: never a second return.
	again, err := committer.Commit(returnInput(), nil)
	if err != nil || again.CommitID != record.CommitID || again.Digest != record.Digest {
		t.Fatalf("again=%+v err=%v", again, err)
	}
	// RED: return before readiness refuses with nothing committed.
	fresh := NewReturnCommitter()
	notReady, _ := EvaluateReturnToWorkReadiness(ReturnToWorkInput{
		LeaveRevision: "rev:leave:7", EvidenceRevision: "rev:evidence:7",
		JobRevision: "rev:job:3", ScheduleRevision: "rev:schedule:3", AccessRevision: "rev:access:2",
		Clearance: ClearanceMissing, RestrictionState: RestrictionNone,
		JobRequirement: JobAvailable, Schedule: ScheduleConfirmed,
		Access: AccessRestored, Qualification: QualificationQualified,
	})
	early := returnInput()
	early.IdempotencyKey = "commit:leave:w1:return-early"
	early.Readiness = notReady
	early.Restrictions = nil
	if _, err := fresh.Commit(early, nil); err == nil {
		t.Fatal("return before readiness committed")
	}
	if _, err := fresh.Commit(returnInput(), nil); err != nil {
		t.Fatalf("clean commit after refused return failed: %v", err)
	}
	// RED: a discarded active restriction refuses.
	dropped := returnInput()
	dropped.IdempotencyKey = "commit:leave:w1:return-dropped"
	dropped.Restrictions = nil
	if _, err := NewReturnCommitter().Commit(dropped, nil); err == nil {
		t.Fatal("return discarded an active restriction")
	}
	// RED: absence ends without availability restoration refuses.
	unrestored := returnInput()
	unrestored.IdempotencyKey = "commit:leave:w1:return-unrestored"
	delete(unrestored.StepReceipts, "availability-restoration")
	if _, err := NewReturnCommitter().Commit(unrestored, nil); err == nil {
		t.Fatal("absence ended without availability restoration")
	}
	// RED: employment inactivity refuses and mutates nothing.
	terminated := returnInput()
	terminated.IdempotencyKey = "commit:leave:w1:return-terminated"
	terminated.EmploymentState = "TERMINATED"
	if _, err := NewReturnCommitter().Commit(terminated, nil); err == nil {
		t.Fatal("return mutated inactive employment")
	}
	// RED: provider failure never rewrites local return truth.
	failed := returnInput()
	failed.IdempotencyKey = "commit:leave:w1:return-failed"
	failed.ReturnEffects[0].State = EffectFailed
	held, err := NewReturnCommitter().Commit(failed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if held.BusinessState != BusinessLeaveEnded || held.ObligationState != "PENDING" {
		t.Fatalf("held=%+v", held)
	}
	var nilCommitter *ReturnCommitter
	if _, err := nilCommitter.Commit(returnInput(), nil); err == nil {
		t.Fatal("nil return committer committed")
	}
}

func TestTodo_LEAVE_013_Race(t *testing.T) {
	committer := NewReturnCommitter()
	const racers = 8
	results := make([]ReturnRecord, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = committer.Commit(returnInput(), nil)
		}(i)
	}
	wg.Wait()
	for i := 0; i < racers; i++ {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if results[i].CommitID != results[0].CommitID || results[i].Digest != results[0].Digest {
			t.Fatalf("racer %d diverged: %+v", i, results[i])
		}
	}
}

func TestTodo_LEAVE_013_Integration(t *testing.T) {
	// Real readiness evaluation flows into the real return commit and the
	// real effect reconciliation: no seam between the three.
	readiness, err := EvaluateReturnToWorkReadiness(ReturnToWorkInput{
		LeaveRevision: "rev:leave:9", EvidenceRevision: "rev:evidence:9",
		JobRevision: "rev:job:4", ScheduleRevision: "rev:schedule:4", AccessRevision: "rev:access:3",
		Clearance: ClearanceValid, RestrictionState: RestrictionNone,
		JobRequirement: JobAvailable, Schedule: ScheduleConfirmed,
		Access: AccessRestored, Qualification: QualificationQualified,
	})
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Result != ReadinessReady {
		t.Fatalf("readiness=%+v", readiness)
	}
	input := returnInput()
	input.IdempotencyKey = "commit:leave:w1:return-integration"
	input.Readiness = readiness
	input.Restrictions = nil
	record, err := NewReturnCommitter().Commit(input, nil)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if record.BusinessState != BusinessLeaveEnded || record.ObligationState != "SATISFIED" {
		t.Fatalf("record=%+v", record)
	}
	if len(record.PreservedRestrictions) != 0 {
		t.Fatalf("restrictions=%+v", record.PreservedRestrictions)
	}
}

func TestTodo_LEAVE_013_Recovery(t *testing.T) {
	committer := NewReturnCommitter()
	// A failpoint at availability restoration commits nothing; the clean
	// retry commits exactly once.
	if _, err := committer.Commit(returnInput(), func(step string) error {
		if step == "availability-restoration" {
			return fmt.Errorf("injected fault at availability restoration")
		}
		return nil
	}); err == nil {
		t.Fatal("failpoint committed a return")
	}
	record, err := committer.Commit(returnInput(), nil)
	if err != nil {
		t.Fatalf("clean retry after failpoint refused: %v", err)
	}
	// A failed payroll observation keeps local return truth and yields a
	// targeted repair without a second local transaction.
	probing := returnInput()
	probing.IdempotencyKey = "commit:leave:w1:return-recovery"
	probing.ReturnEffects[0].State = EffectFailed
	held, err := NewReturnCommitter().Commit(probing, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := held.RepairPlan("payroll")
	if err != nil || plan == "" {
		t.Fatalf("plan=%q err=%v", plan, err)
	}
	if _, err := held.RepairPlan("benefits"); err == nil {
		t.Fatal("passing effect repaired")
	}
	if held.Digest != record.Digest && held.CommitID == record.CommitID {
		t.Fatal("distinct returns share a commit id")
	}
}

func TestTodo_LEAVE_013_Mutation(t *testing.T) {
	committer := NewReturnCommitter()
	base, err := committer.Commit(returnInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// A changed step receipt is a different return with its own seal.
	changed := returnInput()
	changed.IdempotencyKey = "commit:leave:w1:return-2"
	changed.StepReceipts["restriction-carryover"] = "receipt:restr-2"
	other, err := committer.Commit(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if other.Digest == base.Digest {
		t.Fatal("receipt mutation kept the return seal")
	}
	// Forged records never verify.
	forged := base
	forged.BusinessState = BusinessLeaveActive
	if err := forged.Verify(returnInput()); err == nil {
		t.Fatal("forged return verified")
	}
	// Tampered readiness never commits.
	tampered := returnInput()
	tampered.IdempotencyKey = "commit:leave:w1:return-tampered"
	tampered.Readiness.Result = ReadinessReady
	if _, err := NewReturnCommitter().Commit(tampered, nil); err == nil {
		t.Fatal("tampered readiness committed")
	}
}
