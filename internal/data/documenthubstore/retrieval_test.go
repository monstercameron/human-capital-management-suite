package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_HUB_028 is the PRIMARY test for HUB-028: exact retrieval is the
// baseline — only deployed sections the reader may read, bounded to the
// limit with no total disclosed.
func TestTodo_HUB_028(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	vA := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n\n## Feeding\n\nFeed daily.\n\n## Health\n\nVet visits.\n")
	if _, err := s.EmbedSections(ctx, "tenant-a", docA, vA.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatal(err)
	}
	docB, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	vB := searchFixture(t, s, ctx, "tenant-a", docB, "# Secret\n\nNobody may read this.\n")
	if _, err := s.EmbedSections(ctx, "tenant-a", docB, vB.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	qvec, err := stubEmbedder("cats")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.RetrieveVectors(ctx, "tenant-a", qvec, "hub-internal-v1", "person", "u-anna", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("retrieval not bounded to limit: %+v", got)
	}
	for _, h := range got {
		if h.DocumentID != docA {
			t.Fatalf("unauthorized section retrieved: %+v", got)
		}
	}
	full, err := s.RetrieveVectors(ctx, "tenant-a", qvec, "hub-internal-v1", "person", "u-anna", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 3 {
		t.Fatalf("authorized sections missing: %+v", full)
	}
}

// TestTodo_HUB_028_Security is the SECURITY test for HUB-028: revoked and
// grantless readers learn nothing — identical empty results for existing
// and nonexistent documents — and approximate indexes must pass
// conformance before serving.
func TestTodo_HUB_028_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	vA := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	if _, err := s.EmbedSections(ctx, "tenant-a", docA, vA.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatal(err)
	}
	g, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow})
	if err != nil {
		t.Fatal(err)
	}
	qvec, _ := stubEmbedder("cats")
	before, err := s.RetrieveVectors(ctx, "tenant-a", qvec, "hub-internal-v1", "person", "u-anna", 10)
	if err != nil || len(before) != 1 {
		t.Fatalf("granted retrieval wrong: %+v err=%v", before, err)
	}
	if err := s.RevokeGrant(ctx, "tenant-a", g.ID, "u-author"); err != nil {
		t.Fatal(err)
	}
	after, err := s.RetrieveVectors(ctx, "tenant-a", qvec, "hub-internal-v1", "person", "u-anna", 10)
	if err != nil || len(after) != 0 {
		t.Fatalf("revoked reader retrieved: %+v err=%v", after, err)
	}
	ghost, err := s.RetrieveVectors(ctx, "tenant-a", qvec, "hub-internal-v1", "person", "u-ghost", 10)
	if err != nil || len(ghost) != 0 {
		t.Fatalf("grantless reader retrieved: %+v err=%v", ghost, err)
	}
	empty, err := s.RetrieveVectors(ctx, "tenant-b", qvec, "hub-internal-v1", "person", "u-ghost", 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty-tenant read differs: %+v err=%v", empty, err)
	}
	exact := []RetrievedSection{
		{DocumentID: "doc-a", VersionID: "docv-1", BlockID: "a"},
		{DocumentID: "doc-a", VersionID: "docv-1", BlockID: "b"},
	}
	leaky := append(append([]RetrievedSection(nil), exact...), RetrievedSection{DocumentID: "doc-x", VersionID: "docv-9", BlockID: "secret"})
	if err := RequireIndexConformance(exact, leaky, 10); !errors.Is(err, ErrIndexNonconformant) {
		t.Fatalf("leaky index conformed: %v", err)
	}
	if err := RequireIndexConformance(exact, exact[:1], 10); err != nil {
		t.Fatalf("conformant subset rejected: %v", err)
	}
	if rep := CheckIndexConformance(exact, exact[:1], 10); rep.Recall != 0.5 || rep.Leaked != 0 || !rep.WithinBound {
		t.Fatalf("conformance report wrong: %+v", rep)
	}
	if err := RequireIndexConformance(exact, exact, 1); !errors.Is(err, ErrIndexNonconformant) {
		t.Fatalf("over-bound index conformed: %v", err)
	}
}

// BenchmarkTodo_HUB_028 measures conformance-check throughput over
// synthetic exact and approximate candidate sets.
func BenchmarkTodo_HUB_028(b *testing.B) {
	var exact, approx []RetrievedSection
	for i := 0; i < 500; i++ {
		exact = append(exact, RetrievedSection{DocumentID: "doc-a", VersionID: "docv-1", BlockID: "b"})
		approx = append(approx, RetrievedSection{DocumentID: "doc-a", VersionID: "docv-1", BlockID: "b", Cosine: 0.5})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CheckIndexConformance(exact, approx, 500)
	}
}
