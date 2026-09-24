package workflowmaturity

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/designownership"
)

func TestParseCatalogClaimsReadsFlowSectionsExactly(t *testing.T) {
	markdown := `# Catalog

### WF-PEO-003. Change a worker's direct manager

- **Status**: ` + "`EXISTING`" + ` - descriptor hcmnext.people.change_manager
- **Archetype**: ` + "`A2`" + `
- effects:
  - ` + "`hcmnext.people.change_manager/v1`" + ` (REAL, creates)
  - ` + "`hcmnext.work.approve_proposal/v1`" + ` (REAL, advances)

### WF-NEW-001. A brand-new flow

- **Status**: ` + "`NEW`" + `
- effects:
  - ` + "`hcmnext.people.change_manager/v1`" + ` (REAL, creates)

### WF-NODEF-001. No definitions named

- **Status**: ` + "`PARTIAL`" + `
- no definition mentioned anywhere in this section
`

	claims := ParseCatalogClaims(markdown)
	if len(claims) != 2 {
		t.Fatalf("expected 2 claims (WF-NODEF-001 has no definition_ref), got %d: %+v", len(claims), claims)
	}

	first := claims[0]
	if first.FlowID != "WF-NEW-001" {
		t.Fatalf("expected sorted order to start with WF-NEW-001, got %s", first.FlowID)
	}

	var peo *CatalogClaim
	for i := range claims {
		if claims[i].FlowID == "WF-PEO-003" {
			peo = &claims[i]
		}
	}
	if peo == nil {
		t.Fatalf("WF-PEO-003 not parsed: %+v", claims)
	}
	if peo.Status != "EXISTING" {
		t.Fatalf("status = %s, want EXISTING", peo.Status)
	}
	if peo.Title != "Change a worker's direct manager" {
		t.Fatalf("title = %q", peo.Title)
	}
	wantDefs := []string{"hcmnext.people.change_manager/v1", "hcmnext.work.approve_proposal/v1"}
	if len(peo.Definitions) != len(wantDefs) {
		t.Fatalf("definitions = %v, want %v", peo.Definitions, wantDefs)
	}
	for i, d := range wantDefs {
		if peo.Definitions[i] != d {
			t.Fatalf("definitions[%d] = %s, want %s", i, peo.Definitions[i], d)
		}
	}
}

func TestParseCatalogClaimsDedupesRepeatedDefinitionMentions(t *testing.T) {
	markdown := "### WF-DUP-001. Repeats a definition\n\n" +
		"- **Status**: `EXISTING`\n" +
		"- `hcmnext.people.change_manager/v1` (REAL, creates)\n" +
		"- `hcmnext.people.change_manager/v1` (REAL, advances)\n"
	claims := ParseCatalogClaims(markdown)
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(claims))
	}
	if len(claims[0].Definitions) != 1 {
		t.Fatalf("expected deduplicated definitions, got %v", claims[0].Definitions)
	}
}

func TestCrossCheckCatalogFindsDisagreementsDeterministicallyOnce(t *testing.T) {
	report := Report{Results: []DefinitionResult{
		{Definition: "hcmnext.a.a/v1", AllowedStatus: "CATALOGUED", Blockers: []Blocker{{Code: UnboundDesign, Detail: "gap"}}},
		{Definition: "hcmnext.b.b/v1", AllowedStatus: "VERIFIED"},
	}}
	claims := []CatalogClaim{
		{FlowID: "WF-1", Title: "One", Status: "EXISTING", Definitions: []string{"hcmnext.a.a/v1", "hcmnext.b.b/v1"}},
		// A second claim naming the same blocked definition must produce a
		// second, distinct disagreement (one per flow, not deduplicated
		// across flows).
		{FlowID: "WF-2", Title: "Two", Status: "EXISTING", Definitions: []string{"hcmnext.a.a/v1"}},
	}
	got := CrossCheckCatalog(report, claims)
	if len(got) != 2 {
		t.Fatalf("expected 2 disagreements (b.b/v1 is unblocked, WF-1 and WF-2 both name a.a/v1), got %+v", got)
	}
	if got[0].FlowID != "WF-1" || got[1].FlowID != "WF-2" {
		t.Fatalf("expected sorted flow order WF-1,WF-2, got %s,%s", got[0].FlowID, got[1].FlowID)
	}
	for _, d := range got {
		if d.Definition != "hcmnext.a.a/v1" || d.Claimed != "EXISTING" || d.Evidence != "CATALOGUED" {
			t.Fatalf("unexpected disagreement shape: %+v", d)
		}
	}

	// Calling it again must not accumulate duplicates from internal state.
	again := CrossCheckCatalog(report, claims)
	if len(again) != len(got) {
		t.Fatalf("CrossCheckCatalog is not idempotent: %d != %d", len(again), len(got))
	}
}

func TestTodo_REV_053_01(t *testing.T) {
	baseline := CatalogBaseline{SchemaVersion: 1, Status: "REVIEWED", Entries: []CatalogException{{
		FlowID: "WF-PEO-003", Definition: "hcmnext.people.change_manager/v1", Owner: "WF-DISC-009", Reason: "ownership register resolution is pending",
	}}}
	known := CatalogDisagreement{FlowID: "WF-PEO-003", Definition: "hcmnext.people.change_manager/v1"}
	if got := ValidateCatalogBaseline([]CatalogDisagreement{known}, baseline, designownership.Ownership{}); len(got) != 0 {
		t.Fatalf("reviewed owned disagreement rejected: %v", got)
	}
	unassigned := CatalogBaseline{SchemaVersion: 1, Status: "OPEN_UNASSIGNED", Entries: []CatalogException{{
		FlowID: known.FlowID, Definition: known.Definition, Owner: "UNASSIGNED", CandidateRefs: []string{"candidate-1"}, Reason: "ownership is pending",
	}}}
	ownership := designownership.Ownership{Candidates: []designownership.Candidate{{ID: "candidate-1", Owner: "UNASSIGNED", Refs: []string{known.Definition}}}}
	if got := ValidateCatalogBaseline([]CatalogDisagreement{known}, unassigned, ownership); len(got) != 0 {
		t.Fatalf("honest pinned UNASSIGNED mismatch should keep the gate green: %v", got)
	}
	ownership.Candidates[0].Refs = []string{"hcmnext.other.definition/v1"}
	if got := ValidateCatalogBaseline([]CatalogDisagreement{known}, unassigned, ownership); len(got) != 2 {
		t.Fatalf("stale ownership candidate and missing live ref should both be rejected: %v", got)
	}
	unreviewed := CatalogDisagreement{FlowID: "WF-NEW-001", Definition: "hcmnext.new.flow/v1"}
	if got := ValidateCatalogBaseline([]CatalogDisagreement{unreviewed, known}, baseline, designownership.Ownership{}); len(got) != 1 || got[0] != "unreviewed catalog disagreement: WF-NEW-001 hcmnext.new.flow/v1" {
		t.Fatalf("unreviewed disagreement was not rejected exactly: %v", got)
	}
	baseline.Entries[0].Owner = " "
	if got := ValidateCatalogBaseline([]CatalogDisagreement{known}, baseline, designownership.Ownership{}); len(got) != 1 {
		t.Fatalf("ownerless live exception was not rejected: %v", got)
	}
	if _, err := LoadCatalogBaseline(t.TempDir(), "missing.json"); err == nil {
		t.Fatal("missing baseline unexpectedly loaded")
	}
}

func TestTodo_REV_053_01_Golden(t *testing.T) {
	root := repoRoot(t)
	baseline, err := LoadCatalogBaseline(root, DefaultCatalogBaseline)
	if err != nil {
		t.Fatalf("LoadCatalogBaseline: %v", err)
	}
	if baseline.SchemaVersion != 1 || baseline.Status != "OPEN_UNASSIGNED" || len(baseline.Entries) != 30 {
		t.Fatalf("baseline status or count changed: schema=%d status=%q entries=%d", baseline.SchemaVersion, baseline.Status, len(baseline.Entries))
	}
	data, err := os.ReadFile(filepath.Join(root, DefaultCatalogBaseline))
	if err != nil {
		t.Fatalf("read reviewed baseline: %v", err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != "6c49e792d99681fe31da543b7c2479fa8bf3814e3691949b36ad172151e5e2c7" {
		t.Fatalf("baseline bytes changed: sha256=%s", got)
	}
	for _, entry := range baseline.Entries {
		if entry.Owner != "UNASSIGNED" || entry.Reason == "" || len(entry.CandidateRefs) == 0 {
			t.Fatalf("baseline entry must disclose unassigned owner and identify source candidates: %+v", entry)
		}
	}
	if baseline.Entries[0].FlowID != "WF-BEN-003" || baseline.Entries[len(baseline.Entries)-1].FlowID != "WF-WRK-002" {
		t.Fatalf("baseline entries are not pinned in expected sorted order: first=%+v last=%+v", baseline.Entries[0], baseline.Entries[len(baseline.Entries)-1])
	}
}

func TestTodo_REV_053_01_Conformance(t *testing.T) {
	root := repoRoot(t)
	snap, err := LoadSnapshot(root, DefaultIntentCoverageAllowlist)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	claims, err := LoadCatalogClaims(root)
	if err != nil {
		t.Fatalf("LoadCatalogClaims: %v", err)
	}
	baseline, err := LoadCatalogBaseline(root, DefaultCatalogBaseline)
	if err != nil {
		t.Fatalf("LoadCatalogBaseline: %v", err)
	}
	disagreements := CrossCheckCatalog(Reconcile(snap), claims)
	violations := ValidateCatalogBaseline(disagreements, baseline, snap.Ownership)
	if len(violations) != 0 {
		t.Fatalf("the exact 30-item UNASSIGNED baseline should pass while remaining visible: got %d violations: %v", len(violations), violations)
	}
	if len(disagreements) != 30 {
		t.Fatalf("live disagreement count changed; review and update the baseline deliberately: got %d, want 30", len(disagreements))
	}
}
