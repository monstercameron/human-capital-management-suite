package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowdraftstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrWorkflowDraftNotFound hides malformed IDs and absent tenant-scoped drafts
// behind the same application-level refusal.
var ErrWorkflowDraftNotFound = errors.New("workflow draft not found")

// WorkflowDraftRecord is the narrow application read projection for transport.
type WorkflowDraftRecord struct {
	DraftID, AuthorRef string
	Revision           uint64
	Document           json.RawMessage
}

// ReadWorkflowDraftRecord keeps the storage adapter and its error types out of
// transport. The store itself applies the tenant predicate.
func (c *Cell) ReadWorkflowDraftRecord(ctx context.Context, tenant values.TenantId, draftID string) (WorkflowDraftRecord, error) {
	id, err := uuid.Parse(draftID)
	if err != nil || c == nil || c.WorkflowDrafts == nil {
		return WorkflowDraftRecord{}, ErrWorkflowDraftNotFound
	}
	draft, err := c.WorkflowDrafts.Load(ctx, tenant, id)
	if errors.Is(err, workflowdraftstore.ErrInvalid) || errors.Is(err, workflowdraftstore.ErrNotFound) {
		return WorkflowDraftRecord{}, ErrWorkflowDraftNotFound
	}
	if err != nil {
		return WorkflowDraftRecord{}, err
	}
	return WorkflowDraftRecord{DraftID: draft.DraftID.String(), AuthorRef: draft.AuthorRef,
		Revision: draft.Revision, Document: draft.Document}, nil
}
