package workerlifecyclestore

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_021_Onboarding_OptionalReferenceRoundTrip(t *testing.T) {
	snapshot := lifecycleFixture(t, "onboarding-a")
	snapshot.Request.Plan.ApprovalDecision = values.EntityRef{Tenant: "onboarding-a", Kind: "approval_decision", Id: uuid.NewString()}
	snapshot.Request.Plan.CanonicalDigest = ""
	var err error
	snapshot.Request.Plan, err = workerlifecycle.NewPlan(snapshot.Request.Plan)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := workerlifecycle.ResolveOnboardingReadiness(snapshot.Request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Tracker, err = workerlifecycle.NewOnboardingTracker(snapshot.Request.Plan, ready)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Tracker, _, err = workerlifecycle.EmitDueChildren(snapshot.Tracker, ready)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Tracker, err = workerlifecycle.ObserveChild(snapshot.Tracker, "identity-task", values.EntityRef{Tenant: "onboarding-a", Kind: "observation", Id: uuid.NewString()}, workerlifecycle.ChildObserved)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var got Snapshot
	if err = decode(raw, &got); err != nil || got.Validate() != nil || !reflect.DeepEqual(got, snapshot) {
		t.Fatalf("optional owner refs changed: %+v %v", got, err)
	}
	if err = decode([]byte(`{"Request":"bad"}`), &got); err == nil {
		t.Fatal("invalid typed payload accepted")
	}
	snapshot.Request.Plan.CanonicalDigest = ""
	snapshot.Request.Plan.Children = nil
	snapshot.Request.Plan, err = workerlifecycle.NewPlan(snapshot.Request.Plan)
	if err != nil {
		t.Fatal(err)
	}
	ready, _ = workerlifecycle.ResolveOnboardingReadiness(snapshot.Request)
	snapshot.Tracker, err = workerlifecycle.NewOnboardingTracker(snapshot.Request.Plan, ready)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err = decode(raw, &got); err != nil || !reflect.DeepEqual(got, snapshot) {
		t.Fatalf("empty children changed: %+v %v", got, err)
	}
}
