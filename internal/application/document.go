package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// composedDocument owns one Knowledge database pool. The canonical service
// will consume store when document RPCs are composed; the pool never borrows
// the core or chat connection budget.
type composedDocument struct {
	store   *documenthubstore.Store
	service transportdocument.Service
	close   func()
}

type documentService struct {
	store            *documenthubstore.Store
	recipientAllowed func(context.Context, string, string) (bool, error)
}

func (s documentService) ListDocuments(ctx context.Context, tenantID, actorID string, options transportdocument.ListOptions) ([]transportdocument.Summary, error) {
	rows, err := s.store.ListPersonalDocumentsPage(ctx, tenantID, actorID, documenthubstore.ListOptions{
		Limit: options.Limit, Query: options.Query, Collection: options.Collection,
		BeforeTime: options.BeforeTime, BeforeID: options.BeforeID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]transportdocument.Summary, 0, len(rows))
	for _, row := range rows {
		out = append(out, transportdocument.Summary{
			DocumentID: row.ID, Title: row.Title, OwnerID: row.OwnerID,
			VersionID: row.VersionID, Status: row.Status, UpdatedAt: row.UpdatedAt,
			SharingState: sharingState(row.Shared), CanManageAccess: row.CanManage, CanEdit: row.CanEdit,
		})
	}
	return out, nil
}

func (s documentService) CreateDocument(ctx context.Context, tenantID, actorID, title, markdown string) (string, string, error) {
	id, version, err := s.store.CreatePersonalDocument(ctx, tenantID, actorID, title, markdown)
	return id, version.ID, err
}

func (s documentService) GetDocument(ctx context.Context, tenantID, actorID, documentID string) (transportdocument.Summary, string, string, error) {
	row, version, err := s.store.ReadPersonalDocument(ctx, tenantID, actorID, documentID)
	if errors.Is(err, documenthubstore.ErrDenied) {
		return transportdocument.Summary{}, "", "", envelope.New(envelope.CodeNotFound, "document.unavailable", "the document does not exist or is not visible")
	}
	if err != nil {
		return transportdocument.Summary{}, "", "", err
	}
	commentErr := s.store.Authorize(ctx, tenantID, documentID, "person", actorID, documenthubstore.ActionComment)
	if commentErr != nil && !errors.Is(commentErr, documenthubstore.ErrDenied) {
		return transportdocument.Summary{}, "", "", commentErr
	}
	return transportdocument.Summary{
		DocumentID: row.ID, Title: row.Title, OwnerID: row.OwnerID,
		VersionID: row.VersionID, Status: row.Status, SharingState: sharingState(row.Shared), CanManageAccess: row.CanManage,
		CanComment: commentErr == nil, CanEdit: row.CanEdit,
		UpdatedAt: row.UpdatedAt,
	}, version.Markdown, version.Hash, nil
}

func (s documentService) CreateDocumentVersion(ctx context.Context, tenantID, actorID, documentID, baseVersionID, title, markdown string) (string, error) {
	version, err := s.store.CreatePersonalDocumentVersion(ctx, tenantID, documentID, actorID, baseVersionID, title, markdown)
	if errors.Is(err, documenthubstore.ErrDenied) || errors.Is(err, documenthubstore.ErrUnknownDocument) {
		return "", unavailableDocument()
	}
	if errors.Is(err, documenthubstore.ErrVersionConflict) || errors.Is(err, documenthubstore.ErrUnknownBase) {
		return "", envelope.New(envelope.CodeAborted, "document.stale_version", "the document changed while you were editing; reload it before saving")
	}
	if err != nil {
		return "", err
	}
	return version.ID, nil
}

func (s documentService) ListDocumentComments(ctx context.Context, tenantID, actorID, documentID, versionID string) ([]transportdocument.Comment, error) {
	if err := s.checkVisibleCommentVersion(ctx, tenantID, actorID, documentID, versionID); err != nil {
		return nil, err
	}
	rows, err := s.store.ListComments(ctx, tenantID, documentID, versionID, "person", actorID)
	if errors.Is(err, documenthubstore.ErrDenied) {
		return nil, unavailableDocument()
	}
	if err != nil {
		return nil, err
	}
	out := make([]transportdocument.Comment, 0, len(rows))
	for _, row := range rows {
		out = append(out, transportdocument.Comment{ID: row.ID, DocumentID: row.DocumentID, VersionID: row.VersionID, AuthorID: row.AuthorID, Body: row.Body, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func (s documentService) AddDocumentComment(ctx context.Context, tenantID, actorID, documentID, versionID, body string) (transportdocument.Comment, error) {
	if err := s.checkVisibleCommentVersion(ctx, tenantID, actorID, documentID, versionID); err != nil {
		return transportdocument.Comment{}, err
	}
	row, err := s.store.AddComment(ctx, tenantID, documenthubstore.CommentInput{DocumentID: documentID, VersionID: versionID, AuthorID: actorID, Body: body})
	if errors.Is(err, documenthubstore.ErrDenied) {
		return transportdocument.Comment{}, unavailableDocument()
	}
	if err != nil {
		return transportdocument.Comment{}, err
	}
	return transportdocument.Comment{ID: row.ID, DocumentID: row.DocumentID, VersionID: row.VersionID, AuthorID: row.AuthorID, Body: row.Body, CreatedAt: row.CreatedAt}, nil
}

func (s documentService) checkVisibleCommentVersion(ctx context.Context, tenantID, actorID, documentID, versionID string) error {
	summary, _, _, err := s.GetDocument(ctx, tenantID, actorID, documentID)
	if err != nil {
		return err
	}
	if summary.VersionID != versionID {
		return unavailableDocument()
	}
	return nil
}

func unavailableDocument() error {
	return envelope.New(envelope.CodeNotFound, "document.unavailable", "the document does not exist or is not visible")
}

func (s documentService) ShareDocument(ctx context.Context, tenantID, actorID, documentID, recipientID string) error {
	row, _, err := s.store.ReadPersonalDocument(ctx, tenantID, actorID, documentID)
	if err != nil || row.OwnerID != actorID || !row.CanManage {
		return envelope.New(envelope.CodeNotFound, "document.unavailable", "the document does not exist or is not visible")
	}
	if s.recipientAllowed == nil {
		return envelope.New(envelope.CodeUnavailable, "document.directory.unavailable", "recipient verification is unavailable")
	}
	allowed, err := s.recipientAllowed(ctx, tenantID, recipientID)
	if err != nil {
		return err
	}
	if !allowed {
		return envelope.New(envelope.CodeInvalidArgument, "document.recipient_unavailable", "recipient is not an active employee in this workspace")
	}
	err = s.store.SharePersonalDocument(ctx, tenantID, documentID, actorID, recipientID)
	if errors.Is(err, documenthubstore.ErrDenied) {
		return envelope.New(envelope.CodeNotFound, "document.unavailable", "the document does not exist or is not visible")
	}
	return err
}

func documentRecipientValidator(pool *pgxadapter.Pool) func(context.Context, string, string) (bool, error) {
	if pool == nil {
		return nil
	}
	return func(ctx context.Context, tenantID, recipientID string) (bool, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return false, err
		}
		defer tx.Rollback(ctx)
		tenantUUID := pgstore.TenantID(tenantID)
		if err := tenancy.WithTenant(ctx, tx, tenantUUID); err != nil {
			return false, err
		}
		worker, found, err := (workforce.Store{}).Get(ctx, tx, tenantUUID, recipientID)
		if err != nil {
			return false, err
		}
		return found && strings.EqualFold(worker.LifecycleStatus, "active"), nil
	}
}

func sharingState(shared bool) string {
	if shared {
		return "shared"
	}
	return "private"
}

func composeDocument(ctx context.Context, cfg ServeConfig) (composedDocument, error) {
	if strings.TrimSpace(cfg.DocumentDatabaseURL) == "" {
		return composedDocument{}, nil
	}
	store, err := documenthubstore.New(ctx, documenthubstore.Config{
		DSN: cfg.DocumentDatabaseURL, CoreDSN: cfg.DatabaseURL,
		ChatDSN: cfg.ChatDatabaseURL, MaxConns: 8, MinConns: 1,
	})
	if err != nil {
		return composedDocument{}, fmt.Errorf("compose document database: %w", err)
	}
	return composedDocument{store: store, service: documentService{store: store}, close: store.Close}, nil
}
