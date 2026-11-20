package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3998Row(id, key string) InputRequest {
	return InputRequest{
		TenantID: "tenant-a", ID: id, JourneyID: "j-1", ProposalRevision: "rev-1", ConversationID: "c-1", PostID: "p-1",
		AskedBy: "reviewer", AudienceKind: "group", AudienceSize: 4, QuestionDigest: "digest-1", IdempotencyKey: key,
		AskedAt: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC),
	}
}

func TestIssue3998_InputRequestIsRecordedOnceAndKept_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	first, created, err := s.RecordInputRequest(ctx, issue3998Row("r-1", "key-0001"))
	if err != nil || !created || first.ID != "r-1" || first.AudienceSize != 4 || first.AudienceKind != "group" {
		t.Fatalf("record = %+v, %v, %v", first, created, err)
	}
	again := issue3998Row("r-2", "key-0001")
	again.ProposalRevision = "rev-9"
	got, created, err := s.RecordInputRequest(ctx, again)
	if err != nil || created || got.ID != "r-1" || got.ProposalRevision != "rev-1" {
		t.Fatalf("retry = %+v, %v, %v", got, created, err)
	}
	if _, created, err = s.RecordInputRequest(ctx, issue3998Row("r-3", "key-0002")); err != nil || !created {
		t.Fatalf("second request = %v, %v", created, err)
	}
	list, err := s.ListInputRequests(ctx, "tenant-a", "j-1", "reviewer", 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_input_request SET proposal_revision='x' WHERE tenant_id='tenant-a'`)
		return err
	}); err == nil {
		t.Fatal("an update of the record was allowed")
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_input_request WHERE tenant_id='tenant-a'`)
		return err
	}); err == nil {
		t.Fatal("a delete of the record was allowed")
	}
}

func TestIssue3998_OnlyTheAskerReadsTheirRequests_Security(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	if _, _, err := s.RecordInputRequest(ctx, issue3998Row("r-1", "key-0001")); err != nil {
		t.Fatal(err)
	}
	if list, err := s.ListInputRequests(ctx, "tenant-a", "j-1", "reviewer", 10); err != nil || len(list) != 1 {
		t.Fatalf("asker sees %+v, %v", list, err)
	}
	for _, c := range []struct{ tenant, journey, who string }{
		{"tenant-a", "j-1", "someone-else"}, {"tenant-b", "j-1", "reviewer"}, {"tenant-a", "j-2", "reviewer"},
	} {
		if list, err := s.ListInputRequests(ctx, c.tenant, c.journey, c.who, 10); err != nil || len(list) != 0 {
			t.Fatalf("%+v sees %+v, %v", c, list, err)
		}
	}
}

func TestIssue3998_ARequestNeedsAKnownAudienceAndAKey(t *testing.T) {
	s, _ := chatFixture(t)
	bad := []InputRequest{
		func() InputRequest { r := issue3998Row("r-1", "key-0001"); r.AudienceKind = "everyone"; return r }(),
		func() InputRequest { r := issue3998Row("r-1", "key-0001"); r.IdempotencyKey = " "; return r }(),
		func() InputRequest { r := issue3998Row("r-1", "key-0001"); r.ProposalRevision = ""; return r }(),
		func() InputRequest { r := issue3998Row("r-1", "key-0001"); r.AudienceSize = -1; return r }(),
	}
	for i, r := range bad {
		if _, _, err := s.RecordInputRequest(context.Background(), r); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("case %d: err = %v", i, err)
		}
	}
}
