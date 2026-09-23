package chat

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestTodo_CHAT_023_PinProjectionVisibility(t *testing.T) {
	joined := time.Unix(20, 0).UTC()
	f := &fakeStore{
		conversation: conversation(),
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", HistoryVisibility: FullHistory, JoinedAt: &joined},
		post:         Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "author", Body: "older pinned work", Sequence: 9, Revision: 2, CreatedAt: joined.Add(-time.Minute)},
		pins:         []Pin{{TenantID: "t1", ConversationID: "c1", PostID: "p1", Revision: 1}},
	}
	s := newTestService(f, func() time.Time { return joined.Add(time.Minute) })
	list := func() ([]Pin, error) {
		return s.ListPins(context.Background(), ListPinsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"})
	}
	pins, err := list()
	if err != nil || len(pins) != 1 || pins[0].Post == nil || pins[0].Post.Body != "older pinned work" || pins[0].Post.AuthorID != "author" || pins[0].Post.Sequence != 9 || pins[0].Post.Revision != 2 {
		t.Fatalf("visible pin projection = %+v, %v", pins, err)
	}
	f.membership.HistoryVisibility = FromJoin
	if pins, err = list(); err != nil || len(pins) != 0 {
		t.Fatalf("from-join historical pins = %+v, %v", pins, err)
	}
	f.membership.HistoryVisibility = NoHistory
	if pins, err = list(); err != nil || len(pins) != 0 {
		t.Fatalf("no-history pins = %+v, %v", pins, err)
	}
	f.membership.HistoryVisibility = FullHistory
	f.post.Deleted = true
	if pins, err = list(); err != nil || len(pins) != 0 {
		t.Fatalf("deleted pins = %+v, %v", pins, err)
	}
	f.post.Deleted = false
	f.post.TenantID = "other"
	if pins, err = list(); err != nil || len(pins) != 0 {
		t.Fatalf("cross-tenant post projection = %+v, %v", pins, err)
	}
	f.post.TenantID = "t1"
	f.pins[0].TenantID = "other"
	if pins, err = list(); err != nil || len(pins) != 0 {
		t.Fatalf("cross-tenant pin projection = %+v, %v", pins, err)
	}
	f.pins[0].TenantID = "t1"
	if _, err = s.ListPins(context.Background(), ListPinsRequest{Principal: Principal{TenantID: "other", SubjectID: "outsider"}, TenantID: "t1", ConversationID: "c1"}); err != ErrPermissionDenied {
		t.Fatalf("nonmember pin list = %v", err)
	}
}

func TestTodo_CHAT_023_PinProjectionBound(t *testing.T) {
	f := &fakeStore{
		conversation: conversation(),
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", HistoryVisibility: FullHistory},
		post:         Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "pinned"},
	}
	for i := 0; i < 250; i++ {
		f.pins = append(f.pins, Pin{TenantID: "t1", ConversationID: "c1", PostID: "p1", PinnedBy: fmt.Sprint(i)})
	}
	pins, err := newTestService(f, time.Now).ListPins(context.Background(), ListPinsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"})
	if err != nil || len(pins) != 200 || pins[199].PinnedBy != "199" {
		t.Fatalf("bounded pin projection = %d, %v", len(pins), err)
	}
}
