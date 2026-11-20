package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3794Offer(key string) WorkHandoff {
	return WorkHandoff{TenantID: "tenant-a", ID: "h-" + key, ConversationID: "c-1", SourcePostID: "p-1", ProjectID: "ops", TaskID: "t-1",
		Mode: HandoffFollowUp, FromID: "u-1", ToID: "u-2", NextAction: "Confirm the payroll correction", Note: "Needed before Friday",
		OwnerAtOffer: "u-1", DueAtOffer: "2026-12-04", TaskRevision: 7, IdempotencyKey: key, OfferedAt: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)}
}

func TestIssue3794_OfferIsKeptOncePerRetryAndPerOpenOffer_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	first, created, err := s.OfferWorkHandoff(ctx, issue3794Offer("attempt-0001"))
	if err != nil || !created || first.Status != HandoffOffered || first.ID != "h-attempt-0001" {
		t.Fatalf("first = %+v, %v, %v", first, created, err)
	}
	// The same attempt again recovers the same offer.
	retry := issue3794Offer("attempt-0001")
	retry.ID = "h-other-id"
	again, created, err := s.OfferWorkHandoff(ctx, retry)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("retry = %+v, %v, %v", again, created, err)
	}
	// The same key for different work is a conflict, never a silent second offer.
	clash := issue3794Offer("attempt-0001")
	clash.TaskID = "t-2"
	if _, _, err = s.OfferWorkHandoff(ctx, clash); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("clash = %v", err)
	}
	// A new attempt for the same work, recipient and kind while one is open
	// recovers the open offer.
	second, created, err := s.OfferWorkHandoff(ctx, issue3794Offer("attempt-0002"))
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("second attempt = %+v, %v, %v", second, created, err)
	}
	// Another recipient is another offer.
	other := issue3794Offer("attempt-0003")
	other.ToID = "u-3"
	got, created, err := s.OfferWorkHandoff(ctx, other)
	if err != nil || !created || got.ToID != "u-3" {
		t.Fatalf("other recipient = %+v, %v, %v", got, created, err)
	}
	list, err := s.ListWorkHandoffs(ctx, "tenant-a", "c-1", "u-1", 50)
	if err != nil || len(list) != 2 {
		t.Fatalf("sender list = %+v, %v", list, err)
	}
	list, err = s.ListWorkHandoffs(ctx, "tenant-a", "c-1", "u-2", 50)
	if err != nil || len(list) != 1 || list[0].ToID != "u-2" || list[0].Note != "Needed before Friday" {
		t.Fatalf("recipient list = %+v, %v", list, err)
	}
	if list, err = s.ListWorkHandoffs(ctx, "tenant-a", "c-1", "u-4", 50); err != nil || len(list) != 0 {
		t.Fatalf("a bystander sees offers: %+v, %v", list, err)
	}
}

func TestIssue3794_DecisionIsMadeOnceAndKeepsWhatWasOffered_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	offer, _, err := s.OfferWorkHandoff(ctx, issue3794Offer("attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	done, changed, err := s.DecideWorkHandoff(ctx, "tenant-a", offer.ID, HandoffAccepted, "u-2", at)
	if err != nil || !changed || done.Status != HandoffAccepted || done.DecidedBy != "u-2" || !done.DecidedAt.Equal(at) {
		t.Fatalf("accept = %+v, %v, %v", done, changed, err)
	}
	// A later, different decision changes nothing and reports the standing one.
	late, changed, err := s.DecideWorkHandoff(ctx, "tenant-a", offer.ID, HandoffDeclined, "u-2", at.Add(time.Hour))
	if err != nil || changed || late.Status != HandoffAccepted {
		t.Fatalf("late decline = %+v, %v, %v", late, changed, err)
	}
	// The offer text and its original owner and deadline are never rewritten.
	err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE chat_work_handoff SET note='rewritten' WHERE tenant_id='tenant-a' AND id=$1`, offer.ID)
		return e
	})
	if err == nil {
		t.Fatal("a decided offer was rewritten")
	}
	// A decided offer no longer blocks a fresh one for the same work.
	next, created, err := s.OfferWorkHandoff(ctx, issue3794Offer("attempt-0009"))
	if err != nil || !created || next.ID == offer.ID {
		t.Fatalf("fresh offer = %+v, %v, %v", next, created, err)
	}
	if _, _, err = s.DecideWorkHandoff(ctx, "tenant-a", next.ID, "BOGUS", "u-1", at); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("bogus status = %v", err)
	}
	withdrawn, changed, err := s.DecideWorkHandoff(ctx, "tenant-a", next.ID, HandoffWithdrawn, "u-1", at)
	if err != nil || !changed || withdrawn.Status != HandoffWithdrawn {
		t.Fatalf("withdraw = %+v, %v, %v", withdrawn, changed, err)
	}
}

func TestIssue3794_HandoffsStayInTheirTenant_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	offer, _, err := s.OfferWorkHandoff(ctx, issue3794Offer("attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.GetWorkHandoff(ctx, "tenant-b", offer.ID); err != nil || found {
		t.Fatalf("other tenant reads the offer: %v %v", found, err)
	}
	if list, err := s.ListWorkHandoffs(ctx, "tenant-b", "c-1", "u-2", 50); err != nil || len(list) != 0 {
		t.Fatalf("other tenant lists: %v %v", list, err)
	}
	if _, changed, err := s.DecideWorkHandoff(ctx, "tenant-b", offer.ID, HandoffAccepted, "u-2", time.Now()); err == nil && changed {
		t.Fatal("other tenant decided the offer")
	}
	foreign := issue3794Offer("attempt-0002")
	foreign.TenantID = "tenant-b"
	if _, _, err := s.OfferWorkHandoff(ctx, foreign); err == nil {
		t.Fatal("offered inside another tenant's conversation")
	}
	for _, bad := range []WorkHandoff{
		func() WorkHandoff { h := issue3794Offer("a1"); h.ToID = h.FromID; return h }(),
		func() WorkHandoff { h := issue3794Offer("a2"); h.NextAction = " "; return h }(),
		func() WorkHandoff { h := issue3794Offer("a3"); h.Mode = "OTHER"; return h }(),
		func() WorkHandoff { h := issue3794Offer("a4"); h.TaskID = ""; return h }(),
	} {
		if _, _, err := s.OfferWorkHandoff(ctx, bad); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}
