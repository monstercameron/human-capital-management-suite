package chatrecipient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type digestRepo struct {
	repo
	pref      DigestPref
	found     bool
	rows      []DigestItem
	put       []DigestPref
	advanced  time.Time
	scanSince time.Time
	scans     int
	err       error
}

func (r *digestRepo) DigestPref(context.Context, string, string) (DigestPref, bool, error) {
	return r.pref, r.found, r.err
}
func (r *digestRepo) PutDigestPref(_ context.Context, _, _ string, p DigestPref) error {
	r.put = append(r.put, p)
	return r.err
}
func (r *digestRepo) DigestCandidates(_ context.Context, _, _, _ string, since time.Time, _ int) ([]DigestItem, error) {
	r.scans++
	r.scanSince = since
	return append([]DigestItem(nil), r.rows...), r.err
}
func (r *digestRepo) AdvanceDigest(_ context.Context, _, _ string, through time.Time) error {
	r.advanced = through
	return r.err
}

var digestPrincipal = chat.Principal{TenantID: "home", SubjectID: "alice"}

// 2026-03-10 is a Tuesday. The person's day starts at 08:00 UTC.
var digestNow = time.Date(2026, 3, 10, 8, 5, 0, 0, time.UTC)

func TestIssue3786_DigestInstant(t *testing.T) {
	quiet := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 8 * 60}
	got, ok := DigestInstant(DigestPref{Cadence: DigestQuietEnd}, quiet, digestNow)
	if !ok || !got.Equal(time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("quiet end = %v %v", got, ok)
	}
	// Before today's time, the instant is yesterday's.
	got, ok = DigestInstant(DigestPref{Cadence: DigestDaily, Minute: 9 * 60, Timezone: "UTC"}, quiet, digestNow)
	if !ok || !got.Equal(time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily = %v %v", got, ok)
	}
	// Local time zone: 08:00 in New York is 12:00 UTC (daylight time).
	got, ok = DigestInstant(DigestPref{Cadence: DigestDaily, Minute: 8 * 60, Timezone: "America/New_York"}, quiet, digestNow)
	if !ok || !got.Equal(time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("zone = %v %v", got, ok)
	}
	if _, ok := DigestInstant(DigestPref{Cadence: DigestOff}, quiet, digestNow); ok {
		t.Fatal("an off digest has no time")
	}
	if _, ok := DigestInstant(DigestPref{Cadence: DigestQuietEnd}, QuietHours{}, digestNow); ok {
		t.Fatal("quiet-end needs quiet hours switched on")
	}
	if _, ok := DigestInstant(DigestPref{Cadence: DigestDaily, Minute: 5, Timezone: "Nowhere/Land"}, quiet, digestNow); ok {
		t.Fatal("an unknown zone has no time")
	}
}

func TestIssue3786_Digest_HoldsOnlyWhatWasHeldBack_Security(t *testing.T) {
	quiet := QuietHours{Enabled: true, Timezone: "UTC", StartMinute: 22 * 60, EndMinute: 8 * 60}
	at := func(h, m int) time.Time { return time.Date(2026, 3, 10, h, m, 0, 0, time.UTC) }
	db := &digestRepo{found: true,
		pref: DigestPref{Cadence: DigestQuietEnd, Timezone: "UTC", LastDigestAt: time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)},
		rows: []DigestItem{
			{Kind: NotificationMention, PostID: "late", ConversationID: "open", At: at(2, 0), AuthorID: "bob"},
			{Kind: NotificationMention, PostID: "daytime", ConversationID: "open", At: time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC)},
			{Kind: NotificationMention, PostID: "muted", ConversationID: "open", At: time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC), Muted: true},
			{Kind: NotificationMention, PostID: "secret", ConversationID: "secret", At: at(3, 0)},
			{Kind: NotificationMention, PostID: "read", ConversationID: "open", At: at(4, 0), Read: true},
			{Kind: NotificationReply, PostID: "r1", ThreadRootID: "root", ConversationID: "open", At: at(5, 0)},
			{Kind: NotificationReply, PostID: "r2", ThreadRootID: "root", ConversationID: "open", At: at(6, 0)},
			{Kind: NotificationReply, PostID: "gone", ThreadRootID: "root2", ConversationID: "gone", At: at(6, 0)},
		}}
	admission := &refusing{conversations: conversations{allowed: true, denied: map[string]bool{"host/secret": true}}, missing: map[string]bool{"gone": true}}
	db.quiet = quiet
	s := &Service{Conversations: admission, Repo: db}
	d, err := s.Digest(context.Background(), digestPrincipal, "host", digestNow)
	if err != nil || !d.Due {
		t.Fatalf("digest = %+v err=%v", d, err)
	}
	got := map[string]DigestItem{}
	for _, it := range d.Items {
		got[it.PostID] = it
	}
	if len(got) != 3 {
		t.Fatalf("items = %+v, want the late mention, the muted mention and one followed thread", d.Items)
	}
	if got["late"].Why != DigestWhyQuietHours || got["muted"].Why != DigestWhyMuted || got["r2"].Why != DigestWhyFollowed {
		t.Fatalf("reasons = %+v", got)
	}
	if got["r2"].Count != 2 {
		t.Fatalf("a followed thread is one item standing for its replies: %+v", got["r2"])
	}
	if !db.scanSince.Equal(db.pref.LastDigestAt) {
		t.Fatalf("scanned since %v", db.scanSince)
	}
	if d.Items[0].PostID != "r2" {
		t.Fatalf("newest first, got %s", d.Items[0].PostID)
	}
}

func TestIssue3786_Digest_NotDueYet(t *testing.T) {
	db := &digestRepo{found: true, pref: DigestPref{Cadence: DigestDaily, Minute: 8 * 60, Timezone: "UTC", LastDigestAt: time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)},
		rows: []DigestItem{{Kind: NotificationMention, PostID: "m", ConversationID: "open", At: digestNow.Add(-time.Minute), Muted: true}}}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	d, err := s.Digest(context.Background(), digestPrincipal, "host", digestNow)
	if err != nil || d.Due || len(d.Items) != 0 || db.scans != 0 {
		t.Fatalf("a digest already seen for this time must not repeat: %+v err=%v scans=%d", d, err, db.scans)
	}
	// Off never reads.
	db.pref.Cadence = DigestOff
	if d, err = s.Digest(context.Background(), digestPrincipal, "host", digestNow); err != nil || d.Due || db.scans != 0 {
		t.Fatalf("off: %+v %v", d, err)
	}
	// Nothing held back means nothing due.
	db.pref = DigestPref{Cadence: DigestDaily, Minute: 8 * 60, Timezone: "UTC", LastDigestAt: digestNow.Add(-24 * time.Hour)}
	db.rows = nil
	if d, err = s.Digest(context.Background(), digestPrincipal, "host", digestNow); err != nil || d.Due || len(d.Items) != 0 {
		t.Fatalf("empty: %+v %v", d, err)
	}
}

func TestIssue3786_PutDigestPrefs(t *testing.T) {
	ctx := context.Background()
	db := &digestRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	got, err := s.PutDigestPrefs(ctx, digestPrincipal, DigestPref{Cadence: DigestDaily, Minute: 485, Timezone: "Europe/Berlin"}, digestNow)
	if err != nil || len(db.put) != 1 || got.Cadence != DigestDaily || !got.LastDigestAt.Equal(digestNow) {
		t.Fatalf("turning it on starts from now: %+v %+v %v", got, db.put, err)
	}
	// Changing the time keeps the moment the last digest was seen.
	db.found, db.pref = true, DigestPref{Cadence: DigestDaily, Minute: 485, Timezone: "UTC", LastDigestAt: digestNow.Add(-time.Hour)}
	got, _ = s.PutDigestPrefs(ctx, digestPrincipal, DigestPref{Cadence: DigestDaily, Minute: 600}, digestNow)
	if !got.LastDigestAt.Equal(digestNow.Add(-time.Hour)) || got.Timezone != "UTC" {
		t.Fatalf("kept = %+v", got)
	}
	for name, bad := range map[string]DigestPref{
		"cadence": {Cadence: "weekly"},
		"minute":  {Cadence: DigestDaily, Minute: 1440},
		"zone":    {Cadence: DigestDaily, Timezone: "Nowhere/Land"},
	} {
		n := len(db.put)
		if _, err := s.PutDigestPrefs(ctx, digestPrincipal, bad, digestNow); !errors.Is(err, chat.ErrInvalidArgument) || len(db.put) != n {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := s.PutDigestPrefs(ctx, chat.Principal{TenantID: "home"}, DigestPref{Cadence: DigestOff}, digestNow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no subject: %v", err)
	}
	if _, err := (&Service{Conversations: conversations{allowed: true}, Repo: &repo{}}).Digest(ctx, digestPrincipal, "host", digestNow); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a store without a digest: %v", err)
	}
}

func TestIssue3786_DigestSeen_NeverPastNow(t *testing.T) {
	db := &digestRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	if err := s.DigestSeen(context.Background(), digestPrincipal, digestNow.Add(time.Hour), digestNow); err != nil || !db.advanced.Equal(digestNow) {
		t.Fatalf("advanced %v err %v", db.advanced, err)
	}
	if err := s.DigestSeen(context.Background(), digestPrincipal, time.Time{}, digestNow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("zero: %v", err)
	}
}
