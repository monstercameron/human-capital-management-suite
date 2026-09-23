package documenthubstore

import (
	"context"
	"testing"
)

func searchFixture(t *testing.T, s *Store, ctx context.Context, tenant, docID, markdown string) Version {
	t.Helper()
	v, err := s.SubmitCandidate(ctx, tenant, Version{DocumentID: docID, CreatorID: "u-author", Title: "Guide", Markdown: markdown}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, tenant, ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, tenant, DeployInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer"}); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexDeployedVersion(ctx, tenant, docID, v.ID); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestTodo_HUB_025 is the PRIMARY test for HUB-025: search returns the
// deployed version only, never candidates, stale versions or documents
// the reader cannot read.
func TestTodo_HUB_025(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	docB, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docB, CreatorID: "u-author", Title: "Guide", Markdown: "# Dogs\n\nA guide to dogs.\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.IndexDeployedVersion(ctx, "tenant-a", docB, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLexical(ctx, "tenant-a", "guide cats", "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].DocumentID != docA || hits[0].VersionID != v1.ID {
		t.Fatalf("search leaked or missed: %+v", hits)
	}
	if hits[0].Score <= 0 {
		t.Fatalf("hit has no score: %+v", hits[0])
	}
	titleHits, err := s.SearchLexical(ctx, "tenant-a", "guide", "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range titleHits {
		if h.DocumentID == docB {
			t.Fatalf("candidate version searchable: %+v", titleHits)
		}
	}
}

// TestTodo_HUB_025_Security is the SECURITY test for HUB-025: readers
// without a live read grant see nothing, and denied readers see nothing
// even with an allow row present.
func TestTodo_HUB_025_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	hits, err := s.SearchLexical(ctx, "tenant-a", "cats", "person", "u-stranger")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("private snippets searchable: %+v", hits)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-mallory", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-mallory", Action: ActionRead, Effect: EffectDeny}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchLexical(ctx, "tenant-a", "cats", "person", "u-mallory")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("denied reader searched: %+v", hits)
	}
}

// TestTodo_HUB_025_Integration is the INTEGRATION test for HUB-025: the
// index roundtrip reaches the real store, ranks title above body, and a
// withdrawn version leaves the result set while its stale index rows
// remain.
func TestTodo_HUB_025_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Zebra\n\nBody mentions zebra once.\n")
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docA, SubjectKind: "person", SubjectID: "u-author", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLexical(ctx, "tenant-a", "zebra", "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].VersionID != v1.ID {
		t.Fatalf("deployed version missing: %+v", hits)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docA, ScopeKind: "default", ScopeID: "", ActorID: "u-author", ExpectedLive: v1.ID, Reason: "superseded"}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchLexical(ctx, "tenant-a", "zebra", "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("withdrawn version still searchable: %+v", hits)
	}
}
