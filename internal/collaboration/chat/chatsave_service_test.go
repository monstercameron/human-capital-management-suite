package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type chatsaveFixture struct {
	*fakeStore
	ChannelStatusStore
	items     []SavedItem
	reminders int
}

func (f *chatsaveFixture) ReadChannelStatus(_ context.Context, tenant, conversation string) (ChannelStatus, error) {
	return ChannelStatus{TenantID: tenant, ConversationID: conversation, Status: chatpolicy.StatusOpen, Revision: 1}, nil
}

func (f *chatsaveFixture) SweepChannelStatuses(context.Context, string, time.Time) (int, error) {
	return 0, ErrUnavailable
}
func (f *chatsaveFixture) CommitChannelStatus(context.Context, ChangeChannelStatusRequest, time.Time, func(context.Context, ChannelStatus) error) (ChannelStatus, error) {
	return ChannelStatus{}, ErrUnavailable
}

func (f *chatsaveFixture) SaveItem(_ context.Context, p Principal, item SavedItem) (SavedItem, error) {
	for _, x := range f.items {
		if x.PostID == item.PostID && x.PersonID == p.SubjectID {
			return x, nil
		}
	}
	if len(f.items) >= SavedLimit {
		return SavedItem{}, ErrSavedLimit
	}
	f.items = append(f.items, item)
	return item, nil
}
func (f *chatsaveFixture) RemoveSaved(_ context.Context, p Principal, tenant, cid, pid string) error {
	for i, x := range f.items {
		if x.PersonID == p.SubjectID && x.HomeTenantID == p.TenantID && x.TenantID == tenant && x.ConversationID == cid && x.PostID == pid {
			f.items = append(f.items[:i], f.items[i+1:]...)
			break
		}
	}
	return nil
}
func (f *chatsaveFixture) ChangeSaved(_ context.Context, p Principal, tenant, cid, pid string, c SavedChange) (SavedItem, error) {
	for i, x := range f.items {
		if x.PersonID == p.SubjectID && x.HomeTenantID == p.TenantID && x.TenantID == tenant && x.ConversationID == cid && x.PostID == pid {
			if c.State != nil {
				x.State = *c.State
			}
			if c.Note != nil {
				x.Note = *c.Note
			}
			if c.SetDue {
				x.DueAt = c.DueAt
			}
			f.items[i] = x
			return x, nil
		}
	}
	return SavedItem{}, ErrNotFound
}
func (f *chatsaveFixture) ListSavedItems(_ context.Context, p Principal, tenant string, tab SavedState, _ Page) (SavedPage, error) {
	page := SavedPage{Items: []SavedItem{}}
	for _, x := range f.items {
		if x.TenantID == tenant && x.PersonID == p.SubjectID && x.HomeTenantID == p.TenantID && (tab == SavedAll || tab == x.State) {
			page.Items = append(page.Items, x)
		}
	}
	return page, nil
}
func (f *chatsaveFixture) DeliverSavedReminder(ctx context.Context, p Principal, item SavedItem, sink SavedReminderSink) error {
	f.reminders++
	return sink.NotifySaved(ctx, p, item, "private-key")
}

type chatsaveSink struct {
	calls int
	fail  bool
}

func (s *chatsaveSink) NotifySaved(_ context.Context, p Principal, item SavedItem, key string) error {
	if s.fail {
		return ErrUnavailable
	}
	if p.SubjectID != item.PersonID || key == "" {
		return ErrPermissionDenied
	}
	s.calls++
	return nil
}

func chatsaveService() (*Service, *chatsaveFixture, SavedRequest) {
	f := &chatsaveFixture{fakeStore: &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", ConversationID: "c1", HistoryVisibility: FullHistory}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "current body", Sequence: 7}}}
	s := NewService(f, func() time.Time { return time.Unix(1000, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: f.fakeStore})
	return s, f, SavedRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1"}
}

func TestTodo_CHATSAVE_001(t *testing.T) {
	ctx := context.Background()
	s, f, r := chatsaveService()
	x, err := s.SaveForLater(ctx, r)
	if err != nil || x.State != SavedTodo || x.Post != nil || len(f.items) != 1 {
		t.Fatalf("save=%+v %v", x, err)
	}
	if _, err = s.SaveForLater(ctx, r); err != nil || len(f.items) != 1 {
		t.Fatal("duplicate save", err)
	}
	if _, err = s.SetSavedNote(ctx, r, "private follow up"); err != nil {
		t.Fatal(err)
	}
	due := time.Unix(2000, 0).UTC()
	if _, err = s.SetSavedDue(ctx, r, &due); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MarkSavedDone(ctx, r); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListSaved(ctx, SavedListRequest{Principal: r.Principal, TenantID: r.TenantID, Tab: SavedDone})
	if err != nil || len(page.Items) != 1 || page.Items[0].Note != "private follow up" || !page.Items[0].DueAt.Equal(due) || page.Items[0].Post.Sequence != 7 {
		t.Fatalf("done=%+v %v", page, err)
	}
	page, err = s.ListSaved(ctx, SavedListRequest{Principal: r.Principal, TenantID: r.TenantID, Tab: SavedTodo})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("todo contains done")
	}
	if _, err = s.ReopenSaved(ctx, r); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"follow up", "in: saved current body"} {
		matches, err := s.SearchSaved(ctx, r.Principal, q)
		if err != nil || len(matches) != 1 {
			t.Fatalf("search %q: %+v %v", q, matches, err)
		}
	}
	f.post.Body = "edited replacement"
	matches, err := s.SearchSaved(ctx, r.Principal, "current body")
	if err != nil || len(matches) != 0 {
		t.Fatal("search copied stale body")
	}
	if err = s.Unsave(ctx, r); err != nil || len(f.items) != 0 {
		t.Fatal("unsave", err)
	}
	f.items = make([]SavedItem, SavedLimit)
	if _, err = s.SaveForLater(ctx, r); !errors.Is(err, ErrSavedLimit) {
		t.Fatalf("limit=%v", err)
	}
}

func TestTodo_CHATSAVE_001_Security(t *testing.T) {
	ctx := context.Background()
	s, f, r := chatsaveService()
	if _, err := s.SaveForLater(ctx, r); err != nil {
		t.Fatal(err)
	}
	other := Principal{TenantID: "t1", SubjectID: "admin"}
	page, err := s.ListSaved(ctx, SavedListRequest{Principal: other, TenantID: "t1", Tab: SavedAll})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("owner list leaked")
	}
	if _, err = s.SetSavedNote(ctx, SavedRequest{Principal: other, TenantID: "t1", ConversationID: "c1", PostID: "p1"}, "forged"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross person update=%v", err)
	}
	left := time.Unix(900, 0)
	f.membership.LeftAt = &left
	page, err = s.ListSaved(ctx, SavedListRequest{Principal: r.Principal, TenantID: "t1", Tab: SavedAll})
	if err != nil || page.Items[0].Post != nil || page.Items[0].Channel != "" || page.Items[0].Availability != "no_access" {
		t.Fatalf("left projection=%+v %v", page, err)
	}
	if _, err = s.SaveForLater(ctx, r); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("save after leave", err)
	}
	matches, err := s.SearchSaved(ctx, r.Principal, "current body")
	if err != nil || len(matches) != 0 {
		t.Fatal("left text searchable")
	}
	f.membership.LeftAt = nil
	f.post.Deleted = true
	page, err = s.ListSaved(ctx, SavedListRequest{Principal: r.Principal, TenantID: "t1", Tab: SavedAll})
	if err != nil || page.Items[0].Availability != "deleted" || page.Items[0].Post != nil {
		t.Fatal("deleted text leaked")
	}
	f.post.Deleted = false
	f.post.ID = ""
	page, err = s.ListSaved(ctx, SavedListRequest{Principal: r.Principal, TenantID: "t1", Tab: SavedAll})
	if err != nil || page.Items[0].Availability != "removed" || page.Items[0].Post != nil {
		t.Fatal("removed message text leaked")
	}
	f.post.ID = "p1"
	f.membership.HistoryVisibility = NoHistory
	if _, err = s.SaveForLater(ctx, r); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("no history save", err)
	}
	if f.mutations != 0 {
		t.Fatal("saving emitted a shared mutation")
	}
}

func TestTodo_CHATSAVE_001_Validation(t *testing.T) {
	s, _, r := chatsaveService()
	ctx := context.Background()
	if _, err := s.ListSaved(ctx, SavedListRequest{Principal: r.Principal, TenantID: r.TenantID, Tab: "invalid"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("invalid tab", err)
	}
	if _, err := s.ListSaved(ctx, SavedListRequest{Principal: r.Principal, TenantID: r.TenantID, Page: Page{PageSize: 201}}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("invalid page", err)
	}
	bad := r
	bad.PostID = ""
	if _, err := s.SaveForLater(ctx, bad); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("missing reference", err)
	}
	if err := s.Unsave(ctx, bad); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("missing unsave reference", err)
	}
	if _, err := s.SetSavedNote(ctx, r, string(make([]byte, 4001))); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("unbounded note", err)
	}
	var absent *Service
	if _, err := absent.ListSaved(ctx, SavedListRequest{Principal: r.Principal, TenantID: r.TenantID}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil service", err)
	}
}

func TestTodo_CHATSAVE_001_Reminder(t *testing.T) {
	s, f, r := chatsaveService()
	ctx := context.Background()
	_, err := s.SaveForLater(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Unix(900, 0)
	f.items[0].DueAt = &due
	sink := &chatsaveSink{}
	if err = s.DispatchSavedReminders(ctx, r.Principal, r.TenantID, sink); err != nil || sink.calls != 1 {
		t.Fatalf("reminder=%d %v", sink.calls, err)
	}
	sink.fail = true
	if err = s.DispatchSavedReminders(ctx, r.Principal, r.TenantID, sink); !errors.Is(err, ErrUnavailable) {
		t.Fatal("delivery failure swallowed")
	}
	if err = s.DispatchSavedReminders(ctx, r.Principal, r.TenantID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing sink")
	}
	if _, err = s.SetSavedDue(ctx, r, &due); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("past due accepted")
	}
}
