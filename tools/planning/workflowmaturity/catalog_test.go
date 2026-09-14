package workflowmaturity

import "testing"

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
