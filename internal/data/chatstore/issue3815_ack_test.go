package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3815Requirement(post string) AckRequirement {
	return AckRequirement{TenantID: "tenant-a", ConversationID: "c-1", PostID: post, RequiredByID: "u-1", PostRevision: 1, RequiredAt: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)}
}

func TestIssue3815_RequirementAndAcknowledgementAreKeptOnce_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	created, err := s.RequireAck(ctx, issue3815Requirement("p-1"))
	if err != nil || !created {
		t.Fatalf("require = %v, %v", created, err)
	}
	// Requiring twice changes nothing.
	again := issue3815Requirement("p-1")
	again.RequiredByID = "u-9"
	if created, err = s.RequireAck(ctx, again); err != nil || created {
		t.Fatalf("second require = %v, %v", created, err)
	}
	reqs, err := s.AckRequirements(ctx, "tenant-a", "c-1", []string{"p-1", "p-none"})
	if err != nil || len(reqs) != 1 || reqs["p-1"].RequiredByID != "u-1" {
		t.Fatalf("requirements = %+v, %v", reqs, err)
	}
	ok, err := s.RecordAck(ctx, Ack{TenantID: "tenant-a", ConversationID: "c-1", PostID: "p-1", SubjectID: "u-2", PostRevision: 1})
	if err != nil || !ok {
		t.Fatalf("ack = %v, %v", ok, err)
	}
	if ok, err = s.RecordAck(ctx, Ack{TenantID: "tenant-a", ConversationID: "c-1", PostID: "p-1", SubjectID: "u-2", PostRevision: 2}); err != nil || ok {
		t.Fatalf("second ack = %v, %v; want it refused without error", ok, err)
	}
	if _, err = s.RecordAck(ctx, Ack{TenantID: "tenant-a", ConversationID: "c-1", PostID: "p-1", SubjectID: "u-3", PostRevision: 1}); err != nil {
		t.Fatal(err)
	}
	acks, err := s.ListAcks(ctx, "tenant-a", "c-1", "p-1")
	if err != nil || len(acks) != 2 || acks[0].SubjectID != "u-2" || acks[0].PostRevision != 1 || acks[0].AcknowledgedAt.IsZero() {
		t.Fatalf("acks = %+v, %v", acks, err)
	}
	counts, err := s.AckCounts(ctx, "tenant-a", "c-1", []string{"p-1"})
	if err != nil || counts["p-1"] != 2 {
		t.Fatalf("counts = %v, %v", counts, err)
	}
	mine, err := s.MyAcks(ctx, "tenant-a", "c-1", []string{"p-1", "p-none"}, "u-3")
	if err != nil || len(mine) != 1 || mine["p-1"].IsZero() {
		t.Fatalf("mine = %v, %v", mine, err)
	}
	if mine, err = s.MyAcks(ctx, "tenant-a", "c-1", []string{"p-1"}, "u-8"); err != nil || len(mine) != 0 {
		t.Fatalf("a person who did not acknowledge: %v, %v", mine, err)
	}
}

func TestIssue3815_AcknowledgementNeedsARequirement_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	_, err := s.RecordAck(context.Background(), Ack{TenantID: "tenant-a", ConversationID: "c-1", PostID: "p-plain", SubjectID: "u-2"})
	if !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("ack of a post with no requirement: %v", err)
	}
}

func TestIssue3815_AcknowledgementStaysInItsTenant_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	if _, err := s.RequireAck(ctx, issue3815Requirement("p-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordAck(ctx, Ack{TenantID: "tenant-a", ConversationID: "c-1", PostID: "p-1", SubjectID: "u-2"}); err != nil {
		t.Fatal(err)
	}
	if reqs, err := s.AckRequirements(ctx, "tenant-b", "c-1", []string{"p-1"}); err != nil || len(reqs) != 0 {
		t.Fatalf("other tenant sees the requirement: %v %v", reqs, err)
	}
	if acks, err := s.ListAcks(ctx, "tenant-b", "c-1", "p-1"); err != nil || len(acks) != 0 {
		t.Fatalf("other tenant sees acknowledgements: %v %v", acks, err)
	}
	if counts, err := s.AckCounts(ctx, "tenant-b", "c-1", []string{"p-1"}); err != nil || len(counts) != 0 {
		t.Fatalf("other tenant sees counts: %v %v", counts, err)
	}
	other := issue3815Requirement("p-2")
	other.TenantID = "tenant-b"
	if _, err := s.RequireAck(ctx, other); err == nil {
		t.Fatal("required an acknowledgement in another tenant's conversation")
	}
	for _, bad := range []AckRequirement{{TenantID: "tenant-a", ConversationID: "c-1", PostID: "", RequiredByID: "u"}, {TenantID: "tenant-a", ConversationID: "c-1", PostID: "p", RequiredByID: " "}} {
		if _, err := s.RequireAck(ctx, bad); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	if _, err := s.AckRequirements(ctx, "tenant-a", "c-1", make([]string, AckBatch+1)); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Errorf("oversized batch: %v", err)
	}
}

func TestIssue3815_AcknowledgementIsNeverEditedOrRemoved_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	if _, err := s.RequireAck(ctx, issue3815Requirement("p-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordAck(ctx, Ack{TenantID: "tenant-a", ConversationID: "c-1", PostID: "p-1", SubjectID: "u-2"}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`UPDATE chat_ack_record SET subject_id='u-9' WHERE tenant_id='tenant-a'`,
		`UPDATE chat_ack_requirement SET required_by_id='u-9' WHERE tenant_id='tenant-a'`,
	} {
		err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, stmt)
			return err
		})
		if err == nil {
			t.Fatalf("evidence was edited by %q", stmt)
		}
	}
}
