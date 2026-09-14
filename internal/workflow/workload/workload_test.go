package workload

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

func limitsWithCost(cost int) Limits {
	l := DefaultSnapshot().Default
	l.MaxCostUnits = cost
	return l
}

func TestResolvePicksTheMostSpecificMatchingRule(t *testing.T) {
	snap := ControlSnapshot{Version: "v1", Default: limitsWithCost(100), Rules: []Rule{
		{Criticality: "P0", Limits: limitsWithCost(500)},
		{TenantID: "t1", Limits: limitsWithCost(200)},
		{TenantID: "t1", CapabilityID: "wf.a", Limits: limitsWithCost(300)},
		{TenantID: "t1", CapabilityID: "wf.a", Criticality: "P0", Limits: limitsWithCost(400)},
	}}
	for _, tc := range []struct {
		tenant, capability, criticality string
		want                            int
	}{
		{"t2", "wf.b", "P3", 100},
		{"t2", "wf.b", "P0", 500},
		{"t1", "wf.b", "P3", 200},
		{"t1", "wf.a", "P3", 300},
		{"t1", "wf.a", "P0", 400},
	} {
		r, err := snap.Resolve(tc.tenant, tc.capability, tc.criticality)
		if err != nil {
			t.Fatalf("Resolve(%s/%s/%s): %v", tc.tenant, tc.capability, tc.criticality, err)
		}
		if r.Limits.MaxCostUnits != tc.want || r.SnapshotVersion != "v1" {
			t.Errorf("Resolve(%s/%s/%s) cost = %d (%s), want %d", tc.tenant, tc.capability, tc.criticality, r.Limits.MaxCostUnits, r.Source, tc.want)
		}
	}
}

func TestResolveRefusesAmbiguousAndInvalidSnapshots(t *testing.T) {
	ambiguous := ControlSnapshot{Version: "v", Default: limitsWithCost(10), Rules: []Rule{
		{TenantID: "t", Limits: limitsWithCost(20)},
		{CapabilityID: "wf", Limits: limitsWithCost(30)},
	}}
	if _, err := ambiguous.Resolve("t", "wf", ""); !errors.Is(err, ErrAmbiguousLimits) {
		t.Fatalf("ambiguous rules = %v, want ErrAmbiguousLimits", err)
	}
	agreeing := ControlSnapshot{Version: "v", Default: limitsWithCost(10), Rules: []Rule{
		{TenantID: "t", Limits: limitsWithCost(20)},
		{CapabilityID: "wf", Limits: limitsWithCost(20)},
	}}
	if _, err := agreeing.Resolve("t", "wf", ""); err != nil {
		t.Fatalf("equally specific rules that agree = %v, want resolved", err)
	}
	if _, err := (ControlSnapshot{Default: limitsWithCost(10)}).Resolve("t", "wf", ""); err == nil {
		t.Fatal("an unversioned snapshot resolved")
	}
	bad := limitsWithCost(10)
	bad.MaxBranches = 0
	if _, err := (ControlSnapshot{Version: "v", Default: bad}).Resolve("t", "wf", ""); err == nil {
		t.Fatal("a zero limit resolved")
	}
	if _, err := (ControlSnapshot{Version: "v", Default: limitsWithCost(10), Rules: []Rule{{Limits: bad}}}).Resolve("t", "wf", ""); err == nil {
		t.Fatal("a rule with a zero limit resolved")
	}
}

func TestDemandForDerivesBranchesChildrenAndCostFromThePlan(t *testing.T) {
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("CompileApproval: %v", err)
	}
	d, err := DemandFor(plan, 2048, 7)
	if err != nil {
		t.Fatal(err)
	}
	if d.PayloadBytes != 2048 || d.ActiveInstances != 7 || d.Branches < 1 || d.CostUnits < len(plan.Nodes) {
		t.Fatalf("demand = %+v for a %d-node plan", d, len(plan.Nodes))
	}
	wide := &workflow.CompiledWorkflow{
		Nodes: []workflow.CompiledNode{{ID: "a", Type: workflow.StepDecision}, {ID: "b", Type: workflow.StepSubworkflow}, {ID: "c", Type: workflow.StepCapability}},
		Edges: []workflow.Edge{{From: "a", To: "b"}, {From: "a", To: "c"}, {From: "a", To: "a"}},
	}
	d, err = DemandFor(wide, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Branches != 3 || d.Children != 1 || d.CostUnits != costOtherNode+costSubworkflow+costCapability {
		t.Fatalf("demand = %+v, want 3 branches, 1 child, cost %d", d, costOtherNode+costSubworkflow+costCapability)
	}
	if _, err := DemandFor(nil, 0, 0); err == nil {
		t.Fatal("DemandFor accepted a nil plan")
	}
	if _, err := DemandFor(wide, -1, 0); err == nil {
		t.Fatal("DemandFor accepted a negative payload")
	}
}

func TestVerdictReasonNamesEveryViolationStably(t *testing.T) {
	v := Evaluate(Resolved{Limits: Limits{MaxBranches: 1, MaxChildren: 1, MaxPayloadBytes: 1, MaxCostUnits: 1, MaxConcurrentInstances: 1}},
		Demand{Branches: 2, Children: 2, PayloadBytes: 2, CostUnits: 2, ActiveInstances: 1})
	reason := v.Reason()
	for _, dim := range []Dimension{DimensionBranches, DimensionChildren, DimensionPayloadBytes, DimensionCostUnits, DimensionConcurrentInstance} {
		if !strings.Contains(reason, string(dim)) {
			t.Errorf("reason %q omits %s", reason, dim)
		}
	}
	if Evaluate(Resolved{Limits: DefaultSnapshot().Default}, Demand{Branches: 1}).Reason() != string(OutcomeAdmitted) {
		t.Error("an admitted verdict's reason is not its outcome")
	}
}
