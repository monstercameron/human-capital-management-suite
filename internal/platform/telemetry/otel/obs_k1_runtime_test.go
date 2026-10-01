package otel_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/observability"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/diagnostic"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/queue"
)

func servingContracts(t *testing.T) hcmotel.RuntimeContracts {
	t.Helper()
	h := newTestHarness(t, testEvaluator(t))
	t.Cleanup(func() { _ = h.Provider.Shutdown(context.Background()) })
	return h.Provider.RuntimeContracts()
}

func TestTodo_OBS_003_Integration(t *testing.T) {
	contracts := servingContracts(t)
	if contracts.Backends == nil {
		t.Fatal("serving provider did not compose telemetry backends")
	}
	if qualification := contracts.Backends.Qualify(); !qualification.Complete {
		t.Fatalf("serving backend qualification=%+v, want complete", qualification)
	}
}

func TestTodo_OBS_010_ServedRegistry(t *testing.T) {
	contracts := servingContracts(t)
	if contracts.Logs == nil {
		t.Fatal("serving provider did not compose the semantic log registry")
	}
	for _, outcome := range []observability.Outcome{
		observability.OutcomeSuccess,
		observability.OutcomeFailure,
		observability.OutcomePartial,
		observability.OutcomeUnknown,
		observability.OutcomeDenied,
		observability.OutcomeCancelled,
		observability.OutcomeDegraded,
	} {
		var fields *observability.ErrorFields
		if outcome == observability.OutcomeFailure {
			fields = &observability.ErrorFields{Code: "EXPORT_FAILED", Type: "exporter", Retryable: true}
		}
		event, err := contracts.Logs.Resolve("telemetry.signal", outcome, fields)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", outcome, err)
		}
		if event.Severity != observability.DefaultSeverity(outcome) {
			t.Fatalf("Resolve(%s) severity=%q, want %q", outcome, event.Severity, observability.DefaultSeverity(outcome))
		}
	}
}

func TestTodo_OBS_018_Integration(t *testing.T) {
	contracts := servingContracts(t)
	if contracts.Diagnostic == nil {
		t.Fatal("serving provider did not compose diagnostic governance")
	}
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	request := diagnostic.Request{
		Revision: 1, Actor: "operator-a", Approver: "operator-b", Purpose: "incident-123",
		Scope: "tenant:opaque-a", Level: diagnostic.LevelDebug, VolumeBudget: 1,
		StartsAt: start, ExpiresAt: start.Add(30 * time.Minute), Signature: "signed-revision-1",
	}
	snapshot, err := contracts.Diagnostic.Apply(request)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if decision := contracts.Diagnostic.Decide(snapshot.Scope, request.Level, start.Add(time.Minute)); !decision.Allowed {
		t.Fatalf("active diagnostic decision=%+v", decision)
	}
}

func TestTodo_OBS_019_Integration(t *testing.T) {
	contracts := servingContracts(t)
	if contracts.Queue == nil {
		t.Fatal("serving provider did not compose bounded telemetry queue")
	}
	if result, err := contracts.Queue.Submit(queue.Item{ID: "serving-item", Priority: queue.PriorityNormal, Digest: "digest-serving-item"}); err != nil || !result.Accepted {
		t.Fatalf("Submit result=%+v err=%v", result, err)
	}
	if health := contracts.Queue.Health(); health.QueueDepth != 1 || !health.Degraded {
		t.Fatalf("queue health=%+v, want one queued item reported as degraded until export", health)
	}
}
