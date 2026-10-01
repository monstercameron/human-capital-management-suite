package workflowdraftstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowdraftstore"
)

func TestTodo_UXBLIND_081_Integration(t *testing.T) {
	store, _, _, tenant := buildDraftStore(t)
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	emptyID := uuid.New()
	if _, err := store.Save(context.Background(), tenant, workflowdraftstore.SaveRequest{
		DraftID: emptyID, WorkflowID: "workflow.empty", AuthorRef: "user:alice", SemanticVersion: "0.1.0",
		Document: json.RawMessage(`{"nodes":[]}`), ExpiresAt: at.Add(time.Hour), At: at,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), tenant, workflowdraftstore.DeleteRequest{DraftID: emptyID, AuthorRef: "user:bob"}); !errors.Is(err, workflowdraftstore.ErrNotFound) {
		t.Fatalf("wrong-author delete = %v, want ErrNotFound", err)
	}
	if err := store.Delete(context.Background(), tenant, workflowdraftstore.DeleteRequest{DraftID: emptyID, AuthorRef: "user:alice"}); err != nil {
		t.Fatalf("empty draft delete = %v", err)
	}
	if _, err := store.Load(context.Background(), tenant, emptyID); !errors.Is(err, workflowdraftstore.ErrNotFound) {
		t.Fatalf("Load after delete = %v, want ErrNotFound", err)
	}

	authoredID := uuid.New()
	if _, err := store.Save(context.Background(), tenant, workflowdraftstore.SaveRequest{
		DraftID: authoredID, WorkflowID: "workflow.authored", AuthorRef: "user:alice", SemanticVersion: "0.1.0",
		Document: json.RawMessage(`{"nodes":[{"id":"step-1"}]}`), ExpiresAt: at.Add(time.Hour), At: at,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), tenant, workflowdraftstore.DeleteRequest{DraftID: authoredID, AuthorRef: "user:alice"}); !errors.Is(err, workflowdraftstore.ErrNotFound) {
		t.Fatalf("authored draft delete = %v, want ErrNotFound", err)
	}
	if _, err := store.Load(context.Background(), tenant, authoredID); err != nil {
		t.Fatalf("authored draft disappeared after refused delete: %v", err)
	}
}
