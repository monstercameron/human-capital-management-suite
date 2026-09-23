package documenthubstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_HUB_016_Security is the SECURITY test for HUB-016: revocation
// closes histories as well as current reads, the owner path survives, and
// unknown versions stay indistinguishable from denied ones.
func TestTodo_HUB_016_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-owner", Markdown: "secret\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	grant, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", GrantInput{SubjectKind: "person", SubjectID: "u-reader", Action: ActionRead, Effect: EffectAllow})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeGrant(ctx, "tenant-a", grant.ID, "u-owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadVersion(ctx, "tenant-a", docID, v.ID, "person", "u-reader"); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked history still readable")
	}
	if _, err := s.ReadVersion(ctx, "tenant-a", docID, v.ID, "person", "u-owner"); err != nil {
		t.Fatalf("owner locked out by a revocation: %v", err)
	}
	if _, err := s.ReadVersion(ctx, "tenant-a", docID, "docv-missing", "person", "u-owner"); !errors.Is(err, ErrDenied) {
		t.Fatal("unknown version distinguished from denied")
	}
	if _, err := s.ReadVersion(ctx, "tenant-b", docID, v.ID, "person", "u-reader"); !errors.Is(err, ErrDenied) {
		t.Fatal("cross-tenant read passed")
	}
	if _, err := s.PolicyEpoch(ctx, "tenant-b", docID); err == nil {
		t.Fatal("cross-tenant epoch visible")
	}
}

// TestTodo_HUB_016_Golden is the GOLDEN test for HUB-016: the canonical
// invalidation event body is pinned for derived consumers.
func TestTodo_HUB_016_Golden(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", GrantInput{SubjectKind: "person", SubjectID: "u-reader", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload::text FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='grant.revised' ORDER BY id DESC LIMIT 1`, "tenant-a", docID).Scan(&payload)
	}); err != nil {
		t.Fatal(err)
	}
	var event map[string]string
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatal(err)
	}
	event["document_id"], event["tenant_id"], event["grant_id"], event["epoch"] = "D", "T", "G", "N"
	assertGoldenJSON(t, "testdata/hub016_revised.golden.json", event)
}

// TestTodo_HUB_016 is the PRIMARY test for HUB-016: revoking a grant closes
// the gated read path at once, bumps the policy epoch, and publishes the
// invalidation event in the same commit.
func TestTodo_HUB_016(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-owner", Markdown: "secret\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := s.PolicyEpoch(ctx, "tenant-a", docID)
	if err != nil || epoch != 1 {
		t.Fatalf("genesis epoch wrong: %d err=%v", epoch, err)
	}
	grant, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", GrantInput{SubjectKind: "person", SubjectID: "u-reader", Action: ActionRead, Effect: EffectAllow})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err = s.PolicyEpoch(ctx, "tenant-a", docID)
	if err != nil || epoch != 2 {
		t.Fatalf("share did not advance epoch: %d err=%v", epoch, err)
	}
	got, err := s.ReadVersion(ctx, "tenant-a", docID, v.ID, "person", "u-reader")
	if err != nil || got.Markdown != "secret\n" {
		t.Fatalf("shared read failed: %+v err=%v", got, err)
	}
	if err := s.RevokeGrant(ctx, "tenant-a", grant.ID, "u-owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadVersion(ctx, "tenant-a", docID, v.ID, "person", "u-reader"); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked read still open")
	}
	epoch, err = s.PolicyEpoch(ctx, "tenant-a", docID)
	if err != nil || epoch != 3 {
		t.Fatalf("revoke did not advance epoch: %d err=%v", epoch, err)
	}
	var events int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='grant.revised'`, "tenant-a", docID).Scan(&events)
	}); err != nil || events != 2 {
		t.Fatalf("policy mutations missing invalidation events: count=%d err=%v", events, err)
	}
}

// TestTodo_HUB_016_Fault is the FAULT test for HUB-016: every policy
// mutation carries its invalidation event in the same commit, so no revoke
// is ever lost to derived consumers.
func TestTodo_HUB_016_Fault(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	grants := []GrantInput{
		{SubjectKind: "person", SubjectID: "u-1", Action: ActionRead, Effect: EffectAllow},
		{SubjectKind: "person", SubjectID: "u-2", Action: ActionComment, Effect: EffectAllow},
		{SubjectKind: "team", SubjectID: "team-1", Action: ActionRead, Effect: EffectDeny},
	}
	var ids []string
	for _, g := range grants {
		g.DocumentID = docID
		created, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", g)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	for _, id := range ids {
		if err := s.RevokeGrant(ctx, "tenant-a", id, "u-owner"); err != nil {
			t.Fatal(err)
		}
	}
	epoch, err := s.PolicyEpoch(ctx, "tenant-a", docID)
	if err != nil || epoch != 1+uint64(2*len(ids)) {
		t.Fatalf("epoch skipped mutations: %d", epoch)
	}
	var events int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='grant.revised'`, "tenant-a", docID).Scan(&events)
	}); err != nil || events != 2*len(ids) {
		t.Fatalf("lost invalidation events: count=%d err=%v", events, err)
	}
}
