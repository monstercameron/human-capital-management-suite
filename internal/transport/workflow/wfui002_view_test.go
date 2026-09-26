package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ironridgeseed"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	workflowcore "github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestIronridgeCatalogPublicationDoesNotLeakToAnotherTenant(t *testing.T) {
	registry := workflowversion.NewRegistry()
	publication, err := platformexecution.PublishIronridgeWorkOrder(registry, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !publicationVisibleToTenant(publication, ironridgeseed.TenantKey) || publicationVisibleToTenant(publication, transporttest.Tenant) {
		t.Fatal("Ironridge publication was not scoped to Ironridge")
	}
	srv := &server{deps: Dependencies{Definitions: registry, Authorize: allowWorkflowCalls}}
	catalog, err := srv.ListWorkflowPublications(workflowTestContext(t, ListWorkflowPublicationsProcedure), &workflowv1.ListWorkflowPublicationsRequest{})
	if err != nil || len(catalog.GetPublications()) != 0 {
		t.Fatalf("other tenant catalog = %+v, %v", catalog, err)
	}
	_, err = srv.GetWorkflowDefinitionView(workflowTestContext(t, GetWorkflowDefinitionViewProcedure), &workflowv1.GetWorkflowDefinitionViewRequest{WorkflowId: publication.WorkflowID})
	if owned, ok := envelope.As(err); !ok || owned.Code() != envelope.CodeNotFound {
		t.Fatalf("other tenant definition = %v, want not found", err)
	}
}

func TestTodo_WF_UI_002_TransportCatalogAndLiveInspectorOverlay(t *testing.T) {
	registry, publication := wfui002Publication(t)
	live := wfui002Live(publication)
	reader := &workflowTestReader{record: Record{
		Instance: Instance{InstanceID: "instance-42", TenantID: transporttest.Tenant, WorkflowID: publication.WorkflowID,
			WorkflowVersion: publication.DefinitionVersion, CompiledPlanDigest: publication.CompiledPlanDigest},
		Inspector: &live,
	}}
	srv := &server{deps: Dependencies{Definitions: registry, Instances: reader, Authorize: allowWorkflowCalls}}

	catalog, err := srv.ListWorkflowPublications(workflowTestContext(t, ListWorkflowPublicationsProcedure), &workflowv1.ListWorkflowPublicationsRequest{})
	if err != nil {
		t.Fatalf("ListWorkflowPublications: %v", err)
	}
	if len(catalog.GetPublications()) != 1 {
		t.Fatalf("catalog = %+v", catalog.GetPublications())
	}
	entry := catalog.GetPublications()[0]
	if entry.GetWorkflowId() != prototype.ApprovalWorkflowID || entry.GetName() != "Prototype promotion approval" || entry.GetStatus() != "ACTIVE" {
		t.Fatalf("catalog entry = %+v", entry)
	}

	got, err := srv.GetWorkflowDefinitionView(workflowTestContext(t, GetWorkflowDefinitionViewProcedure), &workflowv1.GetWorkflowDefinitionViewRequest{InstanceId: "instance-42"})
	if err != nil {
		t.Fatalf("GetWorkflowDefinitionView: %v", err)
	}
	view := got.GetView()
	if view.GetWorkflowId() != publication.WorkflowID || view.GetInstanceId() != "instance-42" || !view.GetRunDisclosed() || !view.GetComplete() {
		t.Fatalf("view identity = %+v", view)
	}
	if len(view.GetNodes()) != 6 || view.GetNodes()[0].GetState() != "waiting" || !view.GetNodes()[0].GetCurrent() {
		t.Fatalf("live nodes = %+v", view.GetNodes())
	}
}

func TestTodo_WF_UI_002_SecurityRefusesBeforeCatalogOrInstanceRead(t *testing.T) {
	registry, _ := wfui002Publication(t)
	spy := &wfui002DefinitionSpy{DefinitionReader: registry}
	reader := &workflowTestReader{}
	srv := &server{deps: Dependencies{
		Definitions: spy, Instances: reader,
		Authorize: func(context.Context, *trust.Principal, string) bool { return false },
	}}
	if _, err := srv.ListWorkflowPublications(workflowTestContext(t, ListWorkflowPublicationsProcedure), &workflowv1.ListWorkflowPublicationsRequest{}); !wfui002Denied(err) {
		t.Fatalf("catalog denial = %v", err)
	}
	if _, err := srv.GetWorkflowDefinitionView(workflowTestContext(t, GetWorkflowDefinitionViewProcedure), &workflowv1.GetWorkflowDefinitionViewRequest{WorkflowId: prototype.ApprovalWorkflowID}); !wfui002Denied(err) {
		t.Fatalf("view denial = %v", err)
	}
	if spy.calls.Load() != 0 || reader.calls.Load() != 0 {
		t.Fatalf("denied request reached readers: definitions=%d instances=%d", spy.calls.Load(), reader.calls.Load())
	}
}

func TestTodo_WF_UI_002_TransportGolden(t *testing.T) {
	registry, publication := wfui002Publication(t)
	srv := &server{deps: Dependencies{Definitions: registry, Authorize: allowWorkflowCalls}}
	got, err := srv.GetWorkflowDefinitionView(workflowTestContext(t, GetWorkflowDefinitionViewProcedure), &workflowv1.GetWorkflowDefinitionViewRequest{WorkflowId: publication.WorkflowID})
	if err != nil {
		t.Fatalf("GetWorkflowDefinitionView: %v", err)
	}
	var rendered strings.Builder
	view := got.GetView()
	fmt.Fprintf(&rendered, "%s|%s|v%d|%s|%s\n", view.GetWorkflowId(), view.GetName(), view.GetDefinitionVersion(), view.GetSemanticVersion(), view.GetPublicationStatus())
	for _, node := range view.GetNodes() {
		fmt.Fprintf(&rendered, "%s|%s|%s|d%d|l%d|%s\n", node.GetId(), node.GetLabel(), node.GetStepType(), node.GetDepth(), node.GetLane(), node.GetState())
	}
	const want = `hcmnext.workflows.prototype.promotion_approval|Prototype promotion approval|v1|1.0.0|ACTIVE
approve_promotion|Approve Promotion|APPROVAL|d0|l0|not-started
end_approved|End Approved|END|d1|l0|not-started
end_rejected|End Rejected|END|d1|l1|not-started
end_invalidated|End Invalidated|END|d1|l2|not-started
end_expired|End Expired|END|d1|l3|not-started
end_cancelled|End Cancelled|END|d1|l4|not-started
`
	if rendered.String() != want {
		t.Fatalf("transport golden drifted\n got:\n%s\nwant:\n%s", rendered.String(), want)
	}
}

type wfui002DefinitionSpy struct {
	DefinitionReader
	calls atomic.Int64
}

func (s *wfui002DefinitionSpy) ListAll() ([]workflowversion.CompiledVersion, error) {
	s.calls.Add(1)
	return s.DefinitionReader.ListAll()
}

func (s *wfui002DefinitionSpy) GetByDigest(digest string) (workflowversion.CompiledVersion, bool, error) {
	s.calls.Add(1)
	return s.DefinitionReader.GetByDigest(digest)
}

func (s *wfui002DefinitionSpy) GetActiveForWorkflow(workflowID string) (workflowversion.CompiledVersion, bool, error) {
	s.calls.Add(1)
	return s.DefinitionReader.GetActiveForWorkflow(workflowID)
}

func (s *wfui002DefinitionSpy) List(workflowID string) ([]workflowversion.CompiledVersion, error) {
	s.calls.Add(1)
	return s.DefinitionReader.List(workflowID)
}

func wfui002Denied(err error) bool {
	owned, ok := envelope.As(err)
	return ok && owned.Code() == envelope.CodePermissionDenied
}

func wfui002Publication(t *testing.T) (*workflowversion.Registry, workflowversion.CompiledVersion) {
	t.Helper()
	definition := prototype.ApprovalDefinition()
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("CompileApproval: %v", err)
	}
	registry := workflowversion.NewRegistry()
	published, err := workflowversion.Publish(registry, definition, plan, workflowcore.Options{
		Phase: workflowcore.PhaseP1B, IRSchemaVersion: prototype.ApprovalIRSchemaV1,
	}, workflowversion.PublishMeta{
		SemanticVersion: "1.0.0", PublishedAt: time.Date(2026, 9, 19, 15, 4, 5, 0, time.UTC), PublishedBy: "principal:release-manager",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	active, err := workflowversion.Activate(registry, published.CompiledPlanDigest, workflowversion.ActivationEvidence{
		ApprovedBy: "principal:reviewer", Authority: "role:change-governance", Reason: "fixture",
		ApprovedAt: published.PublishedAt.Add(time.Minute), ReviewedPlanDigest: published.CompiledPlanDigest,
		Authorized: true, TestsPassed: true,
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	return registry, active
}

func wfui002Live(publication workflowversion.CompiledVersion) inspect.View {
	started := publication.PublishedAt.Add(2 * time.Minute)
	return inspect.View{
		Definition:   inspect.DefinitionView{Disclosed: true, WorkflowID: publication.WorkflowID, WorkflowVersion: publication.DefinitionVersion, CompiledPlanHash: publication.CompiledPlanDigest},
		Instance:     inspect.InstanceView{Disclosed: true, InstanceID: "instance-42", RuntimeStatus: "RUNNING"},
		Frontier:     []inspect.FrontierEntry{{NodeID: prototype.NodeApproval, AttemptRecorded: true, Attempt: 1, Status: "WAITING", StepType: "APPROVAL"}},
		Nodes:        []inspect.NodeView{{NodeID: prototype.NodeApproval, Attempt: 1, StepType: "APPROVAL", Status: "WAITING", Current: true, StartedAt: &started}},
		Completeness: inspect.Completeness{Complete: true},
	}
}
