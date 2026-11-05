package projectsearch

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
)

func pm018Service(auth *testAuth, standing *testStanding, repo *testRepo, index *testIndex) Service {
	return Service{Auth: auth, Standing: standing, Tasks: repo, Index: index, CursorKey: []byte("0123456789abcdef0123456789abcdef")}
}

func pm018Rows() []Task {
	return []Task{
		{ID: "01", TenantID: "tenant-a", ProjectID: "p1", StatusID: "todo", TypeID: "bug"},
		{ID: "02", TenantID: "tenant-a", ProjectID: "p1", StatusID: "doing", TypeID: "bug"},
		{ID: "03", TenantID: "tenant-a", ProjectID: "p2", StatusID: "todo", TypeID: "bug"},
		{ID: "04", TenantID: "tenant-a", ProjectID: "p1", StatusID: "todo", TypeID: "bug", Archived: true},
	}
}

// TestTodo_PM_018 proves authorized exact search stays project-scoped and
// labels its freshness.
func TestTodo_PM_018(t *testing.T) {
	svc := pm018Service(&testAuth{}, &testStanding{}, &testRepo{sequence: 3, rows: pm018Rows()}, &testIndex{available: true})
	page, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Filter: Filter{StatusIDs: []string{"todo"}}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Freshness != FreshnessCurrent {
		t.Fatalf("freshness = %q, want %q", page.Freshness, FreshnessCurrent)
	}
	if len(page.Tasks) != 1 || page.Tasks[0].ID != "01" {
		t.Fatalf("scoped results = %+v, want only task 01", page.Tasks)
	}
	if page.Next != "" {
		t.Fatalf("complete result carries a cursor: %+v", page)
	}
}

// TestTodo_PM_018_Security proves revocation or termination at any check
// leaks no tasks and no continuation cursor.
func TestTodo_PM_018_Security(t *testing.T) {
	for _, denyAt := range []int{1, 2, 3} {
		auth := &testAuth{denyAt: denyAt}
		svc := pm018Service(auth, &testStanding{}, &testRepo{sequence: 3, rows: pm018Rows()}, &testIndex{available: true})
		page, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 10})
		if !errors.Is(err, projectaccess.ErrUnauthorized) || len(page.Tasks) != 0 || page.Next != "" {
			t.Fatalf("revoked at check %d: page=%+v err=%v", denyAt, page, err)
		}
	}
	for _, denyAt := range []int{1, 2, 3} {
		standing := &testStanding{denyAt: denyAt}
		svc := pm018Service(&testAuth{}, standing, &testRepo{sequence: 3, rows: pm018Rows()}, &testIndex{available: true})
		page, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 10})
		if !errors.Is(err, projectaccess.ErrMemberNotFound) || len(page.Tasks) != 0 || page.Next != "" {
			t.Fatalf("terminated at check %d: page=%+v err=%v", denyAt, page, err)
		}
	}
}

// TestTodo_PM_018_Recovery proves index failure degrades to labeled
// exact-filter paging and free text fails typed instead of pretending.
func TestTodo_PM_018_Recovery(t *testing.T) {
	repo := &testRepo{sequence: 3, rows: pm018Rows()}
	svc := pm018Service(&testAuth{}, &testStanding{}, repo, &testIndex{available: false})
	first, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 1})
	if err != nil || len(first.Tasks) != 1 || first.Next == "" {
		t.Fatalf("degraded first page = %+v, err=%v", first, err)
	}
	if first.Freshness != FreshnessDegraded {
		t.Fatalf("degraded freshness = %q, want %q", first.Freshness, FreshnessDegraded)
	}
	second, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Limit: 1, Cursor: first.Next})
	if err != nil || len(second.Tasks) != 1 || second.Tasks[0].ID == first.Tasks[0].ID {
		t.Fatalf("degraded second page = %+v, err=%v", second, err)
	}
	before := repo.listCalls
	if _, err := svc.Search(context.Background(), testPrincipal(t), Request{ProjectID: "p1", Text: "onboarding", Limit: 10}); !errors.Is(err, ErrTextUnavailable) {
		t.Fatalf("free-text error = %v, want typed unavailable", err)
	}
	if repo.listCalls != before {
		t.Fatalf("free-text path ran %d exact reads, want none", repo.listCalls-before)
	}
}
