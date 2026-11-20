package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type issue3792Store struct {
	rows  []Decision
	calls int
}

func (s *issue3792Store) InsertDecision(_ context.Context, d Decision) (Decision, error) {
	s.calls++
	for _, row := range s.rows {
		if row.ID == d.ID {
			return row, nil
		}
	}
	if d.SupersedesID != "" {
		found := false
		for i := range s.rows {
			if s.rows[i].ID == d.SupersedesID && s.rows[i].ConversationID == d.ConversationID && s.rows[i].TenantID == d.TenantID {
				if s.rows[i].SupersededByID != "" {
					return Decision{}, ErrConflict
				}
				s.rows[i].SupersededByID = d.ID
				found = true
			}
		}
		if !found {
			return Decision{}, ErrNotFound
		}
	}
	s.rows = append(s.rows, d)
	return d, nil
}

func (s *issue3792Store) GetDecision(_ context.Context, tenant, conversation, id string) (Decision, error) {
	for _, row := range s.rows {
		if row.ID == id && row.TenantID == tenant && row.ConversationID == conversation {
			return row, nil
		}
	}
	return Decision{}, ErrNotFound
}

func (s *issue3792Store) ListDecisions(_ context.Context, tenant, conversation string, since *time.Time, _ int) ([]Decision, error) {
	var out []Decision
	for i := len(s.rows) - 1; i >= 0; i-- {
		row := s.rows[i]
		if row.TenantID == tenant && row.ConversationID == conversation && (since == nil || !row.DecidedAt.Before(*since)) {
			out = append(out, row)
		}
	}
	return out, nil
}

func issue3792Service(t *testing.T, role MembershipRole, history ReadHistoryFrom) (*DecisionService, *fakeStore, *issue3792Store) {
	t.Helper()
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", Role: role, HistoryVisibility: history}}
	store := &issue3792Store{}
	return &DecisionService{Chat: chatcmd002TestService(f, func() time.Time { return now }), Store: store}, f, store
}

func issue3792Request(key, text string) DecisionRecordRequest {
	return DecisionRecordRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", Text: text, IdempotencyKey: key, Label: "Decision (informal)"}
}

func TestIssue3792_RecordPostsALineAndKeepsTheRecord(t *testing.T) {
	s, f, store := issue3792Service(t, "", FullHistory)
	r := issue3792Request("k1", "  We freeze promotion changes\n until after calibration ")
	r.Rationale = "Calibration needs one baseline."
	d, err := s.Record(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if d.Text != "We freeze promotion changes until after calibration" || d.Rationale != "Calibration needs one baseline." || d.OwnerID != "writer" || d.OwnerHomeTenantID != "t" || !d.Informal || d.ID == "" || d.DecidedAt.IsZero() {
		t.Fatalf("record %+v", d)
	}
	if want := "Decision (informal): We freeze promotion changes until after calibration"; f.sent.Body != want || f.mutations != 1 {
		t.Fatalf("posted %q writes %d", f.sent.Body, f.mutations)
	}
	if len(store.rows) != 1 || store.rows[0].SourcePostID != d.SourcePostID {
		t.Fatalf("stored %+v", store.rows)
	}
}

func TestIssue3792_RetryRecordsOnce(t *testing.T) {
	s, f, store := issue3792Service(t, "", FullHistory)
	first, err := s.Record(context.Background(), issue3792Request("same", "Ship on Friday"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Record(context.Background(), issue3792Request("same", "Ship on Friday"))
	if err != nil || again.ID != first.ID || len(store.rows) != 1 || f.mutations != 1 {
		t.Fatalf("retry %+v %v rows %d writes %d", again, err, len(store.rows), f.mutations)
	}
}

func TestIssue3792_Refusals_Security(t *testing.T) {
	s, f, store := issue3792Service(t, "", FullHistory)
	ctx := context.Background()
	outsider := issue3792Request("k", "Hire two")
	outsider.Principal = Principal{TenantID: "t", SubjectID: "stranger"}
	if _, err := s.Record(ctx, outsider); !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 || len(store.rows) != 0 {
		t.Fatalf("outsider recorded: %v writes %d rows %d", err, f.mutations, len(store.rows))
	}
	otherTenant := issue3792Request("k", "Hire two")
	otherTenant.Principal = Principal{TenantID: "other", SubjectID: "writer"}
	if _, err := s.Record(ctx, otherTenant); !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("other tenant recorded: %v", err)
	}
	if _, err := s.List(ctx, Principal{TenantID: "t", SubjectID: "stranger"}, "t", "room"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("outsider listed: %v", err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", DecisionTextMax+1), "a\x00b"} {
		if _, err := s.Record(ctx, issue3792Request("k", bad)); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("text %q: %v", bad, err)
		}
	}
	noKey := issue3792Request("", "Fine")
	if _, err := s.Record(ctx, noKey); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("no key: %v", err)
	}
	long := issue3792Request("k", "Fine")
	long.Rationale = strings.Repeat("y", DecisionRationaleMax+1)
	if _, err := s.Record(ctx, long); !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("long rationale: %v", err)
	}
}

func TestIssue3792_SourceMessageMustBeReadable(t *testing.T) {
	s, f, store := issue3792Service(t, "", FullHistory)
	r := issue3792Request("k", "Use the second vendor")
	r.SourcePostID = "missing"
	if _, err := s.Record(context.Background(), r); !errors.Is(err, ErrNotFound) || f.mutations != 0 || len(store.rows) != 0 {
		t.Fatalf("missing source: %v", err)
	}
	f.post = Post{ID: "p1", Sequence: 42, ConversationID: "room", TenantID: "t", Body: "we agreed", CreatedAt: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)}
	r.SourcePostID = "p1"
	d, err := s.Record(context.Background(), r)
	if err != nil || d.SourcePostID != "p1" || d.SourceSequence != 42 || f.mutations != 0 {
		t.Fatalf("marked message: %+v %v writes %d", d, err, f.mutations)
	}
}

func TestIssue3792_ReplaceKeepsHistory(t *testing.T) {
	s, _, store := issue3792Service(t, "", FullHistory)
	ctx := context.Background()
	old, err := s.Record(ctx, issue3792Request("old", "Freeze promotions"))
	if err != nil {
		t.Fatal(err)
	}
	next := issue3792Request("new", "Freeze promotions until June")
	next.SupersedesID = old.ID
	got, err := s.Record(ctx, next)
	if err != nil || got.SupersedesID != old.ID {
		t.Fatalf("replace %+v %v", got, err)
	}
	if store.rows[0].SupersededByID != got.ID {
		t.Fatalf("old not marked: %+v", store.rows[0])
	}
	again := issue3792Request("third", "Something else")
	again.SupersedesID = old.ID
	if _, err := s.Record(ctx, again); !errors.Is(err, ErrConflict) {
		t.Fatalf("replace twice: %v", err)
	}
	ghost := issue3792Request("ghost", "Nope")
	ghost.SupersedesID = "no-such-decision"
	if _, err := s.Record(ctx, ghost); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replace unknown: %v", err)
	}
	store.rows = append(store.rows, Decision{ID: "elsewhere", TenantID: "t", ConversationID: "other-room", Text: "x", OwnerID: "writer", OwnerHomeTenantID: "t"})
	cross := issue3792Request("cross", "Nope")
	cross.SupersedesID = "elsewhere"
	if _, err := s.Record(ctx, cross); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross conversation replace: %v", err)
	}
}

func TestIssue3792_OnlyTheOwnerOrAManagerReplaces_Security(t *testing.T) {
	ctx := context.Background()
	s, f, store := issue3792Service(t, "", FullHistory)
	store.rows = append(store.rows, Decision{ID: "theirs", TenantID: "t", ConversationID: "room", Text: "Their call", OwnerID: "someone", OwnerHomeTenantID: "t", Informal: true})
	r := issue3792Request("k", "My version")
	r.SupersedesID = "theirs"
	if _, err := s.Record(ctx, r); !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 || store.rows[0].SupersededByID != "" {
		t.Fatalf("member replaced a decision that is not theirs: %v", err)
	}
	list, err := s.List(ctx, Principal{TenantID: "t", SubjectID: "writer"}, "t", "room")
	if err != nil || len(list.Decisions) != 1 || list.Decisions[0].CanReplace {
		t.Fatalf("list %+v %v", list, err)
	}
	manager, _, mstore := issue3792Service(t, Manager, FullHistory)
	mstore.rows = append(mstore.rows, Decision{ID: "theirs", TenantID: "t", ConversationID: "room", Text: "Their call", OwnerID: "someone", OwnerHomeTenantID: "t", Informal: true})
	if _, err := manager.Record(ctx, r); err != nil {
		t.Fatalf("manager: %v", err)
	}
	foreign := Decision{ID: "foreign-owned", TenantID: "t", ConversationID: "room", Text: "x", OwnerID: "writer", OwnerHomeTenantID: "other-company", Informal: true}
	if decisionCanReplace(foreign, Principal{TenantID: "t", SubjectID: "writer"}, Membership{}) {
		t.Fatal("owner match ignored the company")
	}
}

func TestIssue3792_ListFollowsHistoryVisibility(t *testing.T) {
	ctx := context.Background()
	joined := time.Date(2026, 11, 19, 9, 30, 0, 0, time.UTC)
	s, f, store := issue3792Service(t, "", FromJoin)
	f.membership.JoinedAt = &joined
	store.rows = []Decision{
		{ID: "before", TenantID: "t", ConversationID: "room", Text: "Before I joined", DecidedAt: joined.Add(-time.Hour)},
		{ID: "after", TenantID: "t", ConversationID: "room", Text: "After I joined", DecidedAt: joined.Add(time.Hour)},
	}
	list, err := s.List(ctx, Principal{TenantID: "t", SubjectID: "writer"}, "t", "room")
	if err != nil || len(list.Decisions) != 1 || list.Decisions[0].ID != "after" || !list.CanRecord {
		t.Fatalf("from join %+v %v", list, err)
	}
	none, _, nstore := issue3792Service(t, "", NoHistory)
	nstore.rows = store.rows
	list, err = none.List(ctx, Principal{TenantID: "t", SubjectID: "writer"}, "t", "room")
	if err != nil || len(list.Decisions) != 0 {
		t.Fatalf("no history %+v %v", list, err)
	}
}
