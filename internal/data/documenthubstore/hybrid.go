// Hybrid fusion for HUB-027: lexical results keep priority and bounded
// semantic candidates fill the remainder, each hit carrying its reason.
// When vectors lag or the provider fails, keyword results still return;
// when the query has no lexical terms, semantic candidates still return.
// Semantic retrieval reuses the grant prefilter and per-hit recheck, so
// fusion never widens access.
package documenthubstore

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// HybridOptions tunes fusion: which vector model backs the semantic leg,
// how the query is embedded, and the per-leg and total bounds.
type HybridOptions struct {
	ModelID  string
	Embed    func(string) ([]float32, error)
	LexLimit int
	SemLimit int
	Total    int
}

// SemanticHit is one section ranked by cosine similarity.
type SemanticHit struct {
	DocumentID, VersionID, Title, BlockID string
	Cosine                                float64
}

// HybridHit is one fused result with its explanation.
type HybridHit struct {
	DocumentID, VersionID, Title, BlockID string
	LexScore                              int
	MatchedTerms                          int
	SemScore                              float64
	Why                                   string
}

// cosine returns the cosine similarity of two vectors, or zero when
// either side has no magnitude or the dimensions disagree.
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// fuseHybrid merges lexical and semantic candidates: lexical order is
// preserved first, then semantic hits for versions not already present.
// Every leg is bounded and the merged list is bounded again.
func fuseHybrid(lex []SearchHit, sem []SemanticHit, lexLimit, semLimit, total int) []HybridHit {
	if lexLimit <= 0 {
		lexLimit = 10
	}
	if semLimit <= 0 {
		semLimit = 10
	}
	if total <= 0 {
		total = 20
	}
	ranked := append([]SearchHit(nil), lex...)
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].DocumentID < ranked[j].DocumentID
	})
	candidates := append([]SemanticHit(nil), sem...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Cosine > candidates[j].Cosine })
	if len(ranked) > lexLimit {
		ranked = ranked[:lexLimit]
	}
	if len(candidates) > semLimit {
		candidates = candidates[:semLimit]
	}
	var out []HybridHit
	seen := map[[2]string]bool{}
	for _, h := range ranked {
		out = append(out, HybridHit{
			DocumentID: h.DocumentID, VersionID: h.VersionID, Title: h.Title,
			LexScore: h.Score, MatchedTerms: h.MatchedTerms,
			Why: fmt.Sprintf("lexical score %d, %d terms", h.Score, h.MatchedTerms),
		})
		seen[[2]string{h.DocumentID, h.VersionID}] = true
	}
	for _, h := range candidates {
		if seen[[2]string{h.DocumentID, h.VersionID}] {
			continue
		}
		seen[[2]string{h.DocumentID, h.VersionID}] = true
		out = append(out, HybridHit{
			DocumentID: h.DocumentID, VersionID: h.VersionID, Title: h.Title, BlockID: h.BlockID,
			SemScore: h.Cosine, Why: fmt.Sprintf("semantic cosine %.4f, block %s", h.Cosine, h.BlockID),
		})
	}
	if len(out) > total {
		out = out[:total]
	}
	return out
}

// hybridManifest renders fused hits as canonical evidence rows for golden pins.
func hybridManifest(hits []HybridHit) []map[string]any {
	rows := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		rows = append(rows, map[string]any{
			"block": h.BlockID, "doc": h.DocumentID, "lex_score": h.LexScore,
			"matched_terms": h.MatchedTerms, "sem_cosine": h.SemScore, "title": h.Title,
			"version": h.VersionID, "why": h.Why,
		})
	}
	return rows
}

// semanticSearch ranks section vectors against the embedded query across
// deployed versions the reader may read. Candidates are bounded before
// the per-hit grant recheck, so approximate scale never widens access.
// semanticSearch ranks authorized sections through the exact retrieval
// baseline, so fusion inherits its access bounds.
func semanticSearch(ctx context.Context, tx dbport.Tx, tenantID string, qvec []float32, modelID, readerKind, readerID string, limit int) ([]SemanticHit, error) {
	got, err := exactRetrieve(ctx, tx, tenantID, qvec, modelID, readerKind, readerID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]SemanticHit, 0, len(got))
	for _, h := range got {
		out = append(out, SemanticHit{
			DocumentID: h.DocumentID, VersionID: h.VersionID, Title: h.Title,
			BlockID: h.BlockID, Cosine: h.Cosine,
		})
	}
	return out, nil
}

// SearchHybrid fuses lexical ranking with bounded semantic candidates.
// A failed provider call degrades to keyword results instead of an empty
// page; a termless query still returns semantic candidates.
func (s *Store) SearchHybrid(ctx context.Context, tenantID, query, readerKind, readerID string, opts HybridOptions) ([]HybridHit, error) {
	lex, err := s.SearchLexical(ctx, tenantID, query, readerKind, readerID)
	if err != nil {
		return nil, err
	}
	var sem []SemanticHit
	if opts.Embed != nil && opts.ModelID != "" {
		if qvec, err := opts.Embed(query); err == nil {
			err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
				candidates, err := semanticSearch(ctx, tx, tenantID, qvec, opts.ModelID, readerKind, readerID, opts.SemLimit)
				if err != nil {
					return err
				}
				sem = candidates
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return fuseHybrid(lex, sem, opts.LexLimit, opts.SemLimit, opts.Total), nil
}
