package wcag

import "testing"

func completeEvidence() Evidence {
	return Evidence{
		Todo: "UX-003", Standard: "WCAG 2.2 AA", Artifact: "tools/uxqual/wcag",
		Scenarios: []Scenario{
			{ID: "zoom-200", Kind: "manual", Status: "PASS", Method: "Chromium run 1"},
			{ID: "reflow-400", Kind: "manual", Status: "PASS", Method: "Chromium run 1"},
			{ID: "reduced-motion", Kind: "manual", Status: "PASS", Method: "Chromium run 1"},
			{ID: "accessible-auth", Kind: "manual", Status: "PASS", Method: "AT run 1"},
		},
	}
}

func TestEvidenceValidationRejectsDuplicateAndUnsafeRecords(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Evidence)
	}{
		{"duplicate scenario", func(e *Evidence) { e.Scenarios = append(e.Scenarios, e.Scenarios[0]) }},
		{"unsafe artifact", func(e *Evidence) { e.Artifact = "../release" }},
		{"duplicate waiver", func(e *Evidence) {
			e.Waivers = []Waiver{{ID: "w1", Criterion: "reflow", Owner: "team", Severity: "low", Workaround: "manual route", Expires: "2099-01-01", ApprovedBy: "owner"}, {ID: "w1", Criterion: "focus", Owner: "team", Severity: "low", Workaround: "manual route", Expires: "2099-01-01", ApprovedBy: "owner"}}
		}},
		{"invalid waiver expiry", func(e *Evidence) {
			e.Waivers = []Waiver{{ID: "w1", Criterion: "reflow", Owner: "team", Severity: "low", Workaround: "manual route", Expires: "tomorrow", ApprovedBy: "owner"}}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := completeEvidence()
			tc.edit(&e)
			if err := e.Validate(); err == nil {
				t.Fatal("invalid evidence was accepted")
			}
		})
	}
}

func TestEvidenceReleaseReadyRejectsExpiredWaiver(t *testing.T) {
	e := completeEvidence()
	e.Waivers = []Waiver{{ID: "w1", Criterion: "reflow", Owner: "team", Severity: "low", Workaround: "manual route", Expires: "2000-01-01", ApprovedBy: "owner"}}
	if err := e.ReleaseReady(); err == nil {
		t.Fatal("expired waiver was accepted as release-ready")
	}
}
