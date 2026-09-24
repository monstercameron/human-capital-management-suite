package documenthubstore

import (
	"fmt"
	"testing"
	"time"
)

// meaningFixture builds n candidates whose best section scores follow
// cosines, newest first, so ties never decide the order.
func meaningFixture(titles []string, cosines []float64) ([]searchCandidate, map[string]sectionMatch) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	candidates := make([]searchCandidate, 0, len(titles))
	vectors := map[string]sectionMatch{}
	for i, title := range titles {
		id := fmt.Sprintf("doc-%02d", i)
		markdown := "# " + title + "\n\nBody of " + title + "."
		candidates = append(candidates, searchCandidate{
			id: id, versionID: "v-" + id, title: title, markdown: markdown, updated: base.Add(-time.Duration(i) * time.Hour),
			versionText: (*versionTextCache)(nil).get("", title, markdown),
		})
		vectors[id] = sectionMatch{blockID: "b0", cosine: cosines[i]}
	}
	return candidates, vectors
}

func hitIDs(hits []searchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.id)
	}
	return out
}

func TestMeaningWithinTopKeepsOnlyNearTheBest(t *testing.T) {
	hits := []searchHit{{id: "a", score: 0.7}, {id: "b", score: 0.6}, {id: "c", score: 0.55}, {id: "d", score: 0.3}}
	got := hitIDs(meaningWithinTop(hits, 0.8))
	if fmt.Sprint(got) != "[a b]" {
		t.Fatalf("within 80%% of 0.7 = %v, want [a b]", got)
	}
	if got := meaningWithinTop(nil, 0.8); len(got) != 0 {
		t.Fatalf("empty input = %v", got)
	}
}

// TestMeaningModeDropsLooseMatches is the M13 regression: every section of
// one model scores in a narrow band above the absolute floor, and meaning
// search used to return all of them, a full page. Only documents close to
// the best match remain.
func TestMeaningModeDropsLooseMatches(t *testing.T) {
	titles := make([]string, 0, 60)
	cosines := make([]float64, 0, 60)
	titles, cosines = append(titles, "Leave of absence policy", "Paid time off guide"), append(cosines, 0.72, 0.66)
	for i := 0; i < 58; i++ {
		titles = append(titles, fmt.Sprintf("Unrelated incident review %d", i))
		cosines = append(cosines, 0.40+float64(i%10)/100)
	}
	candidates, vectors := meaningFixture(titles, cosines)
	hits := rankSearch(SearchMeaning, "time away from work", []string{"time", "away", "from", "work"}, candidates, vectors)
	if fmt.Sprint(hitIDs(hits)) != "[doc-00 doc-01]" {
		t.Fatalf("meaning hits = %v, want only the two leave documents", hitIDs(hits))
	}
}

// TestSmartMeaningIsCappedBesideWordMatches: a multi-word title query that
// finds its document by words gains at most a few meaning-only neighbours,
// and only ones scoring close to the best section.
func TestSmartMeaningIsCappedBesideWordMatches(t *testing.T) {
	titles := []string{"Hiring approval workflow", "Offer letter template", "Interview panel guide", "Recruiting budget", "Onboarding checklist", "Relocation support"}
	cosines := []float64{0.80, 0.78, 0.77, 0.76, 0.75, 0.50}
	candidates, vectors := meaningFixture(titles, cosines)
	hits := rankSearch(SearchSmart, "Hiring approval workflow", []string{"hiring", "approval", "workflow"}, candidates, vectors)
	if len(hits) == 0 || hits[0].id != "doc-00" || hits[0].match != MatchTitle {
		t.Fatalf("smart hits = %+v, want the title match first", hits)
	}
	meaningOnly := 0
	for _, h := range hits {
		if h.match == MatchMeaning {
			meaningOnly++
		}
		if h.id == "doc-05" {
			t.Fatalf("a section far below the best (0.50 of 0.80) was kept: %v", hitIDs(hits))
		}
	}
	if meaningOnly > smartMeaningLexicalLimit {
		t.Fatalf("smart kept %d meaning-only hits beside word matches, cap %d: %v", meaningOnly, smartMeaningLexicalLimit, hitIDs(hits))
	}
}
