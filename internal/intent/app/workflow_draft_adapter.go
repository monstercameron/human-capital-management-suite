package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowdraftstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
)

// workflowDraftStoreAdapter keeps the authoring service persistence-neutral.
// UUID parsing and data-package sentinel translation belong at composition,
// not in the workflow edit kernel or transport.
type workflowDraftStoreAdapter struct{ store *workflowdraftstore.Store }

func (a workflowDraftStoreAdapter) Load(ctx context.Context, tenant values.TenantId, draftID string) (designeredit.Draft, error) {
	id, err := uuid.Parse(draftID)
	if err != nil || a.store == nil {
		return designeredit.Draft{}, designeredit.ErrNotFound
	}
	draft, err := a.store.Load(ctx, tenant, id)
	if err != nil {
		return designeredit.Draft{}, translateWorkflowDraftStoreError(err)
	}
	return designeredit.Draft{
		DraftID: draft.DraftID.String(), WorkflowID: draft.WorkflowID, AuthorRef: draft.AuthorRef,
		SemanticVersion: draft.SemanticVersion, BaseVersionDigest: draft.BaseVersionDigest, Revision: draft.Revision,
		HistoryPosition: draft.HistoryPosition, HistoryLength: draft.HistoryLength,
		Document: append([]byte(nil), draft.Document...), ExpiresAt: draft.ExpiresAt,
	}, nil
}

func (a workflowDraftStoreAdapter) Save(ctx context.Context, tenant values.TenantId, request designeredit.SaveRequest) (designeredit.Draft, error) {
	id, err := uuid.Parse(request.DraftID)
	if err != nil || a.store == nil {
		return designeredit.Draft{}, designeredit.ErrInvalid
	}
	saved, err := a.store.Save(ctx, tenant, workflowdraftstore.SaveRequest{
		DraftID: id, WorkflowID: request.WorkflowID, AuthorRef: request.AuthorRef,
		SemanticVersion: request.SemanticVersion, BaseVersionDigest: request.BaseVersionDigest, ExpectedRevision: request.ExpectedRevision,
		CommandLabel: request.CommandLabel, Document: append([]byte(nil), request.Document...), ExpiresAt: request.ExpiresAt, At: request.At,
	})
	if err != nil {
		return designeredit.Draft{}, translateWorkflowDraftStoreError(err)
	}
	return designeredit.Draft{
		DraftID: saved.DraftID.String(), WorkflowID: saved.WorkflowID, AuthorRef: saved.AuthorRef,
		SemanticVersion: saved.SemanticVersion, BaseVersionDigest: saved.BaseVersionDigest, Revision: saved.Revision,
		HistoryPosition: saved.HistoryPosition, HistoryLength: saved.HistoryLength,
		Document: append([]byte(nil), saved.Document...), ExpiresAt: saved.ExpiresAt,
	}, nil
}

func (a workflowDraftStoreAdapter) LoadHistory(ctx context.Context, tenant values.TenantId, draftID string) (designeredit.History, error) {
	id, err := uuid.Parse(draftID)
	if err != nil || a.store == nil {
		return designeredit.History{}, designeredit.ErrNotFound
	}
	history, err := a.store.LoadHistory(ctx, tenant, id)
	if err != nil {
		return designeredit.History{}, translateWorkflowDraftStoreError(err)
	}
	return designeredit.History{
		Position: history.Position, Length: history.Length, CurrentLabel: history.CurrentLabel,
		Current: append([]byte(nil), history.Current...), Previous: append([]byte(nil), history.Previous...),
	}, nil
}

func (a workflowDraftStoreAdapter) Navigate(ctx context.Context, tenant values.TenantId, request designeredit.NavigateRequest) (designeredit.Draft, error) {
	id, err := uuid.Parse(request.DraftID)
	if err != nil || a.store == nil {
		return designeredit.Draft{}, designeredit.ErrInvalid
	}
	saved, err := a.store.Navigate(ctx, tenant, workflowdraftstore.NavigateRequest{
		DraftID: id, AuthorRef: request.AuthorRef, ExpectedRevision: request.ExpectedRevision,
		Direction: string(request.Direction), At: request.At,
	})
	if err != nil {
		return designeredit.Draft{}, translateWorkflowDraftStoreError(err)
	}
	return designeredit.Draft{
		DraftID: saved.DraftID.String(), WorkflowID: saved.WorkflowID, AuthorRef: saved.AuthorRef,
		SemanticVersion: saved.SemanticVersion, BaseVersionDigest: saved.BaseVersionDigest, Revision: saved.Revision,
		HistoryPosition: saved.HistoryPosition, HistoryLength: saved.HistoryLength,
		Document: append([]byte(nil), saved.Document...), ExpiresAt: saved.ExpiresAt,
	}, nil
}

func translateWorkflowDraftStoreError(err error) error {
	switch {
	case errors.Is(err, workflowdraftstore.ErrInvalid):
		return designeredit.ErrInvalid
	case errors.Is(err, workflowdraftstore.ErrConflict):
		return designeredit.ErrConflict
	case errors.Is(err, workflowdraftstore.ErrNotFound):
		return designeredit.ErrNotFound
	default:
		return err
	}
}
