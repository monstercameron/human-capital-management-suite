package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/documentembed"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
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
	store    *documenthubstore.Store
	service  transportdocument.Service
	embedder documentembed.Embedder
	indexer  *documentembed.Indexer
	close    func()
}

type documentService struct {
	store            *documenthubstore.Store
	recipientAllowed func(context.Context, string, string) (bool, error)
	// embedder embeds search queries for meaning search; nil disables it
	// and meaning requests run smart. Versions are never embedded here:
	// writes enqueue index jobs and indexer drains them in the background.
	embedder documentembed.Embedder
	indexer  *documentembed.Indexer
	// ownerNames resolves owner IDs to display names for the owner sorts;
	// nil sorts by owner ID.
	ownerNames func(ctx context.Context, tenantID string, ownerIDs []string) (map[string]string, error)
	// chat and people resolve a version's chat references as the reader
	// (document_chat_refs.go); nil leaves them unresolved.
	chat   documentChatReader
	people func(ctx context.Context, tenantID string) ([]documentPerson, error)
	// routes is the core document ID route directory (HUB-002); nil skips
	// route registration and enforcement (a development composition without
	// a directory configured). CreateDocument registers a route;
	// documentSummary (GetDocument) and CreateDocumentVersion require it be
	// current before any content read.
	routes documenthubstore.DocumentRouteDirectory
	// aud resolves live team/channel eligibility for placement reads
	// (HUB-013); nil refuses every placement read rather than silently
	// widening access.
	aud documenthubstore.AudienceEligibility
	// agentApps resolves a chat app installation by
	// "tenant:conversation:app" key for agent document search (HUB-031);
	// nil refuses every agent search as not installed.
	agentApps agentInstallationRepository
	// agentNow is the clock AgentSearchDocuments binds its installation
	// currency check to; nil uses time.Now.
	agentNow func() time.Time
}

// agentInstallationRepository is the minimal chat app installation lookup
// AgentSearchDocuments needs; chatapps.Service.Repo (composed in serve.go
// from the chat runtime) satisfies it, and document code never imports
// chatapps beyond this interface and document_agent_chat.go's adapter.
type agentInstallationRepository interface {
	Get(context.Context, string) (chatapps.Installation, error)
}

// queryEmbedTimeout keeps a query embedding interactive; past it the
// search runs without its meaning leg.
const queryEmbedTimeout = 3 * time.Second

func (s documentService) ListDocuments(ctx context.Context, tenantID, actorID string, options transportdocument.ListOptions) (transportdocument.ListResult, error) {
	selection := documenthubstore.ListOptions{
		Limit: options.Limit, Query: options.Query, Collection: options.Collection,
		FolderID: options.FolderID, Starred: options.StarredOnly, OwnerID: options.OwnerID, Sort: options.Sort,
		BeforeTime: options.BeforeTime, BeforeID: options.BeforeID, AfterTitle: options.AfterTitle,
		Offset: options.Offset, Mode: options.Mode,
	}
	semantic := s.embedder != nil
	if (options.Sort == documenthubstore.SortOwner || options.Sort == documenthubstore.SortOwnerDesc) && s.ownerNames != nil {
		owners, err := s.store.SelectionOwners(ctx, tenantID, actorID, selection)
		if err != nil {
			return transportdocument.ListResult{}, err
		}
		names, err := s.ownerNames(ctx, tenantID, owners)
		if err != nil {
			return transportdocument.ListResult{}, err
		}
		selection.OwnerNames = names
	}
	mode := options.Mode
	if mode == "" || (mode == documenthubstore.SearchMeaning && !semantic) {
		mode = documenthubstore.SearchSmart
	}
	result := transportdocument.ListResult{Mode: mode, SemanticAvailable: semantic}
	var rows []documenthubstore.DocumentSummary
	if strings.TrimSpace(options.Query) == "" {
		page, err := s.store.ListPersonalDocumentsPage(ctx, tenantID, actorID, selection)
		if err != nil {
			return transportdocument.ListResult{}, err
		}
		total, err := s.store.CountPersonalDocuments(ctx, tenantID, actorID, selection)
		if err != nil {
			return transportdocument.ListResult{}, err
		}
		rows, result.Total = page, total
	} else {
		if semantic && (mode == documenthubstore.SearchSmart || mode == documenthubstore.SearchMeaning) {
			if vec, err := s.embedQuery(ctx, options.Query); err == nil {
				selection.QueryVector, selection.VectorModel = vec, s.embedder.Model()
			} else {
				slog.WarnContext(ctx, "document.search.embed_failed", "error", err)
			}
		}
		found, err := s.store.SearchPersonalDocuments(ctx, tenantID, actorID, selection)
		if err != nil {
			return transportdocument.ListResult{}, err
		}
		rows, result.Total, result.Mode = found.Rows, found.Total, found.Mode
	}
	if semantic {
		pending, err := s.store.SemanticPending(ctx, tenantID, actorID, s.embedder.Model())
		if err != nil {
			return transportdocument.ListResult{}, err
		}
		result.SemanticPending = pending
	}
	result.Rows = make([]transportdocument.Summary, 0, len(rows))
	for _, row := range rows {
		result.Rows = append(result.Rows, transportdocument.Summary{
			DocumentID: row.ID, Title: row.Title, OwnerID: row.OwnerID,
			VersionID: row.VersionID, Status: row.Status, UpdatedAt: row.UpdatedAt,
			SharingState: sharingState(row.Shared), CanManageAccess: row.CanManage, CanEdit: row.CanEdit,
			Starred: row.Starred, FolderID: row.FolderID, ReaderCount: row.ReaderCount, TitleKey: row.TitleKey,
			Snippet: row.Snippet, Match: row.Match,
		})
	}
	return result, nil
}

func (s documentService) embedQuery(ctx context.Context, query string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, queryEmbedTimeout)
	defer cancel()
	vecs, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return nil, errors.New("document search: the embedding model returned no vector")
	}
	return vecs[0], nil
}

// documentRouteShard is the single core shard every document is registered
// under; documenthubstore.Store is not itself sharded, but a document read
// must still go through route registration and enforcement (HUB-002) so a
// future sharded composition is a directory swap, not a new call site.
const documentRouteShard = "default"

// registerDocumentRoute registers the core route for a newly created
// document (HUB-002). A nil directory (no route composition configured)
// is a no-op: registration is best-effort and never blocks document
// creation on a directory that was never wired up.
func (s documentService) registerDocumentRoute(ctx context.Context, tenantID, documentID string) {
	if s.routes == nil || documentID == "" {
		return
	}
	if _, err := s.routes.Register(ctx, documentID, tenantID, documentRouteShard, documentID); err != nil {
		slog.WarnContext(ctx, "document.route.register_failed", "document_id", documentID, "error", err)
	}
}

// requireDocumentRoute refuses a document read whose route the directory no
// longer recognizes as current for this tenant: a stale migrated copy or a
// route claimed by a foreign tenant is refused before any content read
// (HUB-002). A nil directory skips enforcement.
func (s documentService) requireDocumentRoute(ctx context.Context, tenantID, documentID string) error {
	if s.routes == nil || documentID == "" {
		return nil
	}
	route, err := s.routes.Lookup(ctx, documentID, tenantID)
	if err != nil {
		if errors.Is(err, documenthubstore.ErrRouteNotFound) {
			// A document created before route composition was wired up, or
			// never routed, carries no route to be stale under; only a
			// tenant mismatch or a resolved-but-superseded route is refused.
			return nil
		}
		return err
	}
	return documenthubstore.RequireCurrentRoute(ctx, s.routes, route, tenantID)
}

func (s documentService) CreateDocument(ctx context.Context, tenantID, actorID, title, markdown string) (string, string, error) {
	id, version, err := s.store.CreatePersonalDocument(ctx, tenantID, actorID, title, markdown)
	if err == nil {
		s.indexer.Notify(tenantID)
		s.registerDocumentRoute(ctx, tenantID, id)
	}
	return id, version.ID, err
}

// GetDocument returns the readable version with the caller's view of every
// document it links to.
func (s documentService) GetDocument(ctx context.Context, tenantID, actorID, documentID string) (transportdocument.Summary, string, string, error) {
	if err := s.requireDocumentRoute(ctx, tenantID, documentID); err != nil {
		return transportdocument.Summary{}, "", "", unavailableDocument()
	}
	summary, markdown, hash, err := s.documentSummary(ctx, tenantID, actorID, documentID)
	if err != nil {
		return summary, markdown, hash, err
	}
	targets, err := s.store.LinkTargets(ctx, tenantID, actorID, markdown)
	if err != nil {
		return transportdocument.Summary{}, "", "", err
	}
	summary.Links = make([]transportdocument.LinkTarget, 0, len(targets))
	for _, t := range targets {
		summary.Links = append(summary.Links, transportdocument.LinkTarget{DocumentID: t.DocumentID, Title: t.Title, Readable: t.Readable})
	}
	summary.Chat = s.documentChatReferences(ctx, tenantID, actorID, markdown)
	return summary, markdown, hash, nil
}

func (s documentService) documentSummary(ctx context.Context, tenantID, actorID, documentID string) (transportdocument.Summary, string, string, error) {
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
	placement, err := s.store.DocumentPlacement(ctx, tenantID, actorID, documentID)
	if errors.Is(err, documenthubstore.ErrDenied) {
		return transportdocument.Summary{}, "", "", unavailableDocument()
	}
	if err != nil {
		return transportdocument.Summary{}, "", "", err
	}
	readers := 0
	if row.CanManage {
		readers = placement.ReaderCount
	}
	return transportdocument.Summary{
		DocumentID: row.ID, Title: row.Title, OwnerID: row.OwnerID,
		VersionID: row.VersionID, Status: row.Status, SharingState: sharingState(row.Shared), CanManageAccess: row.CanManage,
		CanComment: commentErr == nil, CanEdit: row.CanEdit,
		UpdatedAt: row.UpdatedAt,
		Starred:   placement.Starred, FolderID: placement.FolderID, ReaderCount: readers,
	}, version.Markdown, version.Hash, nil
}

func (s documentService) CreateDocumentVersion(ctx context.Context, tenantID, actorID, documentID, baseVersionID, title, markdown string) (string, error) {
	if err := s.requireDocumentRoute(ctx, tenantID, documentID); err != nil {
		return "", unavailableDocument()
	}
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
	s.indexer.Notify(tenantID)
	return version.ID, nil
}

// GetDocumentVersion reads one immutable version for HUB-033's compare
// view. A version the caller cannot read reports Readable=false only,
// never an error that would confirm or deny its existence.
func (s documentService) GetDocumentVersion(ctx context.Context, tenantID, actorID, documentID, versionID string) (transportdocument.DocumentVersion, error) {
	version, err := s.store.ReadVersion(ctx, tenantID, documentID, versionID, "person", actorID)
	if errors.Is(err, documenthubstore.ErrDenied) {
		return transportdocument.DocumentVersion{DocumentID: documentID, VersionID: versionID}, nil
	}
	if err != nil {
		return transportdocument.DocumentVersion{}, err
	}
	return transportdocument.DocumentVersion{
		DocumentID: version.DocumentID, VersionID: version.ID, Title: version.Title, Markdown: version.Markdown,
		ContentHash: version.Hash, CreatedAt: version.CreatedAt, Readable: true,
	}, nil
}

// DocumentBacklinks lists inbound links to a document from sources the
// caller may also read (HUB-035/HUB-022); a source the caller cannot read
// never reaches the wire.
func (s documentService) DocumentBacklinks(ctx context.Context, tenantID, actorID, documentID string) ([]transportdocument.Backlink, error) {
	rows, err := s.store.Backlinks(ctx, tenantID, documentID, "person", actorID)
	if errors.Is(err, documenthubstore.ErrDenied) {
		return nil, unavailableDocument()
	}
	if err != nil {
		return nil, err
	}
	out := make([]transportdocument.Backlink, 0, len(rows))
	for _, r := range rows {
		out = append(out, transportdocument.Backlink{
			SourceDocumentID: r.SourceDocID, SourceVersionID: r.SourceVersionID, SourceTitle: r.SourceTitle,
			Label: r.Label, Block: r.Block, State: r.State,
		})
	}
	return out, nil
}

func (s documentService) ListDocumentComments(ctx context.Context, tenantID, actorID, documentID, versionID string) ([]transportdocument.Comment, error) {
	if err := s.checkVisibleCommentVersion(ctx, tenantID, actorID, documentID, versionID); err != nil {
		return nil, err
	}
	rows, err := s.store.ListThread(ctx, tenantID, documentID, versionID, actorID)
	if errors.Is(err, documenthubstore.ErrDenied) {
		return nil, unavailableDocument()
	}
	if err != nil {
		return nil, err
	}
	out := make([]transportdocument.Comment, 0, len(rows))
	for _, row := range rows {
		out = append(out, transportComment(row))
	}
	return out, nil
}

func transportComment(row documenthubstore.Comment) transportdocument.Comment {
	c := transportdocument.Comment{ID: row.ID, DocumentID: row.DocumentID, VersionID: row.VersionID, AuthorID: row.AuthorID, Body: row.Body, CreatedAt: row.CreatedAt,
		ParentID: row.ParentID, Resolved: row.Resolved, Orphaned: row.Orphaned}
	if row.Quote != "" {
		c.Anchor = &transportdocument.CommentAnchor{BlockID: row.AnchorBlock, Quote: row.Quote, Prefix: row.Prefix, Suffix: row.Suffix, Start: row.Start, End: row.End}
	}
	return c
}

func (s documentService) ResolveDocumentComment(ctx context.Context, tenantID, actorID, documentID, commentID string, resolved bool) error {
	return commentError(s.store.ResolveComment(ctx, tenantID, documentID, commentID, actorID, resolved))
}

// commentError maps comment sentinels to owned envelopes; an unreadable
// document stays indistinguishable from a missing one.
func commentError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, documenthubstore.ErrDenied):
		return unavailableDocument()
	case errors.Is(err, documenthubstore.ErrUnknownComment):
		return envelope.New(envelope.CodeNotFound, "document.comment_unavailable", "the comment does not exist or is not visible")
	case errors.Is(err, documenthubstore.ErrInvalidAnchor):
		return envelope.New(envelope.CodeInvalidArgument, "document.invalid_anchor", "the quoted passage is not in this version")
	case errors.Is(err, documenthubstore.ErrInvalidParent):
		return envelope.New(envelope.CodeInvalidArgument, "document.invalid_parent", "replies answer a visible top-level comment of the same document")
	case errors.Is(err, documenthubstore.ErrResolveDenied):
		return envelope.New(envelope.CodePermissionDenied, "document.comment_resolve_denied", "only the comment's author or a document manager may resolve it")
	case errors.Is(err, documenthubstore.ErrResolveNotTopic):
		return envelope.New(envelope.CodeInvalidArgument, "document.comment_not_thread", "only a top-level comment can be resolved")
	}
	return err
}

func (s documentService) AddDocumentComment(ctx context.Context, tenantID, actorID, documentID, versionID, body string, anchor *transportdocument.CommentAnchor, parentID string) (transportdocument.Comment, error) {
	if err := s.checkVisibleCommentVersion(ctx, tenantID, actorID, documentID, versionID); err != nil {
		return transportdocument.Comment{}, err
	}
	in := documenthubstore.CommentInput{DocumentID: documentID, VersionID: versionID, AuthorID: actorID, Body: body, ParentID: parentID}
	if anchor != nil {
		in.AnchorBlock, in.Quote, in.Prefix, in.Suffix = anchor.BlockID, anchor.Quote, anchor.Prefix, anchor.Suffix
	}
	row, err := s.store.AddComment(ctx, tenantID, in)
	if err != nil {
		return transportdocument.Comment{}, commentError(err)
	}
	return transportComment(row), nil
}

func (s documentService) checkVisibleCommentVersion(ctx context.Context, tenantID, actorID, documentID, versionID string) error {
	summary, _, _, err := s.documentSummary(ctx, tenantID, actorID, documentID)
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

func (s documentService) ShareDocument(ctx context.Context, tenantID, actorID, documentID, recipientID, role string) error {
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
	err = s.store.SharePersonalDocumentRole(ctx, tenantID, documentID, actorID, recipientID, role)
	if errors.Is(err, documenthubstore.ErrDenied) {
		return envelope.New(envelope.CodeNotFound, "document.unavailable", "the document does not exist or is not visible")
	}
	if err != nil {
		return libraryError(err)
	}
	s.indexer.Notify(tenantID)
	return nil
}

func (s documentService) GetDocumentLibrary(ctx context.Context, tenantID, actorID string) (transportdocument.Library, error) {
	lib, err := s.store.GetLibrary(ctx, tenantID, actorID)
	if err != nil {
		return transportdocument.Library{}, libraryError(err)
	}
	out := transportdocument.Library{All: lib.All, Mine: lib.Mine, Shared: lib.SharedWithMe, Starred: lib.Starred, Folders: make([]transportdocument.Folder, 0, len(lib.Folders))}
	for _, folder := range lib.Folders {
		out.Folders = append(out.Folders, transportFolder(folder))
	}
	return out, nil
}

func (s documentService) CreateDocumentFolder(ctx context.Context, tenantID, actorID, name string) (transportdocument.Folder, error) {
	folder, err := s.store.CreateFolder(ctx, tenantID, actorID, name)
	if err != nil {
		return transportdocument.Folder{}, libraryError(err)
	}
	return transportFolder(folder), nil
}

func (s documentService) RenameDocumentFolder(ctx context.Context, tenantID, actorID, folderID, name string) error {
	return libraryError(s.store.RenameFolder(ctx, tenantID, actorID, folderID, name))
}

func (s documentService) DeleteDocumentFolder(ctx context.Context, tenantID, actorID, folderID string) error {
	return libraryError(s.store.DeleteFolder(ctx, tenantID, actorID, folderID))
}

func (s documentService) MoveDocuments(ctx context.Context, tenantID, actorID string, documentIDs []string, folderID string) error {
	return libraryError(s.store.MoveDocuments(ctx, tenantID, actorID, documentIDs, folderID))
}

func (s documentService) SetDocumentStarred(ctx context.Context, tenantID, actorID, documentID string, starred bool) error {
	return libraryError(s.store.SetStarred(ctx, tenantID, actorID, documentID, starred))
}

func (s documentService) ListDocumentAccess(ctx context.Context, tenantID, actorID, documentID string) ([]transportdocument.AccessEntry, error) {
	entries, err := s.store.ListAccess(ctx, tenantID, actorID, documentID)
	if err != nil {
		return nil, libraryError(err)
	}
	out := make([]transportdocument.AccessEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, transportdocument.AccessEntry{SubjectKind: entry.SubjectKind, SubjectID: entry.SubjectID, Role: entry.Role, Removable: entry.Removable})
	}
	return out, nil
}

func (s documentService) RevokeDocumentAccess(ctx context.Context, tenantID, actorID, documentID, subjectKind, subjectID string) error {
	if subjectKind != "person" {
		return envelope.New(envelope.CodeInvalidArgument, "document.invalid_revoke", "only a person's access can be removed")
	}
	return libraryError(s.store.RevokePersonAccess(ctx, tenantID, actorID, documentID, subjectID))
}

func transportFolder(folder documenthubstore.Folder) transportdocument.Folder {
	return transportdocument.Folder{ID: folder.ID, Name: folder.Name, DocumentCount: folder.DocumentCount, CreatedAt: folder.CreatedAt}
}

// libraryError maps the store's library and access sentinels to owned
// envelopes. A denied or unknown document is one indistinguishable
// not-found, and so is a folder the caller does not own.
func libraryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, documenthubstore.ErrDenied):
		return unavailableDocument()
	case errors.Is(err, documenthubstore.ErrFolderNotFound):
		return envelope.New(envelope.CodeNotFound, "document.folder_unavailable", "the folder does not exist")
	case errors.Is(err, documenthubstore.ErrFolderNameInvalid):
		return envelope.New(envelope.CodeInvalidArgument, "document.invalid_folder_name", "a folder name of 1 to 80 characters is required")
	case errors.Is(err, documenthubstore.ErrFolderNameTaken):
		return envelope.New(envelope.CodeAlreadyExists, "document.folder_name_taken", "you already have a folder with that name")
	case errors.Is(err, documenthubstore.ErrTooManyDocuments):
		return envelope.New(envelope.CodeInvalidArgument, "document.invalid_move", "at most 100 documents can be moved at once")
	case errors.Is(err, documenthubstore.ErrOwnerAccess):
		return envelope.New(envelope.CodeFailedPrecondition, "document.owner_access", "the owner's access cannot be removed")
	case errors.Is(err, documenthubstore.ErrInvalidRole):
		return envelope.New(envelope.CodeInvalidArgument, "document.invalid_role", "the share role must be viewer or commenter")
	}
	return err
}

// documentOwnerNames resolves owner worker keys to display names from the
// core workforce projection, caching them briefly per tenant: the
// preferred name completed with the legal surname ("Rafa" and "Rafael
// Torres" give "Rafa Torres"), a full preferred name as is, else the legal
// name, else the key.
func documentOwnerNames(pool *pgxadapter.Pool) func(context.Context, string, []string) (map[string]string, error) {
	if pool == nil {
		return nil
	}
	cache := &ownerNameCache{ttl: 5 * time.Minute, entries: map[string]ownerNameEntry{}}
	return func(ctx context.Context, tenantID string, keys []string) (map[string]string, error) {
		out, missing := cache.lookup(tenantID, keys, time.Now())
		if len(missing) == 0 {
			return out, nil
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		tenantUUID := pgstore.TenantID(tenantID)
		if err := tenancy.WithTenant(ctx, tx, tenantUUID); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (worker_key) worker_key, COALESCE(preferred_name,''), COALESCE(legal_name,'')
			FROM journey_worker WHERE tenant_id=$1 AND worker_key=ANY($2::text[]) ORDER BY worker_key, revision_sequence DESC`, tenantUUID, missing)
		if err != nil {
			return nil, err
		}
		found := map[string]string{}
		for rows.Next() {
			var key, preferred, legal string
			if err := rows.Scan(&key, &preferred, &legal); err != nil {
				rows.Close()
				return nil, err
			}
			found[key] = ownerDisplayName(key, preferred, legal)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, key := range missing {
			name, ok := found[key]
			if !ok {
				name = key
			}
			out[key] = name
		}
		cache.store(tenantID, out, time.Now())
		return out, nil
	}
}

// ownerDisplayName completes a one-word preferred name with the legal
// surname; a multi-word preferred name stands; the legal name and then the
// key are the fallbacks.
func ownerDisplayName(key, preferred, legal string) string {
	preferred, legal = strings.TrimSpace(preferred), strings.TrimSpace(legal)
	switch {
	case preferred != "" && strings.Contains(preferred, " "):
		return preferred
	case preferred != "" && legal != "":
		parts := strings.Fields(legal)
		if len(parts) > 1 {
			return preferred + " " + parts[len(parts)-1]
		}
		return preferred
	case preferred != "":
		return preferred
	case legal != "":
		return legal
	}
	return key
}

type ownerNameEntry struct {
	name    string
	expires time.Time
}

type ownerNameCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]ownerNameEntry
}

func (c *ownerNameCache) lookup(tenantID string, keys []string, now time.Time) (map[string]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(keys))
	var missing []string
	for _, key := range keys {
		if e, ok := c.entries[tenantID+"\x00"+key]; ok && now.Before(e.expires) {
			out[key] = e.name
			continue
		}
		missing = append(missing, key)
	}
	return out, missing
}

func (c *ownerNameCache) store(tenantID string, names map[string]string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) > 10000 {
		c.entries = map[string]ownerNameEntry{}
	}
	for key, name := range names {
		c.entries[tenantID+"\x00"+key] = ownerNameEntry{name: name, expires: now.Add(c.ttl)}
	}
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
	embedder, err := documentembed.FromEnv(os.Getenv)
	switch {
	case errors.Is(err, documentembed.ErrNotConfigured):
		return composedDocument{store: store, service: documentService{store: store}, close: store.Close}, nil
	case err != nil:
		store.Close()
		return composedDocument{}, fmt.Errorf("compose document embedding model: %w", err)
	}
	indexCfg, err := documentembed.IndexerConfigFromEnv(os.Getenv)
	if err != nil {
		store.Close()
		return composedDocument{}, fmt.Errorf("compose document indexer: %w", err)
	}
	// Writes enqueue index jobs for this model in their own transaction;
	// the indexer drains them off the request path until shutdown.
	store.SetIndexModel(embedder.Model())
	indexer := documentembed.NewIndexer(store, embedder, indexCfg, cfg.Tenant)
	workerCtx, stopWorkers := context.WithCancel(context.WithoutCancel(ctx))
	indexer.Start(workerCtx)
	shutdown := func() {
		stopWorkers()
		indexer.Wait()
		store.Close()
	}
	return composedDocument{store: store, service: documentService{store: store, embedder: embedder, indexer: indexer}, embedder: embedder, indexer: indexer, close: shutdown}, nil
}
