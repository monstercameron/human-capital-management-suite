package scopefidelity

import (
	"os"
	"path/filepath"
	"testing"
)

func rev001Disposition() Disposition {
	return Disposition{
		AuthorizedP1A: map[string]bool{
			"GOV-001": true,
			"GOV-002": true,
			"GOV-003": true,
			"GOV-004": true,
			"GOV-006": true,
			"GOV-009": true,
			"GOV-025": true,
		},
		DeferredCategories: map[string]bool{
			"coverage-matrix":    true,
			"traceability-graph": true,
			"manifest-compiler":  true,
		},
	}
}

func completeExchange(id string) ExchangeRecord {
	return ExchangeRecord{
		ID:                  id,
		Owner:               "backlog-governance-owner",
		ScheduleImpact:      "none: P1B deferral absorbs the work",
		AcceptanceEvidence:  "plancheck scopeexchange green on the exchange record",
		DisplacedScope:      "GOV-0xx deferred to P1B in its place",
		ApprovalDigest:      "sha256:admission",
		ChangedCriticalPath: "none",
	}
}

// TestTodo_REV_001_01 is the REV-001-01 primary: a ticked completion outside
// the authorized P1A set with no signed GOV-006 exchange record must be
// reported; an exchanged item, an authorized item and an unticked item stay
// clean; an incomplete exchange admits nothing.
func TestTodo_REV_001_01(t *testing.T) {
	disp := rev001Disposition()
	completions := []Completion{
		{ID: "GOV-011", Ticked: true, Category: "coverage-matrix", EvidenceDate: "2026-09-03"},
		{ID: "GOV-024", Ticked: true, Category: "traceability-graph", EvidenceDate: "2026-09-05"},
		{ID: "GOV-001", Ticked: true, Category: "delivery-manifest", EvidenceDate: "2026-09-03"},
		{ID: "GOV-030", Ticked: false, Category: "coverage-matrix", EvidenceDate: ""},
	}
	exchanges := []ExchangeRecord{completeExchange("GOV-024")}

	byID := map[string][]Finding{}
	for _, f := range AuditDisposition(disp, completions, exchanges) {
		byID[f.TodoID] = append(byID[f.TodoID], f)
	}

	if len(byID["GOV-011"]) == 0 {
		t.Fatal("GOV-011 undocumented P1A completion not reported")
	}
	for _, f := range byID["GOV-011"] {
		if f.Code != CodeUndocumentedP1ACompletion {
			t.Errorf("GOV-011 code = %q, want %q", f.Code, CodeUndocumentedP1ACompletion)
		}
	}
	for _, id := range []string{"GOV-024", "GOV-001", "GOV-030"} {
		if len(byID[id]) != 0 {
			t.Errorf("%s unexpectedly flagged: %v", id, byID[id])
		}
	}

	incomplete := completeExchange("GOV-011")
	incomplete.DisplacedScope = ""
	if got := AuditDisposition(disp, completions[:1], []ExchangeRecord{incomplete}); len(got) == 0 {
		t.Error("incomplete scope-exchange record admitted GOV-011")
	}
}

// TestTodo_REV_001_01_Golden pins the exact rendered findings bytes for the
// canonical undocumented-completion fixture.
func TestTodo_REV_001_01_Golden(t *testing.T) {
	findings := AuditDisposition(rev001Disposition(), []Completion{
		{ID: "GOV-011", Ticked: true, Category: "coverage-matrix", EvidenceDate: "2026-09-03"},
		{ID: "GOV-001", Ticked: true, Category: "delivery-manifest", EvidenceDate: "2026-09-03"},
	}, nil)
	want, err := os.ReadFile(filepath.Join("testdata", "rev00101.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderFindings(findings); got != string(want) {
		t.Errorf("golden mismatch:\n got: %q\nwant: %q", got, string(want))
	}
}
