package timestore

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWorkflowDispatchValidation(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	if _, ok, err := (&Store{}).ClaimWorkflowDispatch(nil, "", "consumer", "owner", now, time.Minute); err != ErrInvalid || ok {
		t.Fatalf("invalid claim = ok=%v err=%v", ok, err)
	}
	if err := (&Store{}).AdvanceWorkflowDispatch(nil, "tenant", "consumer", "owner", 1, 0, now); err != ErrInvalid {
		t.Fatalf("invalid advance = %v", err)
	}
	if err := (&Store{}).BindWorkflowSessionRun(nil, WorkflowSessionRun{TenantID: "tenant", SessionID: "s", WorkflowID: "wf", PlanDigest: "sha256:p", StartKey: "k", CorrelationID: "c", InstanceID: uuid.New(), CreatedAt: now}); err == nil {
		t.Fatal("nil store unexpectedly accepted binding")
	}
}
