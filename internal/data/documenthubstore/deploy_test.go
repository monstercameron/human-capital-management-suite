package documenthubstore

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func deployState(t *testing.T, s *Store, tenant, docID string) (live string, events int, statuses map[string]string) {
	t.Helper()
	statuses = map[string]string{}
	if err := s.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT version_id FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 AND scope_kind='default' AND scope_id=''`, tenant, docID).Scan(&live); err != nil {
			live = ""
		}
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='deployment.published'`, tenant, docID).Scan(&events); err != nil {
			return err
		}
		rows, err := tx.Query(context.Background(), `SELECT id,status FROM document_version WHERE tenant_id=$1 AND document_id=$2`, tenant, docID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, status string
			if err := rows.Scan(&id, &status); err != nil {
				return err
			}
			statuses[id] = status
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return live, events, statuses
}

// TestTodo_HUB_009_Recovery is the RECOVERY test for HUB-009: every failed
// deploy leaves the previous deployment live with no partial event, and a
// superseded version retires to stale exactly once.
func TestTodo_HUB_009_Recovery(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "two\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v1.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: v1.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer"}); err != nil {
		t.Fatal(err)
	}
	in := DeployInput{DocumentID: docID, VersionID: v2.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: v1.ID}
	if _, err := s.Deploy(ctx, "tenant-a", in); !errors.Is(err, ErrNoApprovedReview) {
		t.Fatalf("unreviewed deploy: %v", err)
	}
	live, events, statuses := deployState(t, s, "tenant-a", docID)
	if live != v1.ID || events != 1 || statuses[v2.ID] != "candidate" {
		t.Fatalf("failed deploy left partial state: live=%q events=%d statuses=%v", live, events, statuses)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v2.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	stale := in
	stale.ExpectedLive = ""
	if _, err := s.Deploy(ctx, "tenant-a", stale); !errors.Is(err, ErrStalePointer) {
		t.Fatalf("stale CAS deploy: %v", err)
	}
	live, events, _ = deployState(t, s, "tenant-a", docID)
	if live != v1.ID || events != 1 {
		t.Fatalf("stale CAS left partial state: live=%q events=%d", live, events)
	}
	if _, err := s.Deploy(ctx, "tenant-a", in); err != nil {
		t.Fatal(err)
	}
	live, events, statuses = deployState(t, s, "tenant-a", docID)
	if live != v2.ID || events != 2 || statuses[v1.ID] != "stale" || statuses[v2.ID] != "deployed" {
		t.Fatalf("supersede wrong: live=%q events=%d statuses=%v", live, events, statuses)
	}
}

// TestTodo_HUB_009_Golden is the GOLDEN test for HUB-009: the canonical
// publication event body is pinned for outbox consumers.
func TestTodo_HUB_009_Golden(t *testing.T) {
	payload, err := deploymentEventPayload("tenant-a", Deployment{
		ID: "docd-fixed", DocumentID: "doc-fixed", VersionID: "docv-fixed",
		ScopeKind: "placement", ScopeID: "chan-A", DeployerID: "u-deployer",
	}, "docd-prior", "docr-fixed", "9e1e2a8ea86d8c8dcfa4b4a1a0b8e1e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/hub009_deploy.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload+"\n", string(want)) {
		t.Fatalf("deploy evidence bytes changed:\n got %s\nwant %s", payload, want)
	}
}

// TestTodo_HUB_009 is the PRIMARY test for HUB-009: a deploy commits the
// scoped pointer and its publication event once, only for the reviewed
// bytes, and only for an authorized deployer.
func TestTodo_HUB_009(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	deploy := DeployInput{DocumentID: docID, VersionID: v1.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: ""}
	if _, err := s.Deploy(ctx, "tenant-a", deploy); !errors.Is(err, ErrDenied) {
		t.Fatalf("deploy without grant: %v", err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", deploy); !errors.Is(err, ErrNoApprovedReview) {
		t.Fatalf("deploy without review: %v", err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v1.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	d, err := s.Deploy(ctx, "tenant-a", deploy)
	if err != nil {
		t.Fatalf("reviewed deploy refused: %v", err)
	}
	if d.VersionID != v1.ID || d.ScopeKind != "default" {
		t.Fatalf("deployment names wrong target: %+v", d)
	}
	got, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", "")
	if err != nil || got.VersionID != v1.ID {
		t.Fatalf("pointer not live: %+v err=%v", got, err)
	}
	var events int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='deployment.published'`, "tenant-a", docID).Scan(&events)
	}); err != nil || events != 1 {
		t.Fatalf("publication events: count=%d err=%v", events, err)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "two\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: v2.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: v1.ID}); !errors.Is(err, ErrNoApprovedReview) {
		t.Fatalf("deploy of unreviewed version: %v", err)
	}
	got, err = s.ResolveDeployment(ctx, "tenant-a", docID, "default", "")
	if err != nil || got.VersionID != v1.ID {
		t.Fatalf("failed deploy moved the pointer: %+v err=%v", got, err)
	}
}

// TestTodo_HUB_009_Race is the RACE test for HUB-009: concurrent deploys of
// one scope commit exactly once; every loser sees the winner.
func TestTodo_HUB_009_Race(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	const racers = 8
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: ""})
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrStalePointer) {
			t.Fatalf("loser got %v, want stale pointer", err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent deploys committed %d times", wins)
	}
}
