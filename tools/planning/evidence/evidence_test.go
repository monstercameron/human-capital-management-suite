package evidence

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEvidenceFreshnessRejectsHistoricalClaim is the primary red/green test
// for GOV-008.
func TestEvidenceFreshnessRejectsHistoricalClaim(t *testing.T) {
	t.Run("bare historical claim with no dated evidence is rejected", func(t *testing.T) {
		v := CheckFreshness("X-001", "", "Tests passed previously on the old branch.")
		if len(v) != 1 {
			t.Fatalf("expected exactly one violation, got %v", v)
		}
	})

	t.Run("evidence from a different commit/toolchain than sibling entries is incomplete", func(t *testing.T) {
		// No branch/commit identity recorded at all - this is exactly the
		// "generated from a different commit/toolchain" gap: nothing ties
		// the claim to a specific build.
		v := CheckFreshness("X-002", "2026-09-03", "`TestFoo` in `pkg/x` PASS on windows/arm64 (Go 1.26.3).")
		assertHasIssue(t, v, "missing commit/branch identity")
	})

	t.Run("fully structured evidence has zero violations", func(t *testing.T) {
		v := CheckFreshness("X-003", "2026-09-03", "`TestFoo` in `pkg/x`; `go test -count=1 ./pkg/x/...` PASS on windows/arm64 (Go 1.26.3); branch plan-revision-2026-09-02.")
		if len(v) != 0 {
			t.Errorf("expected zero violations, got %v", v)
		}
	})

	t.Run("missing evidence entirely is rejected", func(t *testing.T) {
		v := CheckFreshness("X-004", "", "")
		if len(v) != 1 || v[0].Issue != "missing Evidence" {
			t.Fatalf("expected a single missing-Evidence violation, got %v", v)
		}
	})

	t.Run("dated but structurally incomplete evidence reports every missing subfield", func(t *testing.T) {
		v := CheckFreshness("X-005", "2026-09-03", "looks fine")
		for _, issue := range []string{"missing toolchain version", "missing environment", "missing command", "missing commit/branch identity"} {
			assertHasIssue(t, v, issue)
		}
	})

	t.Run("ScanEvidenceFields attributes each occurrence to the nearest preceding todo ID", func(t *testing.T) {
		md := "- [x] `A-001` **[P0][LUNA] Do a thing.**\n" +
			"  - **Evidence (2026-09-03):** `TestA` PASS.\n" +
			"- [ ] `A-002` **[P0][LUNA] Do another thing.**\n" +
			"  - **Evidence (partial, 2026-09-03):** `TestB` PASS; not complete.\n"
		occs := ScanEvidenceFields(md)
		if len(occs) != 2 {
			t.Fatalf("expected 2 occurrences, got %d: %v", len(occs), occs)
		}
		if occs[0].ID != "A-001" || occs[0].Label != "2026-09-03" {
			t.Errorf("unexpected first occurrence: %+v", occs[0])
		}
		if occs[1].ID != "A-002" || occs[1].Label != "partial, 2026-09-03" {
			t.Errorf("unexpected second occurrence: %+v", occs[1])
		}
	})

	t.Run("real planning corpus has zero unallowed evidence-freshness gaps", func(t *testing.T) {
		content, err := os.ReadFile(filepath.Join("..", "..", "..", "planning", "archive", "todos-2026-10-22.md"))
		if err != nil {
			t.Fatalf("read todos.md: %v", err)
		}

		var found []Violation
		for _, occ := range ScanEvidenceFields(string(content)) {
			found = append(found, CheckFreshness(occ.ID, occ.Label, occ.Body)...)
		}

		knownGaps, err := LoadKnownGaps(filepath.Join("..", "..", "..", "definitions", "planning", "known-defects.yaml"))
		if err != nil {
			t.Fatalf("LoadKnownGaps: %v", err)
		}

		allowed := make(map[string]KnownGap, len(knownGaps))
		for _, g := range knownGaps {
			allowed[g.ID+"|"+g.Issue] = g
		}

		seenAllowed := make(map[string]bool, len(knownGaps))
		for _, v := range found {
			key := v.ID + "|" + v.Issue
			gap, ok := allowed[key]
			if !ok {
				t.Errorf("unallowed evidence-freshness gap: %s", v)
				continue
			}
			seenAllowed[key] = true

			if gap.Owner == "" || gap.Expiry == "" {
				t.Errorf("known-defects.yaml entry %s (%s) is missing owner or expiry", gap.ID, gap.Issue)
				continue
			}
			expiry, err := time.Parse("2006-01-02", gap.Expiry)
			if err != nil {
				t.Errorf("known-defects.yaml entry %s (%s) has an unparseable expiry %q: %v", gap.ID, gap.Issue, gap.Expiry, err)
				continue
			}
			if time.Now().After(expiry) {
				t.Errorf("known-defects.yaml entry %s (%s) expired on %s; backfill the evidence or renew the entry", gap.ID, gap.Issue, gap.Expiry)
			}
		}

		// A stale allow-list entry (no longer a genuine gap) would silently
		// hide a regression once the underlying evidence is tightened.
		for key, gap := range allowed {
			if !seenAllowed[key] {
				t.Errorf("known-defects.yaml entry %s (%s) is no longer a genuine evidence-freshness gap; remove it", gap.ID, gap.Issue)
			}
		}
	})
}

func assertHasIssue(t *testing.T, violations []Violation, issue string) {
	t.Helper()
	for _, v := range violations {
		if v.Issue == issue {
			return
		}
	}
	t.Errorf("expected a violation with issue %q, got %v", issue, violations)
}

// TestTodo_GOV_008_Golden pins the exact violation format.
func TestTodo_GOV_008_Golden(t *testing.T) {
	v := CheckFreshness("GOLDEN-008", "", "Tests passed previously.")
	if len(v) != 1 {
		t.Fatalf("expected exactly one violation, got %v", v)
	}
	const want = "GOLDEN-008: bare historical claim that tests passed, with no dated commit/toolchain/environment/command/result evidence"
	if got := v[0].String(); got != want {
		t.Errorf("violation message changed:\n got:  %s\n want: %s", got, want)
	}
}
