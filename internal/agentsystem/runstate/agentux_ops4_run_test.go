package runstate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAgentUXOps4_FailureRefusalVocabulary(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmission(now))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker-1", now.Add(time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.FailWithRefusal(ctx, run.ID, "worker-1", "TOOL_EXECUTION_FAILED", true, FailureRefusal{Gate: FailureGateToolScope, Owner: "internal/application", Location: "persona_runtime_tools.go:37"}, claimed.Fence, claimed.Version, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != StateFailed || failed.FailureGate != string(FailureGateToolScope) || failed.FailureOwner != "internal/application" || failed.FailureLocation != "persona_runtime_tools.go:37" {
		t.Fatalf("failure refusal = %+v", failed)
	}
	for _, value := range []string{failed.FailureGate, failed.FailureOwner, failed.FailureLocation} {
		for _, forbidden := range []string{"request body", "document body", "model output"} {
			if strings.Contains(value, forbidden) {
				t.Fatalf("stored refusal leaked %q in %q", forbidden, value)
			}
		}
	}
}

func TestAgentUXOps4_FailureRefusalRejectsOpenVocabulary(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmissionWithKey(now, "reject-open-vocabulary"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker-1", now.Add(time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	bad := FailureRefusal{Gate: FailureGate("request body says upload salaries"), Owner: "internal/application", Location: "persona_run_executor.go:1"}
	if _, err := service.FailWithRefusal(ctx, run.ID, "worker-1", "TOOL_EXECUTION_FAILED", false, bad, claimed.Fence, claimed.Version, now.Add(2*time.Second)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("open failure vocabulary accepted: %v", err)
	}
}
