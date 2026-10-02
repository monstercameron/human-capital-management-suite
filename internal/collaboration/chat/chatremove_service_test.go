package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

type chatremoveFixture struct {
	ModerationStore
	rows   []RemovalCandidate
	writes int
	err    error
}

func (f *chatremoveFixture) PreviewRemoval(context.Context, RemovalRequest) ([]RemovalCandidate, error) {
	return f.rows, f.err
}
func (f *chatremoveFixture) CommitRemoval(_ context.Context, r RemovalRequest, _ time.Time) (int, error) {
	if r.Confirmation != RemovalConfirmation(r, f.rows) || r.ConfirmedCount != len(f.rows) {
		return 0, ErrConflict
	}
	f.writes++
	return len(f.rows), f.err
}
func (f *chatremoveFixture) SearchModeration(context.Context, Principal, string, string) ([]ModerationItem, error) {
	return []ModerationItem{{ID: "report:one", Kind: "report"}}, f.err
}
func (f *chatremoveFixture) ResolveModeration(context.Context, Principal, string, string, string, string, time.Time) error {
	f.writes++
	return f.err
}

func chatremoveRequest() RemovalRequest {
	return RemovalRequest{Principal: Principal{TenantID: "tenant", SubjectID: "moderator"}, TenantID: "tenant", Selection: RemovalSelection{ConversationID: "room", PostIDs: []string{"post"}}, ReasonCode: "harassment", Action: "remove"}
}

func TestTodo_CHATMOD_004(t *testing.T) {
	if run, err := (NoModerationRunTrace{}).RunForModerationPost(context.Background(), "tenant", "room", "post"); err != nil || run != "" {
		t.Fatalf("no-op run=%q err=%v", run, err)
	}
	f := &chatremoveFixture{rows: []RemovalCandidate{{ID: "post", Revision: 2}}}
	s := ModerationService{Store: f}
	r := chatremoveRequest()
	p, err := s.Preview(context.Background(), r)
	if err != nil || p.Count != 1 || p.Confirmation == "" {
		t.Fatalf("preview=%+v err=%v", p, err)
	}
	r.Confirmation, r.ConfirmedCount = p.Confirmation, p.Count
	if n, err := s.Apply(context.Background(), r); err != nil || n != 1 || f.writes != 1 {
		t.Fatalf("apply=%d writes=%d err=%v", n, f.writes, err)
	}
	for _, permission := range []string{PermissionRemoveMessages, PermissionReviewRemovedMessages, PermissionManageFilters, PermissionReport} {
		if !ValidModerationPermission(permission) {
			t.Fatal(permission)
		}
	}
	if ValidModerationPermission("admin") {
		t.Fatal("unknown permission accepted")
	}
}

func TestTodo_CHATMOD_004_Security(t *testing.T) {
	f := &chatremoveFixture{rows: []RemovalCandidate{{ID: "post", Revision: 2}}}
	s := ModerationService{Store: f}
	r := chatremoveRequest()
	p, _ := s.Preview(context.Background(), r)
	r.Confirmation, r.ConfirmedCount = p.Confirmation, p.Count
	r.Note = "changed after preview"
	if _, err := s.Apply(context.Background(), r); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	r.Principal.TenantID = "other"
	if _, err := s.Apply(context.Background(), r); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal(err)
	}
	r = chatremoveRequest()
	r.Selection.PostIDs = []string{"post", "post"}
	if _, err := s.Preview(context.Background(), r); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if f.writes != 0 {
		t.Fatal("rejected operation mutated")
	}
}

func TestTodo_CHATMOD_004_Fault(t *testing.T) {
	s := ModerationService{}
	if _, err := s.Preview(context.Background(), chatremoveRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	f := &chatremoveFixture{err: ErrUnavailable}
	s.Store = f
	if _, err := s.Preview(context.Background(), chatremoveRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	r := chatremoveRequest()
	r.Selection = RemovalSelection{ConversationID: "room", AuthorID: "author", AuthorHomeTenantID: "tenant", From: time.Unix(1, 0), Until: time.Unix(2, 0)}
	if err := ValidateRemoval(r); err != nil {
		t.Fatal(err)
	}
	r.Selection.Until = r.Selection.From
	if !errors.Is(ValidateRemoval(r), ErrInvalidArgument) {
		t.Fatal("empty time range accepted")
	}
}

func TestTodo_CHATMOD_005(t *testing.T) {
	f := &chatremoveFixture{}
	s := ModerationService{Store: f}
	p := Principal{TenantID: "tenant", SubjectID: "reviewer"}
	items, err := s.Queue(context.Background(), p, "tenant", "")
	if err != nil || len(items) != 1 || items[0].Kind != "report" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if err = s.Resolve(context.Background(), p, "tenant", "report:one", "dismiss", "not a violation"); err != nil || f.writes != 1 {
		t.Fatalf("writes=%d err=%v", f.writes, err)
	}
}

func TestTodo_CHATMOD_005_Security(t *testing.T) {
	f := &chatremoveFixture{}
	s := ModerationService{Store: f}
	p := Principal{TenantID: "other", SubjectID: "reviewer"}
	if _, err := s.Queue(context.Background(), p, "tenant", ""); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal(err)
	}
	p.TenantID = "tenant"
	if err := s.Resolve(context.Background(), p, "tenant", "filter:one", "remove", "reason"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := s.Resolve(context.Background(), p, "tenant", "report:one", "delete", "reason"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if f.writes != 0 {
		t.Fatal("invalid action mutated")
	}
}
