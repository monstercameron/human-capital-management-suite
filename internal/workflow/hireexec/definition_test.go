package hireexec

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const (
	newHirePlanDigestV1_0 = "9846950ec069fb6e9b62cd4d88b271b2e17018ac26fa13da84a4372323a947de"
	newHirePlanDigestV1_1 = "e7ee18718007d99900f0067a398a1171b5ec9119286a0079fdc4ea6b6baa26ca"
)

// TestTodo_WF_HIRE_001 compiles the definition and proves its compiled shape:
// identity, start node, the full node set, exactly one AUTHORITATIVE_CORE
// node, every declared END reachable from the start, and a plan digest that
// is stable across two independent compiles of the same definition.
func TestTodo_WF_HIRE_001(t *testing.T) {
	def := Definition()
	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if plan.WorkflowID != WorkflowID || plan.Version != Version {
		t.Fatalf("identity = %s/%d, want %s/%d", plan.WorkflowID, plan.Version, WorkflowID, Version)
	}
	if plan.Phase != workflow.PhaseP1B || plan.TerminalProfile != workflow.TerminalProfileExecute {
		t.Fatalf("phase/profile = %s/%s, want P1B/EXECUTE", plan.Phase, plan.TerminalProfile)
	}
	if plan.StartNodeID != NodePrepareHire {
		t.Fatalf("start node = %s, want %s", plan.StartNodeID, NodePrepareHire)
	}

	wantNodes := map[string]bool{}
	for _, n := range def.Nodes {
		wantNodes[n.ID] = true
	}
	gotNodes := map[string]bool{}
	for _, n := range plan.Nodes {
		gotNodes[n.ID] = true
	}
	if !reflect.DeepEqual(gotNodes, wantNodes) {
		t.Fatalf("compiled node set = %v, want %v", gotNodes, wantNodes)
	}

	cores := plan.NodesWithRole(workflow.RoleAuthoritativeCore)
	if len(cores) != 1 || cores[0] != NodeCommitHire {
		t.Fatalf("authoritative-core nodes = %v, want exactly [%s]", cores, NodeCommitHire)
	}

	ends := []string{
		NodeEndHired, NodeEndOfferRejected, NodeEndOfferWithdrawn, NodeEndBackgroundCheckExpired,
		NodeEndCancelled, NodeEndExpired, NodeEndInvalidated, NodeEndFailed,
	}
	reachable := map[string]bool{}
	for _, id := range plan.Reachability.Order {
		reachable[id] = true
	}
	for _, id := range ends {
		if !reachable[id] {
			t.Errorf("terminal %s is not reachable from the start node", id)
		}
	}

	plan2, err := Compile()
	if err != nil {
		t.Fatalf("second Compile: %v", err)
	}
	if plan.Digest() != plan2.Digest() {
		t.Fatalf("plan digest is not stable across compiles: %q vs %q", plan.Digest(), plan2.Digest())
	}
}

// hireSuccessRoute names, for each non-terminal node on the documented main
// path, the route key its success outcome takes.
var hireSuccessRoute = map[string]string{
	NodePrepareHire:             string(workflow.OutcomeSucceeded),
	NodeApproveOffer:            "APPROVED",
	NodeAwaitBackgroundCheck:    string(workflow.OutcomeSucceeded),
	NodeEvaluateBackgroundCheck: RouteBackgroundCheckClear,
	NodeCollectNewHireForms:     string(workflow.OutcomeSucceeded),
	NodeProvisionITAccess:       string(workflow.OutcomeSucceeded),
	NodeProvisionWorkspace:      string(workflow.OutcomeSucceeded),
	NodeEnrollPayroll:           string(workflow.OutcomeSucceeded),
	NodeAwaitStartDate:          string(workflow.OutcomeSucceeded),
	NodeCommitHire:              string(workflow.OutcomeSucceeded),
}

// TestTodo_WF_HIRE_001_Golden walks the definition's own edges from the start
// node to end_hired, following each node's documented success route, and
// pins the exact ordered path against [NodeOrder]. A route silently
// re-plumbed to a different node changes this list, which is the point.
func TestTodo_WF_HIRE_001_Golden(t *testing.T) {
	def := Definition()
	byFrom := map[string][]workflow.Edge{}
	for _, e := range def.Edges {
		byFrom[e.From] = append(byFrom[e.From], e)
	}

	var got []string
	node := def.StartNodeID
	for {
		got = append(got, node)
		if node == NodeEndHired {
			break
		}
		route, ok := hireSuccessRoute[node]
		if !ok {
			t.Fatalf("no documented success route for node %s", node)
		}
		next := ""
		for _, e := range byFrom[node] {
			if e.RouteKey == route {
				next = e.To
				break
			}
		}
		if next == "" {
			t.Fatalf("no edge for %s on route %q", node, route)
		}
		node = next
	}

	want := NodeOrder()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("main path = %v, want %v", got, want)
	}
}

// A publisher recompiles with CompileOptions; it must reproduce Compile's plan.
func TestCompileOptionsReproduceTheCompiledPlan(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatal(err)
	}
	again, err := workflow.Compile(Definition(), CompileOptions())
	if err != nil || again.Digest() != plan.Digest() {
		t.Fatalf("CompileOptions plan = %v, %v; want digest %s", again, err, plan.Digest())
	}
}

func TestNewHirePublishedVersionsPreserveFrozenPlan(t *testing.T) {
	frozen, err := CompileV1_0()
	if err != nil {
		t.Fatalf("CompileV1_0: %v", err)
	}
	if frozen.WorkflowID != WorkflowID || frozen.Version != VersionV1_0 || frozen.SchemaVersion() != 1 {
		t.Fatalf("frozen plan identity/schema = %s/%d/%d, want %s/%d/1", frozen.WorkflowID, frozen.Version, frozen.SchemaVersion(), WorkflowID, VersionV1_0)
	}
	commit, ok := frozen.Node(NodeCommitHire)
	if !ok || commit.Capability == nil || commit.Capability.Version != 1 || commit.InputSchema.Version != 1 || commit.OutputSchema.Version != 1 {
		t.Fatalf("frozen commit binding = %+v, want capability and schemas v1", commit)
	}

	current, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := frozen.Digest(); got != newHirePlanDigestV1_0 {
		t.Fatalf("frozen 1.0.0 digest = %q, want persisted %q", got, newHirePlanDigestV1_0)
	}
	if current.WorkflowID != WorkflowID || current.Version != Version || current.SchemaVersion() != workflow.CurrentIRSchemaVersion {
		t.Fatalf("current plan identity/schema = %s/%d/%d, want %s/%d/%d", current.WorkflowID, current.Version, current.SchemaVersion(), WorkflowID, Version, workflow.CurrentIRSchemaVersion)
	}
	if frozen.Digest() == current.Digest() {
		t.Fatalf("frozen and current plans share digest %q", frozen.Digest())
	}
	if got := current.Digest(); got != newHirePlanDigestV1_1 {
		t.Fatalf("current 1.1.0 digest = %q, want pinned %q", got, newHirePlanDigestV1_1)
	}
	currentCommit, ok := current.Node(NodeCommitHire)
	if !ok || currentCommit.Capability == nil || currentCommit.Capability.Version != 1 || currentCommit.InputSchema.Version != 1 || currentCommit.OutputSchema.Version != 1 {
		t.Fatalf("current commit binding = %+v, want unchanged capability and schemas v1", currentCommit)
	}
}
