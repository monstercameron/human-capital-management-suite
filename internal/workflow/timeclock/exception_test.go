package timeclock

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestExceptionPeriodDefinition_DeclaresTypedCaptureAndTerminals(t *testing.T) {
	def := ExceptionPeriodDefinition(DefaultParams())
	if def.WorkflowID != ExceptionWorkflowID || def.IntentType != IntentReportExceptions {
		t.Fatalf("identity = %q/%q, want %q/%q", def.WorkflowID, def.IntentType, ExceptionWorkflowID, IntentReportExceptions)
	}
	if def.StartNodeID != ExceptionNodeSeedPattern {
		t.Fatalf("start node = %q, want %q", def.StartNodeID, ExceptionNodeSeedPattern)
	}

	nodes := make(map[string]workflow.Node, len(def.Nodes))
	for _, node := range def.Nodes {
		if _, exists := nodes[node.ID]; exists {
			t.Fatalf("duplicate node id %q", node.ID)
		}
		nodes[node.ID] = node
	}
	for _, id := range []string{
		ExceptionNodeSeedPattern, ExceptionNodeAwaitDeviations, ExceptionNodeRecordDeviation,
		ExceptionNodePrioritize, ExceptionNodeClassify, ExceptionNodeReview, ExceptionNodeApprove,
		ExceptionNodeAwaitReopen, ExceptionNodeEndAccepted, ExceptionNodeEndNoDeviation,
		ExceptionNodeEndRejected, ExceptionNodeEndReopened, ExceptionNodeEndFailed, ExceptionNodeEndCancelled,
	} {
		if _, ok := nodes[id]; !ok {
			t.Fatalf("missing node %q", id)
		}
	}

	signal := nodes[ExceptionNodeAwaitDeviations]
	if signal.Signal == nil || signal.Signal.EventType != ExceptionEventTypeDeviation ||
		signal.Signal.CorrelationKeyExpression != ExceptionCorrelationPeriod {
		t.Fatalf("deviation signal = %#v", signal.Signal)
	}
	if len(signal.Signal.AcceptedSources) != 4 {
		t.Fatalf("deviation sources = %v, want four typed sources", signal.Signal.AcceptedSources)
	}
	if nodes[ExceptionNodeSeedPattern].Capability == nil || nodes[ExceptionNodeSeedPattern].Capability.ID != CapSeedExpectedPattern {
		t.Fatalf("seed capability = %#v", nodes[ExceptionNodeSeedPattern].Capability)
	}
	if nodes[ExceptionNodeRecordDeviation].Capability == nil || nodes[ExceptionNodeRecordDeviation].Capability.ID != CapRecordExceptionLine {
		t.Fatalf("record capability = %#v", nodes[ExceptionNodeRecordDeviation].Capability)
	}
	if nodes[ExceptionNodeReview].Metadata["form_ref"] != FormReviewException {
		t.Fatalf("review form = %q, want %q", nodes[ExceptionNodeReview].Metadata["form_ref"], FormReviewException)
	}
	if len(def.ApprovalRequirements) != 1 || def.ApprovalRequirements[0].ID != ExceptionApprovalResolution {
		t.Fatalf("approval requirements = %#v", def.ApprovalRequirements)
	}
	if len(def.Obligations) != 3 {
		t.Fatalf("obligations = %#v, want period collection, review, and reopen", def.Obligations)
	}
}

func TestExceptionPeriodDefinition_Compiles(t *testing.T) {
	plan, err := CompileDefinition(ExceptionPeriodDefinition(DefaultParams()))
	if err != nil {
		t.Fatalf("exception period definition does not compile: %v", err)
	}
	if plan.WorkflowID != ExceptionWorkflowID || plan.Version != ExceptionPeriodVersion {
		t.Fatalf("compiled identity = %q/%d", plan.WorkflowID, plan.Version)
	}
	if len(plan.Terminals) != 6 {
		t.Fatalf("compiled terminal count = %d, want six explicit outcomes", len(plan.Terminals))
	}
}

func TestExceptionPeriodDefinition_ResolutionMatrix(t *testing.T) {
	def := ExceptionPeriodDefinition(Params{PeriodCloseAfterSeconds: 10, ReopenWindowSeconds: 20, MaxPeriodPasses: 3})
	checks := []struct {
		name  string
		from  string
		to    string
		route string
	}{
		{"timeout closes no deviation", ExceptionNodeAwaitDeviations, ExceptionNodeClassify, "TIMED_OUT"},
		{"needs review", ExceptionNodeClassify, ExceptionNodeReview, ExceptionRouteNeedsReview},
		{"default remains reviewable", ExceptionNodeClassify, ExceptionNodeReview, ExceptionRouteDefaultAtCutoff},
		{"reopen is explicit", ExceptionNodeClassify, ExceptionNodeAwaitReopen, ExceptionRouteReopen},
		{"approval accepts", ExceptionNodeApprove, ExceptionNodeEndAccepted, ExceptionRouteApproved},
		{"approval invalidation reopens", ExceptionNodeApprove, ExceptionNodeAwaitReopen, "INVALIDATED"},
		{"reopen timeout is terminal", ExceptionNodeAwaitReopen, ExceptionNodeEndReopened, "TIMED_OUT"},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if !hasEdge(def.Edges, tc.from, tc.to, tc.route) {
				t.Fatalf("missing edge %s -> %s on %q", tc.from, tc.to, tc.route)
			}
		})
	}
}

func TestExceptionPeriodDefinition_ParamsAreBound(t *testing.T) {
	def := ExceptionPeriodDefinition(Params{TenantScope: "tenant/a", OrganizationScope: "org/a", PeriodCloseAfterSeconds: 77, ReopenWindowSeconds: 88, MaxPeriodPasses: 2})
	if def.TenantScope != "tenant/a" || def.OrganizationScope != "org/a" {
		t.Fatalf("scope = %q/%q", def.TenantScope, def.OrganizationScope)
	}
	for _, node := range def.Nodes {
		if node.Signal == nil {
			continue
		}
		want := uint64(77)
		if node.ID == ExceptionNodeAwaitReopen {
			want = 88
		}
		if node.Signal.CloseAfterSeconds != want {
			t.Fatalf("%s close_after = %d, want %d", node.ID, node.Signal.CloseAfterSeconds, want)
		}
	}
}

func hasEdge(edges []workflow.Edge, from, to, route string) bool {
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.RouteKey == route {
			return true
		}
	}
	return false
}
