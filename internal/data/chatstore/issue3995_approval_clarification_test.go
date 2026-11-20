package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3995Row(id, key string) ApprovalClarification {
	return ApprovalClarification{
		TenantID: "tenant-a", ID: id, JourneyID: "j-1", ProposalRevision: "rev-1", ConversationID: "c-1", PostID: "p-1",
		AskedBy: "approver", RecipientID: "requester", QuestionDigest: "digest-1", IdempotencyKey: key,
		AskedAt: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC),
	}
}

func TestIssue3995_ClarificationIsRecordedOnceAndKept_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	first, created, err := s.RecordApprovalClarification(ctx, issue3995Row("a-1", "key-0001"))
	if err != nil || !created || first.ID != "a-1" {
		t.Fatalf("record = %+v, %v, %v", first, created, err)
	}
	// A retry with the same key returns the first record and writes nothing.
	again := issue3995Row("a-2", "key-0001")
	again.ProposalRevision = "rev-9"
	got, created, err := s.RecordApprovalClarification(ctx, again)
	if err != nil || created || got.ID != "a-1" || got.ProposalRevision != "rev-1" {
		t.Fatalf("retry = %+v, %v, %v", got, created, err)
	}
	// A new key is a new request.
	if _, created, err = s.RecordApprovalClarification(ctx, issue3995Row("a-3", "key-0002")); err != nil || !created {
		t.Fatalf("second request = %v, %v", created, err)
	}
	list, err := s.ListApprovalClarifications(ctx, "tenant-a", "j-1", "approver", 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	// The record is evidence: it cannot be changed or removed.
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_approval_clarification SET proposal_revision='x' WHERE tenant_id='tenant-a'`)
		return err
	}); err == nil {
		t.Fatal("an update of the governed record was allowed")
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_approval_clarification WHERE tenant_id='tenant-a'`)
		return err
	}); err == nil {
		t.Fatal("a delete of the governed record was allowed")
	}
}

func TestIssue3995_OnlyTheTwoPeopleOfARequestReadIt_Security(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	if _, _, err := s.RecordApprovalClarification(ctx, issue3995Row("a-1", "key-0001")); err != nil {
		t.Fatal(err)
	}
	for _, person := range []string{"approver", "requester"} {
		if list, err := s.ListApprovalClarifications(ctx, "tenant-a", "j-1", person, 10); err != nil || len(list) != 1 {
			t.Fatalf("%s sees %+v, %v", person, list, err)
		}
	}
	if list, err := s.ListApprovalClarifications(ctx, "tenant-a", "j-1", "bystander", 10); err != nil || len(list) != 0 {
		t.Fatalf("a bystander sees %+v, %v", list, err)
	}
	// Another tenant never sees this tenant's rows, even for the same ids.
	if list, err := s.ListApprovalClarifications(ctx, "tenant-b", "j-1", "approver", 10); err != nil || len(list) != 0 {
		t.Fatalf("another tenant sees %+v, %v", list, err)
	}
	if list, err := s.ListApprovalClarifications(ctx, "tenant-a", "j-2", "approver", 10); err != nil || len(list) != 0 {
		t.Fatalf("another journey shows %+v, %v", list, err)
	}
}

func TestIssue3995_ARequestNeedsTwoPeopleAndAKey(t *testing.T) {
	s, _ := chatFixture(t)
	bad := []ApprovalClarification{
		func() ApprovalClarification {
			c := issue3995Row("a-1", "key-0001")
			c.RecipientID = c.AskedBy
			return c
		}(),
		func() ApprovalClarification { c := issue3995Row("a-1", "key-0001"); c.IdempotencyKey = " "; return c }(),
		func() ApprovalClarification { c := issue3995Row("a-1", "key-0001"); c.ProposalRevision = ""; return c }(),
	}
	for i, c := range bad {
		if _, _, err := s.RecordApprovalClarification(context.Background(), c); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
}
