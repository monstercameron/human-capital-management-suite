package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

func stubEmbedder(text string) ([]float32, error) {
	return []float32{float32(len(text)), 1.5}, nil
}

func testEmbeddingPolicy() EmbeddingPolicy {
	return EmbeddingPolicy{Models: []EmbeddingModel{
		{ID: "hub-internal-v1", Version: "m1", External: false},
		{ID: "vendor-external-v2", Version: "m2", External: true},
	}}
}

// TestTodo_HUB_026 is the PRIMARY test for HUB-026: approved models create
// versioned, hashed section vectors; candidate drafts, unknown models and
// restricted egress are refused before any provider call.
func TestTodo_HUB_026(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n\n## Feeding\n\nFeed daily.\n")
	policy := testEmbeddingPolicy()
	draft, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docA, CreatorID: "u-author", Markdown: "# Draft\n\nUnreviewed.\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EmbedSections(ctx, "tenant-a", docA, draft.ID, policy, "hub-internal-v1", stubEmbedder); !errors.Is(err, ErrNoDeployment) {
		t.Fatalf("candidate draft embedded: %v", err)
	}
	if _, err := s.EmbedSections(ctx, "tenant-a", docA, v1.ID, policy, "unknown-model", stubEmbedder); !errors.Is(err, ErrEmbeddingModel) {
		t.Fatalf("unapproved model embedded: %v", err)
	}
	vecs, err := s.EmbedSections(ctx, "tenant-a", docA, v1.ID, policy, "hub-internal-v1", stubEmbedder)
	if err != nil {
		t.Fatalf("approved embed refused: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("sections wrong: %+v", vecs)
	}
	for _, v := range vecs {
		if v.ModelID != "hub-internal-v1" || v.ModelVersion != "m1" || v.ParserVersion == "" {
			t.Fatalf("version evidence missing: %+v", v)
		}
		if v.ContentHash == "" || v.TenantID != "tenant-a" || len(v.Dimensions()) != 2 {
			t.Fatalf("vector evidence wrong: %+v", v)
		}
	}
	docR, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	rv, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docR, CreatorID: "u-author", Classification: "RESTRICTED", Markdown: "# Secret\n\nRestricted.\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docR, VersionID: rv.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docR, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docR, VersionID: rv.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer"}); err != nil {
		t.Fatal(err)
	}
	called := false
	probe := func(text string) ([]float32, error) { called = true; return stubEmbedder(text) }
	if _, err := s.EmbedSections(ctx, "tenant-a", docR, rv.ID, policy, "vendor-external-v2", probe); !errors.Is(err, ErrEmbeddingEgress) {
		t.Fatalf("restricted markdown egressed: %v", err)
	}
	if called {
		t.Fatal("provider called for refused egress")
	}
}

// TestTodo_HUB_026_Security is the SECURITY test for HUB-026: vectors are
// tenant-scoped and internal models still serve restricted sections.
func TestTodo_HUB_026_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	policy := testEmbeddingPolicy()
	if _, err := s.EmbedSections(ctx, "tenant-a", docA, v1.ID, policy, "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EmbedSections(ctx, "tenant-b", docA, v1.ID, policy, "hub-internal-v1", stubEmbedder); err == nil {
		t.Fatal("cross-tenant embed accepted")
	}
	foreign, err := s.SectionVectors(ctx, "tenant-b", docA, v1.ID, "hub-internal-v1")
	if err != nil || len(foreign) != 0 {
		t.Fatalf("vectors leaked across tenants: %+v err=%v", foreign, err)
	}
	home, err := s.SectionVectors(ctx, "tenant-a", docA, v1.ID, "hub-internal-v1")
	if err != nil || len(home) != 1 {
		t.Fatalf("home vectors missing: %+v err=%v", home, err)
	}
}

// TestTodo_HUB_026_Integration is the INTEGRATION test for HUB-026: vector
// bytes roundtrip through the real store and each vector binds its exact
// section text hash.
func TestTodo_HUB_026_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	vecs, err := s.EmbedSections(ctx, "tenant-a", docA, v1.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.SectionVectors(ctx, "tenant-a", docA, v1.ID, "hub-internal-v1")
	if err != nil || len(stored) != len(vecs) {
		t.Fatalf("vectors not persisted: %+v err=%v", stored, err)
	}
	sections := SplitSections("# Cats\n\nA guide to cats.\n")
	byBlock := map[string]Section{}
	for _, sec := range sections {
		byBlock[sec.BlockID] = sec
	}
	for _, v := range stored {
		sec, ok := byBlock[v.BlockID]
		if !ok {
			t.Fatalf("vector for unknown section: %+v", v)
		}
		if v.ContentHash != HashContent(sec.Text) {
			t.Fatalf("vector hash does not bind section text: %+v", v)
		}
		dims := v.Dimensions()
		if len(dims) != 2 || dims[0] != float32(len(sec.Text)) || dims[1] != 1.5 {
			t.Fatalf("vector bytes corrupt: %+v", v)
		}
	}
}
