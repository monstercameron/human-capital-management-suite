package projectactivity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type testAuthorizer struct {
	err   error
	calls int
}

func (a *testAuthorizer) Authorize(context.Context, AccessRequest) error { a.calls++; return a.err }

type testSanitizer struct{}

func (testSanitizer) Sanitize(_ context.Context, raw string) (SafeText, error) {
	if strings.Contains(strings.ToLower(raw), "<script") {
		return SafeText{}, errors.New("active markup")
	}
	return SafeText{Source: raw, HTML: "<p>" + strings.ReplaceAll(strings.ReplaceAll(raw, "&", "&amp;"), "<", "&lt;") + "</p>"}, nil
}

type testMentions struct {
	calls    int
	access   AccessRequest
	resolved []Mention
}

func (m *testMentions) ResolveVisibleMentions(_ context.Context, access AccessRequest, handles []string) ([]Mention, error) {
	m.calls++
	m.access = access
	if len(handles) == 0 {
		return nil, nil
	}
	return append([]Mention(nil), m.resolved...), nil
}

type readCountingStore struct {
	Store
	pageCalls int
}

func (s *readCountingStore) Page(ctx context.Context, tenant, project, task string, after, ceiling uint64, limit int) ([]Comment, []Activity, uint64, uint64, bool, error) {
	s.pageCalls++
	return s.Store.Page(ctx, tenant, project, task, after, ceiling, limit)
}

func testService() (Service, *MemoryStore, *testAuthorizer) {
	store := NewMemoryStore()
	auth := &testAuthorizer{}
	return Service{Store: store, Authorizer: auth, Sanitizer: testSanitizer{}, CursorKey: []byte("a sufficiently long test cursor key")}, store, auth
}

func request(id string, rev uint64, text string, at time.Time) Request {
	return Request{Principal: Principal{TenantID: "tenant-a", SubjectID: "actor-a"}, ProjectID: "project-a", TaskID: "task-a", CommentID: id, ExpectedRevision: rev, Text: text, At: at}
}

func TestTodo_PM_019(t *testing.T) {
	s, store, _ := testService()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	if _, err := s.Add(context.Background(), request("c1", 0, "original", now)); err != nil {
		t.Fatal(err)
	}
	corrected, err := s.Correct(context.Background(), request("c1", 1, "corrected", now.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if len(corrected.Revisions) != 2 || corrected.Revisions[0].Text.Source != "original" || corrected.Current().Text.Source != "corrected" {
		t.Fatalf("correction did not preserve revision history: %#v", corrected.Revisions)
	}
	if _, err = s.Correct(context.Background(), request("c1", 1, "stale overwrite", now.Add(2*time.Minute))); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale correction error = %v", err)
	}
	tombstoned, err := s.Tombstone(context.Background(), request("c1", 2, "", now.Add(3*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if !tombstoned.Current().Tombstone || len(tombstoned.Revisions) != 3 || tombstoned.Revisions[0].Text.Source != "original" {
		t.Fatalf("tombstone erased held history: %#v", tombstoned.Revisions)
	}
	comments, events, last, snapshot, more, err := store.Page(context.Background(), "tenant-a", "project-a", "task-a", 0, 0, 10)
	if err != nil || more || len(events) != 3 || len(comments) != 3 || last != snapshot {
		t.Fatalf("append history page = events:%d comments:%d last:%d snapshot:%d more:%v err:%v", len(events), len(comments), last, snapshot, more, err)
	}
	for i, want := range []string{"COMMENT_CREATED", "COMMENT_CORRECTED", "COMMENT_TOMBSTONED"} {
		if events[i].Kind != want || events[i].Sequence != uint64(i+1) {
			t.Fatalf("event[%d] = %#v", i, events[i])
		}
	}
}

func TestTodo_PM_019_PagingStableSnapshot(t *testing.T) {
	s, _, _ := testService()
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		if _, err := s.Add(ctx, request(fmt.Sprintf("c%d", i), 0, fmt.Sprintf("comment %d", i), now.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.List(ctx, PageRequest{Principal: Principal{"tenant-a", "actor-a"}, ProjectID: "project-a", TaskID: "task-a", PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Activity) != 1 || first.Activity[0].CommentID != "c1" || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	if _, err := s.Add(ctx, request("c4", 0, "later append", now.Add(4*time.Minute))); err != nil {
		t.Fatal(err)
	}
	seen := []string{first.Activity[0].CommentID}
	cursor := first.NextCursor
	for cursor != "" {
		page, err := s.List(ctx, PageRequest{Principal: Principal{"tenant-a", "actor-a"}, ProjectID: "project-a", TaskID: "task-a", Cursor: cursor, PageSize: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Activity) == 0 {
			t.Fatal("cursor returned empty page while advertising continuation")
		}
		seen = append(seen, page.Activity[0].CommentID)
		cursor = page.NextCursor
	}
	if strings.Join(seen, ",") != "c1,c2,c3" {
		t.Fatalf("snapshot page sequence = %v", seen)
	}
	bad := first.NextCursor + "x"
	if _, err := s.List(ctx, PageRequest{Principal: Principal{"tenant-a", "actor-a"}, ProjectID: "project-a", TaskID: "task-a", Cursor: bad, PageSize: 1}); !errors.Is(err, ErrCursor) {
		t.Fatalf("tampered cursor error = %v", err)
	}
}

func TestTodo_PM_019_Security(t *testing.T) {
	s, store, auth := testService()
	wrapped := &readCountingStore{Store: store}
	s.Store = wrapped
	auth.err = errors.New("revoked membership")
	_, err := s.List(context.Background(), PageRequest{Principal: Principal{"tenant-a", "actor-a"}, ProjectID: "private-project", TaskID: "secret-task", PageSize: 1})
	if !errors.Is(err, ErrDenied) || wrapped.pageCalls != 0 {
		t.Fatalf("denied read reached store: err=%v page calls=%d", err, wrapped.pageCalls)
	}
}

func TestTodo_PM_024(t *testing.T) {
	s, _, _ := testService()
	mentions := &testMentions{resolved: []Mention{{SubjectID: "member-visible", DisplayName: "Visible member"}}}
	s.Mentions = mentions
	// The sanitizer extracts mention handles while returning an inert HTML rendering.
	s.Sanitizer = mentionSanitizer{}
	created, err := s.Add(context.Background(), request("mention-comment", 0, "hello @visible", time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	text := created.Current().Text
	if strings.Contains(strings.ToLower(text.HTML), "<script") || len(text.Mentions) != 1 || text.Mentions[0].SubjectID != "member-visible" {
		t.Fatalf("unsafe text or unresolved visible mention: %#v", text)
	}
	if mentions.calls != 1 || mentions.access.TaskID != "task-a" || mentions.access.Action != ActionRead {
		t.Fatalf("mention resolution lacked task-scoped read authority: %#v", mentions)
	}
	if _, err = s.Add(context.Background(), request("unsafe", 0, "<script>alert(1)</script>", time.Now())); err == nil {
		t.Fatal("active markup was accepted")
	}
}

type mentionSanitizer struct{}

func (mentionSanitizer) Sanitize(_ context.Context, raw string) (SafeText, error) {
	if strings.Contains(strings.ToLower(raw), "<script") {
		return SafeText{}, errors.New("active markup")
	}
	handles := []string{}
	if strings.Contains(raw, "@visible") {
		handles = append(handles, "visible")
	}
	if strings.Contains(raw, "@hidden") {
		handles = append(handles, "hidden")
	}
	return SafeText{Source: raw, HTML: "<p>hello @visible</p>", MentionHandles: handles}, nil
}

func TestTodo_PM_024_Security(t *testing.T) {
	s, _, auth := testService()
	mentions := &testMentions{resolved: []Mention{}}
	s.Mentions = mentions
	s.Sanitizer = mentionSanitizer{}
	created, err := s.Add(context.Background(), request("hidden-mention", 0, "@hidden", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Current().Text.Mentions) != 0 {
		t.Fatalf("hidden mention was returned: %#v", created.Current().Text.Mentions)
	}
	auth.err = errors.New("no comment permission")
	if _, err = s.Add(context.Background(), request("denied", 0, "@visible", time.Now())); !errors.Is(err, ErrDenied) || mentions.calls != 1 {
		t.Fatalf("denied operation resolved mentions: err=%v calls=%d", err, mentions.calls)
	}
}

func FuzzTodo_PM_024(f *testing.F) {
	f.Add("plain text")
	f.Add("<b>safe</b>")
	f.Add("<script>bad</script>")
	s := testSanitizer{}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > MaxTextBytes {
			t.Skip()
		}
		got, err := s.Sanitize(context.Background(), input)
		if err == nil && (len(got.HTML) > MaxTextBytes || strings.Contains(strings.ToLower(got.HTML), "<script")) {
			t.Fatalf("unsafe sanitizer output %q", got.HTML)
		}
	})
}
