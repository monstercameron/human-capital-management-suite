package designeredit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// singleBlockService is a draft holding exactly one authored block, which is
// also its start node. It is the only shape in which the START lock yields.
func singleBlockService(t *testing.T) (*designeredit.Service, *memoryDraftStore, designeredit.View) {
	t.Helper()
	store := &memoryDraftStore{}
	service := &designeredit.Service{
		Store:   store,
		Catalog: fixedCatalog{{ID: "kernel.task", Version: 1, Name: "Task", Kind: designerpalette.KindBlock, StepType: workflow.StepTask}},
		NewID:   func() (string, error) { return "01999f37-9f42-7000-8000-000000000201", nil },
	}
	created, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{Name: "Single"})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	inserted, err := service.Insert(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.InsertRequest{
		DraftID: created.Draft.DraftID, ExpectedRevision: created.Draft.Revision, EntryID: "kernel.task", EntryVersion: 1,
	})
	if err != nil {
		t.Fatalf("insert block: %v", err)
	}
	if len(inserted.Draft.Nodes) != 1 || inserted.Draft.StartNodeID != inserted.Draft.Nodes[0].ID {
		t.Fatalf("single block draft = %+v", inserted.Draft)
	}
	return service, store, inserted.Draft
}

func TestRemoveNodeDeletesEdgesAndDependentMappings(t *testing.T) {
	service, _, draft := refinementService(t)
	before := draftNode(t, draft, promotionexec.NodeRaiseThreshold)
	if bindingSource(before, "raise_ratio") != promotionexec.NodeSimulateCompensation {
		t.Fatalf("fixture no longer binds raise_ratio to %q: %+v", promotionexec.NodeSimulateCompensation, before.Bindings)
	}
	if !touchesNode(draft, promotionexec.NodeSimulateCompensation) {
		t.Fatal("fixture has no edge touching the node under test")
	}

	change, err := service.RemoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RemoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: promotionexec.NodeSimulateCompensation,
	})
	if err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}
	if change.Draft.Revision != draft.Revision+1 {
		t.Fatalf("revision = %d, want %d", change.Draft.Revision, draft.Revision+1)
	}
	for _, node := range change.Draft.Nodes {
		if node.ID == promotionexec.NodeSimulateCompensation {
			t.Fatal("removed node is still projected")
		}
	}
	if touchesNode(change.Draft, promotionexec.NodeSimulateCompensation) {
		t.Fatalf("edges still reference the removed node: %+v", change.Draft.Edges)
	}
	after := draftNode(t, change.Draft, promotionexec.NodeRaiseThreshold)
	if source := bindingSource(after, "raise_ratio"); source != "" {
		t.Fatalf("raise_ratio still bound to %q after its producer was removed", source)
	}
	if change.Draft.HistoryLabel != "" && !strings.Contains(change.Draft.HistoryLabel, promotionexec.NodeSimulateCompensation) {
		t.Fatalf("history label = %q, want the removed node named", change.Draft.HistoryLabel)
	}
}

func TestRemoveNodeRefusesLockedPhasesAndStaleOrForeignCallers(t *testing.T) {
	service, store, draft := refinementService(t)
	locked := 0
	for _, node := range draft.Nodes {
		if !node.Locked {
			continue
		}
		locked++
		beforeDocument := append([]byte(nil), store.draft.Document...)
		_, err := service.RemoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RemoveNodeRequest{
			DraftID: draft.DraftID, ExpectedRevision: store.draft.Revision, NodeID: node.ID,
		})
		if !errors.Is(err, designeredit.ErrInvalid) {
			t.Fatalf("locked %s node %q removal = %v, want ErrInvalid", node.LockKind, node.ID, err)
		}
		if string(store.draft.Document) != string(beforeDocument) || store.draft.Revision != draft.Revision {
			t.Fatalf("refused removal of %q changed the draft", node.ID)
		}
	}
	if locked == 0 {
		t.Fatal("promotion fixture has no locked node")
	}
	if _, err := service.RemoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RemoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: draft.StartNodeID,
	}); !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("start node removal in a populated draft = %v, want ErrInvalid", err)
	}
	if _, err := service.RemoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RemoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision + 1, NodeID: promotionexec.NodeSimulateCompensation,
	}); !errors.Is(err, designeredit.ErrConflict) {
		t.Fatalf("stale removal = %v, want ErrConflict", err)
	}
	if _, err := service.RemoveNode(context.Background(), values.TenantId("tenant-a"), "author-b", designeredit.RemoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: promotionexec.NodeSimulateCompensation,
	}); !errors.Is(err, designeredit.ErrNotFound) {
		t.Fatalf("cross-author removal = %v, want ErrNotFound", err)
	}
	if _, err := service.RemoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RemoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: "missing",
	}); !errors.Is(err, designeredit.ErrNotFound) {
		t.Fatalf("unknown node removal = %v, want ErrNotFound", err)
	}
	if _, err := service.RemoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RemoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: 0, NodeID: promotionexec.NodeSimulateCompensation,
	}); !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("unfenced removal = %v, want ErrInvalid", err)
	}
	if store.draft.Revision != draft.Revision {
		t.Fatalf("refusals advanced the stored revision to %d", store.draft.Revision)
	}
}

func TestRemoveNodeEmptiesASingleNodeDraftAndClearsItsStart(t *testing.T) {
	service, _, draft := singleBlockService(t)
	change, err := service.RemoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RemoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: draft.Nodes[0].ID,
	})
	if err != nil {
		t.Fatalf("RemoveNode of the only node: %v", err)
	}
	if len(change.Draft.Nodes) != 0 || len(change.Draft.Edges) != 0 || change.Draft.StartNodeID != "" {
		t.Fatalf("emptied draft = %+v", change.Draft)
	}
	if change.Draft.Revision != draft.Revision+1 {
		t.Fatalf("revision = %d, want %d", change.Draft.Revision, draft.Revision+1)
	}
}

func TestClearOutcomeRemovesOnlyTheNamedRoute(t *testing.T) {
	service, _, draft := refinementService(t)
	if !hasDraftEdge(draft, promotionexec.NodeRaiseThreshold, "UNKNOWN", promotionexec.NodeEndInvalidated) {
		t.Fatal("fixture no longer routes raise_threshold UNKNOWN")
	}
	cleared, err := service.ClearOutcome(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.ClearOutcomeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, FromNodeID: promotionexec.NodeRaiseThreshold, RouteKey: "UNKNOWN",
	})
	if err != nil {
		t.Fatalf("ClearOutcome: %v", err)
	}
	if cleared.Draft.Revision != draft.Revision+1 {
		t.Fatalf("revision = %d, want %d", cleared.Draft.Revision, draft.Revision+1)
	}
	if hasDraftEdge(cleared.Draft, promotionexec.NodeRaiseThreshold, "UNKNOWN", promotionexec.NodeEndInvalidated) {
		t.Fatal("cleared route is still connected")
	}
	if !hasDraftEdge(cleared.Draft, promotionexec.NodeRaiseThreshold, "ABOVE_THRESHOLD", promotionexec.NodeApproveFinance) {
		t.Fatal("clearing one route disconnected a sibling route")
	}
	node := draftNode(t, cleared.Draft, promotionexec.NodeRaiseThreshold)
	for _, outcome := range node.Outcomes {
		if outcome.RouteKey == "UNKNOWN" && len(outcome.TargetNodeIDs) != 0 {
			t.Fatalf("cleared outcome port = %+v", outcome)
		}
	}
}

func TestClearOutcomeClearingNothingPreservesTheRevision(t *testing.T) {
	service, store, draft := refinementService(t)
	cleared, err := service.ClearOutcome(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.ClearOutcomeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, FromNodeID: promotionexec.NodeRaiseThreshold,
		RouteKey: "UNKNOWN", ToNodeID: promotionexec.NodeEndComplete,
	})
	if err != nil {
		t.Fatalf("ClearOutcome of an unconnected target: %v", err)
	}
	if cleared.Draft.Revision != draft.Revision || store.draft.Revision != draft.Revision {
		t.Fatalf("no-op clear invented a revision: view=%d store=%d", cleared.Draft.Revision, store.draft.Revision)
	}
	if !hasDraftEdge(cleared.Draft, promotionexec.NodeRaiseThreshold, "UNKNOWN", promotionexec.NodeEndInvalidated) {
		t.Fatal("no-op clear removed an unrelated target")
	}
}

func TestClearOutcomeRefusesUndeclaredRoutesAndStaleOrForeignCallers(t *testing.T) {
	service, store, draft := refinementService(t)
	if _, err := service.ClearOutcome(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.ClearOutcomeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, FromNodeID: promotionexec.NodeRaiseThreshold, RouteKey: "NOT_A_ROUTE",
	}); !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("undeclared route = %v, want ErrInvalid", err)
	}
	if _, err := service.ClearOutcome(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.ClearOutcomeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, FromNodeID: "missing", RouteKey: "UNKNOWN",
	}); !errors.Is(err, designeredit.ErrNotFound) {
		t.Fatalf("unknown source node = %v, want ErrNotFound", err)
	}
	if _, err := service.ClearOutcome(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.ClearOutcomeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision + 1, FromNodeID: promotionexec.NodeRaiseThreshold, RouteKey: "UNKNOWN",
	}); !errors.Is(err, designeredit.ErrConflict) {
		t.Fatalf("stale clear = %v, want ErrConflict", err)
	}
	if _, err := service.ClearOutcome(context.Background(), values.TenantId("tenant-a"), "author-b", designeredit.ClearOutcomeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, FromNodeID: promotionexec.NodeRaiseThreshold, RouteKey: "UNKNOWN",
	}); !errors.Is(err, designeredit.ErrNotFound) {
		t.Fatalf("cross-author clear = %v, want ErrNotFound", err)
	}
	if _, err := service.ClearOutcome(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.ClearOutcomeRequest{
		DraftID: draft.DraftID, ExpectedRevision: 0, FromNodeID: promotionexec.NodeRaiseThreshold, RouteKey: "UNKNOWN",
	}); !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("unfenced clear = %v, want ErrInvalid", err)
	}
	if store.draft.Revision != draft.Revision {
		t.Fatalf("refusals advanced the stored revision to %d", store.draft.Revision)
	}
}

func TestRenameSetsTheDurableNameAndBoundsIt(t *testing.T) {
	service, store, draft := refinementService(t)
	renamed, err := service.Rename(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RenameRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, Name: "  Promotion, revised  ",
	})
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.Draft.Name != "Promotion, revised" || renamed.Draft.Revision != draft.Revision+1 {
		t.Fatalf("renamed draft = %q at revision %d", renamed.Draft.Name, renamed.Draft.Revision)
	}
	repeat, err := service.Rename(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RenameRequest{
		DraftID: draft.DraftID, ExpectedRevision: renamed.Draft.Revision, Name: "Promotion, revised",
	})
	if err != nil {
		t.Fatalf("repeat Rename: %v", err)
	}
	if repeat.Draft.Revision != renamed.Draft.Revision || store.draft.Revision != renamed.Draft.Revision {
		t.Fatalf("same-name rename invented a revision: view=%d store=%d", repeat.Draft.Revision, store.draft.Revision)
	}
	for _, name := range []string{"", "   ", strings.Repeat("n", 121)} {
		if _, err := service.Rename(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RenameRequest{
			DraftID: draft.DraftID, ExpectedRevision: store.draft.Revision, Name: name,
		}); !errors.Is(err, designeredit.ErrInvalid) {
			t.Fatalf("Rename(%q) = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := service.Rename(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RenameRequest{
		DraftID: draft.DraftID, ExpectedRevision: store.draft.Revision, Name: strings.Repeat("n", 120),
	}); err != nil {
		t.Fatalf("Rename at the 120-rune bound = %v, want acceptance", err)
	}
}

func TestRenameRefusesStaleOrForeignCallers(t *testing.T) {
	service, store, draft := refinementService(t)
	if _, err := service.Rename(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RenameRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision + 1, Name: "Elsewhere",
	}); !errors.Is(err, designeredit.ErrConflict) {
		t.Fatalf("stale rename = %v, want ErrConflict", err)
	}
	if _, err := service.Rename(context.Background(), values.TenantId("tenant-a"), "author-b", designeredit.RenameRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, Name: "Elsewhere",
	}); !errors.Is(err, designeredit.ErrNotFound) {
		t.Fatalf("cross-author rename = %v, want ErrNotFound", err)
	}
	if _, err := service.Rename(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.RenameRequest{
		DraftID: draft.DraftID, ExpectedRevision: 0, Name: "Elsewhere",
	}); !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("unfenced rename = %v, want ErrInvalid", err)
	}
	if store.draft.Revision != draft.Revision || draftName(store) == "Elsewhere" {
		t.Fatalf("refused rename changed the stored draft at revision %d", store.draft.Revision)
	}
}

func TestFragmentNodeIDsThatCollideAfterRemappingAreRefused(t *testing.T) {
	_, err := designeredit.Insert(workflow.Definition{WorkflowID: "draft", Version: 1}, designerpalette.Entry{
		ID: "fragment.collide", Version: 1, Name: "Collide", Kind: designerpalette.KindFragment,
		Expansion: designerpalette.Expansion{Nodes: []workflow.Node{
			{ID: "a-b", Type: workflow.StepTask},
			{ID: "a_b", Type: workflow.StepTask},
		}},
	})
	if !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("colliding fragment ids = %v, want ErrInvalid", err)
	}
	distinct, err := designeredit.Insert(workflow.Definition{WorkflowID: "draft", Version: 1}, designerpalette.Entry{
		ID: "fragment.distinct", Version: 1, Name: "Distinct", Kind: designerpalette.KindFragment,
		Expansion: designerpalette.Expansion{Nodes: []workflow.Node{
			{ID: "a-b", Type: workflow.StepTask},
			{ID: "a-c", Type: workflow.StepTask},
		}},
	})
	if err != nil || len(distinct.InsertedNodeIDs) != 2 || distinct.InsertedNodeIDs[0] == distinct.InsertedNodeIDs[1] {
		t.Fatalf("distinct fragment ids = %+v, %v", distinct.InsertedNodeIDs, err)
	}
}

func bindingSource(node designeredit.NodeView, path string) string {
	for _, binding := range node.Bindings {
		if binding.TargetPath == path {
			return binding.SourceNodeID
		}
	}
	return ""
}

func touchesNode(draft designeredit.View, id string) bool {
	for _, edge := range draft.Edges {
		if edge.From == id || edge.To == id {
			return true
		}
	}
	return false
}

func draftName(store *memoryDraftStore) string {
	definition, err := workflow.Load(store.draft.Document)
	if err != nil {
		return ""
	}
	return definition.Name
}
