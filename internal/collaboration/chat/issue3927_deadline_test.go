package chat

import (
	"errors"
	"testing"
	"time"
)

func TestIssue3927_ResolveDateTime(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tz database")
	}
	now := time.Date(2027, 3, 1, 9, 0, 0, 0, ny)
	cases := []struct {
		name, date, clock string
		want              error
		utc               string
	}{
		{"noon", "2027-03-05", "12:00", nil, "2027-03-05T17:00:00Z"},
		{"summer noon", "2027-06-05", "12:00", nil, "2027-06-05T16:00:00Z"},
		{"past", "2027-02-01", "12:00", ErrDeadlinePast, ""},
		{"earlier today", "2027-03-01", "08:00", ErrDeadlinePast, ""},
		{"spring gap", "2027-03-14", "02:30", ErrDeadlineNotExists, ""},
		{"just after gap", "2027-03-14", "03:00", nil, "2027-03-14T07:00:00Z"},
		{"autumn repeat", "2027-11-07", "01:30", ErrDeadlineAmbiguous, ""},
		{"after repeat", "2027-11-07", "02:30", nil, "2027-11-07T07:30:00Z"},
		{"bad date", "tomorrow", "12:00", ErrDeadlineInvalid, ""},
		{"bad clock", "2027-03-05", "25:00", ErrDeadlineInvalid, ""},
		{"empty clock", "2027-03-05", "", ErrDeadlineInvalid, ""},
	}
	for _, c := range cases {
		got, err := Chatcmd004ResolveDateTime(c.date, c.clock, ny, now)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: err %v, want %v", c.name, err, c.want)
		}
		if c.want == nil && got.UTC().Format(time.RFC3339) != c.utc {
			t.Fatalf("%s: got %s, want %s", c.name, got.UTC().Format(time.RFC3339), c.utc)
		}
	}
}

func TestIssue3927_PostRefusesAClosingTimeThatHasPassed(t *testing.T) {
	now := time.Now()
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", HistoryVisibility: FullHistory}}
	s := &Chatcmd002Service{Chat: chatcmd002TestService(f, func() time.Time { return now }), Repository: chatcmd002FixtureRepository{}}
	d, _ := Chatcmd003ParsePoll(`"Q?" 1="A" 2="B"`, now, nil)
	past := now.Add(-time.Minute)
	d.Card.Poll.ClosesAt = &past
	r := Chatcmd002PostRequest{Accepted: true, SendPostRequest: SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", IdempotencyKey: "late"}, Card: d.Card}
	if _, err := s.Post(t.Context(), r); !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("a past closing time posted: %v writes %d", err, f.mutations)
	}
	future := now.Add(time.Hour)
	d.Card.Poll.ClosesAt = &future
	r.Card, r.IdempotencyKey = d.Card, "ok"
	if _, err := s.Post(t.Context(), r); err != nil {
		t.Fatal(err)
	}
}

func TestIssue3927_ResolveDateTime_SouthernAndHalfHourZones(t *testing.T) {
	cases := []struct {
		zone, date, clock string
		want              error
	}{
		{"Australia/Sydney", "2027-10-03", "02:30", ErrDeadlineNotExists},
		{"Australia/Sydney", "2027-04-04", "02:30", ErrDeadlineAmbiguous},
		{"Australia/Sydney", "2027-10-03", "03:00", nil},
		{"Australia/Lord_Howe", "2027-10-03", "02:15", ErrDeadlineNotExists},
		{"Australia/Lord_Howe", "2027-04-04", "01:45", ErrDeadlineAmbiguous},
		{"Europe/Berlin", "2027-03-28", "02:30", ErrDeadlineNotExists},
		{"Europe/Berlin", "2027-10-31", "02:30", ErrDeadlineAmbiguous},
		{"Europe/Berlin", "2027-10-31", "03:00", nil},
		{"Asia/Kolkata", "2027-06-01", "12:00", nil},
	}
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range cases {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Skip("no tz database")
		}
		got, err := Chatcmd004ResolveDateTime(c.date, c.clock, loc, now)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s %s %s: err %v, want %v", c.zone, c.date, c.clock, err, c.want)
		}
		if c.want == nil && got.In(loc).Format("2006-01-02 15:04") != c.date+" "+c.clock {
			t.Fatalf("%s: resolved to %v", c.zone, got)
		}
	}
	// No zone given reads the time as UTC rather than failing.
	if got, err := Chatcmd004ResolveDateTime("2027-06-01", "12:00", nil, now); err != nil || got.UTC().Hour() != 12 {
		t.Fatalf("nil zone: %v %v", got, err)
	}
}

func TestIssue3927_PostChecksWhoMayPostBeforeTheClosingTime_Security(t *testing.T) {
	now := time.Now()
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", HistoryVisibility: FullHistory}}
	s := &Chatcmd002Service{Chat: chatcmd002TestService(f, func() time.Time { return now }), Repository: chatcmd002FixtureRepository{}}
	d, _ := Chatcmd003ParsePoll(`"Q?" 1="A" 2="B"`, now, nil)
	past := now.Add(-time.Minute)
	d.Card.Poll.ClosesAt = &past
	// A caller from another tenant is refused as a caller, not told about the
	// closing time, and nothing is written.
	r := Chatcmd002PostRequest{Accepted: true, SendPostRequest: SendPostRequest{Principal: Principal{TenantID: "other", SubjectID: "stranger"}, TenantID: "t", ConversationID: "room", IdempotencyKey: "x"}, Card: d.Card}
	if _, err := s.Post(t.Context(), r); err == nil || errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("another tenant was not refused as a caller: %v writes %d", err, f.mutations)
	}
}
