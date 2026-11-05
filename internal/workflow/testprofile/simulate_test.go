package testprofile

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestProfileSimulateResponse proves the WF-TEST-003 REFACTOR mechanism: a
// conformance family serves its capability answers from a resolved profile,
// with the author picking the outcome per node for what-if branches.
func TestProfileSimulateResponse(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("load checked-in profiles: %v", err)
	}
	p, err := registry.ResolveForTenant(ProfileRef{ID: "harborcare-conformance", Version: 1}, "acme")
	if err != nil {
		t.Fatalf("resolve conformance profile: %v", err)
	}

	succeeded, err := p.SimulateResponse("read_worker", "hcmnext.conformance.managerchange.read_worker", 1, workflow.OutcomeSucceeded)
	if err != nil {
		t.Fatalf("SUCCEEDED sample: %v", err)
	}
	if succeeded.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("outcome = %s, want SUCCEEDED", succeeded.Outcome)
	}
	active, err := succeeded.Outputs.Get("employment_active")
	if err != nil {
		t.Fatalf("employment_active: %v", err)
	}
	if current, err := active.Bool(); err != nil || !current {
		t.Fatalf("employment_active = %v, %v; want true", active.Text, err)
	}
	manager, err := succeeded.Outputs.Text("current_manager_id")
	if err != nil {
		t.Fatalf("current_manager_id: %v", err)
	}
	if manager != "manager-alice" {
		t.Fatalf("current_manager_id = %q, want manager-alice", manager)
	}

	rejected, err := p.SimulateResponse("read_worker", "hcmnext.conformance.managerchange.read_worker", 1, workflow.OutcomeRejected)
	if err != nil {
		t.Fatalf("REJECTED sample: %v", err)
	}
	if rejected.Outcome != workflow.OutcomeRejected {
		t.Fatalf("picked outcome = %s, want REJECTED", rejected.Outcome)
	}

	if _, err := p.SimulateResponse("read_worker", "hcmnext.conformance.managerchange.read_worker", 1, workflow.OutcomeUnknown); !errors.Is(err, ErrResponseAbsent) {
		t.Fatalf("undeclared outcome error = %v, want ErrResponseAbsent", err)
	}
	if _, err := p.SimulateResponse("read_worker", "hcmnext.conformance.managerchange.read_worker", 2, workflow.OutcomeSucceeded); !errors.Is(err, ErrCapabilityMismatch) {
		t.Fatalf("wrong bound version error = %v, want ErrCapabilityMismatch", err)
	}
}
