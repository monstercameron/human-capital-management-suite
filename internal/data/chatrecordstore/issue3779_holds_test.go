package chatrecordstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
)

func issue3779Place(tenant, id, conversation string, at time.Time) (chatrecords.Hold, chatrecords.AuditEvent) {
	return chatrecords.Hold{TenantID: tenant, HoldID: id, MatterRef: "Matter 7", Reason: "claim", PlacedBy: "counsel", PlacedAt: at, ConversationID: conversation},
		chatrecords.AuditEvent{TenantID: tenant, ActorID: "counsel", Action: chatrecords.ActionHoldPlace, TargetType: "chat_hold", TargetID: id, Reason: "claim", PolicyEvidence: "chat:tenant-admin:" + tenant, At: at}
}

func TestIssue3779_Integration_PlaceAndReleaseHoldWithAudit(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	hold, event := issue3779Place("tenant-a", "h-1", "conv-1", at)
	placed, err := s.PutHoldAudited(ctx, hold, event)
	if err != nil || placed.Sequence != 1 || placed.Digest == "" || placed.EventID != "hold:h-1:1" {
		t.Fatalf("placed=%+v err=%v", placed, err)
	}
	if _, err = s.PutHoldAudited(ctx, hold, event); !errors.Is(err, chatrecords.ErrConflict) {
		t.Fatalf("the same hold twice: %v", err)
	}
	holds, err := s.Holds(ctx, "tenant-a")
	if err != nil || len(holds) != 1 || holds[0].ConversationID != "conv-1" || !holds[0].Active() || holds[0].MatterRef != "Matter 7" {
		t.Fatalf("holds=%+v err=%v", holds, err)
	}
	// The audit log finds the hold by conversation and by actor.
	page, err := s.ListAudit(ctx, "tenant-a", chatrecords.AuditQuery{AuditFilter: chatrecords.AuditFilter{ConversationID: "conv-1"}, Limit: 10})
	if err != nil || len(page) != 1 || page[0].Action != "hold.place" || page[0].Digest != placed.Digest {
		t.Fatalf("audit=%+v err=%v", page, err)
	}

	releasedAt := at.Add(time.Hour)
	releaseEvent := chatrecords.AuditEvent{TenantID: "tenant-a", ActorID: "other", Action: chatrecords.ActionHoldRelease, TargetType: "chat_hold", TargetID: "h-1", Reason: "settled", PolicyEvidence: "chat:tenant-admin:tenant-a", At: releasedAt}
	released, ev, err := s.ReleaseHoldAudited(ctx, "tenant-a", "h-1", "other", "settled", releasedAt, releaseEvent)
	if err != nil || released.Active() || released.ReleasedBy != "other" || released.ReleaseReason != "settled" || ev.Sequence != 2 {
		t.Fatalf("released=%+v ev=%+v err=%v", released, ev, err)
	}
	if _, _, err = s.ReleaseHoldAudited(ctx, "tenant-a", "h-1", "other", "again", releasedAt, releaseEvent); !errors.Is(err, chatrecords.ErrConflict) {
		t.Fatalf("second release: %v", err)
	}
	holds, _ = s.Holds(ctx, "tenant-a")
	if len(holds) != 1 || holds[0].Active() || holds[0].ReleasedBy != "other" || holds[0].ReleaseReason != "settled" || holds[0].PlacedBy != "counsel" {
		t.Fatalf("a released hold stays as history: %+v", holds)
	}
	events, _ := s.Events(ctx, "tenant-a")
	if len(events) != 2 || events[1].Action != "hold.release" {
		t.Fatalf("events=%+v", events)
	}
	if r, err := s.Reconcile(ctx, "tenant-a"); err != nil || !r.Ready {
		t.Fatalf("reconcile=%+v err=%v", r, err)
	}
}

func TestIssue3779_Integration_HoldsDoNotCrossTenants_Security(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	hold, event := issue3779Place("tenant-a", "h-1", "conv-1", at)
	if _, err := s.PutHoldAudited(ctx, hold, event); err != nil {
		t.Fatal(err)
	}
	if holds, err := s.Holds(ctx, "tenant-b"); err != nil || len(holds) != 0 {
		t.Fatalf("tenant-b sees %+v err=%v", holds, err)
	}
	foreign := chatrecords.AuditEvent{TenantID: "tenant-b", ActorID: "evil", Action: chatrecords.ActionHoldRelease, TargetType: "chat_hold", TargetID: "h-1", Reason: "no", PolicyEvidence: "x", At: at}
	if _, _, err := s.ReleaseHoldAudited(ctx, "tenant-b", "h-1", "evil", "no", at, foreign); !errors.Is(err, chatrecords.ErrHoldNotFound) {
		t.Fatalf("another tenant released it: %v", err)
	}
	if holds, _ := s.Holds(ctx, "tenant-a"); len(holds) != 1 || !holds[0].Active() {
		t.Fatalf("the hold must be untouched: %+v", holds)
	}
	// The same hold id in another tenant is its own hold.
	other, otherEvent := issue3779Place("tenant-b", "h-1", "conv-9", at)
	if _, err := s.PutHoldAudited(ctx, other, otherEvent); err != nil {
		t.Fatalf("same id in another tenant: %v", err)
	}
	// A mismatched event is refused before anything is written.
	bad := event
	bad.TenantID = "tenant-b"
	hold2, _ := issue3779Place("tenant-a", "h-2", "conv-2", at)
	if _, err := s.PutHoldAudited(ctx, hold2, bad); !errors.Is(err, chatrecords.ErrInvalid) {
		t.Fatalf("mismatched event: %v", err)
	}
	if holds, _ := s.Holds(ctx, "tenant-a"); len(holds) != 1 {
		t.Fatalf("a refused hold must not be stored: %+v", holds)
	}
}
