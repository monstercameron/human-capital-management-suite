package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3788Post(author string, at time.Time) chatrecipient.ScheduledPost {
	return chatrecipient.ScheduledPost{HostTenantID: "host", HomeTenantID: "host", AuthorID: author, OrgScopeID: "org", ConversationID: "c-1", Body: "Training is tomorrow", SendAt: at, Zone: "UTC"}
}

func TestIssue3788_ScheduledPost_StoreLifecycle_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	store := NewRecipientStateStore(s)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	saved, err := store.InsertScheduled(ctx, issue3788Post("u-1", now.Add(time.Hour)), 50)
	if err != nil || saved.ID == "" || saved.State != chatrecipient.ScheduledPending || saved.Revision != 1 {
		t.Fatalf("insert = %+v err=%v", saved, err)
	}

	// Another person of the same workspace sees nothing and can change nothing.
	if rows, err := store.ListScheduled(ctx, "host", "host", "u-2"); err != nil || len(rows) != 0 {
		t.Fatalf("another person listed %+v err=%v", rows, err)
	}
	if _, err = store.UpdateScheduled(ctx, "host", "host", "u-2", saved.ID, "hijacked", now.Add(2*time.Hour), "UTC", 1); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("another person edited the row: %v", err)
	}
	if err = store.DeleteScheduled(ctx, "host", "host", "u-2", saved.ID); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("another person cancelled the row: %v", err)
	}
	// Another workspace sees nothing either.
	if rows, err := store.ListScheduled(ctx, "other", "host", "u-1"); err != nil || len(rows) != 0 {
		t.Fatalf("another workspace listed %+v err=%v", rows, err)
	}

	// The author edits: the revision moves and a stale revision is a conflict.
	edited, err := store.UpdateScheduled(ctx, "host", "host", "u-1", saved.ID, "Training is at nine", now.Add(3*time.Hour), "UTC", 1)
	if err != nil || edited.Body != "Training is at nine" || edited.Revision != 2 {
		t.Fatalf("edit = %+v err=%v", edited, err)
	}
	if _, err = store.UpdateScheduled(ctx, "host", "host", "u-1", saved.ID, "stale", now.Add(3*time.Hour), "UTC", 1); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("a stale edit: %v", err)
	}

	// Nothing is due yet.
	if due, err := store.DueScheduled(ctx, "host", now, 10); err != nil || len(due) != 0 {
		t.Fatalf("due early: %+v err=%v", due, err)
	}
	later := now.Add(4 * time.Hour)
	due, err := store.DueScheduled(ctx, "host", later, 10)
	if err != nil || len(due) != 1 || due[0].ID != saved.ID || due[0].OrgScopeID != "org" {
		t.Fatalf("due = %+v err=%v", due, err)
	}

	// Only one worker can claim it, and the claim carries the edited text.
	claimed, ok, err := store.ClaimScheduled(ctx, "host", saved.ID, later, time.Minute)
	if err != nil || !ok || claimed.Body != "Training is at nine" || claimed.Attempts != 1 {
		t.Fatalf("claim = %+v ok=%v err=%v", claimed, ok, err)
	}
	if _, ok, err = store.ClaimScheduled(ctx, "host", saved.ID, later, time.Minute); err != nil || ok {
		t.Fatalf("a second claim succeeded: ok=%v err=%v", ok, err)
	}
	// A row being sent cannot be edited or cancelled.
	if _, err = store.UpdateScheduled(ctx, "host", "host", "u-1", saved.ID, "late", later.Add(time.Hour), "UTC", 2); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("edit while sending: %v", err)
	}
	if err = store.DeleteScheduled(ctx, "host", "host", "u-1", saved.ID); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("cancel while sending: %v", err)
	}
	// A claim that ran out is taken over.
	if _, ok, err = store.ClaimScheduled(ctx, "host", saved.ID, later.Add(2*time.Minute), time.Minute); err != nil || !ok {
		t.Fatalf("the expired claim was not taken over: ok=%v err=%v", ok, err)
	}

	// A refused row stays for its author with the reason, then can be rescheduled.
	if err = store.FailScheduled(ctx, "host", saved.ID, chatrecipient.ScheduledRefusedAccess); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListScheduled(ctx, "host", "host", "u-1")
	if err != nil || len(rows) != 1 || rows[0].State != chatrecipient.ScheduledFailed || rows[0].Refusal != chatrecipient.ScheduledRefusedAccess {
		t.Fatalf("failed row = %+v err=%v", rows, err)
	}
	again, err := store.UpdateScheduled(ctx, "host", "host", "u-1", saved.ID, "Training is at nine", later.Add(time.Hour), "UTC", rows[0].Revision)
	if err != nil || again.State != chatrecipient.ScheduledPending || again.Refusal != "" || again.Attempts != 0 {
		t.Fatalf("reschedule = %+v err=%v", again, err)
	}

	// Sending it deletes the row and its text.
	if _, ok, err = store.ClaimScheduled(ctx, "host", saved.ID, later.Add(2*time.Hour), time.Minute); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err = store.FinishScheduled(ctx, "host", saved.ID); err != nil {
		t.Fatal(err)
	}
	if rows, _ = store.ListScheduled(ctx, "host", "host", "u-1"); len(rows) != 0 {
		t.Fatalf("a sent row is still listed: %+v", rows)
	}
}

func TestIssue3788_ScheduledPost_CancelRemovesTheText_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	store := NewRecipientStateStore(s)
	ctx := context.Background()
	saved, err := store.InsertScheduled(ctx, issue3788Post("u-1", time.Now().Add(time.Hour)), 50)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteScheduled(ctx, "host", "host", "u-1", saved.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	err = s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_scheduled_post WHERE id=$1`, saved.ID).Scan(&left)
	})
	if err != nil || left != 0 {
		t.Fatalf("rows left after cancel: %d err=%v", left, err)
	}
	if err = store.DeleteScheduled(ctx, "host", "host", "u-1", saved.ID); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("a second cancel: %v", err)
	}
}

func TestIssue3788_ScheduledPost_PerPersonLimit_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	store := NewRecipientStateStore(s)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := store.InsertScheduled(ctx, issue3788Post("u-1", time.Now().Add(time.Hour)), 2); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.InsertScheduled(ctx, issue3788Post("u-1", time.Now().Add(time.Hour)), 2); !errors.Is(err, chatrecipient.ErrScheduledLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	// The limit is the person's own.
	if _, err := store.InsertScheduled(ctx, issue3788Post("u-2", time.Now().Add(time.Hour)), 2); err != nil {
		t.Fatalf("another person was held to the first person's count: %v", err)
	}
}

func TestIssue3788_ScheduledPost_ReleaseFailsAfterTheLastAttempt_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	store := NewRecipientStateStore(s)
	ctx := context.Background()
	now := time.Now().UTC()
	saved, err := store.InsertScheduled(ctx, issue3788Post("u-1", now.Add(time.Minute)), 50)
	if err != nil {
		t.Fatal(err)
	}
	at := now.Add(time.Hour)
	for attempt := 1; attempt <= 2; attempt++ {
		if _, ok, err := store.ClaimScheduled(ctx, "host", saved.ID, at, time.Minute); err != nil || !ok {
			t.Fatalf("claim %d: ok=%v err=%v", attempt, ok, err)
		}
		if err = store.ReleaseScheduled(ctx, "host", saved.ID, 2); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := store.ListScheduled(ctx, "host", "host", "u-1")
	if len(rows) != 1 || rows[0].State != chatrecipient.ScheduledFailed || rows[0].Refusal != chatrecipient.ScheduledRefusedUnavailable {
		t.Fatalf("after the last attempt: %+v", rows)
	}
}
