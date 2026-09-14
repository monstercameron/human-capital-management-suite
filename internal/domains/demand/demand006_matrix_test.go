package demand

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func scaledSupply(t *testing.T, ref, amount string) SupplyReference {
	t.Helper()
	supply := validSupply(t, ref, "1")
	quantity, err := values.NewQuantity(amount, "FTE", 1, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	supply.Quantity = quantity
	return supply
}

func proposeForSupply(t *testing.T, supplyAmount string) IntentProposal {
	t.Helper()
	signal := validSignal(t)
	// Scale-1 quantities throughout: the kernel requires matching
	// declared scales for quantity arithmetic.
	two, err := values.NewQuantity("2", "FTE", 1, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	signal.Quantity = two
	requirement, err := NewCoverageRequirement("coverage-1", signal.Scenario, signal.Version, []DemandSignal{signal})
	if err != nil {
		t.Fatal(err)
	}
	supply := scaledSupply(t, "supply-1", supplyAmount)
	assignments := []SupplyAssignment{{Supply: supply, WorkerRef: "worker-1", Qualified: true, Available: true}}
	partition := mustPartition(t, requirement, assignments, "snapshot-7")
	explanation := mustExplain(t, partition, requirement, assignments)
	if len(explanation.Gaps) != 1 {
		t.Fatalf("gaps=%d", len(explanation.Gaps))
	}
	proposal, err := ProposeIntent(explanation.Gaps[0], IntentParams{
		IntentName: "intent-1", Scope: "loc-nyc", Snapshot: "snapshot-7",
		ApprovalRole: "workforce-planner", Quorum: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

// TestTodo_DEMAND_006_Property: risk derives deterministically from
// the gap share on both sides of every boundary.
func TestTodo_DEMAND_006_Property(t *testing.T) {
	cases := []struct {
		supply string
		kind   IntentKind
		risk   RiskTier
	}{
		{"1", IntentCloseShortfall, RiskHigh},     // short 1/2
		{"1.5", IntentCloseShortfall, RiskMedium}, // short 0.5/2
		{"1.9", IntentCloseShortfall, RiskLow},    // short 0.1/2
		{"3", IntentAbsorbExcess, RiskMedium},     // surplus 1/2
		{"2.1", IntentAbsorbExcess, RiskLow},      // surplus 0.1/2
	}
	for _, tc := range cases {
		proposal := proposeForSupply(t, tc.supply)
		if proposal.Kind != tc.kind || proposal.Risk != tc.risk {
			t.Fatalf("supply %s = %v/%v, want %v/%v", tc.supply, proposal.Kind, proposal.Risk, tc.kind, tc.risk)
		}
	}
	// A costed proposal binds cost and basis together.
	costed, err := ProposeIntent(demand006Gap(t), IntentParams{
		IntentName: "intent-1", Scope: "loc-nyc", Snapshot: "snapshot-7",
		Cost: demandQuantity(t, "50000"), HasCost: true, CostBasis: "rates:burdened-2026",
		ApprovalRole: "workforce-planner", Quorum: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !costed.CostKnown || costed.Cost.String() != "50000 FTE" || costed.CostBasis == "" || costed.Quorum != 2 {
		t.Fatalf("costed=%+v", costed)
	}
}

// TestTodo_DEMAND_006_Golden: the canonical fixture proposal pins its
// exact digest, kind and risk.
func TestTodo_DEMAND_006_Golden(t *testing.T) {
	proposal, err := ProposeIntent(demand006Gap(t), IntentParams{
		IntentName: "close-nyc-nursing-shortfall", Scope: "loc-nyc",
		Snapshot: "snapshot-7", ApprovalRole: "workforce-planner", Quorum: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:916b28b6e91d18433eb5c0fdaa56ea181f1489348cdd296b013bb7d265422d9c"
	if proposal.CanonicalDigest != want {
		t.Fatalf("digest = %q, want %q", proposal.CanonicalDigest, want)
	}
	if proposal.Kind != IntentCloseShortfall || proposal.Risk != RiskHigh {
		t.Fatalf("proposal=%+v", proposal)
	}
	if proposal.Shortfall.String() != "1 FTE" || proposal.Target != "nursing" || proposal.Version != "v1" {
		t.Fatalf("proposal=%+v", proposal)
	}
}

// TestTodo_DEMAND_006_Mutation: uncredible and ungoverned proposals
// refuse on the documented side.
func TestTodo_DEMAND_006_Mutation(t *testing.T) {
	gap := demand006Gap(t)
	valid := IntentParams{
		IntentName: "intent-1", Scope: "loc-nyc", Snapshot: "snapshot-7",
		ApprovalRole: "workforce-planner", Quorum: 1,
	}
	cases := map[string]func(*IntentParams){
		"empty name":     func(p *IntentParams) { p.IntentName = "" },
		"empty scope":    func(p *IntentParams) { p.Scope = "" },
		"empty snapshot": func(p *IntentParams) { p.Snapshot = "" },
		"empty role":     func(p *IntentParams) { p.ApprovalRole = "" },
		"zero quorum":    func(p *IntentParams) { p.Quorum = 0 },
		"cost no basis":  func(p *IntentParams) { p.Cost = demandQuantity(t, "1"); p.HasCost = true },
		"basis no cost":  func(p *IntentParams) { p.CostBasis = "rates:x" },
	}
	for name, mutate := range cases {
		params := valid
		mutate(&params)
		if _, err := ProposeIntent(gap, params); err == nil {
			t.Fatalf("%s proposed", name)
		}
	}
	// Unknown gaps have no credible basis.
	mystery := validSignal(t)
	mystery.ConfidenceClass = ConfidenceUnknown
	mysteryReq, err := NewCoverageRequirement("coverage-9", mystery.Scenario, mystery.Version, []DemandSignal{mystery})
	if err != nil {
		t.Fatal(err)
	}
	supply := validSupply(t, "supply-9", "2")
	assignments := []SupplyAssignment{{Supply: supply, WorkerRef: "w", Qualified: true, Available: true}}
	mysteryPartition := mustPartition(t, mysteryReq, assignments, "snapshot-7")
	mysteryExplanation := mustExplain(t, mysteryPartition, mysteryReq, assignments)
	if _, err := ProposeIntent(mysteryExplanation.Gaps[0], valid); err == nil {
		t.Fatal("unknown gap proposed")
	}
}
