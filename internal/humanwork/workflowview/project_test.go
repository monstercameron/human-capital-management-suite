package workflowview_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var viewInstant = time.Date(2026, 9, 19, 15, 4, 5, 0, time.UTC)

func TestTodo_WF_UI_002(t *testing.T) {
	publication := publishedApproval(t)
	live := liveApproval(publication)

	got, err := workflowview.Build(publication, &live)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.WorkflowID != prototype.ApprovalWorkflowID || got.Name != "Prototype promotion approval" ||
		got.Version != 1 || got.SemanticVersion != "1.0.0" || got.PublicationStatus != "ACTIVE" {
		t.Fatalf("publication identity = %+v", got)
	}
	if len(got.Nodes) != 6 || len(got.Edges) != 5 {
		t.Fatalf("projection sizes = %d nodes, %d edges", len(got.Nodes), len(got.Edges))
	}
	if got.Nodes[0].ID != prototype.NodeApproval || !got.Nodes[0].Start || got.Nodes[0].State != workflowview.NodeWaiting || !got.Nodes[0].Current {
		t.Fatalf("approval overlay = %+v", got.Nodes[0])
	}
	if got.Nodes[1].ID != prototype.NodeApproved || got.Nodes[1].State != workflowview.NodeNotStarted || !got.Nodes[1].Terminal {
		t.Fatalf("approved terminal = %+v", got.Nodes[1])
	}
	wantRoutes := []workflowview.Route{
		{Key: "APPROVED", TargetID: prototype.NodeApproved},
		{Key: "CANCELLED", TargetID: prototype.NodeCancelled},
		{Key: "EXPIRED", TargetID: prototype.NodeExpired},
		{Key: "INVALIDATED", TargetID: prototype.NodeInvalidated},
		{Key: "REJECTED", TargetID: prototype.NodeRejected},
	}
	if !reflect.DeepEqual(got.Nodes[0].Routes, wantRoutes) {
		t.Fatalf("routes = %+v, want %+v", got.Nodes[0].Routes, wantRoutes)
	}
	if !got.HasRun || !got.RunDisclosed || got.InstanceID != "instance-42" || got.RuntimeStatus != "RUNNING" || !got.Completeness {
		t.Fatalf("run identity = %+v", got)
	}
	assertEveryEdgeAddressesNode(t, got)
}

func TestTodo_WF_UI_002_RejectsMismatchedOrMalformedEvidence(t *testing.T) {
	publication := publishedApproval(t)
	live := liveApproval(publication)
	live.Definition.CompiledPlanHash = "sha256:another-plan"
	if _, err := workflowview.Build(publication, &live); !errors.Is(err, workflowview.ErrRunMismatch) {
		t.Fatalf("mismatch error = %v", err)
	}

	tampered := publication
	tampered.CanonicalPlanBytes = append([]byte(nil), publication.CanonicalPlanBytes...)
	tampered.CanonicalPlanBytes[10] ^= 0xff
	if _, err := workflowview.Build(tampered, nil); !errors.Is(err, workflowview.ErrInvalidPublication) {
		t.Fatalf("tampered publication error = %v", err)
	}
}

func TestTodo_WF_UI_002_RedactedRunNeverLeaksNodeState(t *testing.T) {
	publication := publishedApproval(t)
	live := liveApproval(publication)
	live.Definition = inspect.DefinitionView{Disclosed: false, DeniedReason: "policy.workflow.definition"}
	live.Completeness = inspect.Completeness{Complete: false, Redactions: []string{"definition", "nodes"}}

	got, err := workflowview.Build(publication, &live)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !got.HasRun || got.RunDisclosed || got.Completeness {
		t.Fatalf("redacted run state = %+v", got)
	}
	for _, node := range got.Nodes {
		if node.RuntimeKnown || node.Status != "" || node.Attempt != 0 {
			t.Fatalf("redacted runtime leaked through node %+v", node)
		}
	}
	if !reflect.DeepEqual(got.Redactions, []string{"definition", "nodes"}) {
		t.Fatalf("redactions = %v", got.Redactions)
	}
}

func TestTodo_WF_UI_002_Golden(t *testing.T) {
	publication := publishedApproval(t)
	live := liveApproval(publication)
	got, err := workflowview.Build(publication, &live)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var rendered strings.Builder
	fmt.Fprintf(&rendered, "%s|%s|v%d|%s|%s|%s\n", got.WorkflowID, got.Name, got.Version, got.SemanticVersion, got.PublicationStatus, got.RuntimeStatus)
	for _, node := range got.Nodes {
		fmt.Fprintf(&rendered, "node|%s|%s|%s|d%d|l%d|%s|a%d|start=%t|terminal=%t|current=%t",
			node.ID, node.Label, node.StepType, node.Depth, node.Lane, node.State, node.Attempt, node.Start, node.Terminal, node.Current)
		for _, route := range node.Routes {
			fmt.Fprintf(&rendered, "|%s>%s", route.Key, route.TargetID)
		}
		rendered.WriteByte('\n')
	}
	for _, edge := range got.Edges {
		fmt.Fprintf(&rendered, "edge|%s|%s|%s>%s\n", edge.ID, edge.RouteKey, edge.FromID, edge.ToID)
	}
	const expected = `hcmnext.workflows.prototype.promotion_approval|Prototype promotion approval|v1|1.0.0|ACTIVE|RUNNING
node|approve_promotion|Approve Promotion|APPROVAL|d0|l0|waiting|a1|start=true|terminal=false|current=true|APPROVED>end_approved|CANCELLED>end_cancelled|EXPIRED>end_expired|INVALIDATED>end_invalidated|REJECTED>end_rejected
node|end_approved|End Approved|END|d1|l0|not-started|a0|start=false|terminal=true|current=false
node|end_rejected|End Rejected|END|d1|l1|not-started|a0|start=false|terminal=true|current=false
node|end_invalidated|End Invalidated|END|d1|l2|not-started|a0|start=false|terminal=true|current=false
node|end_expired|End Expired|END|d1|l3|not-started|a0|start=false|terminal=true|current=false
node|end_cancelled|End Cancelled|END|d1|l4|not-started|a0|start=false|terminal=true|current=false
edge|edge-000-approve_promotion-approved-end_approved|APPROVED|approve_promotion>end_approved
edge|edge-001-approve_promotion-cancelled-end_cancelled|CANCELLED|approve_promotion>end_cancelled
edge|edge-002-approve_promotion-expired-end_expired|EXPIRED|approve_promotion>end_expired
edge|edge-003-approve_promotion-invalidated-end_invalidated|INVALIDATED|approve_promotion>end_invalidated
edge|edge-004-approve_promotion-rejected-end_rejected|REJECTED|approve_promotion>end_rejected
`
	if rendered.String() != expected {
		t.Fatalf("golden drifted\n got:\n%s\nwant:\n%s", rendered.String(), expected)
	}
}

func publishedApproval(t *testing.T) version.CompiledVersion {
	t.Helper()
	definition := prototype.ApprovalDefinition()
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("CompileApproval: %v", err)
	}
	registry := version.NewRegistry()
	published, err := version.Publish(registry, definition, plan, workflow.Options{Phase: workflow.PhaseP1B}, version.PublishMeta{
		SemanticVersion: "1.0.0", PublishedAt: viewInstant, PublishedBy: "principal:release-manager",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	active, err := version.Activate(registry, published.CompiledPlanDigest, version.ActivationEvidence{
		ApprovedBy: "principal:reviewer", Authority: "role:change-governance", Reason: "fixture",
		ApprovedAt: viewInstant.Add(time.Minute), ReviewedPlanDigest: published.CompiledPlanDigest,
		Authorized: true, TestsPassed: true,
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	return active
}

func liveApproval(publication version.CompiledVersion) inspect.View {
	started := viewInstant.Add(2 * time.Minute)
	return inspect.View{
		Definition: inspect.DefinitionView{
			Disclosed: true, WorkflowID: publication.WorkflowID,
			WorkflowVersion: publication.DefinitionVersion, CompiledPlanHash: publication.CompiledPlanDigest,
		},
		Instance:     inspect.InstanceView{Disclosed: true, InstanceID: "instance-42", RuntimeStatus: "RUNNING"},
		Frontier:     []inspect.FrontierEntry{{NodeID: prototype.NodeApproval, AttemptRecorded: true, Attempt: 1, Status: "WAITING", StepType: "APPROVAL"}},
		Nodes:        []inspect.NodeView{{NodeID: prototype.NodeApproval, Attempt: 1, StepType: "APPROVAL", Status: "WAITING", Current: true, StartedAt: &started}},
		Completeness: inspect.Completeness{Complete: true},
	}
}

func assertEveryEdgeAddressesNode(t *testing.T, view workflowview.View) {
	t.Helper()
	ids := make(map[string]bool, len(view.Nodes))
	for _, node := range view.Nodes {
		if ids[node.ID] {
			t.Fatalf("duplicate projected node %q", node.ID)
		}
		ids[node.ID] = true
	}
	for _, edge := range view.Edges {
		if !ids[edge.FromID] || !ids[edge.ToID] || strings.TrimSpace(edge.RouteKey) == "" {
			t.Fatalf("invalid projected edge %+v", edge)
		}
	}
}
