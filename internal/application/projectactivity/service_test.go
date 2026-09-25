package projectactivity

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectcommentstore"
	projectaccess "github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	domain "github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var _ IdempotentStore = (*projectcommentstore.Store)(nil)

type appAuthorizer struct {
	denied error
	calls  []projectaccess.Capability
}

func (a *appAuthorizer) Authorize(_ context.Context, _ *trust.Principal, _ string, capability projectaccess.Capability) error {
	a.calls = append(a.calls, capability)
	return a.denied
}

type idemEntry struct {
	fingerprint string
	comment     domain.Comment
}
type idemStore struct {
	*domain.MemoryStore
	entries   map[string]idemEntry
	pageCalls int
}

func newIdemStore() *idemStore {
	return &idemStore{MemoryStore: domain.NewMemoryStore(), entries: map[string]idemEntry{}}
}
func idemKey(tenant, actor, action, key string) string {
	return tenant + "\x00" + actor + "\x00" + action + "\x00" + key
}
func addFingerprint(c domain.Comment) string {
	return c.ProjectID + "\x00" + c.TaskID + "\x00" + c.ID + "\x00" + c.Current().Text.Source + "\x00" + c.Current().Text.HTML
}
func reviseFingerprint(project, task, id string, expected uint64, r domain.Revision) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%t\x00%s\x00%s", project, task, id, expected, r.Number, r.Tombstone, r.Text.Source, r.Text.HTML)
}
func (s *idemStore) CreateIdempotent(ctx context.Context, c domain.Comment, a domain.Activity, key string) (domain.Comment, error) {
	cacheKey := idemKey(c.TenantID, a.ActorID, "create", key)
	fingerprint := addFingerprint(c)
	if prior, ok := s.entries[cacheKey]; ok {
		if prior.fingerprint != fingerprint {
			return domain.Comment{}, domain.ErrConflict
		}
		return prior.comment, nil
	}
	if err := s.MemoryStore.Create(ctx, c, a); err != nil {
		return domain.Comment{}, err
	}
	s.entries[cacheKey] = idemEntry{fingerprint: fingerprint, comment: cloneAppComment(c)}
	return cloneAppComment(c), nil
}
func (s *idemStore) CorrectIdempotent(ctx context.Context, tenant, project, task, id string, expected uint64, r domain.Revision, a domain.Activity, key string) (domain.Comment, error) {
	return s.reviseIdempotent(ctx, tenant, project, task, id, expected, r, a, key, "edit")
}
func (s *idemStore) DeleteIdempotent(ctx context.Context, tenant, project, task, id string, expected uint64, r domain.Revision, a domain.Activity, key string) (domain.Comment, error) {
	if !r.Tombstone {
		return domain.Comment{}, domain.ErrInvalidRequest
	}
	return s.reviseIdempotent(ctx, tenant, project, task, id, expected, r, a, key, "delete")
}
func (s *idemStore) reviseIdempotent(ctx context.Context, tenant, project, task, id string, expected uint64, r domain.Revision, a domain.Activity, key, operation string) (domain.Comment, error) {
	cacheKey := idemKey(tenant, a.ActorID, operation, key)
	fingerprint := reviseFingerprint(project, task, id, expected, r)
	if prior, ok := s.entries[cacheKey]; ok {
		if prior.fingerprint != fingerprint {
			return domain.Comment{}, domain.ErrConflict
		}
		return cloneAppComment(prior.comment), nil
	}
	result, err := s.MemoryStore.Correct(ctx, tenant, project, task, id, expected, r, a)
	if err != nil {
		return domain.Comment{}, err
	}
	s.entries[cacheKey] = idemEntry{fingerprint: fingerprint, comment: cloneAppComment(result)}
	return result, nil
}
func (s *idemStore) Page(ctx context.Context, tenant, project, task string, after, ceiling uint64, limit int) ([]domain.Comment, []domain.Activity, uint64, uint64, bool, error) {
	s.pageCalls++
	return s.MemoryStore.Page(ctx, tenant, project, task, after, ceiling, limit)
}
func cloneAppComment(c domain.Comment) domain.Comment {
	c.Revisions = append([]domain.Revision(nil), c.Revisions...)
	return c
}

func appPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "actor-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: at, ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential-digest"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func appService(t *testing.T) (*Service, *appAuthorizer, *idemStore) {
	t.Helper()
	auth := &appAuthorizer{}
	store := newIdemStore()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	s, err := New(Config{Authorizer: auth, Store: store, Sanitizer: NewPlainTextSanitizer(), CursorKey: []byte("a sufficiently long app cursor key"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return s, auth, store
}

func TestApplicationProjectActivity_MutationsAndIdempotency(t *testing.T) {
	s, auth, _ := appService(t)
	p := appPrincipal(t)
	ctx := context.Background()
	req := AddCommentRequest{ProjectID: "project-a", TaskID: "task-a", Text: "first", IdempotencyKey: "add-1"}
	first, err := s.AddComment(ctx, p, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Current().Text.HTML != "<p>first</p>" {
		t.Fatalf("comment did not carry the escaped preview: %#v", first.Current().Text)
	}
	replay, err := s.AddComment(ctx, p, req)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != first.ID || replay.CreatedAt != first.CreatedAt || len(replay.Revisions) != 1 {
		t.Fatalf("retry did not return original result: first=%#v replay=%#v", first, replay)
	}
	changed := req
	changed.Text = "different payload"
	if _, err = s.AddComment(ctx, p, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("same key with changed payload error = %v", err)
	}
	editRequest := ReviseCommentRequest{ProjectID: "project-a", TaskID: "task-a", CommentID: first.ID, ExpectedRevision: 1, Text: "edited", IdempotencyKey: "edit-1"}
	edited, err := s.EditComment(ctx, p, editRequest)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Current().Number != 2 || edited.Current().Text.Source != "edited" || edited.Revisions[0].Text.Source != "first" {
		t.Fatalf("edit did not preserve history: %#v", edited)
	}
	editedReplay, err := s.EditComment(ctx, p, editRequest)
	if err != nil || editedReplay.Current().At != edited.Current().At || editedReplay.Current().Number != 2 {
		t.Fatalf("edit retry did not return the first result: replay=%#v err=%v", editedReplay, err)
	}
	if _, err = s.EditComment(ctx, p, ReviseCommentRequest{ProjectID: "project-a", TaskID: "task-a", CommentID: first.ID, ExpectedRevision: 1, Text: "stale", IdempotencyKey: "edit-2"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale edit error = %v", err)
	}
	deleteRequest := ReviseCommentRequest{ProjectID: "project-a", TaskID: "task-a", CommentID: first.ID, ExpectedRevision: 2, IdempotencyKey: "delete-1"}
	deleted, err := s.DeleteComment(ctx, p, deleteRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Current().Tombstone || deleted.Revisions[0].Text.Source != "first" || deleted.Revisions[1].Text.Source != "edited" {
		t.Fatalf("delete erased correction history: %#v", deleted.Revisions)
	}
	deletedReplay, err := s.DeleteComment(ctx, p, deleteRequest)
	if err != nil || !deletedReplay.Current().Tombstone || deletedReplay.Current().At != deleted.Current().At {
		t.Fatalf("delete retry did not return the first result: replay=%#v err=%v", deletedReplay, err)
	}
	_, events, _, _, _, err := s.store.Page(ctx, "tenant-a", "project-a", "task-a", 0, 0, 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("idempotent retries added duplicate activity: events=%d err=%v", len(events), err)
	}
	if len(auth.calls) != 8 {
		t.Fatalf("expected one current authorization per command, got %v", auth.calls)
	}
	for _, capability := range auth.calls {
		if capability != projectaccess.Comment {
			t.Fatalf("mutation used capability %q", capability)
		}
	}
}

func TestApplicationProjectActivity_PagesAndAuthorizesBeforeRead(t *testing.T) {
	s, auth, store := appService(t)
	p := appPrincipal(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		if _, err := s.AddComment(ctx, p, AddCommentRequest{ProjectID: "project-a", TaskID: "task-a", Text: fmt.Sprintf("comment %d", i), IdempotencyKey: fmt.Sprintf("add-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.ListActivity(ctx, p, ListRequest{ProjectID: "project-a", TaskID: "task-a", PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 1 || first.Entries[0].CommentID == "" || first.NextCursor == "" {
		t.Fatalf("bad first activity page: %#v", first)
	}
	second, err := s.ListComments(ctx, p, ListRequest{ProjectID: "project-a", TaskID: "task-a", Cursor: first.NextCursor, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Comments) != 1 || second.Comments[0].Revisions[0].Number != 1 {
		t.Fatalf("bad second comment page: %#v", second)
	}
	if auth.calls[len(auth.calls)-1] != projectaccess.ReadTask {
		t.Fatalf("read used capability %q", auth.calls[len(auth.calls)-1])
	}
	store.pageCalls = 0
	auth.denied = errors.New("membership revoked")
	if _, err = s.ListActivity(ctx, p, ListRequest{ProjectID: "private-project", TaskID: "secret-task", PageSize: 1}); !errors.Is(err, domain.ErrDenied) || store.pageCalls != 0 {
		t.Fatalf("denied list read store: err=%v calls=%d", err, store.pageCalls)
	}
}

func TestApplicationProjectActivity_ValidationAndSanitizer(t *testing.T) {
	s, _, _ := appService(t)
	p := appPrincipal(t)
	if _, err := s.AddComment(context.Background(), nil, AddCommentRequest{}); !errors.Is(err, ErrInvalidPrincipal) {
		t.Fatalf("nil principal error=%v", err)
	}
	if _, err := s.AddComment(context.Background(), p, AddCommentRequest{ProjectID: "project-a", TaskID: "task-a", Text: "<script>bad</script>", IdempotencyKey: "unsafe"}); err == nil {
		t.Fatal("unsafe text accepted")
	}
	if _, err := s.DeleteComment(context.Background(), p, ReviseCommentRequest{ProjectID: "project-a", TaskID: "task-a", CommentID: "c", IdempotencyKey: "delete"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing expected revision error=%v", err)
	}
}
