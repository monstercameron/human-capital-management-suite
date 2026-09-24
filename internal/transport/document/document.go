// Package document adapts the authenticated workspace document RPC. Tenant
// and actor are always derived from the trusted request context.
package document

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const MaxPageSize = 100

// MaxMoveDocuments bounds one MoveDocuments request.
const MaxMoveDocuments = 100

// List sort orders.
const (
	SortRelevance  = "relevance"
	SortUpdated    = "updated"
	SortUpdatedAsc = "updated_asc"
	SortTitle      = "title"
	SortTitleDesc  = "title_desc"
	SortOwner      = "owner"
	SortOwnerDesc  = "owner_desc"
)

// Search modes. Meaning falls back to smart when no embedding model is
// configured.
const (
	SearchSmart    = "smart"
	SearchContains = "contains"
	SearchFuzzy    = "fuzzy"
	SearchMeaning  = "meaning"
)

// maxPage bounds page numbers so an offset can never overflow.
const maxPage = 100000

// Share roles.
const (
	RoleViewer    = "viewer"
	RoleCommenter = "commenter"
)

type Summary struct {
	DocumentID, Title, OwnerID, VersionID, Status string
	ScopeKind, ScopeID, SharingState              string
	UpdatedAt, ReviewDueAt                        time.Time
	CanManageAccess                               bool
	CanComment                                    bool
	CanEdit                                       bool
	// Starred and FolderID are the caller's own organization. ReaderCount
	// reaches the wire only when CanManageAccess is true.
	Starred     bool
	FolderID    string
	ReaderCount int
	// TitleKey is the store's title sort key; it never reaches the wire.
	TitleKey string
	// Links is the caller's view of the documents this version links to;
	// GetDocument fills it.
	Links []LinkTarget
	// Chat is the caller's view of the chat references in this version;
	// GetDocument fills it when chat is composed.
	Chat ChatReferences
	// Snippet and Match explain a search hit.
	Snippet, Match string
}

// LinkTarget is one linked document: its title only when Readable.
type LinkTarget struct {
	DocumentID, Title string
	Readable          bool
}

// ListResult is one page, the selection's total, the search mode that
// actually ran and whether meaning search is available.
type ListResult struct {
	Rows              []Summary
	Total             int
	Mode              string
	SemanticAvailable bool
	// SemanticPending counts caller-visible documents not yet indexed for
	// meaning search.
	SemanticPending int
}

type Comment struct {
	ID, DocumentID, VersionID, AuthorID, Body string
	CreatedAt                                 time.Time
	// Anchor is nil for a comment that quotes no passage.
	Anchor             *CommentAnchor
	ParentID           string
	Resolved, Orphaned bool
}

// CommentAnchor is a quoted passage. Start and End are code-point offsets
// into the plain text of the version being read, filled by the server.
type CommentAnchor struct {
	BlockID, Quote, Prefix, Suffix string
	Start, End                     int
}

// Anchor and reply bounds, in code points.
const (
	maxAnchorQuote   = 500
	maxAnchorContext = 64
)

// Folder is one of the caller's folders.
type Folder struct {
	ID, Name      string
	DocumentCount int
	CreatedAt     time.Time
}

// Library is the caller's folders and collection counts.
type Library struct {
	Folders                    []Folder
	All, Mine, Shared, Starred int
}

// AccessEntry is one person who can read a document.
type AccessEntry struct {
	SubjectKind, SubjectID, Role string
	Removable                    bool
}

type ListOptions struct {
	Limit                       int
	Query, Collection, BeforeID string
	BeforeTime                  time.Time
	FolderID, OwnerID, Sort     string
	StarredOnly                 bool
	// AfterTitle is the title-order cursor key; nil on the first page.
	AfterTitle *string
	// Mode is the search mode; Offset skips rows (page paging, and every
	// page after the first while searching).
	Mode   string
	Offset int
}

// listSelection is everything a page token must match besides tenant and
// actor.
type listSelection struct {
	Query, Collection, Folder, Owner, Order, Mode string
	Starred                                       bool
}

// listCursor binds a page token to the whole selection that produced it.
// Tokens of an older shape carry another Version and are refused.
type listCursor struct {
	UpdatedAt  time.Time `json:"t"`
	ID         string    `json:"i"`
	TitleKey   string    `json:"k,omitempty"`
	Query      string    `json:"q"`
	Collection string    `json:"c"`
	Folder     string    `json:"f,omitempty"`
	Starred    bool      `json:"s,omitempty"`
	Owner      string    `json:"o,omitempty"`
	Order      string    `json:"r"`
	Mode       string    `json:"m,omitempty"`
	Offset     int       `json:"n,omitempty"`
	Tenant     string    `json:"tenant"`
	Actor      string    `json:"actor"`
	Expires    int64     `json:"expires"`
	Version    int       `json:"sort"`
}

const cursorVersion = 3

// Deployment is a scoped deployment pointer: the default pointer or a
// team/channel placement. Official is true only when a placement bound a
// custodian and review due date (HUB-014), never a bare deploy.
type Deployment struct {
	ID, DocumentID, VersionID, ScopeKind, ScopeID string
	DeployerID, CustodianID                       string
	EffectiveAt, ReviewDueAt                      time.Time
	Official                                      bool
}

// OwnershipTransfer is one immutable custody-change record (HUB-040).
type OwnershipTransfer struct {
	ID, DocumentID, PriorOwnerID, SuccessorOwnerID, Reason, TransferredBy string
	CreatedAt                                                             time.Time
}

// CrossCompanyGrant is one bilateral cross-company document grant
// (HUB-015).
type CrossCompanyGrant struct {
	ID, DocumentID, HostTenant, ConsumerTenant, Classification, Residency string
	Version                                                               uint64
	Proposed, AcceptedByHost, AcceptedByConsumer                          bool
	ExpiresAt, RevokedAt                                                  time.Time
}

// SearchFilters narrows a typed lexical search (HUB-030/HUB-031). An empty
// field or zero time skips that filter.
type SearchFilters struct {
	TeamID, ChannelID, Status, OwnerID, Locale string
	DateFrom, DateTo                           time.Time
}

// SearchHit is one authorized, filtered, currently deployed result.
type SearchHit struct {
	DocumentID, VersionID, Title, Status, Locale, OwnerID string
	DeployedAt                                            time.Time
	Score, MatchedTerms                                   int
}

// SearchResult is one page of hits plus the authorized, filtered total.
type SearchResult struct {
	Hits  []SearchHit
	Total int
}

type Service interface {
	ListDocuments(ctx context.Context, tenant, actor string, options ListOptions) (ListResult, error)
	CreateDocument(context.Context, string, string, string, string) (string, string, error)
	GetDocument(context.Context, string, string, string) (Summary, string, string, error)
	ShareDocument(ctx context.Context, tenant, actor, documentID, recipientID, role string) error
	ListDocumentComments(context.Context, string, string, string, string) ([]Comment, error)
	// AddDocumentComment takes tenant, actor, document, version and body,
	// an optional anchor (nil for none) and an optional parent comment.
	AddDocumentComment(ctx context.Context, tenant, actor, documentID, versionID, body string, anchor *CommentAnchor, parentID string) (Comment, error)
	ResolveDocumentComment(ctx context.Context, tenant, actor, documentID, commentID string, resolved bool) error
	CreateDocumentVersion(context.Context, string, string, string, string, string, string) (string, error)
	GetDocumentLibrary(ctx context.Context, tenant, actor string) (Library, error)
	CreateDocumentFolder(ctx context.Context, tenant, actor, name string) (Folder, error)
	RenameDocumentFolder(ctx context.Context, tenant, actor, folderID, name string) error
	DeleteDocumentFolder(ctx context.Context, tenant, actor, folderID string) error
	MoveDocuments(ctx context.Context, tenant, actor string, documentIDs []string, folderID string) error
	SetDocumentStarred(ctx context.Context, tenant, actor, documentID string, starred bool) error
	ListDocumentAccess(ctx context.Context, tenant, actor, documentID string) ([]AccessEntry, error)
	RevokeDocumentAccess(ctx context.Context, tenant, actor, documentID, subjectKind, subjectID string) error
	// PlaceDocument deploys a reviewed version into a team/channel scope as
	// an official Docs-tab placement (HUB-014).
	PlaceDocument(ctx context.Context, tenant, actor, documentID, versionID, scopeID, expectedLive, custodianID string, reviewDueAt time.Time) (Deployment, error)
	// GetDocumentPlacement resolves the live official placement for one
	// team/channel scope, rechecking the caller's live eligibility
	// (HUB-013).
	GetDocumentPlacement(ctx context.Context, tenant, actor, documentID, scopeID string) (Deployment, error)
	// TransferDocumentOwnership moves custody of a document to a successor
	// custodian (HUB-040).
	TransferDocumentOwnership(ctx context.Context, tenant, actor, documentID, successorOwnerID, reason string) (OwnershipTransfer, error)
	ListDocumentOwnershipHistory(ctx context.Context, tenant, actor, documentID string) ([]OwnershipTransfer, error)
	// ProposeCrossCompanyGrant, AcceptCrossCompanyGrant and
	// RevokeCrossCompanyGrant gate a bilateral cross-company document grant
	// (HUB-015). The caller's own tenant is the host for propose/revoke and
	// the consumer for accept.
	ProposeCrossCompanyGrant(ctx context.Context, tenant, actor, documentID, consumerTenant, classification, residency string, expiresAt time.Time) (CrossCompanyGrant, error)
	AcceptCrossCompanyGrant(ctx context.Context, tenant, actor, hostTenant, grantID string) (CrossCompanyGrant, error)
	RevokeCrossCompanyGrant(ctx context.Context, tenant, actor, grantID string) error
	// SearchDocuments runs an authorized, typed-filter lexical search over
	// currently deployed versions (HUB-030).
	SearchDocuments(ctx context.Context, tenant, actor, query string, filters SearchFilters) (SearchResult, error)
	// AgentSearchDocuments runs the same search on behalf of an installed
	// chat agent, acting within conversationID, for requesterID (HUB-031).
	AgentSearchDocuments(ctx context.Context, tenant, conversationID, agentID, requesterID, query string, filters SearchFilters) (SearchResult, error)
}

type server struct {
	documentv1.UnimplementedDocumentServiceServer
	service   Service
	cursorKey []byte
}

func Register(srv *grpc.Server, service Service, cursorKey []byte) {
	if srv == nil {
		return
	}
	documentv1.RegisterDocumentServiceServer(srv, &server{service: service, cursorKey: append([]byte(nil), cursorKey...)})
}

func (s *server) signCursor(cursor listCursor) string {
	if len(s.cursorKey) == 0 {
		return ""
	}
	payload, _ := json.Marshal(cursor)
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *server) verifyCursor(token, tenant, actor string, selection listSelection) (listCursor, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(s.cursorKey) == 0 {
		return listCursor{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return listCursor{}, false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return listCursor{}, false
	}
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return listCursor{}, false
	}
	var cursor listCursor
	offsetPaged := selection.Query != "" || selection.Order == SortOwner || selection.Order == SortOwnerDesc
	if json.Unmarshal(payload, &cursor) != nil || (cursor.ID == "" && !offsetPaged) || cursor.Version != cursorVersion || cursor.Tenant != tenant || cursor.Actor != actor || cursor.Expires <= time.Now().Unix() {
		return listCursor{}, false
	}
	if cursor.Query != selection.Query || cursor.Collection != selection.Collection || cursor.Folder != selection.Folder || cursor.Owner != selection.Owner || cursor.Starred != selection.Starred || cursor.Order != selection.Order || cursor.Mode != selection.Mode || cursor.Offset < 0 {
		return listCursor{}, false
	}
	if selection.Query == "" && (cursor.Order == SortUpdated || cursor.Order == SortUpdatedAsc) && cursor.UpdatedAt.IsZero() {
		return listCursor{}, false
	}
	return cursor, true
}

func (s *server) caller(ctx context.Context) (string, string, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return "", "", envelope.New(envelope.CodeUnauthenticated, "document.no_principal", "the request carries no authenticated principal")
	}
	if p.SubjectKind() != trust.SubjectKindHuman {
		return "", "", envelope.New(envelope.CodePermissionDenied, "document.human_required", "a human principal is required")
	}
	if s.service == nil {
		return "", "", envelope.New(envelope.CodeUnavailable, "document.service.unavailable", "document service is not configured")
	}
	return p.Tenant().String(), p.Subject(), nil
}

func (s *server) ListDocuments(ctx context.Context, req *documentv1.ListDocumentsRequest) (*documentv1.ListDocumentsResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	limit := MaxPageSize
	if req != nil && req.GetPageSize() > 0 && req.GetPageSize() < MaxPageSize {
		limit = int(req.GetPageSize())
	}
	selection := listSelection{Collection: "all", Mode: SearchSmart}
	token, page := "", 0
	if req != nil {
		selection.Query, token, page = strings.TrimSpace(req.GetQuery()), req.GetPageToken(), int(req.GetPage())
		if req.GetCollection() != "" {
			selection.Collection = req.GetCollection()
		}
		selection.Order = req.GetSort()
		if req.GetSearchMode() != "" {
			selection.Mode = req.GetSearchMode()
		}
		selection.Folder, selection.Owner, selection.Starred = req.GetFolderId(), req.GetOwnerId(), req.GetStarredOnly()
	}
	switch {
	case selection.Order == "" && selection.Query != "":
		selection.Order = SortRelevance
	case selection.Order == "" || (selection.Order == SortRelevance && selection.Query == ""):
		selection.Order = SortUpdated
	}
	validOrder := map[string]bool{SortRelevance: true, SortUpdated: true, SortUpdatedAsc: true, SortTitle: true, SortTitleDesc: true, SortOwner: true, SortOwnerDesc: true}[selection.Order]
	// Relevance and owner orders page by offset; the field orders of a plain
	// listing page by keyset.
	offsetPaged := selection.Query != "" || selection.Order == SortOwner || selection.Order == SortOwnerDesc
	validMode := map[string]bool{SearchSmart: true, SearchContains: true, SearchFuzzy: true, SearchMeaning: true}[selection.Mode]
	if len(selection.Query) > 200 || (selection.Collection != "all" && selection.Collection != "private" && selection.Collection != "shared") || len(token) > 2048 ||
		!validOrder || !validMode || page < 0 || page > maxPage || !validOptionalID(selection.Folder) || !validOptionalID(selection.Owner) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_list", "document list selection is invalid")
	}
	options := ListOptions{Limit: limit + 1, Query: selection.Query, Collection: selection.Collection, FolderID: selection.Folder, OwnerID: selection.Owner, Sort: selection.Order, StarredOnly: selection.Starred, Mode: selection.Mode}
	switch {
	case page > 0:
		options.Offset = (page - 1) * limit
	case token != "":
		cursor, ok := s.verifyCursor(token, tenant, actor, selection)
		if !ok {
			return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_cursor", "document page cursor is invalid")
		}
		switch {
		case offsetPaged:
			options.Offset = cursor.Offset
		case selection.Order == SortTitle || selection.Order == SortTitleDesc:
			key := cursor.TitleKey
			options.AfterTitle, options.BeforeID = &key, cursor.ID
		default:
			options.BeforeTime, options.BeforeID = cursor.UpdatedAt, cursor.ID
		}
	}
	result, err := s.service.ListDocuments(ctx, tenant, actor, options)
	if err != nil {
		return nil, owned(err)
	}
	rows := result.Rows
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := &documentv1.ListDocumentsResponse{
		Documents: make([]*documentv1.DocumentSummary, 0, len(rows)), TotalCount: clampCount(result.Total),
		SearchMode: result.Mode, SemanticAvailable: result.SemanticAvailable, SemanticPending: clampCount(result.SemanticPending),
	}
	for _, row := range rows {
		out.Documents = append(out.Documents, summaryMessage(row))
	}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		cursor := listCursor{
			Query: selection.Query, Collection: selection.Collection, Folder: selection.Folder, Starred: selection.Starred, Owner: selection.Owner, Order: selection.Order, Mode: selection.Mode,
			Tenant: tenant, Actor: actor, Version: cursorVersion, Expires: time.Now().Add(15 * time.Minute).Unix(),
		}
		if offsetPaged {
			cursor.Offset = options.Offset + len(rows)
		} else {
			cursor.UpdatedAt, cursor.ID, cursor.TitleKey = last.UpdatedAt, last.DocumentID, last.TitleKey
		}
		out.NextPageToken = s.signCursor(cursor)
	}
	return out, nil
}

func (s *server) CreateDocument(ctx context.Context, req *documentv1.CreateDocumentRequest) (*documentv1.CreateDocumentResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetTitle()) == "" || len(req.GetTitle()) > 256 || len(req.GetMarkdown()) > 1<<20 {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_argument", "title or document body is invalid")
	}
	id, versionID, err := s.service.CreateDocument(ctx, tenant, actor, req.GetTitle(), req.GetMarkdown())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.CreateDocumentResponse{DocumentId: id, VersionId: versionID}, nil
}

func (s *server) GetDocument(ctx context.Context, req *documentv1.GetDocumentRequest) (*documentv1.GetDocumentResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetDocumentId()) == "" {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_id", "a document ID is required")
	}
	summary, markdown, hash, err := s.service.GetDocument(ctx, tenant, actor, req.GetDocumentId())
	if err != nil {
		return nil, owned(err)
	}
	out := &documentv1.GetDocumentResponse{Document: summaryMessage(summary), Markdown: markdown, ContentHash: hash, Links: make([]*documentv1.DocumentLinkTarget, 0, len(summary.Links))}
	for _, l := range summary.Links {
		title := ""
		if l.Readable {
			title = l.Title
		}
		out.Links = append(out.Links, &documentv1.DocumentLinkTarget{DocumentId: l.DocumentID, Title: title, Readable: l.Readable})
	}
	chatReferenceMessages(summary.Chat, out)
	return out, nil
}

func (s *server) ShareDocument(ctx context.Context, req *documentv1.ShareDocumentRequest) (*documentv1.ShareDocumentResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetDocumentId()) == "" || strings.TrimSpace(req.GetRecipientId()) == "" || len(req.GetDocumentId()) > 128 || len(req.GetRecipientId()) > 128 || req.GetRecipientId() == actor {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_share", "a document and another person are required")
	}
	role := req.GetRole()
	if role == "" {
		role = RoleCommenter
	}
	if role != RoleViewer && role != RoleCommenter {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_role", "the share role must be viewer or commenter")
	}
	if err := s.service.ShareDocument(ctx, tenant, actor, req.GetDocumentId(), req.GetRecipientId(), role); err != nil {
		return nil, owned(err)
	}
	return &documentv1.ShareDocumentResponse{}, nil
}

func (s *server) ListDocumentComments(ctx context.Context, req *documentv1.ListDocumentCommentsRequest) (*documentv1.ListDocumentCommentsResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validCommentRef(req.GetDocumentId(), req.GetVersionId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_comment_ref", "a document and version are required")
	}
	comments, err := s.service.ListDocumentComments(ctx, tenant, actor, req.GetDocumentId(), req.GetVersionId())
	if err != nil {
		return nil, owned(err)
	}
	out := &documentv1.ListDocumentCommentsResponse{Comments: make([]*documentv1.DocumentComment, 0, len(comments))}
	for _, comment := range comments {
		out.Comments = append(out.Comments, commentMessage(comment))
	}
	return out, nil
}

func (s *server) AddDocumentComment(ctx context.Context, req *documentv1.AddDocumentCommentRequest) (*documentv1.AddDocumentCommentResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validCommentRef(req.GetDocumentId(), req.GetVersionId()) || strings.TrimSpace(req.GetBody()) == "" || len(req.GetBody()) > 16000 {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_comment", "a document, version and comment of at most 16000 bytes are required")
	}
	var anchor *CommentAnchor
	if a := req.GetAnchor(); a != nil && (a.GetQuote() != "" || a.GetPrefix() != "" || a.GetSuffix() != "" || a.GetBlockId() != "") {
		anchor = &CommentAnchor{BlockID: a.GetBlockId(), Quote: a.GetQuote(), Prefix: a.GetPrefix(), Suffix: a.GetSuffix()}
		if strings.TrimSpace(anchor.Quote) == "" || utf8.RuneCountInString(anchor.Quote) > maxAnchorQuote ||
			utf8.RuneCountInString(anchor.Prefix) > maxAnchorContext || utf8.RuneCountInString(anchor.Suffix) > maxAnchorContext ||
			len(anchor.BlockID) > 128 || req.GetParentCommentId() != "" {
			return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_anchor", "an anchor needs a quote of at most 500 characters, context of at most 64 characters either side, and no parent comment")
		}
	}
	if !validOptionalID(req.GetParentCommentId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_parent", "the parent comment ID is invalid")
	}
	comment, err := s.service.AddDocumentComment(ctx, tenant, actor, req.GetDocumentId(), req.GetVersionId(), req.GetBody(), anchor, req.GetParentCommentId())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.AddDocumentCommentResponse{Comment: commentMessage(comment)}, nil
}

func (s *server) ResolveDocumentComment(ctx context.Context, req *documentv1.ResolveDocumentCommentRequest) (*documentv1.ResolveDocumentCommentResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if !validID(req.GetDocumentId()) || !validID(req.GetCommentId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_comment_ref", "a document and comment are required")
	}
	if err := s.service.ResolveDocumentComment(ctx, tenant, actor, req.GetDocumentId(), req.GetCommentId(), req.GetResolved()); err != nil {
		return nil, owned(err)
	}
	return &documentv1.ResolveDocumentCommentResponse{}, nil
}

func (s *server) CreateDocumentVersion(ctx context.Context, req *documentv1.CreateDocumentVersionRequest) (*documentv1.CreateDocumentVersionResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validCommentRef(req.GetDocumentId(), req.GetBaseVersionId()) || strings.TrimSpace(req.GetTitle()) == "" || len(req.GetTitle()) > 256 || strings.TrimSpace(req.GetMarkdown()) == "" || len(req.GetMarkdown()) > 1<<20 {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_version", "a document, base version, title, and body of valid size are required")
	}
	versionID, err := s.service.CreateDocumentVersion(ctx, tenant, actor, req.GetDocumentId(), req.GetBaseVersionId(), req.GetTitle(), req.GetMarkdown())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.CreateDocumentVersionResponse{VersionId: versionID}, nil
}

func (s *server) GetDocumentLibrary(ctx context.Context, _ *documentv1.GetDocumentLibraryRequest) (*documentv1.GetDocumentLibraryResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	lib, err := s.service.GetDocumentLibrary(ctx, tenant, actor)
	if err != nil {
		return nil, owned(err)
	}
	out := &documentv1.GetDocumentLibraryResponse{
		Folders:  make([]*documentv1.DocumentFolder, 0, len(lib.Folders)),
		AllCount: clampCount(lib.All), MineCount: clampCount(lib.Mine), SharedCount: clampCount(lib.Shared), StarredCount: clampCount(lib.Starred),
	}
	for _, folder := range lib.Folders {
		out.Folders = append(out.Folders, folderMessage(folder))
	}
	return out, nil
}

func (s *server) CreateDocumentFolder(ctx context.Context, req *documentv1.CreateDocumentFolderRequest) (*documentv1.CreateDocumentFolderResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	name, ok := folderName(req.GetName())
	if !ok {
		return nil, invalidFolderName()
	}
	folder, err := s.service.CreateDocumentFolder(ctx, tenant, actor, name)
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.CreateDocumentFolderResponse{Folder: folderMessage(folder)}, nil
}

func (s *server) RenameDocumentFolder(ctx context.Context, req *documentv1.RenameDocumentFolderRequest) (*documentv1.RenameDocumentFolderResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if !validID(req.GetFolderId()) {
		return nil, invalidFolderID()
	}
	name, ok := folderName(req.GetName())
	if !ok {
		return nil, invalidFolderName()
	}
	if err := s.service.RenameDocumentFolder(ctx, tenant, actor, req.GetFolderId(), name); err != nil {
		return nil, owned(err)
	}
	return &documentv1.RenameDocumentFolderResponse{}, nil
}

func (s *server) DeleteDocumentFolder(ctx context.Context, req *documentv1.DeleteDocumentFolderRequest) (*documentv1.DeleteDocumentFolderResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if !validID(req.GetFolderId()) {
		return nil, invalidFolderID()
	}
	if err := s.service.DeleteDocumentFolder(ctx, tenant, actor, req.GetFolderId()); err != nil {
		return nil, owned(err)
	}
	return &documentv1.DeleteDocumentFolderResponse{}, nil
}

func (s *server) MoveDocuments(ctx context.Context, req *documentv1.MoveDocumentsRequest) (*documentv1.MoveDocumentsResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	ids := req.GetDocumentIds()
	valid := len(ids) > 0 && len(ids) <= MaxMoveDocuments && validOptionalID(req.GetFolderId())
	for _, id := range ids {
		valid = valid && validID(id)
	}
	if !valid {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_move", "one to 100 document IDs and an optional folder are required")
	}
	if err := s.service.MoveDocuments(ctx, tenant, actor, append([]string(nil), ids...), req.GetFolderId()); err != nil {
		return nil, owned(err)
	}
	return &documentv1.MoveDocumentsResponse{}, nil
}

func (s *server) SetDocumentStarred(ctx context.Context, req *documentv1.SetDocumentStarredRequest) (*documentv1.SetDocumentStarredResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if !validID(req.GetDocumentId()) {
		return nil, invalidDocumentID()
	}
	if err := s.service.SetDocumentStarred(ctx, tenant, actor, req.GetDocumentId(), req.GetStarred()); err != nil {
		return nil, owned(err)
	}
	return &documentv1.SetDocumentStarredResponse{}, nil
}

func (s *server) ListDocumentAccess(ctx context.Context, req *documentv1.ListDocumentAccessRequest) (*documentv1.ListDocumentAccessResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if !validID(req.GetDocumentId()) {
		return nil, invalidDocumentID()
	}
	entries, err := s.service.ListDocumentAccess(ctx, tenant, actor, req.GetDocumentId())
	if err != nil {
		return nil, owned(err)
	}
	out := &documentv1.ListDocumentAccessResponse{Entries: make([]*documentv1.DocumentAccessEntry, 0, len(entries))}
	for _, entry := range entries {
		out.Entries = append(out.Entries, &documentv1.DocumentAccessEntry{SubjectKind: entry.SubjectKind, SubjectId: entry.SubjectID, Role: entry.Role, Removable: entry.Removable})
	}
	return out, nil
}

func (s *server) RevokeDocumentAccess(ctx context.Context, req *documentv1.RevokeDocumentAccessRequest) (*documentv1.RevokeDocumentAccessResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	kind := req.GetSubjectKind()
	if kind == "" {
		kind = "person"
	}
	if !validID(req.GetDocumentId()) || kind != "person" || !validID(req.GetSubjectId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_revoke", "a document and a person are required")
	}
	if err := s.service.RevokeDocumentAccess(ctx, tenant, actor, req.GetDocumentId(), kind, req.GetSubjectId()); err != nil {
		return nil, owned(err)
	}
	return &documentv1.RevokeDocumentAccessResponse{}, nil
}

func (s *server) PlaceDocument(ctx context.Context, req *documentv1.PlaceDocumentRequest) (*documentv1.PlaceDocumentResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetDocumentId()) || !validID(req.GetVersionId()) || !validID(req.GetScopeId()) || !validOptionalID(req.GetExpectedLive()) || !validID(req.GetCustodianId()) || req.GetReviewDueAt() == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_placement", "a document, version, scope, custodian and review due date are required")
	}
	deployment, err := s.service.PlaceDocument(ctx, tenant, actor, req.GetDocumentId(), req.GetVersionId(), req.GetScopeId(), req.GetExpectedLive(), req.GetCustodianId(), req.GetReviewDueAt().AsTime())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.PlaceDocumentResponse{Deployment: deploymentMessage(deployment)}, nil
}

func (s *server) GetDocumentPlacement(ctx context.Context, req *documentv1.GetDocumentPlacementRequest) (*documentv1.GetDocumentPlacementResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetDocumentId()) || !validID(req.GetScopeId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_placement_ref", "a document and scope are required")
	}
	deployment, err := s.service.GetDocumentPlacement(ctx, tenant, actor, req.GetDocumentId(), req.GetScopeId())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.GetDocumentPlacementResponse{Deployment: deploymentMessage(deployment)}, nil
}

func (s *server) TransferDocumentOwnership(ctx context.Context, req *documentv1.TransferDocumentOwnershipRequest) (*documentv1.TransferDocumentOwnershipResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetDocumentId()) || !validID(req.GetSuccessorOwnerId()) || strings.TrimSpace(req.GetReason()) == "" || len(req.GetReason()) > 2000 {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_transfer", "a document, successor and reason are required")
	}
	transfer, err := s.service.TransferDocumentOwnership(ctx, tenant, actor, req.GetDocumentId(), req.GetSuccessorOwnerId(), req.GetReason())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.TransferDocumentOwnershipResponse{Transfer: ownershipTransferMessage(transfer)}, nil
}

func (s *server) ListDocumentOwnershipHistory(ctx context.Context, req *documentv1.ListDocumentOwnershipHistoryRequest) (*documentv1.ListDocumentOwnershipHistoryResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetDocumentId()) {
		return nil, invalidDocumentID()
	}
	history, err := s.service.ListDocumentOwnershipHistory(ctx, tenant, actor, req.GetDocumentId())
	if err != nil {
		return nil, owned(err)
	}
	out := &documentv1.ListDocumentOwnershipHistoryResponse{Transfers: make([]*documentv1.DocumentOwnershipTransfer, 0, len(history))}
	for _, t := range history {
		out.Transfers = append(out.Transfers, ownershipTransferMessage(t))
	}
	return out, nil
}

func (s *server) ProposeCrossCompanyGrant(ctx context.Context, req *documentv1.ProposeCrossCompanyGrantRequest) (*documentv1.ProposeCrossCompanyGrantResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetDocumentId()) || !validID(req.GetConsumerTenant()) || strings.TrimSpace(req.GetClassification()) == "" || strings.TrimSpace(req.GetResidency()) == "" || req.GetExpiresAt() == nil {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_grant_proposal", "a document, consumer tenant, classification, residency and expiry are required")
	}
	grant, err := s.service.ProposeCrossCompanyGrant(ctx, tenant, actor, req.GetDocumentId(), req.GetConsumerTenant(), req.GetClassification(), req.GetResidency(), req.GetExpiresAt().AsTime())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.ProposeCrossCompanyGrantResponse{Grant: crossCompanyGrantMessage(grant)}, nil
}

func (s *server) AcceptCrossCompanyGrant(ctx context.Context, req *documentv1.AcceptCrossCompanyGrantRequest) (*documentv1.AcceptCrossCompanyGrantResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetHostTenant()) || !validID(req.GetGrantId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_grant_accept", "a host tenant and grant are required")
	}
	grant, err := s.service.AcceptCrossCompanyGrant(ctx, tenant, actor, req.GetHostTenant(), req.GetGrantId())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.AcceptCrossCompanyGrantResponse{Grant: crossCompanyGrantMessage(grant)}, nil
}

func (s *server) RevokeCrossCompanyGrant(ctx context.Context, req *documentv1.RevokeCrossCompanyGrantRequest) (*documentv1.RevokeCrossCompanyGrantResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetGrantId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_grant_revoke", "a grant is required")
	}
	if err := s.service.RevokeCrossCompanyGrant(ctx, tenant, actor, req.GetGrantId()); err != nil {
		return nil, owned(err)
	}
	return &documentv1.RevokeCrossCompanyGrantResponse{}, nil
}

func searchFiltersFromProto(f *documentv1.DocumentSearchFilters) SearchFilters {
	if f == nil {
		return SearchFilters{}
	}
	out := SearchFilters{TeamID: f.GetTeamId(), ChannelID: f.GetChannelId(), Status: f.GetStatus(), OwnerID: f.GetOwnerId(), Locale: f.GetLocale()}
	if f.GetDateFrom() != nil {
		out.DateFrom = f.GetDateFrom().AsTime()
	}
	if f.GetDateTo() != nil {
		out.DateTo = f.GetDateTo().AsTime()
	}
	return out
}

func (s *server) SearchDocuments(ctx context.Context, req *documentv1.SearchDocumentsRequest) (*documentv1.SearchDocumentsResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || len(req.GetQuery()) > 200 {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_search", "the search query is invalid")
	}
	result, err := s.service.SearchDocuments(ctx, tenant, actor, req.GetQuery(), searchFiltersFromProto(req.GetFilters()))
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.SearchDocumentsResponse{Hits: searchHitMessages(result.Hits), Total: clampCount(result.Total)}, nil
}

func (s *server) AgentSearchDocuments(ctx context.Context, req *documentv1.AgentSearchDocumentsRequest) (*documentv1.AgentSearchDocumentsResponse, error) {
	tenant, _, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetConversationId()) || !validID(req.GetAgentId()) || !validID(req.GetRequesterId()) || len(req.GetQuery()) > 200 {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_agent_search", "a conversation, agent, requester and valid query are required")
	}
	result, err := s.service.AgentSearchDocuments(ctx, tenant, req.GetConversationId(), req.GetAgentId(), req.GetRequesterId(), req.GetQuery(), searchFiltersFromProto(req.GetFilters()))
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.AgentSearchDocumentsResponse{Hits: searchHitMessages(result.Hits), Total: clampCount(result.Total)}, nil
}

func deploymentMessage(d Deployment) *documentv1.DocumentDeployment {
	out := &documentv1.DocumentDeployment{
		Id: d.ID, DocumentId: d.DocumentID, VersionId: d.VersionID, ScopeKind: d.ScopeKind, ScopeId: d.ScopeID,
		DeployerId: d.DeployerID, CustodianId: d.CustodianID, Official: d.Official,
	}
	if !d.EffectiveAt.IsZero() {
		out.EffectiveAt = timestamppb.New(d.EffectiveAt)
	}
	if !d.ReviewDueAt.IsZero() {
		out.ReviewDueAt = timestamppb.New(d.ReviewDueAt)
	}
	return out
}

func ownershipTransferMessage(t OwnershipTransfer) *documentv1.DocumentOwnershipTransfer {
	out := &documentv1.DocumentOwnershipTransfer{
		Id: t.ID, DocumentId: t.DocumentID, PriorOwnerId: t.PriorOwnerID, SuccessorOwnerId: t.SuccessorOwnerID,
		Reason: t.Reason, TransferredBy: t.TransferredBy,
	}
	if !t.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(t.CreatedAt)
	}
	return out
}

func crossCompanyGrantMessage(g CrossCompanyGrant) *documentv1.DocumentCrossCompanyGrant {
	out := &documentv1.DocumentCrossCompanyGrant{
		Id: g.ID, DocumentId: g.DocumentID, HostTenant: g.HostTenant, ConsumerTenant: g.ConsumerTenant,
		Classification: g.Classification, Residency: g.Residency, Version: g.Version,
		Proposed: g.Proposed, AcceptedByHost: g.AcceptedByHost, AcceptedByConsumer: g.AcceptedByConsumer,
	}
	if !g.ExpiresAt.IsZero() {
		out.ExpiresAt = timestamppb.New(g.ExpiresAt)
	}
	if !g.RevokedAt.IsZero() {
		out.RevokedAt = timestamppb.New(g.RevokedAt)
	}
	return out
}

func searchHitMessages(hits []SearchHit) []*documentv1.DocumentSearchHit {
	out := make([]*documentv1.DocumentSearchHit, 0, len(hits))
	for _, h := range hits {
		m := &documentv1.DocumentSearchHit{
			DocumentId: h.DocumentID, VersionId: h.VersionID, Title: h.Title, Status: h.Status, Locale: h.Locale,
			OwnerId: h.OwnerID, Score: clampCount(h.Score), MatchedTerms: clampCount(h.MatchedTerms),
		}
		if !h.DeployedAt.IsZero() {
			m.DeployedAt = timestamppb.New(h.DeployedAt)
		}
		out = append(out, m)
	}
	return out
}

func summaryMessage(row Summary) *documentv1.DocumentSummary {
	d := &documentv1.DocumentSummary{
		DocumentId: row.DocumentID, Title: row.Title, OwnerId: row.OwnerID,
		VersionId: row.VersionID, Status: row.Status, ScopeKind: row.ScopeKind,
		ScopeId: row.ScopeID, SharingState: row.SharingState, CanManageAccess: row.CanManageAccess, CanComment: row.CanComment, CanEdit: row.CanEdit,
		Starred: row.Starred, FolderId: row.FolderID, Snippet: row.Snippet, Match: row.Match,
	}
	if row.CanManageAccess {
		d.ReaderCount = clampCount(row.ReaderCount)
	}
	if !row.UpdatedAt.IsZero() {
		d.UpdatedAt = timestamppb.New(row.UpdatedAt)
	}
	if !row.ReviewDueAt.IsZero() {
		d.ReviewDueAt = timestamppb.New(row.ReviewDueAt)
	}
	return d
}

func folderMessage(folder Folder) *documentv1.DocumentFolder {
	out := &documentv1.DocumentFolder{FolderId: folder.ID, Name: folder.Name, DocumentCount: clampCount(folder.DocumentCount)}
	if !folder.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(folder.CreatedAt)
	}
	return out
}

// folderName trims a folder name and accepts 1 to 80 characters.
func folderName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 80
}

func invalidFolderName() error {
	return envelope.New(envelope.CodeInvalidArgument, "document.invalid_folder_name", "a folder name of 1 to 80 characters is required")
}

func invalidFolderID() error {
	return envelope.New(envelope.CodeInvalidArgument, "document.invalid_folder", "a folder ID is required")
}

func invalidDocumentID() error {
	return envelope.New(envelope.CodeInvalidArgument, "document.invalid_id", "a document ID is required")
}

func clampCount(n int) int32 {
	if n < 0 {
		return 0
	}
	if n > 1<<31-1 {
		return 1<<31 - 1
	}
	return int32(n)
}

func validOptionalID(id string) bool {
	return len(id) <= 128 && id == strings.TrimSpace(id)
}

func validID(id string) bool {
	return id != "" && validOptionalID(id)
}

func validCommentRef(docID, versionID string) bool {
	return strings.TrimSpace(docID) != "" && len(docID) <= 128 && strings.TrimSpace(versionID) != "" && len(versionID) <= 128
}

func commentMessage(comment Comment) *documentv1.DocumentComment {
	out := &documentv1.DocumentComment{Id: comment.ID, DocumentId: comment.DocumentID, VersionId: comment.VersionID, AuthorId: comment.AuthorID, Body: comment.Body,
		ParentCommentId: comment.ParentID, Resolved: comment.Resolved, AnchorOrphaned: comment.Orphaned}
	if a := comment.Anchor; a != nil {
		out.Anchor = &documentv1.CommentAnchor{BlockId: a.BlockID, Quote: a.Quote, Prefix: a.Prefix, Suffix: a.Suffix, Start: int32(max(a.Start, -1)), End: int32(max(a.End, -1))}
	}
	if !comment.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(comment.CreatedAt)
	}
	return out
}

func owned(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := envelope.As(err); ok {
		return err
	}
	code := envelope.CodeUnspecified
	reason := "document.internal_error"
	if errors.Is(err, context.DeadlineExceeded) {
		code, reason = envelope.CodeDeadlineExceeded, "transport.deadline_exceeded"
	} else if errors.Is(err, context.Canceled) {
		code, reason = envelope.CodeUnavailable, "transport.request_canceled"
	}
	e := envelope.New(code, reason, "the document operation could not be completed")
	if code == envelope.CodeUnspecified {
		e.WithDiagnostic(err)
	}
	return e
}
