package managerchange

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

type stubSink struct{}

func (stubSink) RecordInvocation(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return "ev-test", nil
}

func (stubSink) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return "ev-test", nil
}

func testInstant() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }

// TestManagerChangeLoadsFromProfile proves the WF-TEST-003 REFACTOR for the
// Manager Change family: capability answers come from the checked-in
// harborcare-conformance profile instead of hand-written literals, input
// echoes stay explicit in the handler, and the author picks outcomes per
// node for what-if branches.
func TestManagerChangeLoadsFromProfile(t *testing.T) {
	env := NewEnvironment()
	registry, err := env.Registry()
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	gateway := capability.NewGateway(registry, stubSink{})

	invoke := func(nodeID, capabilityID, inputPath, inputValue string) simulate.CapabilityResponse {
		t.Helper()
		worker := simulate.NewBranded("WorkerID", inputValue)
		res, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
			Capability: capability.Key{ID: capabilityID, Version: 1},
			Payload: simulate.CapabilityRequest{
				NodeID:     nodeID,
				Capability: capability.Key{ID: capabilityID, Version: 1},
				Inputs:     simulate.Bag{inputPath: worker},
				Outputs:    nil,
				At:         testInstant(),
			},
			Authorization: capability.Authorization{
				Decision: capability.Allow, Scopes: []string{"scope:people.read"},
				Reason: "test", SubjectRef: "test:profile", Tenant: "acme",
			},
		})
		if err != nil {
			t.Fatalf("invoke %s: %v", nodeID, err)
		}
		response, ok := res.Response.(simulate.CapabilityResponse)
		if !ok {
			t.Fatalf("response is %T, want simulate.CapabilityResponse", res.Response)
		}
		return response
	}

	worker := invoke(NodeReadWorker, CapabilityWorker, "worker_id", "worker-jane")
	if worker.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("default outcome = %s, want SUCCEEDED", worker.Outcome)
	}
	if got, _ := worker.Outputs.Text("worker_id"); got != "worker-jane" {
		t.Fatalf("worker_id = %q, want the explicit input echo", got)
	}
	if got, _ := worker.Outputs.Text("current_manager_id"); got != "manager-alice" {
		t.Fatalf("current_manager_id = %q, want the profile sample", got)
	}

	manager := invoke(NodeReadManager, CapabilityManager, "proposed_manager_id", "manager-bob")
	if manager.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("default outcome = %s, want SUCCEEDED", manager.Outcome)
	}
	if got, _ := manager.Outputs.Text("proposed_manager_id"); got != "manager-bob" {
		t.Fatalf("proposed_manager_id = %q, want the explicit input echo", got)
	}
	if eligible, err := manager.Outputs.Get("management_eligible"); err != nil {
		t.Fatalf("management_eligible: %v", err)
	} else if ok, err := eligible.Bool(); err != nil || !ok {
		t.Fatalf("management_eligible = %v, %v; want the profile sample true", eligible.Text, err)
	}

	// What-if: the author picks REJECTED for one node only.
	env.Outcomes[NodeReadWorker] = workflow.OutcomeRejected
	rejected := invoke(NodeReadWorker, CapabilityWorker, "worker_id", "worker-jane")
	if rejected.Outcome != workflow.OutcomeRejected {
		t.Fatalf("picked outcome = %s, want REJECTED", rejected.Outcome)
	}
	still := invoke(NodeReadManager, CapabilityManager, "proposed_manager_id", "manager-bob")
	if still.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("unpicked node outcome = %s, want SUCCEEDED", still.Outcome)
	}
}
