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

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const MaxPageSize = 100

type Summary struct {
	DocumentID, Title, OwnerID, VersionID, Status string
	ScopeKind, ScopeID, SharingState              string
	UpdatedAt, ReviewDueAt                        time.Time
	CanManageAccess                               bool
	CanComment                                    bool
	CanEdit                                       bool
}

type Comment struct {
	ID, DocumentID, VersionID, AuthorID, Body string
	CreatedAt                                 time.Time
}

type ListOptions struct {
	Limit                       int
	Query, Collection, BeforeID string
	BeforeTime                  time.Time
}

type listCursor struct {
	UpdatedAt  time.Time `json:"t"`
	ID         string    `json:"i"`
	Query      string    `json:"q"`
	Collection string    `json:"c"`
	Tenant     string    `json:"tenant"`
	Actor      string    `json:"actor"`
	Expires    int64     `json:"expires"`
	Sort       int       `json:"sort"`
}

type Service interface {
	ListDocuments(context.Context, string, string, ListOptions) ([]Summary, error)
	CreateDocument(context.Context, string, string, string, string) (string, string, error)
	GetDocument(context.Context, string, string, string) (Summary, string, string, error)
	ShareDocument(context.Context, string, string, string, string) error
	ListDocumentComments(context.Context, string, string, string, string) ([]Comment, error)
	AddDocumentComment(context.Context, string, string, string, string, string) (Comment, error)
	CreateDocumentVersion(context.Context, string, string, string, string, string, string) (string, error)
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

func (s *server) verifyCursor(token, tenant, actor, query, collection string) (listCursor, bool) {
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
	if json.Unmarshal(payload, &cursor) != nil || cursor.ID == "" || cursor.UpdatedAt.IsZero() || cursor.Query != query || cursor.Collection != collection || cursor.Tenant != tenant || cursor.Actor != actor || cursor.Sort != 1 || cursor.Expires <= time.Now().Unix() {
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
	query, collection, token := "", "all", ""
	if req != nil {
		query, token = strings.TrimSpace(req.GetQuery()), req.GetPageToken()
		if req.GetCollection() != "" {
			collection = req.GetCollection()
		}
	}
	if len(query) > 200 || (collection != "all" && collection != "private" && collection != "shared") || len(token) > 2048 {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_list", "document list selection is invalid")
	}
	options := ListOptions{Limit: limit + 1, Query: query, Collection: collection}
	if token != "" {
		cursor, ok := s.verifyCursor(token, tenant, actor, query, collection)
		if !ok {
			return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_cursor", "document page cursor is invalid")
		}
		options.BeforeTime, options.BeforeID = cursor.UpdatedAt, cursor.ID
	}
	rows, err := s.service.ListDocuments(ctx, tenant, actor, options)
	if err != nil {
		return nil, owned(err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := &documentv1.ListDocumentsResponse{Documents: make([]*documentv1.DocumentSummary, 0, len(rows))}
	for _, row := range rows {
		d := &documentv1.DocumentSummary{
			DocumentId: row.DocumentID, Title: row.Title, OwnerId: row.OwnerID,
			VersionId: row.VersionID, Status: row.Status, ScopeKind: row.ScopeKind,
			ScopeId: row.ScopeID, SharingState: row.SharingState, CanManageAccess: row.CanManageAccess, CanComment: row.CanComment, CanEdit: row.CanEdit,
		}
		if !row.UpdatedAt.IsZero() {
			d.UpdatedAt = timestamppb.New(row.UpdatedAt)
		}
		if !row.ReviewDueAt.IsZero() {
			d.ReviewDueAt = timestamppb.New(row.ReviewDueAt)
		}
		out.Documents = append(out.Documents, d)
	}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		out.NextPageToken = s.signCursor(listCursor{UpdatedAt: last.UpdatedAt, ID: last.DocumentID, Query: query, Collection: collection, Tenant: tenant, Actor: actor, Sort: 1, Expires: time.Now().Add(15 * time.Minute).Unix()})
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
	d := &documentv1.DocumentSummary{
		DocumentId: summary.DocumentID, Title: summary.Title, OwnerId: summary.OwnerID,
		VersionId: summary.VersionID, Status: summary.Status, ScopeKind: summary.ScopeKind,
		ScopeId: summary.ScopeID, SharingState: summary.SharingState, CanManageAccess: summary.CanManageAccess, CanComment: summary.CanComment, CanEdit: summary.CanEdit,
	}
	if !summary.UpdatedAt.IsZero() {
		d.UpdatedAt = timestamppb.New(summary.UpdatedAt)
	}
	return &documentv1.GetDocumentResponse{Document: d, Markdown: markdown, ContentHash: hash}, nil
}

func (s *server) ShareDocument(ctx context.Context, req *documentv1.ShareDocumentRequest) (*documentv1.ShareDocumentResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetDocumentId()) == "" || strings.TrimSpace(req.GetRecipientId()) == "" || len(req.GetDocumentId()) > 128 || len(req.GetRecipientId()) > 128 || req.GetRecipientId() == actor {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_share", "a document and another person are required")
	}
	if err := s.service.ShareDocument(ctx, tenant, actor, req.GetDocumentId(), req.GetRecipientId()); err != nil {
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
	comment, err := s.service.AddDocumentComment(ctx, tenant, actor, req.GetDocumentId(), req.GetVersionId(), req.GetBody())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.AddDocumentCommentResponse{Comment: commentMessage(comment)}, nil
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

func validCommentRef(docID, versionID string) bool {
	return strings.TrimSpace(docID) != "" && len(docID) <= 128 && strings.TrimSpace(versionID) != "" && len(versionID) <= 128
}

func commentMessage(comment Comment) *documentv1.DocumentComment {
	out := &documentv1.DocumentComment{Id: comment.ID, DocumentId: comment.DocumentID, VersionId: comment.VersionID, AuthorId: comment.AuthorID, Body: comment.Body}
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
