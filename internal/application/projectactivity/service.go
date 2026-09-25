// Package projectactivity adapts project comment and activity domain behavior
// to the authenticated application boundary.
package projectactivity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	projectaccess "github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	domain "github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrInvalidPrincipal = errors.New("projectactivity application: trusted principal is required")
	ErrInvalidRequest   = errors.New("projectactivity application: invalid request")
	ErrUnavailable      = errors.New("projectactivity application: required port unavailable")
)

// Authorizer checks current membership and capability from trusted identity.
type Authorizer interface {
	Authorize(context.Context, *trust.Principal, string, projectaccess.Capability) error
}

// IdempotentStore is the persistence command boundary. Implementations must
// commit the idempotency fingerprint/result with the revision and activity.
// It embeds the read/direct domain port so keyset pages share the same store.
type IdempotentStore interface {
	domain.Store
	CreateIdempotent(context.Context, domain.Comment, domain.Activity, string) (domain.Comment, error)
	CorrectIdempotent(context.Context, string, string, string, string, uint64, domain.Revision, domain.Activity, string) (domain.Comment, error)
	DeleteIdempotent(context.Context, string, string, string, string, uint64, domain.Revision, domain.Activity, string) (domain.Comment, error)
}

type Config struct {
	Authorizer Authorizer
	Store      IdempotentStore
	Sanitizer  domain.TextSanitizer
	Mentions   domain.MentionResolver
	CursorKey  []byte
	Now        func() time.Time
}

type Service struct {
	auth      Authorizer
	store     IdempotentStore
	sanitizer domain.TextSanitizer
	mentions  domain.MentionResolver
	cursorKey []byte
	now       func() time.Time
}

func New(cfg Config) (*Service, error) {
	if cfg.Authorizer == nil || cfg.Store == nil || cfg.Sanitizer == nil || len(cfg.CursorKey) < 16 {
		return nil, ErrUnavailable
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{auth: cfg.Authorizer, store: cfg.Store, sanitizer: cfg.Sanitizer, mentions: cfg.Mentions, cursorKey: append([]byte(nil), cfg.CursorKey...), now: cfg.Now}, nil
}

type AddCommentRequest struct {
	ProjectID, TaskID    string
	Text, IdempotencyKey string
}
type ReviseCommentRequest struct {
	ProjectID, TaskID, CommentID string
	ExpectedRevision             uint64
	Text, IdempotencyKey         string
}
type ListRequest struct {
	ProjectID, TaskID, Cursor string
	PageSize                  int
}
type CommentPage struct {
	Comments   []domain.Comment
	NextCursor string
}
type ActivityPage struct {
	Entries    []domain.Activity
	NextCursor string
}

func (s *Service) AddComment(ctx context.Context, principal *trust.Principal, req AddCommentRequest) (domain.Comment, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return domain.Comment{}, err
	}
	if !validTarget(req.ProjectID, req.TaskID) || strings.TrimSpace(req.IdempotencyKey) == "" || len(req.IdempotencyKey) > 200 {
		return domain.Comment{}, ErrInvalidRequest
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	id := stableCommentID(principal, req.ProjectID, req.TaskID, "create", key)
	store := &keyedStore{IdempotentStore: s.store, key: key}
	ds := s.domainService(principal, store)
	result, err := ds.Add(ctx, domain.Request{Principal: domain.Principal{TenantID: string(principal.Tenant()), SubjectID: principal.Subject()}, ProjectID: req.ProjectID, TaskID: req.TaskID, CommentID: id, Text: req.Text, At: s.now().UTC()})
	if err != nil {
		return domain.Comment{}, err
	}
	if store.created != nil {
		return *store.created, nil
	}
	return result, nil
}

func (s *Service) EditComment(ctx context.Context, principal *trust.Principal, req ReviseCommentRequest) (domain.Comment, error) {
	return s.revise(ctx, principal, req, false)
}

func (s *Service) DeleteComment(ctx context.Context, principal *trust.Principal, req ReviseCommentRequest) (domain.Comment, error) {
	return s.revise(ctx, principal, req, true)
}

func (s *Service) revise(ctx context.Context, principal *trust.Principal, req ReviseCommentRequest, tombstone bool) (domain.Comment, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return domain.Comment{}, err
	}
	if !validTarget(req.ProjectID, req.TaskID) || req.CommentID == "" || req.ExpectedRevision == 0 || req.ExpectedRevision >= ^uint64(0)>>1 || strings.TrimSpace(req.IdempotencyKey) == "" || len(req.IdempotencyKey) > 200 {
		return domain.Comment{}, ErrInvalidRequest
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	store := &keyedStore{IdempotentStore: s.store, key: key, delete: tombstone}
	ds := s.domainService(principal, store)
	input := domain.Request{Principal: domain.Principal{TenantID: string(principal.Tenant()), SubjectID: principal.Subject()}, ProjectID: req.ProjectID, TaskID: req.TaskID, CommentID: req.CommentID, ExpectedRevision: req.ExpectedRevision, At: s.now().UTC()}
	if tombstone {
		return ds.Tombstone(ctx, input)
	}
	input.Text = req.Text
	return ds.Correct(ctx, input)
}

func (s *Service) ListComments(ctx context.Context, principal *trust.Principal, req ListRequest) (CommentPage, error) {
	page, err := s.list(ctx, principal, req)
	if err != nil {
		return CommentPage{}, err
	}
	return CommentPage{Comments: page.Comments, NextCursor: page.NextCursor}, nil
}

func (s *Service) ListActivity(ctx context.Context, principal *trust.Principal, req ListRequest) (ActivityPage, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return ActivityPage{}, err
	}
	if !validTarget(req.ProjectID, req.TaskID) {
		return ActivityPage{}, ErrInvalidRequest
	}
	entries, cursor, err := s.domainService(principal, s.store).Timeline(ctx, domain.PageRequest{Principal: domain.Principal{TenantID: string(principal.Tenant()), SubjectID: principal.Subject()}, ProjectID: req.ProjectID, TaskID: req.TaskID, Cursor: req.Cursor, PageSize: req.PageSize})
	if err != nil {
		return ActivityPage{}, err
	}
	return ActivityPage{Entries: entries, NextCursor: cursor}, nil
}

func (s *Service) list(ctx context.Context, principal *trust.Principal, req ListRequest) (domain.Page, error) {
	if err := s.validatePrincipal(principal); err != nil {
		return domain.Page{}, err
	}
	if !validTarget(req.ProjectID, req.TaskID) {
		return domain.Page{}, ErrInvalidRequest
	}
	return s.domainService(principal, s.store).List(ctx, domain.PageRequest{Principal: domain.Principal{TenantID: string(principal.Tenant()), SubjectID: principal.Subject()}, ProjectID: req.ProjectID, TaskID: req.TaskID, Cursor: req.Cursor, PageSize: req.PageSize})
}

func (s *Service) validatePrincipal(p *trust.Principal) error {
	if s == nil || s.auth == nil || s.store == nil || s.sanitizer == nil || len(s.cursorKey) < 16 || s.now == nil {
		return ErrUnavailable
	}
	if p == nil || p.Tenant().String() == "" || p.Subject() == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

func validTarget(projectID, taskID string) bool {
	return projectID != "" && taskID != "" && strings.TrimSpace(projectID) == projectID && strings.TrimSpace(taskID) == taskID && len(projectID) <= 200 && len(taskID) <= 200
}

func (s *Service) domainService(p *trust.Principal, store domain.Store) domain.Service {
	return domain.Service{Store: store, Authorizer: authorizerAdapter{auth: s.auth, principal: p}, Sanitizer: s.sanitizer, Mentions: s.mentions, CursorKey: s.cursorKey}
}

type authorizerAdapter struct {
	auth      Authorizer
	principal *trust.Principal
}

func (a authorizerAdapter) Authorize(ctx context.Context, req domain.AccessRequest) error {
	if a.principal == nil || req.Principal.TenantID != string(a.principal.Tenant()) || req.Principal.SubjectID != a.principal.Subject() {
		return domain.ErrDenied
	}
	var capability projectaccess.Capability
	switch req.Action {
	case domain.ActionRead:
		capability = projectaccess.ReadTask
	case domain.ActionComment, domain.ActionCorrect:
		capability = projectaccess.Comment
	default:
		return domain.ErrDenied
	}
	return a.auth.Authorize(ctx, a.principal, req.ProjectID, capability)
}

// keyedStore supplies one call's idempotency key to the domain service while
// retaining the established Store shape. Each operation has its own instance.
type keyedStore struct {
	IdempotentStore
	key     string
	delete  bool
	created *domain.Comment
}

func (s *keyedStore) Create(ctx context.Context, c domain.Comment, a domain.Activity) error {
	result, err := s.CreateIdempotent(ctx, c, a, s.key)
	if err == nil {
		copy := result
		s.created = &copy
	}
	return err
}
func (s *keyedStore) Correct(ctx context.Context, tenant, project, task, id string, expected uint64, revision domain.Revision, activity domain.Activity) (domain.Comment, error) {
	if s.delete {
		return s.DeleteIdempotent(ctx, tenant, project, task, id, expected, revision, activity, s.key)
	}
	return s.CorrectIdempotent(ctx, tenant, project, task, id, expected, revision, activity, s.key)
}

func stableCommentID(p *trust.Principal, project, task, operation, key string) string {
	name := strings.Join([]string{p.Tenant().String(), p.Subject(), project, task, operation, key}, "\x00")
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}
