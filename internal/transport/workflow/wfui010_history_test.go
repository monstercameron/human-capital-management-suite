package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	workflowcore "github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
)

type wfui010HistoryStore struct {
	*wfui005AuthoringStore
	documents []json.RawMessage
	position  int
}

func (s *wfui010HistoryStore) LoadHistory(_ context.Context, _ values.TenantId, id string) (designeredit.History, error) {
	if s == nil || s.draft.DraftID != id || s.position < 1 || s.position > len(s.documents) {
		return designeredit.History{}, designeredit.ErrNotFound
	}
	history := designeredit.History{Position: uint64(s.position), Length: uint64(len(s.documents)), CurrentLabel: "Edit workflow", Current: append(json.RawMessage(nil), s.documents[s.position-1]...)}
	if s.position > 1 {
		history.Previous = append(json.RawMessage(nil), s.documents[s.position-2]...)
	}
	return history, nil
}

func (s *wfui010HistoryStore) Navigate(_ context.Context, _ values.TenantId, request designeredit.NavigateRequest) (designeredit.Draft, error) {
	if s == nil || request.DraftID != s.draft.DraftID || request.AuthorRef != s.draft.AuthorRef || request.ExpectedRevision != s.draft.Revision {
		return designeredit.Draft{}, designeredit.ErrConflict
	}
	if request.Direction == designeredit.HistoryUndo && s.position > 1 {
		s.position--
	} else if request.Direction == designeredit.HistoryRedo && s.position < len(s.documents) {
		s.position++
	} else {
		return designeredit.Draft{}, designeredit.ErrConflict
	}
	s.draft.Revision++
	s.draft.Document = append(json.RawMessage(nil), s.documents[s.position-1]...)
	s.draft.HistoryPosition, s.draft.HistoryLength = uint64(s.position), uint64(len(s.documents))
	return s.draft, nil
}

func TestTodo_WF_UI_010_TransportProjectsDiffAndNavigatesHistory(t *testing.T) {
	srv, store, draft := wfui006Server(t, nil)
	current, err := workflowcore.Load(store.draft.Document)
	if err != nil {
		t.Fatal(err)
	}
	previous := current
	previous.Name = "Promotion before review"
	if previous.Nodes[0].Metadata == nil {
		previous.Nodes[0].Metadata = make(map[string]string)
	}
	previous.Nodes[0].Metadata[designeredit.MetadataDisplayName] = "Earlier start"
	previousDocument, err := workflowcore.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	historyStore := &wfui010HistoryStore{wfui005AuthoringStore: store, documents: []json.RawMessage{previousDocument, append(json.RawMessage(nil), store.draft.Document...)}, position: 2}
	srv.deps.DraftAuthoring.Store = historyStore

	loaded, err := srv.GetWorkflowDraft(workflowTestContext(t, GetWorkflowDraftProcedure), &workflowv1.GetWorkflowDraftRequest{DraftId: draft.GetDraftId()})
	if err != nil {
		t.Fatalf("GetWorkflowDraft: %v", err)
	}
	if !loaded.GetDraft().GetCanUndo() || loaded.GetDraft().GetCanRedo() || loaded.GetDraft().GetHistoryPosition() != 2 || loaded.GetDraft().GetHistoryLength() != 2 || loaded.GetDraft().GetLayoutMode() != "AUTO" || len(loaded.GetDraft().GetSemanticChanges()) == 0 {
		t.Fatalf("projected history = %+v", loaded.GetDraft())
	}
	undone, err := srv.NavigateWorkflowDraftHistory(workflowTestContext(t, NavigateWorkflowDraftHistoryProcedure), &workflowv1.NavigateWorkflowDraftHistoryRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), Direction: "UNDO",
	})
	if err != nil {
		t.Fatalf("NavigateWorkflowDraftHistory: %v", err)
	}
	if undone.GetDraft().GetRevision() != draft.GetRevision()+1 || undone.GetDraft().GetCanUndo() || !undone.GetDraft().GetCanRedo() || undone.GetDraft().GetName() != "Promotion before review" {
		t.Fatalf("undone draft = %+v", undone.GetDraft())
	}
}

func TestTodo_WF_UI_010_TransportAuthorizesBeforeHistoryMutation(t *testing.T) {
	srv, store, draft := wfui006Server(t, nil)
	srv.deps.Authorize = func(*trust.Principal, string) bool { return false }
	before := store.saves
	_, err := srv.NavigateWorkflowDraftHistory(workflowTestContext(t, NavigateWorkflowDraftHistoryProcedure), &workflowv1.NavigateWorkflowDraftHistoryRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), Direction: "UNDO",
	})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodePermissionDenied || store.saves != before {
		t.Fatalf("denied history navigation = %v, saves %d -> %d", err, before, store.saves)
	}
	srv.deps.Authorize = nil
	_, err = srv.NavigateWorkflowDraftHistory(workflowTestContext(t, NavigateWorkflowDraftHistoryProcedure), &workflowv1.NavigateWorkflowDraftHistoryRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), Direction: "SIDEWAYS",
	})
	if err == nil || errors.Is(err, designeredit.ErrConflict) {
		t.Fatalf("invalid direction was not rejected at transport boundary: %v", err)
	}
}
