package documenthubstore

import (
	"context"
	"strings"
	"testing"
)

func hybridOpts() HybridOptions {
	return HybridOptions{ModelID: "hub-internal-v1", Embed: stubEmbedder}
}

// TestTodo_HUB_027 is the PRIMARY test for HUB-027: exact lexical matches
// keep priority, semantic candidates fill the remainder, and keyword
// results survive when vectors lag.
func TestTodo_HUB_027(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	vA := searchFixture(t, s, ctx, "tenant-a", docA, "# Guide to Cats\n\nAll about cats.\n")
	docB, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	vB := searchFixture(t, s, ctx, "tenant-a", docB, "# Other\n\nA guide appears here in the body text only.\n")
	if _, err := s.EmbedSections(ctx, "tenant-a", docA, vA.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EmbedSections(ctx, "tenant-a", docB, vB.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatal(err)
	}
	docC, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	searchFixture(t, s, ctx, "tenant-a", docC, "# Guide to Hamsters\n\nHamsters only, no vectors.\n")
	for _, doc := range []string{docA, docB, docC} {
		if _, err := s.ShareDocument(ctx, "tenant-a", doc, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := s.SearchHybrid(ctx, "tenant-a", "guide", "person", "u-anna", hybridOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("fused hits wrong: %+v", hits)
	}
	seen := map[string]int{}
	for i, h := range hits {
		seen[h.DocumentID] = i
		if h.Why == "" {
			t.Fatalf("hit without explanation: %+v", h)
		}
	}
	if seen[docB] != 2 {
		t.Fatalf("body-only match outranked a title match: %+v", hits)
	}
	if _, ok := seen[docC]; !ok {
		t.Fatalf("vector-less document missing from fallback: %+v", hits)
	}
	semOnly, err := s.SearchHybrid(ctx, "tenant-a", "a", "person", "u-anna", hybridOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(semOnly) == 0 || !strings.HasPrefix(semOnly[0].Why, "semantic") {
		t.Fatalf("semantic branch empty: %+v", semOnly)
	}
}

// TestTodo_HUB_027_Golden is the GOLDEN test for HUB-027: fused ranking
// pins byte-for-byte over fixed inputs.
func TestTodo_HUB_027_Golden(t *testing.T) {
	lex := []SearchHit{
		{DocumentID: "doc-b", VersionID: "docv-2", Title: "Other", Score: 4, MatchedTerms: 1},
		{DocumentID: "doc-a", VersionID: "docv-1", Title: "Guide", Score: 9, MatchedTerms: 2},
	}
	sem := []SemanticHit{
		{DocumentID: "doc-c", VersionID: "docv-3", Title: "Third", BlockID: "intro", Cosine: 0.875},
		{DocumentID: "doc-a", VersionID: "docv-1", Title: "Guide", BlockID: "guide-to-cats", Cosine: 0.99},
	}
	assertGoldenJSON(t, "testdata/hub027_hybrid.golden.json", hybridManifest(fuseHybrid(lex, sem, 10, 10, 20)))
}

// BenchmarkTodo_HUB_027 measures fused ranking throughput over synthetic
// lexical and semantic candidates.
func BenchmarkTodo_HUB_027(b *testing.B) {
	var lex []SearchHit
	var sem []SemanticHit
	for i := 0; i < 250; i++ {
		lex = append(lex, SearchHit{DocumentID: "doc-l", VersionID: "docv-l", Title: "Lex", Score: i})
		sem = append(sem, SemanticHit{DocumentID: "doc-s", VersionID: "docv-s", Title: "Sem", BlockID: "b", Cosine: float64(i) / 1000})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fuseHybrid(lex, sem, 10, 10, 20)
	}
}
