package chatrecipient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type awayRepo struct {
	notificationRepo
	stored    Away
	put       Away
	puts      int
	expected  uint64
	public    []AwayPublic
	askedIDs  []string
	cover     []Notification
	coverRead []string
	coverAll  bool
}

func (r *awayRepo) Away(context.Context, string, string) (Away, error) { return r.stored, nil }
func (r *awayRepo) PutAway(_ context.Context, _, _ string, a Away, expected uint64) (Away, error) {
	r.puts++
	r.put, r.expected = a, expected
	a.Revision = expected + 1
	return a, nil
}
func (r *awayRepo) AwayOf(_ context.Context, _ string, ids []string, _ time.Time) ([]AwayPublic, error) {
	r.askedIDs = ids
	return r.public, nil
}
func (r *awayRepo) CoverNotices(context.Context, string, string, string, time.Time, time.Time, int) ([]Notification, error) {
	return append([]Notification(nil), r.cover...), nil
}
func (r *awayRepo) MarkCoverRead(_ context.Context, _, _, _ string, _, _ time.Time, ids []string, all bool) (int64, error) {
	r.coverRead, r.coverAll = ids, all
	return 2, nil
}

var awayNow = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

func TestIssue3789_PutAway_Validates(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	ctx := context.Background()
	until := awayNow.Add(7 * 24 * time.Hour)
	cases := []struct {
		name string
		a    Away
		exp  uint64
		ok   bool
	}{
		{"away with cover", Away{Until: until, CoverID: "bob", NotifyCover: true}, 1, true},
		{"away without cover", Away{Until: until}, 1, true},
		{"ending the absence", Away{Hidden: true}, 1, true},
		{"in the past", Away{Until: awayNow.Add(-time.Minute)}, 1, false},
		{"now", Away{Until: awayNow}, 1, false},
		{"too far", Away{Until: awayNow.Add(AwayMaxDuration + time.Hour)}, 1, false},
		{"covering yourself", Away{Until: until, CoverID: "alice"}, 1, false},
		{"notices without a cover", Away{Until: until, NotifyCover: true}, 1, false},
		{"a cover with no absence", Away{CoverID: "bob"}, 1, false},
		{"notices with no absence", Away{NotifyCover: true}, 1, false},
		{"no revision", Away{Until: until}, 0, false},
	}
	for _, c := range cases {
		db := &awayRepo{}
		s := &Service{Conversations: conversations{allowed: true}, Repo: db}
		got, err := s.PutAway(ctx, p, c.a, c.exp, awayNow)
		if c.ok != (err == nil) {
			t.Fatalf("%s: err=%v", c.name, err)
		}
		if !c.ok {
			if db.puts != 0 || !errors.Is(err, chat.ErrInvalidArgument) {
				t.Fatalf("%s: a refused save reached the store or had the wrong error: %v puts=%d", c.name, err, db.puts)
			}
			continue
		}
		if db.puts != 1 || got.Revision != c.exp+1 {
			t.Fatalf("%s: %+v", c.name, got)
		}
	}
	// The person comes from the session: no subject, no save.
	if _, err := (&Service{Repo: &awayRepo{}}).PutAway(ctx, chat.Principal{TenantID: "home"}, Away{}, 1, awayNow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no subject: %v", err)
	}
	if _, err := (&Service{Repo: &repo{}}).PutAway(ctx, p, Away{}, 1, awayNow); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a store that cannot keep it: %v", err)
	}
}

func TestIssue3789_PutAway_KeepsWhenARunningAbsenceBegan(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	began := awayNow.Add(-24 * time.Hour)
	db := &awayRepo{stored: Away{Since: began, Until: awayNow.Add(time.Hour), Revision: 4}}
	s := &Service{Repo: db}
	if _, err := s.PutAway(context.Background(), p, Away{Until: awayNow.Add(48 * time.Hour), CoverID: "bob", NotifyCover: true}, 4, awayNow); err != nil {
		t.Fatal(err)
	}
	if !db.put.Since.Equal(began) {
		t.Fatalf("extending an absence moved its start, so the cover would be told again: %v", db.put.Since)
	}
	// A new absence starts now.
	db = &awayRepo{stored: Away{Revision: 1}}
	if _, err := (&Service{Repo: db}).PutAway(context.Background(), p, Away{Until: awayNow.Add(time.Hour)}, 1, awayNow); err != nil || !db.put.Since.Equal(awayNow) {
		t.Fatalf("new absence: %v %v", db.put.Since, err)
	}
}

func TestIssue3789_Away_AnEndedAbsenceReadsAsNone(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	ended := Away{Since: awayNow.Add(-72 * time.Hour), Until: awayNow.Add(-time.Hour), CoverID: "bob", NotifyCover: true, Hidden: true, Revision: 5}
	got, err := (&Service{Repo: &awayRepo{stored: ended}}).Away(context.Background(), p, awayNow)
	if err != nil || got.Active(awayNow) || got.CoverID != "" || got.NotifyCover || !got.Until.IsZero() {
		t.Fatalf("ended absence: %+v %v", got, err)
	}
	// The person's own display choice and revision survive the end of an absence.
	if !got.Hidden || got.Revision != 5 {
		t.Fatalf("lost the person's choice: %+v", got)
	}
}

func TestIssue3789_AwayOf_CleansAndBoundsTheLookup_Security(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	ctx := context.Background()
	db := &awayRepo{public: []AwayPublic{{MemberID: "bob", Until: awayNow.Add(time.Hour), CoverID: "cy"}}}
	s := &Service{Repo: db}
	got, err := s.AwayOf(ctx, p, []string{" bob ", "bob", "dee"}, awayNow)
	if err != nil || len(got) != 1 || got[0].CoverID != "cy" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if len(db.askedIDs) != 2 || db.askedIDs[0] != "bob" || db.askedIDs[1] != "dee" {
		t.Fatalf("ids were not cleaned: %v", db.askedIDs)
	}
	many := make([]string, AwayLookupLimit+1)
	for i := range many {
		many[i] = string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if _, err := s.AwayOf(ctx, p, many, awayNow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("an unbounded lookup was allowed: %v", err)
	}
	if _, err := s.AwayOf(ctx, p, []string{""}, awayNow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("an empty id: %v", err)
	}
	if _, err := s.AwayOf(ctx, chat.Principal{TenantID: "home"}, []string{"bob"}, awayNow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no subject: %v", err)
	}
}

func TestIssue3789_CoverNotices_JoinTheFeedAndAreAdmittedAgain_Security(t *testing.T) {
	base := awayNow
	db := &awayRepo{}
	db.rows = []Notification{{PostID: "m1", ConversationID: "open", Kind: NotificationMention, At: base}}
	db.cover = []Notification{
		{PostID: "m1", ConversationID: "open", CoveringID: "bob", At: base},                                     // already a mention of the cover
		{PostID: "c1", ConversationID: "open", CoveringID: "bob", Body: "never shown", At: base.Add(time.Hour)}, // a cover notice
		{PostID: "c2", ConversationID: "secret", CoveringID: "bob", At: base.Add(2 * time.Hour)},                // cover lost access
	}
	admission := &refusing{conversations: conversations{allowed: true, denied: map[string]bool{"host/secret": true}}}
	s := &Service{Conversations: admission, Repo: db}
	got, err := s.Notifications(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, "host", base.Add(time.Hour))
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got[0].PostID != "c1" || got[0].Kind != NotificationCover || got[0].CoveringID != "bob" || got[0].Body != "" {
		t.Fatalf("cover item: %+v", got[0])
	}
	if got[1].PostID != "m1" || got[1].Kind != NotificationMention {
		t.Fatalf("a post that mentions the cover must stay a mention: %+v", got[1])
	}
}

func TestIssue3789_MarkingReadAlsoMarksCoverNotices(t *testing.T) {
	db := &awayRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	n, err := s.MarkNotificationsRead(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, "host", []string{"c1"}, false, awayNow)
	if err != nil || n != 3 || len(db.coverRead) != 1 || db.coverRead[0] != "c1" || db.coverAll {
		t.Fatalf("n=%d err=%v read=%v", n, err, db.coverRead)
	}
}
