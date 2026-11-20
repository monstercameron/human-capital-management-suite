package chatrecipient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// scheduledRepo is an in-memory ScheduledStore that remembers what it was
// asked, so a test can prove nothing reached it before a check passed.
type scheduledRepo struct {
	repo
	inserted []ScheduledPost
	limit    int
	listed   [][3]string
	updated  [][4]string
	deleted  [][4]string
	rows     []ScheduledPost
	err      error
}

func (r *scheduledRepo) InsertScheduled(_ context.Context, p ScheduledPost, limit int) (ScheduledPost, error) {
	r.inserted, r.limit = append(r.inserted, p), limit
	p.ID, p.Revision = "s1", 1
	return p, r.err
}
func (r *scheduledRepo) ListScheduled(_ context.Context, host, home, author string) ([]ScheduledPost, error) {
	r.listed = append(r.listed, [3]string{host, home, author})
	return append([]ScheduledPost(nil), r.rows...), r.err
}
func (r *scheduledRepo) UpdateScheduled(_ context.Context, host, home, author, id, body string, at time.Time, zone string, expected uint64) (ScheduledPost, error) {
	r.updated = append(r.updated, [4]string{host, home, author, id})
	return ScheduledPost{ID: id, HostTenantID: host, HomeTenantID: home, AuthorID: author, ConversationID: "c", Body: body, SendAt: at, Zone: zone, Revision: expected + 1}, r.err
}
func (r *scheduledRepo) DeleteScheduled(_ context.Context, host, home, author, id string) error {
	r.deleted = append(r.deleted, [4]string{host, home, author, id})
	return r.err
}
func (r *scheduledRepo) DueScheduled(context.Context, string, time.Time, int) ([]ScheduledPost, error) {
	return nil, nil
}
func (r *scheduledRepo) ClaimScheduled(context.Context, string, string, time.Time, time.Duration) (ScheduledPost, bool, error) {
	return ScheduledPost{}, false, nil
}
func (r *scheduledRepo) FinishScheduled(context.Context, string, string) error { return nil }
func (r *scheduledRepo) FailScheduled(context.Context, string, string, string) error {
	return nil
}
func (r *scheduledRepo) ReleaseScheduled(context.Context, string, string, int) error { return nil }

// roomWith answers a conversation of one kind and its members.
type roomWith struct {
	conversations
	kind    chat.ConversationKind
	members []chat.Membership
}

func (c roomWith) GetConversation(ctx context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	got, err := c.conversations.GetConversation(ctx, r)
	got.Kind = c.kind
	return got, err
}
func (c roomWith) ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	return chat.ListMembershipsResponse{Memberships: c.members}, nil
}

var scheduledNow = time.Date(2026, 3, 3, 23, 0, 0, 0, time.UTC)

func TestIssue3788_ScheduleSend_KeepsTheMessageForItsAuthor(t *testing.T) {
	db := &scheduledRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	at := scheduledNow.Add(9*time.Hour + 30*time.Minute)
	got, err := s.ScheduleSend(context.Background(), p, "host", "room", "  Training is tomorrow  ", at, "Europe/Berlin", "org-1", scheduledNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.inserted) != 1 {
		t.Fatalf("inserted = %d", len(db.inserted))
	}
	row := db.inserted[0]
	if row.AuthorID != "alice" || row.HomeTenantID != "home" || row.HostTenantID != "host" || row.ConversationID != "room" || row.Body != "Training is tomorrow" || row.State != ScheduledPending || row.Zone != "Europe/Berlin" || !row.SendAt.Equal(at) || row.OrgScopeID != "org-1" {
		t.Fatalf("stored row = %+v", row)
	}
	if db.limit != ScheduledPerPersonMax || got.ID == "" {
		t.Fatalf("limit=%d got=%+v", db.limit, got)
	}
}

func TestIssue3788_ScheduleSend_RefusesBeforeAnythingIsStored(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	cases := map[string]struct {
		conv chat.ConversationService
		body string
		at   time.Time
		zone string
		want error
	}{
		"empty body":           {conversations{allowed: true}, "   ", scheduledNow.Add(time.Hour), "UTC", chat.ErrInvalidArgument},
		"too long":             {conversations{allowed: true}, strings.Repeat("x", ScheduledBodyRunes+1), scheduledNow.Add(time.Hour), "UTC", chat.ErrInvalidArgument},
		"in the past":          {conversations{allowed: true}, "hi", scheduledNow.Add(-time.Hour), "UTC", ErrScheduledTime},
		"too soon":             {conversations{allowed: true}, "hi", scheduledNow.Add(10 * time.Second), "UTC", ErrScheduledTime},
		"more than a year":     {conversations{allowed: true}, "hi", scheduledNow.Add(ScheduledMaxLead + time.Hour), "UTC", ErrScheduledTime},
		"unknown zone":         {conversations{allowed: true}, "hi", scheduledNow.Add(time.Hour), "Mars/Base", chat.ErrInvalidArgument},
		"cannot open the room": {conversations{denied: map[string]bool{"host/room": true}, allowed: true}, "hi", scheduledNow.Add(time.Hour), "UTC", chat.ErrPermissionDenied},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db := &scheduledRepo{}
			s := &Service{Conversations: tc.conv, Repo: db}
			_, err := s.ScheduleSend(context.Background(), p, "host", "room", tc.body, tc.at, tc.zone, "", scheduledNow)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(db.inserted) != 0 {
				t.Fatal("a refused message reached the store")
			}
		})
	}
}

func TestIssue3788_ScheduleSend_NeedsAStoreThatKeepsThem(t *testing.T) {
	s := &Service{Conversations: conversations{allowed: true}, Repo: &repo{}}
	_, err := s.ScheduleSend(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, "host", "room", "hi", scheduledNow.Add(time.Hour), "UTC", "", scheduledNow)
	if !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestIssue3788_ScheduledPosts_AreReadAsTheCallerOnly_Security(t *testing.T) {
	db := &scheduledRepo{rows: []ScheduledPost{{ID: "a", OrgScopeID: "secret-scope", ConversationID: "room", State: ScheduledFailed, Refusal: ScheduledRefusedAccess}}}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	rows, err := s.ScheduledPosts(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, "host")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if len(db.listed) != 1 || db.listed[0] != [3]string{"host", "home", "alice"} {
		t.Fatalf("the list was not scoped to the caller: %v", db.listed)
	}
	if rows[0].OrgScopeID != "" {
		t.Fatal("the organization scope left the service")
	}
	if _, err := s.ScheduledPosts(context.Background(), chat.Principal{TenantID: "home"}, "host"); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no subject: %v", err)
	}
}

func TestIssue3788_EditAndCancel_AreScopedToTheCaller_Security(t *testing.T) {
	db := &scheduledRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	got, err := s.EditScheduled(context.Background(), p, "host", "s1", "new text", scheduledNow.Add(2*time.Hour), "UTC", 3, scheduledNow)
	if err != nil || got.Body != "new text" || got.Revision != 4 {
		t.Fatalf("edit = %+v err=%v", got, err)
	}
	if db.updated[0] != [4]string{"host", "home", "alice", "s1"} {
		t.Fatalf("update scope = %v", db.updated)
	}
	if _, err = s.EditScheduled(context.Background(), p, "host", "s1", "text", scheduledNow.Add(2*time.Hour), "UTC", 0, scheduledNow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a missing revision must be refused: %v", err)
	}
	if err = s.CancelScheduled(context.Background(), p, "host", "s1"); err != nil {
		t.Fatal(err)
	}
	if db.deleted[0] != [4]string{"host", "home", "alice", "s1"} {
		t.Fatalf("delete scope = %v", db.deleted)
	}
	if err = s.CancelScheduled(context.Background(), p, "host", " "); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a blank id must be refused: %v", err)
	}
}

func TestIssue3788_QuietHours_WarnWithoutNamingAnyone(t *testing.T) {
	// 08:30 Berlin is 07:30 UTC; the other person's quiet hours are 22:00 to 09:00 in Berlin.
	at := time.Date(2026, 3, 4, 7, 30, 0, 0, time.UTC)
	db := &scheduledRepo{repo: repo{quiet: QuietHours{Enabled: true, Timezone: "Europe/Berlin", StartMinute: 22 * 60, EndMinute: 9 * 60}}}
	room := roomWith{conversations: conversations{allowed: true}, kind: chat.Direct, members: []chat.Membership{
		{SubjectID: "alice", HomeTenantID: "home"}, {SubjectID: "bob", HomeTenantID: "home"}}}
	s := &Service{Conversations: room, Repo: db}
	got, err := s.ScheduleSend(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, "host", "room", "hi", at, "Europe/Berlin", "", scheduledNow)
	if err != nil || got.QuietPeople != 1 {
		t.Fatalf("quiet people = %d err=%v", got.QuietPeople, err)
	}
	// A channel addresses nobody in particular.
	room.kind = chat.PublicChannel
	s = &Service{Conversations: room, Repo: db}
	got, err = s.ScheduleSend(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, "host", "room", "hi", at, "Europe/Berlin", "", scheduledNow)
	if err != nil || got.QuietPeople != 0 {
		t.Fatalf("a channel must not warn: %d err=%v", got.QuietPeople, err)
	}
}

func TestIssue3788_InQuietHours(t *testing.T) {
	night := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 7 * 60}
	day := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 12 * 60, EndMinute: 13 * 60}
	at := func(h, m int) time.Time { return time.Date(2026, 3, 4, h, m, 0, 0, time.UTC) }
	for name, tc := range map[string]struct {
		q    QuietHours
		at   time.Time
		want bool
	}{
		"overnight, late":    {night, at(23, 0), true},
		"overnight, early":   {night, at(6, 59), true},
		"overnight, the end": {night, at(7, 0), false},
		"overnight, midday":  {night, at(12, 0), false},
		"same day, inside":   {day, at(12, 30), true},
		"same day, outside":  {day, at(13, 0), false},
		"switched off":       {QuietHours{Timezone: "UTC", StartMinute: 0, EndMinute: 1439}, at(1, 0), false},
		"unknown zone":       {QuietHours{Enabled: true, Timezone: "Nowhere/Land", StartMinute: 0, EndMinute: 1439}, at(1, 0), false},
		"an empty window":    {QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 60, EndMinute: 60}, at(1, 0), false},
	} {
		if got := InQuietHours(tc.q, tc.at); got != tc.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}
