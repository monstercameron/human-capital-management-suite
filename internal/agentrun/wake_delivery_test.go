package agentrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestTodo_AGENT2_011_WaitStepDigestStable proves the optional Wait field
// leaves the digest of every plan that does not use it byte-for-byte what it
// was before the field existed, so stored plans and approvals stay valid.
func TestTodo_AGENT2_011_WaitStepDigestStable(t *testing.T) {
	type legacyIdentity struct {
		ID                   string     `json:"id"`
		Type                 StepType   `json:"type"`
		SkillID              string     `json:"skill_id"`
		SkillVersion         uint32     `json:"skill_version"`
		ConnectionID         string     `json:"connection_id,omitempty"`
		Inputs               []InputRef `json:"inputs,omitempty"`
		ExpectedOutput       string     `json:"expected_output"`
		Tier                 Tier       `json:"tier"`
		Destination          string     `json:"destination,omitempty"`
		DestinationConfirmed bool       `json:"destination_confirmed,omitempty"`
	}
	type legacyBinding struct {
		Revision uint64           `json:"revision"`
		Steps    []legacyIdentity `json:"steps"`
	}
	plan := planForTest(t, step("read", StepRead, TierRead), step("ask", StepAskUser, TierRead), step("wait", StepWait, TierRead))
	legacy := legacyBinding{Revision: plan.Revision}
	for _, s := range plan.Steps {
		legacy.Steps = append(legacy.Steps, legacyIdentity{ID: s.ID, Type: s.Type, SkillID: s.SkillID, SkillVersion: s.SkillVersion, ConnectionID: s.ConnectionID, Inputs: s.Inputs, ExpectedOutput: s.ExpectedOutput, Tier: s.Tier, Destination: s.Destination, DestinationConfirmed: s.DestinationConfirmed})
	}
	encoded, _ := json.Marshal(legacy)
	sum := sha256.Sum256(append([]byte("hcm-next-agent-plan/v1\x00"), encoded...))
	if want := "sha256:" + hex.EncodeToString(sum[:]); plan.Digest != want {
		t.Fatalf("plan digest = %s, want the pre-Wait digest %s", plan.Digest, want)
	}
	if got, _ := json.Marshal(plan.Steps[2]); strings.Contains(string(got), `"wait":`) {
		t.Fatalf("a step without a wait condition serialised one: %s", got)
	}

	waiting := step("wait", StepWait, TierRead)
	waiting.Wait = &WakeCondition{Kind: WakeSignal, Key: "promotion-complete"}
	other := planForTest(t, step("read", StepRead, TierRead), step("ask", StepAskUser, TierRead), waiting)
	if other.Digest == plan.Digest {
		t.Fatal("a wait condition does not change the plan digest, so an approval would not bind it")
	}
	clone := cloneSteps(other.Steps)
	clone[2].Wait.Key = "changed"
	if other.Steps[2].Wait.Key != "promotion-complete" {
		t.Fatal("cloneSteps shares the Wait pointer with its source")
	}
}

func TestTodo_AGENT2_011_WaitSpecValidation(t *testing.T) {
	cases := map[string]PlanStep{}
	add := func(name string, typ StepType, w WakeCondition) {
		s := step(name, typ, TierRead)
		s.Wait = &w
		cases[name] = s
	}
	add("timer-without-time", StepWait, WakeCondition{Kind: WakeTimer})
	add("poll-without-interval", StepWait, WakeCondition{Kind: WakePolling})
	add("signal-without-key", StepWait, WakeCondition{Kind: WakeSignal})
	add("approval-wait", StepWait, WakeCondition{Kind: WakeApproval, Key: "k"})
	add("unknown-kind", StepWait, WakeCondition{Kind: "BOGUS", Key: "k"})
	add("ask-with-timer", StepAskUser, WakeCondition{Kind: WakeTimer, PollAfter: time.Hour})
	add("wait-on-read", StepRead, WakeCondition{Kind: WakeUserReply})
	for name, s := range cases {
		if _, err := NewPlan([]PlanStep{s}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: NewPlan = %v, want ErrInvalid", name, err)
		}
	}
	for name, w := range map[string]WakeCondition{
		"relative-timer": {Kind: WakeTimer, PollAfter: time.Hour},
		"absolute-timer": {Kind: WakeTimer, DueAt: testNow},
		"polling":        {Kind: WakePolling, PollAfter: time.Minute},
		"signal":         {Kind: WakeSignal, Key: "k"},
		"reply":          {Kind: WakeUserReply},
	} {
		s := step(name, StepWait, TierRead)
		s.Wait = &w
		if _, err := NewPlan([]PlanStep{s}); err != nil {
			t.Errorf("%s: NewPlan = %v, want a valid plan", name, err)
		}
	}
}

func TestTodo_AGENT2_011_ParkStepGuards(t *testing.T) {
	ctx := context.Background()
	wait := step("wait", StepWait, TierRead)
	wait.Wait = &WakeCondition{Kind: WakeSignal, Key: "k"}
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead), wait))
	task, err := runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	cond := WakeCondition{Kind: WakeSignal, Key: "k"}
	if _, err := runtime.ParkStep(ctx, task.ID, task.Version, cond, testNow); !errors.Is(err, ErrStepNotReady) {
		t.Fatalf("ParkStep on a READ step = %v, want ErrStepNotReady", err)
	}
	if _, err := runtime.CompleteWait(ctx, task.ID, task.Version, StepResult{Ref: "r"}, testNow); !errors.Is(err, ErrStepNotReady) {
		t.Fatalf("CompleteWait on a pending READ step = %v, want ErrStepNotReady", err)
	}
	task, err = runtime.ExecuteNext(ctx, task.ID, task.Version, &fakeExecutor{}, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ParkStep(ctx, task.ID, task.Version-1, cond, testNow); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version = %v, want ErrConflict", err)
	}
	if _, err := runtime.ParkStep(ctx, task.ID, task.Version, WakeCondition{Kind: WakeSignal}, testNow); !errors.Is(err, ErrInvalidWake) {
		t.Fatalf("keyless condition = %v, want ErrInvalidWake", err)
	}
	parked, err := runtime.ParkStep(ctx, task.ID, task.Version, cond, testNow)
	if err != nil || parked.State != StateWaiting || parked.Plan.Steps[1].State != StepWaiting || parked.WorkerLease != "" || parked.Wake == nil {
		t.Fatalf("ParkStep = %+v, %v", parked, err)
	}
	if _, err := runtime.CompleteWait(ctx, task.ID, parked.Version, StepResult{Ref: "r"}, testNow); !errors.Is(err, ErrStepNotReady) {
		t.Fatalf("CompleteWait before the wake resumed the task = %v, want ErrStepNotReady", err)
	}
	woken, err := runtime.Wake(ctx, task.ID, WakeEvent{ID: "e1", Kind: WakeSignal, Key: "k", OccurredAt: testNow})
	if err != nil || !woken.Accepted {
		t.Fatalf("wake = %+v, %v", woken, err)
	}
	done, err := runtime.CompleteWait(ctx, task.ID, woken.Task.Version, StepResult{Ref: "wake:e1", Digest: "sha256:x"}, testNow)
	if err != nil || done.State != StateCompleted || done.Plan.Steps[1].ResultRef != "wake:e1" {
		t.Fatalf("CompleteWait = %+v, %v, want the wake as the step result and the task completed", done, err)
	}
}
