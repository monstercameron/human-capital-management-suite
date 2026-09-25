package projectsearch

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type testAuth struct {
	calls  int
	denyAt int
}

type testStanding struct {
	calls  int
	denyAt int
}

func (s *testStanding) CheckInvitee(_ context.Context, p *trust.Principal, subject string) (uint8, error) {
	s.calls++
	if p == nil || subject != p.Subject() {
		return 0, ErrInvalidRequest
	}
	if s.denyAt != 0 && s.calls == s.denyAt {
		return 0, projectaccess.ErrMemberNotFound
	}
	return 0, nil
}

func (a *testAuth) Authorize(context.Context, string, string, string, projectaccess.Capability) error {
	a.calls++
	if a.denyAt != 0 && a.calls == a.denyAt {
		return projectaccess.ErrUnauthorized
	}
	return nil
}

type testRepo struct {
	rows                    []Task
	listCalls               int
	lastAfter               string
	lastFilter              Filter
	sequence                uint64
	sequenceCalls           int
	changeSequenceAfterRead bool
}

func (r *testRepo) EventSequence(context.Context, string, string) (uint64, error) {
	r.sequenceCalls++
	if r.changeSequenceAfterRead && r.sequenceCalls > 1 {
		r.sequence++
	}
	return r.sequence, nil
}

func (r *testRepo) ListExact(_ context.Context, tenant, project, after string, f Filter, limit int) ([]Task, error) {
	r.listCalls++
	r.lastAfter, r.lastFilter = after, f
	filtered := make([]Task, 0)
	for _, task := range r.rows {
		if task.TenantID != tenant || task.ProjectID != project || task.ID <= after || task.Archived {
			continue
		}
		if len(f.StatusIDs) > 0 && !contains(f.StatusIDs, task.StatusID) || f.AssigneeID != "" && f.AssigneeID != task.AssigneeID || len(f.TypeIDs) > 0 && !contains(f.TypeIDs, task.TypeID) {
			continue
		}
		if f.DueDateFrom != "" && task.DueDate < f.DueDateFrom || f.DueDateTo != "" && task.DueDate > f.DueDateTo {
			continue
		}
		match := true
		for id, values := range f.Fields {
			var canonical string
			if json.Unmarshal([]byte(task.Fields[id].CanonicalValue), &canonical) != nil {
				canonical = task.Fields[id].CanonicalValue
			}
			if !contains(values, canonical) {
				match = false
			}
		}
		if match {
			filtered = append(filtered, task)
		}
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

type testIndex struct {
	available bool
	called    bool
}

func (i *testIndex) Available(context.Context, string, string) (bool, error) {
	i.called = true
	return i.available, nil
}

func testPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "search-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func TestTodo_PM_018_ExactFallbackPagesAndBindsCursor(t *testing.T) {
	auth := &testAuth{}
	repo := &testRepo{sequence: 3, rows: []Task{
		{ID: "01", TenantID: "tenant-a", ProjectID: "p1", StatusID: "todo", TypeID: "bug", AssigneeID: "alice", DueDate: "2026-03-01", Fields: map[string]project.TaskFieldEdit{"team": {Type: "ENUM", CanonicalValue: `"design"`}}},
		{ID: "02", TenantID: "tenant-a", ProjectID: "p1", StatusID: "doing", TypeID: "bug", AssigneeID: "alice", DueDate: "2026-03-02", Fields: map[string]project.TaskFieldEdit{"team": {Type: "ENUM", CanonicalValue: `"design"`}}},
		{ID: "03", TenantID: "tenant-a", ProjectID: "p1", StatusID: "todo", TypeID: "bug", AssigneeID: "alice", DueDate: "2026-03-03", Fields: map[string]project.TaskFieldEdit{"team": {Type: "ENUM", CanonicalValue: `"design"`}}},
		{ID: "04", TenantID: "tenant-a", ProjectID: "p1", StatusID: "todo", TypeID: "feature", AssigneeID: "alice", DueDate: "2026-03-02", Fields: map[string]project.TaskFieldEdit{"team": {Type: "ENUM", CanonicalValue: `"design"`}}},
	}}
	index := &testIndex{available: false}
	standing := &testStanding{}
	svc := Service{Auth: auth, Standing: standing, Tasks: repo, Index: index, CursorKey: []byte("0123456789abcdef0123456789abcdef")}
	f := Filter{StatusIDs: []string{"todo"}, AssigneeID: "alice", TypeIDs: []string{"bug"}, DueDateFrom: "2026-03-01", DueDateTo: "2026-03-03", Fields: map[string][]string{"team": {"design"}}}
	first, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Filter: f, Limit: 1})
	if err != nil || len(first.Tasks) != 1 || first.Tasks[0].ID != "01" || first.Next == "" || first.Freshness != FreshnessDegraded || !index.called {
		t.Fatalf("first page=%+v err=%v index-called=%v", first, err, index.called)
	}
	second, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Filter: f, Limit: 1, Cursor: first.Next})
	if err != nil || len(second.Tasks) != 1 || second.Tasks[0].ID != "03" || second.Next != "" {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
	if repo.lastAfter != "01" || repo.lastFilter.DueDateFrom != "2026-03-01" || auth.calls != 6 || standing.calls != 6 {
		t.Fatalf("after=%q filter=%+v auth checks=%d standing checks=%d", repo.lastAfter, repo.lastFilter, auth.calls, standing.calls)
	}
	if _, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Filter: Filter{StatusIDs: []string{"other"}}, Limit: 1, Cursor: first.Next}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("changed filters accepted cursor: %v", err)
	}
	tampered := first.Next[:len(first.Next)-1] + "x"
	if _, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Filter: f, Limit: 1, Cursor: tampered}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("tampered cursor error=%v", err)
	}
}

func TestTodo_PM_018_SecurityRechecksMembershipAfterRead(t *testing.T) {
	auth := &testAuth{denyAt: 2}
	repo := &testRepo{sequence: 3, rows: []Task{{ID: "01", TenantID: "tenant-a", ProjectID: "p1"}}}
	standing := &testStanding{}
	svc := Service{Auth: auth, Standing: standing, Tasks: repo, CursorKey: []byte("0123456789abcdef0123456789abcdef")}
	page, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 1})
	if !errors.Is(err, projectaccess.ErrUnauthorized) || len(page.Tasks) != 0 || page.Next != "" || auth.calls != 2 || standing.calls != 2 {
		t.Fatalf("revoked read page=%+v err=%v auth checks=%d standing checks=%d", page, err, auth.calls, standing.calls)
	}
}

func TestTodo_PM_018_RecoveryReturnsTypedTextUnavailable(t *testing.T) {
	repo := &testRepo{sequence: 3}
	standing := &testStanding{}
	svc := Service{Auth: &testAuth{}, Standing: standing, Tasks: repo, CursorKey: []byte("0123456789abcdef0123456789abcdef")}
	_, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Text: "build onboarding", Limit: 10})
	if !errors.Is(err, ErrTextUnavailable) || standing.calls != 2 || repo.listCalls != 0 {
		t.Fatalf("free-text error=%v standing checks=%d exact reads=%d", err, standing.calls, repo.listCalls)
	}
	terminated := &testStanding{denyAt: 2}
	svc.Standing = terminated
	if _, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Text: "build onboarding", Limit: 10}); !errors.Is(err, projectaccess.ErrMemberNotFound) || terminated.calls != 2 {
		t.Fatalf("terminated during text path error=%v standing checks=%d", err, terminated.calls)
	}
	if err := validateFilter(Filter{DueDateFrom: "2026-02-30"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid civil date accepted: %v", err)
	}
	if err := validateFilter(Filter{DueDateFrom: "2026-03-02", DueDateTo: "2026-03-01"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("reversed due-date range accepted: %v", err)
	}
}

func TestTodo_PM_018_RecoveryRejectsCursorAfterTaskMutation(t *testing.T) {
	repo := &testRepo{sequence: 3, rows: []Task{{ID: "01", TenantID: "tenant-a", ProjectID: "p1"}, {ID: "02", TenantID: "tenant-a", ProjectID: "p1"}}}
	standing := &testStanding{}
	svc := Service{Auth: &testAuth{}, Standing: standing, Tasks: repo, CursorKey: []byte("0123456789abcdef0123456789abcdef")}
	first, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 1})
	if err != nil || first.Next == "" {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	repo.sequence++
	if _, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 1, Cursor: first.Next}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("stale cursor error=%v", err)
	}
	repo.changeSequenceAfterRead = true
	if _, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 1}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("concurrent mutation error=%v", err)
	}
}

func TestTodo_PM_018_SecurityRejectsEmployeeTerminatedDuringRead(t *testing.T) {
	standing := &testStanding{denyAt: 2}
	repo := &testRepo{sequence: 3, rows: []Task{{ID: "01", TenantID: "tenant-a", ProjectID: "p1"}}}
	svc := Service{Auth: &testAuth{}, Standing: standing, Tasks: repo, CursorKey: []byte("0123456789abcdef0123456789abcdef")}
	page, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 1})
	if !errors.Is(err, projectaccess.ErrMemberNotFound) || len(page.Tasks) != 0 || page.Next != "" || standing.calls != 2 {
		t.Fatalf("termination during read page=%+v err=%v standing checks=%d", page, err, standing.calls)
	}
}

func TestTodo_PM_018_SecurityRejectsEmployeeTerminatedBeforeResponse(t *testing.T) {
	standing := &testStanding{denyAt: 3}
	repo := &testRepo{sequence: 3, rows: []Task{{ID: "01", TenantID: "tenant-a", ProjectID: "p1"}}}
	svc := Service{Auth: &testAuth{}, Standing: standing, Tasks: repo, CursorKey: []byte("0123456789abcdef0123456789abcdef")}
	page, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 1})
	if !errors.Is(err, projectaccess.ErrMemberNotFound) || len(page.Tasks) != 0 || page.Next != "" || standing.calls != 3 {
		t.Fatalf("termination before response page=%+v err=%v standing checks=%d", page, err, standing.calls)
	}
}

func TestTodo_PM_018_SecurityRequiresEmployeeStandingPort(t *testing.T) {
	repo := &testRepo{sequence: 3, rows: []Task{{ID: "01", TenantID: "tenant-a", ProjectID: "p1"}}}
	svc := Service{Auth: &testAuth{}, Tasks: repo, CursorKey: []byte("0123456789abcdef0123456789abcdef")}
	if _, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 1}); !errors.Is(err, ErrUnavailable) || repo.listCalls != 0 {
		t.Fatalf("missing standing port error=%v task reads=%d", err, repo.listCalls)
	}
}
