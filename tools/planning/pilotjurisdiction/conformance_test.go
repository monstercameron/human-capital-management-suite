package pilotjurisdiction

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
)

// TestTodo_SELECT_001_Conformance is SELECT-001's named CONFORMANCE test: it
// cross-checks the checked-in profile against the real, live artifacts it
// claims to pin, rather than trusting the profile's own bookkeeping.
func TestTodo_SELECT_001_Conformance(t *testing.T) {
	p := mustLoadProfile(t)
	pack := mustLoadPinnedCaliforniaPack(t)

	t.Run("source content digests match the real files on disk", func(t *testing.T) {
		for _, s := range p.Sources {
			want, err := FileDigest(repoRoot + "/" + s.Path)
			if err != nil {
				t.Fatalf("FileDigest(%s): %v", s.Path, err)
			}
			if s.ContentDigest != want {
				t.Errorf("source %s pins content_digest %s, but the file on disk now hashes to %s - the profile is pinning stale content", s.Path, s.ContentDigest, want)
			}
		}
	})

	t.Run("pinned pack identity matches the RULE_PACK_DEFINITION source", func(t *testing.T) {
		found := false
		for _, s := range p.Sources {
			if s.Kind != "RULE_PACK_DEFINITION" {
				continue
			}
			found = true
			if s.PackID != pack.PackID {
				t.Errorf("source pins pack_id %q, live pack has %q", s.PackID, pack.PackID)
			}
			if s.VersionMajor != pack.Version || s.VersionMinor != pack.MinorVersion {
				t.Errorf("source pins version %d.%d, live pack has %d.%d", s.VersionMajor, s.VersionMinor, pack.Version, pack.MinorVersion)
			}
			if legal.VocabularyVersion(s.VocabularyVersion) != pack.EffectiveVocabulary() {
				t.Errorf("source pins vocabulary_version %d, live pack has %d", s.VocabularyVersion, pack.EffectiveVocabulary())
			}
			if s.ReviewStatus != pack.ReviewStatus.String() {
				t.Errorf("source pins review_status %q, live pack has %q", s.ReviewStatus, pack.ReviewStatus)
			}
		}
		if !found {
			t.Fatal("profile carries no RULE_PACK_DEFINITION source")
		}
	})

	t.Run("obligation mappings equal exactly the pinned pack's populated kinds", func(t *testing.T) {
		liveCounts := pack.KindCounts()
		mapped := map[string]bool{}
		for _, m := range p.ObligationMappings {
			mapped[m.Kind] = true
		}
		for kind, count := range liveCounts {
			if count == 0 {
				continue
			}
			if !mapped[kind.String()] {
				t.Errorf("the pinned pack populates %s (%d obligations) but the profile has no obligation_mappings entry for it", kind, count)
			}
		}
		for kindToken := range mapped {
			parsed, err := legal.ParseObligationType(kindToken)
			if err != nil {
				t.Errorf("obligation_mappings names %q, which is not a known obligation kind", kindToken)
				continue
			}
			if liveCounts[parsed] == 0 {
				t.Errorf("the profile maps %s but the pinned pack populates zero obligations of that kind", kindToken)
			}
		}
	})

	t.Run("exclusions equal exactly the complement of the pinned pack's populated kinds", func(t *testing.T) {
		liveCounts := pack.KindCounts()
		excluded := map[string]bool{}
		for _, e := range p.Exclusions {
			if e.Kind == ExclusionKindObligation {
				excluded[e.Value] = true
			}
		}
		for _, kind := range legal.AllObligationTypes() {
			isPresent := liveCounts[kind] > 0
			isExcluded := excluded[kind.String()]
			if isPresent && isExcluded {
				t.Errorf("obligation kind %s is populated by the pinned pack but the profile excludes it as out of scope", kind)
			}
			if !isPresent && !isExcluded {
				t.Errorf("obligation kind %s is absent from the pinned pack but the profile does not declare it excluded", kind)
			}
		}
	})

	t.Run("the pinned pack is state-level with no locality overlay, matching the LOCALITY exclusion", func(t *testing.T) {
		if pack.Jurisdiction.Locality != "" {
			t.Errorf("pinned pack now carries a locality (%s) - the LOCALITY exclusion is stale", pack.Jurisdiction.Locality)
		}
		hasLocalityExclusion := false
		for _, e := range p.Exclusions {
			if e.Kind == ExclusionKindLocality {
				hasLocalityExclusion = true
			}
		}
		if !hasLocalityExclusion {
			t.Error("profile has no LOCALITY exclusion even though the pinned pack carries no locality overlay")
		}
	})

	t.Run("the scope intent is a live capability-coverage.yaml intent", func(t *testing.T) {
		b, err := os.ReadFile(repoRoot + "/definitions/planning/capability-coverage.yaml")
		if err != nil {
			t.Fatalf("read capability-coverage.yaml: %v", err)
		}
		text := string(b)
		for _, item := range p.ScopeItems {
			base := strings.TrimSuffix(item.IntentID, "/v1")
			needle := "id: " + base + "\n    kind: intent"
			if !strings.Contains(text, needle) {
				t.Errorf("scope item %s (base id %s) is not a live `kind: intent` entry in capability-coverage.yaml", item.IntentID, base)
			}
		}
	})

	t.Run("the scope intent is INCLUDE in PHASE-001's scope ceiling, which named this profile as its jurisdiction slot filler", func(t *testing.T) {
		ceiling, err := scopeceiling.LoadManifest(repoRoot + "/definitions/planning/gates/phase1-scope-ceiling.yaml")
		if err != nil {
			t.Fatalf("scopeceiling.LoadManifest: %v", err)
		}
		foundSlot := false
		for _, slot := range ceiling.SelectionSlots {
			if slot.Name == "jurisdiction" {
				foundSlot = true
				if slot.FillingTodoID != "SELECT-001" {
					t.Errorf("ceiling's jurisdiction slot names filling_todo_id %q, want SELECT-001", slot.FillingTodoID)
				}
				if slot.Filled {
					t.Error("ceiling's jurisdiction slot is filled - PHASE-001 requires it stay empty; this profile fills it externally, not by editing the ceiling")
				}
			}
		}
		if !foundSlot {
			t.Fatal("scope ceiling carries no jurisdiction selection slot")
		}
		for _, item := range p.ScopeItems {
			found := false
			for _, it := range ceiling.Intents {
				if it.ID == item.IntentID {
					found = true
					if it.Disposition != scopeceiling.Include {
						t.Errorf("ceiling intent %s has disposition %q, not INCLUDE", item.IntentID, it.Disposition)
					}
				}
			}
			if !found {
				t.Errorf("scope item %s is ABSENT from the Phase 1 scope ceiling", item.IntentID)
			}
		}
	})
}
