package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3795Submission(post string) FormSubmission {
	return FormSubmission{TenantID: "tenant-a", ConversationID: "c-1", PostID: post, SubmittedByID: "u-2", RequestedByID: "agent-1",
		Answers: map[string]string{"reason": "merit", "effective": "2027-02-01"}, SubmittedAt: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)}
}

func TestIssue3795_FormSubmissionIsKeptOnce_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	created, err := s.RecordFormSubmission(ctx, issue3795Submission("p-1"))
	if err != nil || !created {
		t.Fatalf("first record = %v, %v", created, err)
	}
	// A second answer to the same card changes nothing.
	second := issue3795Submission("p-1")
	second.SubmittedByID, second.Answers = "u-3", map[string]string{"reason": "other"}
	if created, err = s.RecordFormSubmission(ctx, second); err != nil || created {
		t.Fatalf("second record = %v, %v; want it refused without error", created, err)
	}
	got, found, err := s.GetFormSubmission(ctx, "tenant-a", "c-1", "p-1")
	if err != nil || !found || got.SubmittedByID != "u-2" || got.RequestedByID != "agent-1" || got.Answers["reason"] != "merit" || got.Answers["effective"] != "2027-02-01" {
		t.Fatalf("get = %+v, %v, %v", got, found, err)
	}
	if _, found, err = s.GetFormSubmission(ctx, "tenant-a", "c-1", "p-none"); err != nil || found {
		t.Fatalf("missing card: %v %v", found, err)
	}
	at, err := s.FormSubmittedAt(ctx, "tenant-a", "c-1", []string{"p-1", "p-none"})
	if err != nil || len(at) != 1 || at["p-1"].IsZero() {
		t.Fatalf("status = %v, %v", at, err)
	}
}

func TestIssue3795_FormSubmissionStaysInItsTenant_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	if _, err := s.RecordFormSubmission(ctx, issue3795Submission("p-1")); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.GetFormSubmission(ctx, "tenant-b", "c-1", "p-1"); err != nil || found {
		t.Fatalf("other tenant reads the answer: %v %v", found, err)
	}
	if at, err := s.FormSubmittedAt(ctx, "tenant-b", "c-1", []string{"p-1"}); err != nil || len(at) != 0 {
		t.Fatalf("other tenant sees status: %v %v", at, err)
	}
	other := issue3795Submission("p-2")
	other.TenantID = "tenant-b"
	if _, err := s.RecordFormSubmission(ctx, other); err == nil {
		t.Fatal("recorded an answer in another tenant's conversation")
	}
	for _, bad := range []FormSubmission{
		{TenantID: "tenant-a", ConversationID: "c-1", PostID: "", SubmittedByID: "u", RequestedByID: "a", Answers: map[string]string{}},
		{TenantID: "tenant-a", ConversationID: "c-1", PostID: "p", SubmittedByID: " ", RequestedByID: "a", Answers: map[string]string{}},
		{TenantID: "tenant-a", ConversationID: "c-1", PostID: "p", SubmittedByID: "u", RequestedByID: "a"},
	} {
		if _, err := s.RecordFormSubmission(ctx, bad); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	if _, err := s.FormSubmittedAt(ctx, "tenant-a", "c-1", make([]string, FormSubmissionBatch+1)); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Errorf("oversized batch: %v", err)
	}
}

func TestIssue3795_FormSubmissionIsNeverEdited_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	if _, err := s.RecordFormSubmission(ctx, issue3795Submission("p-1")); err != nil {
		t.Fatal(err)
	}
	err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_form_submission SET answers='{}'::jsonb WHERE tenant_id='tenant-a'`)
		return err
	})
	if err == nil {
		t.Fatal("an answer was edited")
	}
}
