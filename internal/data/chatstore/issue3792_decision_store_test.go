package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3792Decision(id, text string, at time.Time) chat.Decision {
	return chat.Decision{ID: id, TenantID: "tenant-a", ConversationID: "c-1", Text: text, Rationale: "because", OwnerHomeTenantID: "tenant-a", OwnerID: "u-1", DecidedAt: at, SourcePostID: "p-1", SourceSequence: 7, Informal: true}
}

func TestIssue3792_DecisionsStoreRecordListAndReplace_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	first, err := s.InsertDecision(ctx, issue3792Decision("d-1", "Freeze promotions", at))
	if err != nil || first.Text != "Freeze promotions" || first.Rationale != "because" || first.SourceSequence != 7 || !first.Informal || !first.DecidedAt.Equal(at) || first.SupersededByID != "" {
		t.Fatalf("insert %+v %v", first, err)
	}
	again, err := s.InsertDecision(ctx, issue3792Decision("d-1", "A different text", at.Add(time.Hour)))
	if err != nil || again.Text != "Freeze promotions" {
		t.Fatalf("replay changed the record: %+v %v", again, err)
	}
	next := issue3792Decision("d-2", "Freeze promotions until June", at.Add(time.Hour))
	next.SupersedesID = "d-1"
	if _, err = s.InsertDecision(ctx, next); err != nil {
		t.Fatal(err)
	}
	old, err := s.GetDecision(ctx, "tenant-a", "c-1", "d-1")
	if err != nil || old.SupersededByID != "d-2" {
		t.Fatalf("old %+v %v", old, err)
	}
	third := issue3792Decision("d-3", "Third", at.Add(2*time.Hour))
	third.SupersedesID = "d-1"
	if _, err = s.InsertDecision(ctx, third); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("replace twice: %v", err)
	}
	if _, err = s.GetDecision(ctx, "tenant-a", "c-1", "d-3"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("refused replacement was stored: %v", err)
	}
	list, err := s.ListDecisions(ctx, "tenant-a", "c-1", nil, 10)
	if err != nil || len(list) != 2 || list[0].ID != "d-2" || list[1].ID != "d-1" {
		t.Fatalf("list %+v %v", list, err)
	}
	since := at.Add(30 * time.Minute)
	list, err = s.ListDecisions(ctx, "tenant-a", "c-1", &since, 10)
	if err != nil || len(list) != 1 || list[0].ID != "d-2" {
		t.Fatalf("since %+v %v", list, err)
	}
}

func TestIssue3792_DecisionsStayInTheirConversationAndTenant_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	seedConversationRow(t, s, Conversation{ID: "c-2", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	seedConversationRow(t, s, Conversation{ID: "c-b", TenantID: "tenant-b", Kind: "PUBLIC_CHANNEL", OwnerID: "u-9", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	if _, err := s.InsertDecision(ctx, issue3792Decision("d-1", "Only for c-1", at)); err != nil {
		t.Fatal(err)
	}
	other := issue3792Decision("d-x", "Tries to replace across rooms", at.Add(time.Hour))
	other.ConversationID, other.SupersedesID = "c-2", "d-1"
	if _, err := s.InsertDecision(ctx, other); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("cross conversation replace: %v", err)
	}
	if old, _ := s.GetDecision(ctx, "tenant-a", "c-1", "d-1"); old.SupersededByID != "" {
		t.Fatalf("decision of another room was replaced: %+v", old)
	}
	if list, err := s.ListDecisions(ctx, "tenant-a", "c-2", nil, 10); err != nil || len(list) != 0 {
		t.Fatalf("c-2 list %+v %v", list, err)
	}
	if list, err := s.ListDecisions(ctx, "tenant-b", "c-1", nil, 10); err != nil || len(list) != 0 {
		t.Fatalf("tenant-b list %+v %v", list, err)
	}
	if _, err := s.GetDecision(ctx, "tenant-b", "c-1", "d-1"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("tenant-b read: %v", err)
	}
	foreign := issue3792Decision("d-1", "forged", at)
	foreign.TenantID, foreign.ConversationID = "tenant-b", "c-b"
	if stored, err := s.InsertDecision(ctx, foreign); err != nil || stored.Text != "forged" {
		t.Fatalf("tenant-b own record: %+v %v", stored, err)
	}
	if a, _ := s.GetDecision(ctx, "tenant-a", "c-1", "d-1"); a.Text != "Only for c-1" {
		t.Fatalf("tenant-a record overwritten: %+v", a)
	}
}

func TestIssue3792_ARecordedDecisionIsNotEdited_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	if _, err := s.InsertDecision(ctx, issue3792Decision("d-1", "Freeze promotions", at)); err != nil {
		t.Fatal(err)
	}
	err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_decision SET decision='rewritten' WHERE tenant_id='tenant-a' AND id='d-1'`)
		return err
	})
	if err == nil {
		t.Fatal("a recorded decision was rewritten")
	}
	if d, _ := s.GetDecision(ctx, "tenant-a", "c-1", "d-1"); d.Text != "Freeze promotions" {
		t.Fatalf("text changed: %+v", d)
	}
	bad := issue3792Decision("d-2", "   ", at)
	if _, err := s.InsertDecision(ctx, bad); err == nil {
		t.Fatal("an empty decision was stored")
	}
	informal := issue3792Decision("d-3", "Not informal", at)
	informal.Informal = false
	if stored, err := s.InsertDecision(ctx, informal); err != nil || !stored.Informal {
		t.Fatalf("informal flag: %+v %v", stored, err)
	}
}
