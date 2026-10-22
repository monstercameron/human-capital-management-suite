package documenthubstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

func agentux065Deploy(t *testing.T, s *Store, docID, versionID, live string, bump semver.Bump) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: versionID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: versionID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: live, Bump: bump}); err != nil {
		t.Fatal(err)
	}
}

func agentux065Label(t *testing.T, s *Store, docID, versionID string) string {
	t.Helper()
	var label string
	if err := s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT semantic_version FROM document_version WHERE tenant_id='tenant-a' AND document_id=$1 AND id=$2`, docID, versionID).Scan(&label)
	}); err != nil {
		t.Fatal(err)
	}
	return label
}

// A deployment labels its version: the first publication is 1.0.0 and each
// later one computes its label from the author's bump choice, never from
// typed text (AGENTUX-065).
func TestAgentUX065_DeployLabelsVersions(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	mk := func(body, parent string) Version {
		v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: body}, parent)
		if err != nil {
			t.Fatal(err)
		}
		if v.SemanticVersion != "" {
			t.Fatalf("a candidate carries %q before publication", v.SemanticVersion)
		}
		return v
	}
	v1 := mk("one\n", "")
	v2 := mk("two\n", v1.ID)
	v3 := mk("three\n", v2.ID)
	agentux065Deploy(t, s, docID, v1.ID, "", semver.BumpMajor)
	agentux065Deploy(t, s, docID, v2.ID, v1.ID, semver.BumpMinor)
	agentux065Deploy(t, s, docID, v3.ID, v2.ID, semver.BumpPatch)
	if got := []string{agentux065Label(t, s, docID, v1.ID), agentux065Label(t, s, docID, v2.ID), agentux065Label(t, s, docID, v3.ID)}; got[0] != "1.0.0" || got[1] != "1.1.0" || got[2] != "1.1.1" {
		t.Fatalf("labels = %v, want [1.0.0 1.1.0 1.1.1]", got)
	}
	// The publication event seals the label it published.
	var payload string
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload->>'semantic_version' FROM document_outbox WHERE tenant_id='tenant-a' AND aggregate_id=$1 AND event_type='deployment.published' ORDER BY id DESC LIMIT 1`, docID).Scan(&payload)
	}); err != nil || payload != "1.1.1" {
		t.Fatalf("sealed label = %q, %v", payload, err)
	}
}

// A published label is never changed: a redeploy with another choice keeps
// the sealed label, and the database refuses a relabel outright (AGENTUX-065).
func TestAgentUX065_PublishedLabelImmutable(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	agentux065Deploy(t, s, docID, v1.ID, "", semver.BumpPatch)
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v1.ID, ScopeKind: "other", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: v1.ID, ScopeKind: "other", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: "", Bump: semver.BumpMajor}); err != nil {
		t.Fatal(err)
	}
	if got := agentux065Label(t, s, docID, v1.ID); got != "1.0.0" {
		t.Fatalf("a redeploy relabeled to %q", got)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE document_version SET semantic_version='9.9.9' WHERE tenant_id='tenant-a' AND id=$1`, v1.ID)
		return err
	}); err == nil {
		t.Fatal("the database accepted a relabel")
	}
	if got := agentux065Label(t, s, docID, v1.ID); got != "1.0.0" {
		t.Fatalf("label after refused relabel = %q", got)
	}
}

// Document history reports each version's sealed label, so the compare
// pickers name labels instead of positional fallbacks (AGENTUX-065).
func TestAgentUX065_HistoryReportsLabels(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
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
	agentux065Deploy(t, s, docID, v1.ID, "", semver.BumpPatch)
	agentux065Deploy(t, s, docID, v2.ID, v1.ID, semver.BumpMinor)
	history, err := s.ReadHistory(ctx, "tenant-a", docID, "person", "u-author")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, entry := range history {
		got[entry.VersionID] = entry.SemanticVersion
	}
	if got[v1.ID] != "1.0.0" || got[v2.ID] != "1.1.0" {
		t.Fatalf("history labels = %v", got)
	}
}
