package designeredit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func TestTodo_WF_UI_009_Property(t *testing.T) {
	service, _, draft := refinementService(t)
	originalDigest := draft.DefinitionDigest
	originalOrder := draftNodeIDs(draft)

	moved, err := service.MoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.MoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: promotionexec.NodeWaitEffectiveDate, Direction: designeredit.MoveLater,
	})
	if err != nil {
		t.Fatalf("MoveNode later: %v", err)
	}
	if moved.Draft.Revision != draft.Revision+1 || moved.Draft.DefinitionDigest == originalDigest {
		t.Fatalf("move did not create one distinct saved revision: %+v", moved.Draft)
	}

	restored, err := service.MoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.MoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: moved.Draft.Revision, NodeID: promotionexec.NodeWaitEffectiveDate, Direction: designeredit.MoveEarlier,
	})
	if err != nil {
		t.Fatalf("MoveNode earlier: %v", err)
	}
	if restored.Draft.DefinitionDigest != originalDigest {
		t.Fatalf("round trip digest = %q, want %q", restored.Draft.DefinitionDigest, originalDigest)
	}
	if got := draftNodeIDs(restored.Draft); !equalStrings(got, originalOrder) {
		t.Fatalf("round trip order = %v, want %v", got, originalOrder)
	}
}

func TestTodo_WF_UI_009_MoveNodeBoundaryIsIdempotent(t *testing.T) {
	service, store, draft := refinementService(t)
	before := append([]byte(nil), store.draft.Document...)
	change, err := service.MoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.MoveNodeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: draft.Nodes[0].ID, Direction: designeredit.MoveEarlier,
	})
	if err != nil {
		t.Fatalf("MoveNode boundary: %v", err)
	}
	if change.Draft.Revision != draft.Revision || string(store.draft.Document) != string(before) {
		t.Fatalf("boundary move mutated draft: revision=%d", change.Draft.Revision)
	}
}

func TestTodo_WF_UI_009(t *testing.T) {
	service, _, draft := refinementService(t)
	for _, request := range []designeredit.MoveNodeRequest{
		{DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: "missing", Direction: designeredit.MoveLater},
		{DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: draft.Nodes[0].ID, Direction: "SIDEWAYS"},
		{DraftID: draft.DraftID, ExpectedRevision: draft.Revision + 1, NodeID: draft.Nodes[0].ID, Direction: designeredit.MoveLater},
	} {
		if _, err := service.MoveNode(context.Background(), values.TenantId("tenant-a"), "author-a", request); err == nil || (!errors.Is(err, designeredit.ErrInvalid) && !errors.Is(err, designeredit.ErrNotFound) && !errors.Is(err, designeredit.ErrConflict)) {
			t.Fatalf("MoveNode(%+v) = %v, want typed refusal", request, err)
		}
	}
}

func draftNodeIDs(draft designeredit.View) []string {
	ids := make([]string, 0, len(draft.Nodes))
	for _, node := range draft.Nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
