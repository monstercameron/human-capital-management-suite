package chatrecords

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func issue3779Service(auth Authorizer) (*Service, *MemoryRepository) {
	repo := NewMemoryRepository()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	return &Service{Repo: repo, Auth: auth, Clock: func() time.Time { now = now.Add(time.Minute); return now }}, repo
}

func TestIssue3779_PlaceConversationHoldIsStoredAndAudited(t *testing.T) {
	s, repo := issue3779Service(allowAuth{})
	ctx := context.Background()
	hold, err := s.PlaceConversationHold(ctx, "counsel", "t", "conv-1", "h-1", " Matter 7 ", " Preserve for the Smith claim ")
	if err != nil {
		t.Fatal(err)
	}
	if hold.ConversationID != "conv-1" || hold.PlacedBy != "counsel" || hold.Reason != "Preserve for the Smith claim" || hold.MatterRef != "Matter 7" || !hold.Active() {
		t.Fatalf("hold=%+v", hold)
	}
	listed, err := s.ListHolds(ctx, "counsel", "t")
	if err != nil || len(listed) != 1 || listed[0].HoldID != "h-1" {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	events, _ := repo.Events(ctx, "t")
	if len(events) != 1 || events[0].Action != "hold.place" || events[0].TargetID != "h-1" || events[0].ActorID != "counsel" || events[0].Digest == "" || events[0].Digest != DigestEvent(events[0]) {
		t.Fatalf("a hold must write one audit event: %+v", events)
	}
	if rec, _ := repo.Reconcile(ctx, "t"); !rec.Ready {
		t.Fatalf("audit and outbox must agree: %+v", rec)
	}
}

func TestIssue3779_PlaceHoldValidationAndDuplicates(t *testing.T) {
	s, repo := issue3779Service(allowAuth{})
	ctx := context.Background()
	type args struct{ conversation, id, matter, reason string }
	for name, a := range map[string]args{
		"no reason":       {"conv", "h", "", ""},
		"blank reason":    {"conv", "h", "", "  "},
		"no conversation": {"", "h", "", "r"},
		"no id":           {"conv", "", "", "r"},
		"reason too long": {"conv", "h", "", strings.Repeat("x", HoldMaxReason+1)},
		"matter too long": {"conv", "h", strings.Repeat("m", HoldMaxMatter+1), "r"},
	} {
		if _, err := s.PlaceConversationHold(ctx, "counsel", "t", a.conversation, a.id, a.matter, a.reason); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err=%v, want ErrInvalid", name, err)
		}
	}
	if events, _ := repo.Events(ctx, "t"); len(events) != 0 {
		t.Fatalf("a refused hold must leave no audit event: %+v", events)
	}
	if _, err := s.PlaceConversationHold(ctx, "counsel", "t", "conv", "h", "", "reason"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlaceConversationHold(ctx, "counsel", "t", "conv", "h", "", "again"); !errors.Is(err, ErrConflict) {
		t.Fatalf("same hold id twice: err=%v", err)
	}
	if events, _ := repo.Events(ctx, "t"); len(events) != 1 {
		t.Fatalf("a conflicting hold must not add an event: %+v", events)
	}
}

func TestIssue3779_HoldActionsAreRefusedBeforeAnySideEffect_Security(t *testing.T) {
	s, repo := issue3779Service(denyAuth{})
	ctx := context.Background()
	if _, err := s.PlaceConversationHold(ctx, "intruder", "t", "conv", "h", "", "reason"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("place err=%v", err)
	}
	if _, err := s.ReleaseConversationHold(ctx, "intruder", "t", "h", "reason"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("release err=%v", err)
	}
	if _, err := s.ListHolds(ctx, "intruder", "t"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("list err=%v", err)
	}
	if holds, _ := repo.Holds(ctx, "t"); len(holds) != 0 {
		t.Fatalf("a refused place wrote a hold: %+v", holds)
	}
	if events, _ := repo.Events(ctx, "t"); len(events) != 0 {
		t.Fatalf("a refused call wrote an event: %+v", events)
	}
	none := &Service{Repo: repo}
	if _, err := none.PlaceConversationHold(ctx, "a", "t", "conv", "h", "", "reason"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("no authorizer must fail closed: %v", err)
	}
}

func TestIssue3779_ReleaseKeepsTheRecordAndAuditsIt(t *testing.T) {
	s, repo := issue3779Service(allowAuth{})
	ctx := context.Background()
	if _, err := s.PlaceConversationHold(ctx, "counsel", "t", "conv", "h-1", "", "claim"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReleaseConversationHold(ctx, "counsel", "t", "h-1", " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("release without a reason: %v", err)
	}
	if _, err := s.ReleaseConversationHold(ctx, "counsel", "t", "missing", "done"); !errors.Is(err, ErrHoldNotFound) {
		t.Fatalf("unknown hold: %v", err)
	}
	released, err := s.ReleaseConversationHold(ctx, "other-admin", "t", "h-1", "Claim settled")
	if err != nil {
		t.Fatal(err)
	}
	if released.Active() || released.ReleasedBy != "other-admin" || released.ReleaseReason != "Claim settled" || released.PlacedBy != "counsel" {
		t.Fatalf("released=%+v", released)
	}
	if _, err := s.ReleaseConversationHold(ctx, "counsel", "t", "h-1", "again"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second release: %v", err)
	}
	listed, _ := s.ListHolds(ctx, "counsel", "t")
	if len(listed) != 1 || listed[0].Active() {
		t.Fatalf("a released hold stays listed as history: %+v", listed)
	}
	events, _ := repo.Events(ctx, "t")
	if len(events) != 2 || events[1].Action != "hold.release" || events[1].ActorID != "other-admin" || events[1].Reason != "Claim settled" {
		t.Fatalf("events=%+v", events)
	}
}

func TestIssue3779_HoldsAreTenantScoped_Security(t *testing.T) {
	s, _ := issue3779Service(allowAuth{})
	ctx := context.Background()
	if _, err := s.PlaceConversationHold(ctx, "counsel", "t", "conv", "h-1", "", "claim"); err != nil {
		t.Fatal(err)
	}
	if listed, err := s.ListHolds(ctx, "counsel", "other"); err != nil || len(listed) != 0 {
		t.Fatalf("another tenant must see no holds: %+v err=%v", listed, err)
	}
	if _, err := s.ReleaseConversationHold(ctx, "counsel", "other", "h-1", "no"); !errors.Is(err, ErrHoldNotFound) {
		t.Fatalf("another tenant must not release it: %v", err)
	}
	if held, _ := s.ActiveConversationHolds(ctx, "counsel", "t", "conv"); len(held) != 1 {
		t.Fatalf("held=%+v", held)
	}
	if held, _ := s.ActiveConversationHolds(ctx, "counsel", "t", "elsewhere"); len(held) != 0 {
		t.Fatalf("held=%+v", held)
	}
}

func TestIssue3779_ConversationHoldBlocksDispositionUntilReleased(t *testing.T) {
	s, _ := issue3779Service(allowAuth{})
	ctx := context.Background()
	record := Record{TenantID: "t", ConversationID: "conv", RecordID: "post:1", Kind: KindPost, Revision: 2}
	if _, err := s.PlaceConversationHold(ctx, "counsel", "t", "conv", "h-1", "", "claim"); err != nil {
		t.Fatal(err)
	}
	if err := s.Dispose(ctx, "admin", record, "retention"); !errors.Is(err, ErrHeld) {
		t.Fatalf("a record of a held conversation must not be disposed: %v", err)
	}
	other := Record{TenantID: "t", ConversationID: "elsewhere", RecordID: "post:2", Kind: KindPost, Revision: 1}
	if err := s.Dispose(ctx, "admin", other, "retention"); err != nil {
		t.Fatalf("a record of another conversation is unaffected: %v", err)
	}
	if _, err := s.ReleaseConversationHold(ctx, "counsel", "t", "h-1", "done"); err != nil {
		t.Fatal(err)
	}
	if err := s.Dispose(ctx, "admin", record, "retention"); err != nil {
		t.Fatalf("after release the record may be disposed: %v", err)
	}
}

func TestIssue3779_RetentionPreviewSkipsAHeldConversation(t *testing.T) {
	cutoff := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -10)
	s, _, _ := issue3778Service(t, RetentionPolicy{Mode: RetentionDate, BeforeDate: cutoff}, allowAuth{})
	ctx := context.Background()
	before, err := s.PreviewRetention(ctx, "admin", "t")
	if err != nil || before.WouldRemoveTotal != 4 || before.HeldSkipped != 2 {
		t.Fatalf("baseline %+v err=%v", before, err)
	}
	// Every record of the fixture lives in conversation "c".
	if _, err = s.PlaceConversationHold(ctx, "counsel", "t", "c", "conv-hold", "", "claim"); err != nil {
		t.Fatal(err)
	}
	held, err := s.PreviewRetention(ctx, "admin", "t")
	if err != nil {
		t.Fatal(err)
	}
	if held.WouldRemoveTotal != 0 || held.HeldSkipped != before.WouldRemoveTotal+before.HeldSkipped {
		t.Fatalf("a held conversation must be listed as skipped, not removed: %+v", held)
	}
	if _, err = s.ReleaseConversationHold(ctx, "counsel", "t", "conv-hold", "done"); err != nil {
		t.Fatal(err)
	}
	after, err := s.PreviewRetention(ctx, "admin", "t")
	if err != nil || after.WouldRemoveTotal != before.WouldRemoveTotal || after.HeldSkipped != before.HeldSkipped {
		t.Fatalf("release must restore the baseline: %+v err=%v", after, err)
	}
}
