package vpat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

func fixtureResults() []qual.CriterionResult {
	return []qual.CriterionResult{
		{Name: "Keyboard-only completion", Pass: true, Detail: "keyboard path completed in the UX-003 fixture"},
		{Name: "Screen-reader semantics", Pass: true, Detail: "landmarks, labels, and status region present in the fixture"},
		{Name: "Error association (aria-describedby)", Pass: true, Detail: "invalid field is programmatically associated with its error"},
		{Name: "WCAG 2.2 AA contrast", Pass: true, Detail: "all token pairs meet the 4.5:1 threshold"},
		{Name: "Reflow at 320px", Pass: true, Detail: "fixture uses relative widths and reflows at 320 CSS px"},
		{Name: "Focus order and accessible names", Pass: true, Detail: "all fixture controls have accessible names"},
	}
}

func fixtureEvidence() wcag.Evidence {
	return wcag.Evidence{Todo: "UX-003", Standard: "WCAG 2.2 AA", Artifact: "tools/uxqual/wcag", Scenarios: []wcag.Scenario{
		{ID: "zoom-200", Kind: "manual", Status: "PENDING", Method: "test fixture"},
		{ID: "reflow-400", Kind: "manual", Status: "PENDING", Method: "test fixture"},
		{ID: "reduced-motion", Kind: "manual", Status: "PENDING", Method: "test fixture"},
		{ID: "accessible-auth", Kind: "manual", Status: "PENDING", Method: "test fixture"},
	}}
}

func fixtureReport(t *testing.T) (Report, wcag.RunRecord) {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv("UX003_RUN_JOURNAL", journal)
	results := fixtureResults()
	gwcResults := append([]qual.CriterionResult(nil), results...)
	for i := range gwcResults {
		if gwcResults[i].Name == "Focus order and accessible names" {
			gwcResults[i].Pass = false
			gwcResults[i].Detail = "unlabeled control found in GWC fixture"
		}
	}
	gwcResults = append(gwcResults, qual.CriterionResult{Name: "Accessible authorization projection", Pass: false, Detail: "authorized transition missing: approve"})
	record, err := wcag.RecordUX003Run(results, gwcResults, fixtureEvidence(), true)
	if err != nil {
		t.Fatalf("record fixture UX-003 run: %v", err)
	}
	report, err := GenerateFromRun(record, nil)
	if err != nil {
		t.Fatalf("generate report from recorded run: %v", err)
	}
	return report, record
}

func TestTodo_REV_099_04(t *testing.T) {
	report, record := fixtureReport(t)
	if err := report.ValidateCurrent(); err != nil {
		t.Fatalf("generated report did not validate: %v", err)
	}
	if got := len(report.Rows); got != 55 {
		t.Fatalf("row count = %d, want every WCAG 2.2 A/AA criterion (55)", got)
	}
	byID := map[string]Row{}
	for _, row := range report.Rows {
		byID[row.ID] = row
	}
	if byID["2.1.1"].Rating != NotAssessed || byID["2.1.2"].Rating != NotAssessed {
		t.Fatalf("keyboard rows not tied to scorecard: %+v %+v", byID["2.1.1"], byID["2.1.2"])
	}
	if byID["1.4.3"].Rating != NotAssessed || byID["1.4.10"].Rating != NotAssessed {
		t.Fatal("contrast or reflow scorecard evidence was not mapped")
	}
	if byID["2.4.3"].Rating != Partial || byID["2.4.3"].Remarks == "" {
		t.Fatalf("failed check did not produce a reasoned row: %+v", byID["2.4.3"])
	}
	if byID["3.3.1"].Rating != NotAssessed || len(byID["3.3.1"].Evidence) != 2 ||
		byID["3.3.1"].Evidence[0] != "GWC / Error association (aria-describedby)" ||
		byID["3.3.1"].Evidence[1] != "SSR / Error association (aria-describedby)" {
		t.Fatalf("error association evidence must be disclosed without claiming criterion-wide support: %+v", byID["3.3.1"])
	}
	if byID["1.1.1"].Rating != NotAssessed || !strings.Contains(byID["1.1.1"].Remarks, "not an ITI conformance level") {
		t.Fatalf("unknown criterion must disclose lack of assessment without claiming failure: %+v", byID["1.1.1"])
	}
	if byID["3.3.8"].Rating != NotAssessed {
		t.Fatalf("authorization projection must not be misrepresented as WCAG authentication evidence: %+v", byID["3.3.8"])
	}
	if !strings.Contains(report.Markdown(), "GWC auxiliary checks: 5/7 checks passed; findings: Focus order and accessible names, Accessible authorization projection") || !strings.Contains(report.Markdown(), "Overall WCAG conformance: Not established") {
		t.Fatalf("Markdown overstates UX-003 outcome despite GWC finding: %s", report.Markdown())
	}
	if _, exists := byID["4.1.1"]; exists {
		t.Fatal("obsolete WCAG 2.2 criterion 4.1.1 must not appear")
	}
	if report.Digest == "" || len(report.Digest) != 64 {
		t.Fatalf("invalid report digest %q", report.Digest)
	}
	ref := report.Reference()
	if ref.Version != ReportVersion || ref.Digest != report.Digest || ref.SourceRunID != record.ID || ref.SourceRunSequence != record.Sequence || ref.SourceRunJournalSHA != record.Digest {
		t.Fatalf("bad procurement reference: %+v", ref)
	}
	markdown := report.Markdown()
	if !strings.Contains(markdown, "| Success Criterion | Conformance level / assessment status | Remarks |") || strings.Count(markdown, "(Level ") != 55 {
		t.Fatal("human-readable report is missing the per-criterion table")
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := report.WriteJSON(path); err != nil {
		t.Fatalf("write JSON report: %v", err)
	}
	newerResults := fixtureResults()
	newerResults[0].Detail += " newer test run"
	_, err := wcag.RecordUX003Run(newerResults, newerResults, fixtureEvidence(), true)
	if err != nil {
		t.Fatalf("append subsequent UX-003 run: %v", err)
	}
	if err := report.WriteJSON(path); err == nil || !strings.Contains(err.Error(), "is stale") {
		t.Fatalf("writer accepted stale report after subsequent UX-003 run: %v", err)
	}
}

func TestTodo_REV_099_04_Golden(t *testing.T) {
	report, _ := fixtureReport(t)
	got := CanonicalReport(report)
	path := filepath.Join("testdata", "vpat_report.golden")
	if os.Getenv("UPDATE_VPAT_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden report: %v", err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(string(want)) {
		t.Fatalf("WCAG report differs from golden; regenerate intentionally with UPDATE_VPAT_GOLDEN=1")
	}
}

func TestTodo_REV_099_04_Conformance(t *testing.T) {
	catalog := Catalog()
	if len(catalog) != 55 {
		t.Fatalf("catalog has %d criteria, want 55", len(catalog))
	}
	wantIDs := "1.1.1 1.2.1 1.2.2 1.2.3 1.2.4 1.2.5 1.3.1 1.3.2 1.3.3 1.3.4 1.3.5 1.4.1 1.4.2 1.4.3 1.4.4 1.4.5 1.4.10 1.4.11 1.4.12 1.4.13 2.1.1 2.1.2 2.1.4 2.2.1 2.2.2 2.3.1 2.4.1 2.4.2 2.4.3 2.4.4 2.4.5 2.4.6 2.4.7 2.4.11 2.5.1 2.5.2 2.5.3 2.5.4 2.5.7 2.5.8 3.1.1 3.1.2 3.2.1 3.2.2 3.2.3 3.2.4 3.2.6 3.3.1 3.3.2 3.3.3 3.3.4 3.3.7 3.3.8 4.1.2 4.1.3"
	seen := map[string]bool{}
	ids := make([]string, 0, len(catalog))
	for _, c := range catalog {
		if c.ID == "" || c.Name == "" || (c.Level != "A" && c.Level != "AA") {
			t.Fatalf("invalid catalog entry: %+v", c)
		}
		if seen[c.ID] {
			t.Fatalf("duplicate criterion %s", c.ID)
		}
		seen[c.ID] = true
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, " ") != wantIDs {
		t.Fatalf("catalog ID/order mismatch:\n got %s\nwant %s", strings.Join(ids, " "), wantIDs)
	}
	report, _ := fixtureReport(t)
	for _, row := range report.Rows {
		if !validRating(row.Rating) {
			t.Errorf("criterion %s has non-ITI rating %q", row.ID, row.Rating)
		}
	}
}

func TestTodo_REV_099_04_Mutation(t *testing.T) {
	mutated, _ := fixtureReport(t)
	newerResults := fixtureResults()
	newerResults[0].Detail += " later"
	_, err := wcag.RecordUX003Run(newerResults, newerResults, fixtureEvidence(), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := mutated.ValidateCurrent(); err == nil || !strings.Contains(err.Error(), "is stale") {
		t.Fatalf("stale report was accepted: %v", err)
	}
	mutated.Rows[0].Remarks = "tampered"
	if err := mutated.ValidateIntegrity(); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered report was accepted: %v", err)
	}
	mutated, _ = fixtureReport(t)
	mutated.Rows[0].ID = "4.1.1"
	if err := mutated.ValidateIntegrity(); err == nil || !strings.Contains(err.Error(), "altered") {
		t.Fatalf("catalog mutation was accepted: %v", err)
	}
	mutated, _ = fixtureReport(t)
	mutated.Rows[0].Rating = "Not Evaluated"
	mutated.Digest, _ = mutated.DigestValue()
	if err := mutated.ValidateIntegrity(); err == nil || !strings.Contains(err.Error(), "invalid rating") {
		t.Fatalf("non-ITI status was accepted: %v", err)
	}
	mutated, _ = fixtureReport(t)
	mutated.SourceResults[0].Detail += " substituted result"
	mutated.Digest, _ = mutated.DigestValue()
	if err := mutated.ValidateIntegrity(); err == nil || !strings.Contains(err.Error(), "do not match source UX-003 run") {
		t.Fatalf("unrelated caller results were accepted: %v", err)
	}
}

func TestGeneratedArtifactDigest(t *testing.T) {
	path := filepath.Join("reports", "wcag-2.2-aa-v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated report artifact: %v", err)
	}
	var report Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode report artifact: %v", err)
	}
	if err := report.ValidateIntegrity(); err != nil {
		t.Fatalf("generated report artifact is invalid: %v", err)
	}
	rows := map[string]Row{}
	for _, row := range report.Rows {
		rows[row.ID] = row
	}
	if rows["3.3.8"].Rating != NotAssessed {
		t.Fatalf("checked-in report incorrectly uses authorization as WCAG 3.3.8 evidence: %+v", rows["3.3.8"])
	}
	markdownCurrent, err := os.ReadFile(filepath.Join("reports", "wcag-2.2-aa-v1.md"))
	if err != nil {
		t.Fatalf("read current Markdown report: %v", err)
	}
	if !strings.Contains(string(markdownCurrent), "SSR primary release route gate: passed (SSR primary release route only)") || !strings.Contains(string(markdownCurrent), "GWC auxiliary checks: 8/9 checks passed; findings: Accessible authorization projection") || !strings.Contains(string(markdownCurrent), "Overall WCAG conformance: Not established") || !strings.Contains(string(markdownCurrent), "not mapped to WCAG 3.3.8") {
		t.Fatal("checked-in Markdown report does not distinguish SSR gate outcome from GWC findings")
	}

	// A clean checkout has the checked-in journal but no local cache. Recreate
	// exactly the journal prefix pinned by this artifact and validate it there.
	journalPath := filepath.Join("..", "..", "..", "definitions", "ux", "wcag", "ux-003-run-journal.jsonl")
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read governed UX-003 journal: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(journal)), "\n")
	var pinned []string
	for _, line := range lines {
		var record wcag.RunRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode governed UX-003 journal: %v", err)
		}
		if record.Sequence > report.SourceRun.Sequence {
			break
		}
		pinned = append(pinned, line)
	}
	if uint64(len(pinned)) != report.SourceRun.Sequence {
		t.Fatalf("journal does not contain report source sequence %d", report.SourceRun.Sequence)
	}
	cleanJournal := filepath.Join(t.TempDir(), "ux003-run-journal.jsonl")
	if err := os.WriteFile(cleanJournal, []byte(strings.Join(pinned, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UX003_RUN_JOURNAL", cleanJournal)
	if err := report.ValidateCurrent(); err != nil {
		t.Fatalf("report failed clean-checkout journal validation: %v", err)
	}
	t.Setenv("UX003_RUN_JOURNAL", filepath.Join(t.TempDir(), "missing.jsonl"))
	if err := report.ValidateCurrent(); err == nil {
		t.Fatal("report validated without its UX-003 run journal")
	}
	t.Setenv("UX003_RUN_JOURNAL", "")
	markdown, err := os.ReadFile(filepath.Join("reports", "wcag-2.2-aa-v1.md"))
	if err != nil {
		t.Fatalf("read Markdown report artifact: %v", err)
	}
	if !strings.Contains(string(markdown), report.Digest) || strings.Count(string(markdown), "(Level ") != 55 {
		t.Fatal("Markdown report does not match the pinned complete report")
	}
}
