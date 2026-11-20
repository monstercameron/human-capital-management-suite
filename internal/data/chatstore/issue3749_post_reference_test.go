package chatstore

import (
	"context"
	"testing"
	"time"
)

func issue3749Targets(t *testing.T, s *Store, tenant, doc string) []string {
	t.Helper()
	got, err := s.ListPostReferencesByTarget(context.Background(), tenant, PostReferenceDocument, doc, 50)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, r := range got {
		ids = append(ids, r.PostID)
	}
	return ids
}

func issue3749Exec(t *testing.T, s *Store, tenant, sql string, args ...any) {
	t.Helper()
	if err := s.execTenant(context.Background(), tenant, sql, args...); err != nil {
		t.Fatal(err)
	}
}

// Every way a message is written keeps the index current: posting indexes the
// document tokens and Documents addresses, editing re-indexes, removing drops
// the rows, restoring brings them back, and deleting the message leaves none.
func TestIssue3749_MessagesAreIndexedByTheDocumentsTheyName_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	ins := `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,created_at) VALUES($1,'tenant-a','c-1','u-1','tenant-a',$2,$3,$4)`
	issue3749Exec(t, s, "tenant-a", ins, "p1", 1, "see doc:handbook-1, then /workspace/app/docs?document=policy_7 and doc:handbook-1 again", at.Add(1*time.Minute))
	issue3749Exec(t, s, "tenant-a", ins, "p2", 2, "nothing to see; undoc:nope and https://example.com/workspace/app/docs?view=list", at.Add(2*time.Minute))
	issue3749Exec(t, s, "tenant-a", ins, "p3", 3, "link https://hcm.example/workspace/app/docs?view=list&document=handbook-1 end", at.Add(3*time.Minute))
	if got := issue3749Targets(t, s, "tenant-a", "handbook-1"); len(got) != 2 || got[0] != "p3" || got[1] != "p1" {
		t.Fatalf("handbook-1 index %v, want p3 then p1 once each", got)
	}
	if got := issue3749Targets(t, s, "tenant-a", "policy_7"); len(got) != 1 || got[0] != "p1" {
		t.Fatalf("policy_7 index %v", got)
	}
	if got := issue3749Targets(t, s, "tenant-a", "nope"); len(got) != 0 {
		t.Fatalf("undoc matched: %v", got)
	}
	// An edit that drops one document and adds another.
	issue3749Exec(t, s, "tenant-a", `UPDATE chat_post SET body='now only doc:other',revision=revision+1 WHERE id='p1'`)
	if got := issue3749Targets(t, s, "tenant-a", "handbook-1"); len(got) != 1 || got[0] != "p3" {
		t.Fatalf("after edit handbook-1 %v", got)
	}
	if got := issue3749Targets(t, s, "tenant-a", "other"); len(got) != 1 || got[0] != "p1" {
		t.Fatalf("after edit other %v", got)
	}
	// Removing a message takes it out; restoring brings it back.
	issue3749Exec(t, s, "tenant-a", `UPDATE chat_post SET tombstoned=true WHERE id='p3'`)
	if got := issue3749Targets(t, s, "tenant-a", "handbook-1"); len(got) != 0 {
		t.Fatalf("removed message still indexed: %v", got)
	}
	issue3749Exec(t, s, "tenant-a", `UPDATE chat_post SET tombstoned=false WHERE id='p3'`)
	if got := issue3749Targets(t, s, "tenant-a", "handbook-1"); len(got) != 1 {
		t.Fatalf("restored message not indexed: %v", got)
	}
	issue3749Exec(t, s, "tenant-a", `DELETE FROM chat_post WHERE id='p3'`)
	if got := issue3749Targets(t, s, "tenant-a", "handbook-1"); len(got) != 0 {
		t.Fatalf("deleted message still indexed: %v", got)
	}
}

func TestIssue3749_TheIndexStaysInItsTenantAndRefusesBadInput_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	issue3749Exec(t, s, "tenant-a", `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('p1','tenant-a','c-1','u-1','tenant-a',1,'doc:shared-id')`)
	if got := issue3749Targets(t, s, "tenant-b", "shared-id"); len(got) != 0 {
		t.Fatalf("another tenant reads the index: %v", got)
	}
	ctx := context.Background()
	if _, err := s.ListPostReferencesByTarget(ctx, "tenant-a", "TICKET", "shared-id", 10); err == nil {
		t.Fatal("listed an unknown kind")
	}
	if _, err := s.ListPostReferencesByTarget(ctx, "tenant-a", PostReferenceDocument, " ", 10); err == nil {
		t.Fatal("listed a blank target")
	}
	if _, err := s.ListPostReferencesByTarget(ctx, " ", PostReferenceDocument, "x", 10); err == nil {
		t.Fatal("listed a blank tenant")
	}
}
