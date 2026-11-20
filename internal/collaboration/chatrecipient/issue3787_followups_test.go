package chatrecipient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type followUpRepo struct {
	repo
	rows                      []FollowUp
	err                       error
	host, home, subject       string
	minAge                    time.Duration
	limit, lists, puts        int
	putID                     Identity
	putPost, putDir, putState string
	putUntil                  time.Time
}

func (r *followUpRepo) ListFollowUps(_ context.Context, host, home, subject string, _ time.Time, minAge time.Duration, limit int) ([]FollowUp, error) {
	r.lists++
	r.host, r.home, r.subject, r.minAge, r.limit = host, home, subject, minAge, limit
	return append([]FollowUp(nil), r.rows...), r.err
}

func (r *followUpRepo) PutFollowUpState(_ context.Context, id Identity, post, direction, status string, until time.Time) error {
	r.puts++
	r.putID, r.putPost, r.putDir, r.putState, r.putUntil = id, post, direction, status, until
	return r.err
}

func TestIssue3787_FollowUps_DropsConversationsTheCallerCannotOpen_Security(t *testing.T) {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	db := &followUpRepo{rows: []FollowUp{
		{ConversationID: "open", PostID: "new", Direction: FollowUpOwed, AskedAt: base.Add(time.Hour), Body: "later"},
		{ConversationID: "secret", PostID: "s", Direction: FollowUpOwed, AskedAt: base, Body: "private plans"},
		{ConversationID: "gone", PostID: "g", Direction: FollowUpWaiting, AskedAt: base},
		{ConversationID: "open", PostID: "old", Direction: FollowUpWaiting, AskedAt: base, Body: strings.Repeat("é", 500)},
	}}
	admission := &refusing{conversations: conversations{allowed: true, denied: map[string]bool{"host/secret": true}}, missing: map[string]bool{"gone": true}}
	s := &Service{Conversations: admission, Repo: db}
	got, err := s.FollowUps(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, "host", base.Add(48*time.Hour))
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	// Oldest first: age is what the list is for.
	if got[0].PostID != "old" || got[1].PostID != "new" {
		t.Fatalf("order = %s, %s", got[0].PostID, got[1].PostID)
	}
	if db.host != "host" || db.home != "home" || db.subject != "alice" || db.limit != FollowUpsLimit || db.minAge != FollowUpMinAge {
		t.Fatalf("the list was not scoped to the caller: %+v", db)
	}
	if got[0].HostTenantID != "host" || len([]rune(got[0].Body)) != FollowedPreviewRunes || !strings.HasSuffix(got[0].Body, "…") {
		t.Fatalf("host or preview not set: %+v", got)
	}
	if admission.asked != 3 {
		t.Fatalf("each conversation must be admitted once, asked=%d", admission.asked)
	}
}

func TestIssue3787_FollowUps_Refusals(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	now := time.Now()
	if _, err := (&Service{Conversations: conversations{allowed: true}, Repo: &repo{}}).FollowUps(ctx, p, "host", now); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a store that cannot list: %v", err)
	}
	db := &followUpRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	if _, err := s.FollowUps(ctx, chat.Principal{TenantID: "home"}, "host", now); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no subject: %v", err)
	}
	if db.lists != 0 {
		t.Fatal("the store was read before the request was checked")
	}
	broken := &refusing{conversations: conversations{allowed: true}, broken: "x"}
	s2 := &Service{Conversations: broken, Repo: &followUpRepo{rows: []FollowUp{{ConversationID: "x"}}}}
	if _, err := s2.FollowUps(ctx, p, "host", now); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("an outage must not read as an empty list: %v", err)
	}
}

func TestIssue3787_SetFollowUp_ChecksBeforeAnySideEffect_Security(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	db := &followUpRepo{}
	s := &Service{Conversations: conversations{allowed: true, denied: map[string]bool{"host/secret": true}}, Repo: db}
	soon := now.Add(4 * time.Hour)
	set := func(conversation, post, direction, status string, until time.Time, who chat.Principal) error {
		return s.SetFollowUp(ctx, who, "host", conversation, post, direction, status, until, now)
	}
	for name, err := range map[string]error{
		"conversation the caller cannot open": set("secret", "m", FollowUpOwed, FollowUpResolved, time.Time{}, p),
		"unknown action":                      set("c", "m", FollowUpOwed, "deleted", time.Time{}, p),
		"unknown direction":                   set("c", "m", "sideways", FollowUpResolved, time.Time{}, p),
		"snooze without time":                 set("c", "m", FollowUpOwed, FollowUpSnoozed, time.Time{}, p),
		"snooze in the past":                  set("c", "m", FollowUpOwed, FollowUpSnoozed, now.Add(-time.Minute), p),
		"snooze for years":                    set("c", "m", FollowUpOwed, FollowUpSnoozed, now.Add(5*365*24*time.Hour), p),
		"no post":                             set("c", " ", FollowUpOwed, FollowUpResolved, time.Time{}, p),
		"no principal":                        set("c", "m", FollowUpOwed, FollowUpResolved, time.Time{}, chat.Principal{}),
	} {
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if db.puts != 0 {
		t.Fatalf("a refused request wrote %d times", db.puts)
	}
	if err := set("c", "m", FollowUpWaiting, FollowUpSnoozed, soon, p); err != nil {
		t.Fatal(err)
	}
	if db.putID != (Identity{HostTenantID: "host", HomeTenantID: "home", SubjectID: "alice", ConversationID: "c"}) || db.putPost != "m" || db.putDir != FollowUpWaiting || db.putState != FollowUpSnoozed || !db.putUntil.Equal(soon) {
		t.Fatalf("stored: %+v %s %s %s %v", db.putID, db.putPost, db.putDir, db.putState, db.putUntil)
	}
	if err := set("c", "m", FollowUpOwed, FollowUpResolved, soon, p); err != nil || !db.putUntil.IsZero() {
		t.Fatalf("a resolved row carries no snooze time: err=%v until=%v", err, db.putUntil)
	}
}
