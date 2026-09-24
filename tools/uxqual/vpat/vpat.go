// Package vpat generates a versioned, digest-pinned accessibility evidence
// report from the UX-003 WCAG scorecard.
package vpat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

const (
	ReportVersion  = "wcag22-aa-v1"
	Standard       = "WCAG 2.2 Level A and AA"
	StandardURI    = "https://www.w3.org/TR/WCAG22/"
	VPATEdition    = "VPAT 2.5Rev WCAG Edition (April 2025)"
	Supports       = "Supports"
	Partial        = "Partially Supports"
	DoesNotSupport = "Does Not Support"
	NotApplicable  = "Not Applicable"
	NotAssessed    = "Not Assessed"
)

// Criterion is one normative WCAG 2.2 A/AA success criterion. Catalog is
// transcribed from the W3C WCAG 2.2 Recommendation; it intentionally excludes
// AAA criteria and the obsolete 4.1.1 criterion.
type Criterion struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Level string `json:"level"`
}

// Catalog returns the full normative WCAG 2.2 A/AA set in numeric order.
func Catalog() []Criterion {
	return []Criterion{
		{"1.1.1", "Non-text Content", "A"},
		{"1.2.1", "Audio-only and Video-only (Prerecorded)", "A"},
		{"1.2.2", "Captions (Prerecorded)", "A"},
		{"1.2.3", "Audio Description or Media Alternative (Prerecorded)", "A"},
		{"1.2.4", "Captions (Live)", "AA"},
		{"1.2.5", "Audio Description (Prerecorded)", "AA"},
		{"1.3.1", "Info and Relationships", "A"},
		{"1.3.2", "Meaningful Sequence", "A"},
		{"1.3.3", "Sensory Characteristics", "A"},
		{"1.3.4", "Orientation", "AA"},
		{"1.3.5", "Identify Input Purpose", "AA"},
		{"1.4.1", "Use of Color", "A"},
		{"1.4.2", "Audio Control", "A"},
		{"1.4.3", "Contrast (Minimum)", "AA"},
		{"1.4.4", "Resize Text", "AA"},
		{"1.4.5", "Images of Text", "AA"},
		{"1.4.10", "Reflow", "AA"},
		{"1.4.11", "Non-text Contrast", "AA"},
		{"1.4.12", "Text Spacing", "AA"},
		{"1.4.13", "Content on Hover or Focus", "AA"},
		{"2.1.1", "Keyboard", "A"},
		{"2.1.2", "No Keyboard Trap", "A"},
		{"2.1.4", "Character Key Shortcuts", "A"},
		{"2.2.1", "Timing Adjustable", "A"},
		{"2.2.2", "Pause, Stop, Hide", "A"},
		{"2.3.1", "Three Flashes or Below Threshold", "A"},
		{"2.4.1", "Bypass Blocks", "A"},
		{"2.4.2", "Page Titled", "A"},
		{"2.4.3", "Focus Order", "A"},
		{"2.4.4", "Link Purpose (In Context)", "A"},
		{"2.4.5", "Multiple Ways", "AA"},
		{"2.4.6", "Headings and Labels", "AA"},
		{"2.4.7", "Focus Visible", "AA"},
		{"2.4.11", "Focus Not Obscured (Minimum)", "AA"},
		{"2.5.1", "Pointer Gestures", "A"},
		{"2.5.2", "Pointer Cancellation", "A"},
		{"2.5.3", "Label in Name", "A"},
		{"2.5.4", "Motion Actuation", "A"},
		{"2.5.7", "Dragging Movements", "AA"},
		{"2.5.8", "Target Size (Minimum)", "AA"},
		{"3.1.1", "Language of Page", "A"},
		{"3.1.2", "Language of Parts", "AA"},
		{"3.2.1", "On Focus", "A"},
		{"3.2.2", "On Input", "A"},
		{"3.2.3", "Consistent Navigation", "AA"},
		{"3.2.4", "Consistent Identification", "AA"},
		{"3.2.6", "Consistent Help", "A"},
		{"3.3.1", "Error Identification", "A"},
		{"3.3.2", "Labels or Instructions", "A"},
		{"3.3.3", "Error Suggestion", "AA"},
		{"3.3.4", "Error Prevention (Legal, Financial, Data)", "AA"},
		{"3.3.7", "Redundant Entry", "A"},
		{"3.3.8", "Accessible Authentication (Minimum)", "AA"},
		{"4.1.2", "Name, Role, Value", "A"},
		{"4.1.3", "Status Messages", "AA"},
	}
}

// CriterionEvidence binds a WCAG tool result to one or more success criteria.
// Criterion IDs are explicit because one broad UX-003 check can cover only a
// subset of a criterion, never an entire conformance claim by itself.
type CriterionEvidence struct {
	CriterionID string `json:"criterion_id"`
	ResultName  string `json:"result_name"`
	Rating      string `json:"rating"`
	Remarks     string `json:"remarks"`
}

// Run identifies the UX-003 result set used to generate the report.
type Run struct {
	ID             string    `json:"id"`
	At             time.Time `json:"at"`
	Sequence       uint64    `json:"sequence"`
	GatePassed     bool      `json:"gate_passed"`
	GateScope      string    `json:"gate_scope"`
	JournalDigest  string    `json:"journal_digest"`
	ResultsDigest  string    `json:"results_digest"`
	EvidenceDigest string    `json:"evidence_digest"`
}

// RunFromRecord converts a verified UX-003 journal record into a report source.
func RunFromRecord(record wcag.RunRecord) Run {
	scope := record.GateScope
	if scope == "" { // legacy journal entry predates the persisted scope field
		scope = wcag.PrimaryGateScope
	}
	return Run{ID: record.ID, At: record.At, Sequence: record.Sequence, GatePassed: record.GatePassed, GateScope: scope, JournalDigest: record.Digest, ResultsDigest: record.ResultsDigest, EvidenceDigest: record.EvidenceDigest}
}

// Row is one ITI VPAT WCAG-table entry.
type Row struct {
	Criterion
	Rating   string   `json:"rating"`
	Remarks  string   `json:"remarks"`
	Evidence []string `json:"evidence,omitempty"`
}

// Report is a versioned evidence report in the VPAT WCAG-edition structure.
// It is not a completed conformance report while Not Assessed rows remain.
// Digest is SHA-256 over canonical JSON with Digest blank.
type Report struct {
	Version       string           `json:"version"`
	Edition       string           `json:"edition"`
	Standard      string           `json:"standard"`
	StandardURI   string           `json:"standard_uri"`
	GeneratedAt   time.Time        `json:"generated_at"`
	SourceRun     Run              `json:"source_run"`
	SourceResults []wcag.RunResult `json:"source_results"`
	Scope         string           `json:"scope"`
	Methodology   string           `json:"methodology"`
	Rows          []Row            `json:"rows"`
	Digest        string           `json:"digest"`
}

var resultCriteria = map[string][]string{
	"200% zoom and 400% reflow":                  {"1.4.4", "1.4.10"},
	"Focus order and accessible names":           {"2.4.3", "4.1.2"},
	"Keyboard-only completion":                   {"2.1.1", "2.1.2"},
	"Screen-reader semantics":                    {"1.3.1", "1.3.2", "3.3.2", "4.1.3"},
	"WCAG 2.2 AA contrast":                       {"1.4.3"},
	"Reflow at 320px":                            {"1.4.10"},
	"Contrast AA":                                {"1.4.3"},
	"Error association (aria-describedby)":       {"3.3.1"},
	"Error association":                          {"3.3.1"},
	"Screen-reader names, states and references": {"1.3.1", "1.3.2", "3.3.2", "4.1.2", "4.1.3"},
	"Visible focus indicator":                    {"2.4.7"},
	"Production focus order and names":           {"2.4.3", "4.1.2"},
	"Locale and direction":                       {"3.1.1"},
}

// GenerateFromRun emits a report directly from one recorded UX-003 run.
func GenerateFromRun(record wcag.RunRecord, explicit []CriterionEvidence) (Report, error) {
	if err := record.Validate(record.PreviousDigest, record.Sequence); err != nil {
		return Report{}, err
	}
	report := generate(record, explicit)
	if err := report.ValidateIntegrity(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func generate(record wcag.RunRecord, explicit []CriterionEvidence) Report {
	run := RunFromRecord(record)
	byID := make(map[string][]CriterionEvidence)
	for _, ev := range explicit {
		byID[ev.CriterionID] = append(byID[ev.CriterionID], ev)
	}
	for _, result := range record.Results {
		for _, id := range resultCriteria[result.Name] {
			rating := NotAssessed
			detail := "Automated " + result.Surface + " check did not assess this criterion in full; criterion-wide conformance is not established."
			if !result.Pass {
				rating = Partial
				detail += " The check found this issue: " + result.Detail
			} else {
				detail += " Check passed: " + result.Detail
			}
			byID[id] = append(byID[id], CriterionEvidence{CriterionID: id, ResultName: result.Surface + " / " + result.Name, Rating: rating, Remarks: detail})
		}
	}
	report := Report{Version: ReportVersion, Edition: VPATEdition, Standard: Standard, StandardURI: StandardURI,
		GeneratedAt: run.At.UTC(), SourceRun: run, SourceResults: append([]wcag.RunResult(nil), record.Results...), Scope: "UX-003 Promotion workspace accessibility qualification surfaces (SSR and GWC fixtures).",
		Methodology: "This is a preliminary evidence report, not a completed Accessibility Conformance Report: per-criterion automated checks are partial evidence, unassessed rows use the report-only Not Assessed marker (not an ITI conformance level), and manual browser and assistive-technology scenarios remain pending in definitions/ux/wcag/ux-003-evidence.yaml. The gate outcome refers only to the selected SSR primary release route; GWC results are separately disclosed. The product-specific authorization projection check is not mapped to WCAG 3.3.8, which addresses accessible authentication."}
	for _, c := range Catalog() {
		row := Row{Criterion: c, Rating: NotAssessed, Remarks: "No per-criterion UX-003 evidence is recorded. Not Assessed is a report-only marker, not an ITI conformance level; complete evaluation before using this report as an Accessibility Conformance Report."}
		evidence := byID[c.ID]
		if len(evidence) != 0 {
			row.Rating, row.Remarks = mergeEvidence(evidence)
			for _, ev := range evidence {
				row.Evidence = append(row.Evidence, ev.ResultName)
			}
			row.Evidence = uniqueSorted(row.Evidence)
		}
		report.Rows = append(report.Rows, row)
	}
	digest, _ := report.DigestValue()
	report.Digest = digest
	return report
}

func mergeEvidence(evidence []CriterionEvidence) (string, string) {
	rating := Supports
	hasPartial, hasNotAssessed, hasNotApplicable := false, false, false
	remarks := make([]string, 0, len(evidence))
	for _, ev := range evidence {
		if ev.Rating == DoesNotSupport {
			rating = DoesNotSupport
		} else if ev.Rating == Partial {
			hasPartial = true
		} else if ev.Rating == NotAssessed {
			hasNotAssessed = true
		} else if ev.Rating == NotApplicable {
			hasNotApplicable = true
		}
		if strings.TrimSpace(ev.Remarks) != "" {
			remarks = append(remarks, ev.ResultName+": "+strings.TrimSpace(ev.Remarks))
		}
	}
	if rating != DoesNotSupport {
		switch {
		case hasPartial:
			rating = Partial
		case hasNotAssessed:
			rating = NotAssessed
		case hasNotApplicable:
			rating = NotApplicable
		}
	}
	if len(remarks) == 0 {
		remarks = append(remarks, "Criterion-level evidence was recorded; see evidence references.")
	}
	return rating, strings.Join(uniqueSorted(remarks), " | ")
}

func uniqueSorted(values []string) []string {
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if value != "" && (len(out) == 0 || out[len(out)-1] != value) {
			out = append(out, value)
		}
	}
	return out
}

func validRating(rating string) bool {
	return rating == Supports || rating == Partial || rating == DoesNotSupport || rating == NotApplicable || rating == NotAssessed
}

// Validate rejects malformed or altered reports and reports built from a
// UX-003 run older than the latest recorded run.
func (r Report) validateAgainst(latest wcag.RunRecord) error {
	if err := r.ValidateIntegrity(); err != nil {
		return err
	}
	if r.SourceRun.Sequence != latest.Sequence || r.SourceRun.ID != latest.ID || !r.SourceRun.At.Equal(latest.At) || r.SourceRun.GatePassed != latest.GatePassed || r.SourceRun.JournalDigest != latest.Digest || r.SourceRun.ResultsDigest != latest.ResultsDigest || r.SourceRun.EvidenceDigest != latest.EvidenceDigest {
		return fmt.Errorf("vpat: report source UX-003 run %s (sequence %d) is stale; latest journal run is %s (sequence %d)", r.SourceRun.ID, r.SourceRun.Sequence, latest.ID, latest.Sequence)
	}
	return nil
}

// ValidateIntegrity checks report identity, full criterion coverage and digest
// without asserting that its source run is still the latest journal entry.
func (r Report) ValidateIntegrity() error {
	if r.Version != ReportVersion || r.Edition != VPATEdition || r.Standard != Standard || r.StandardURI != StandardURI {
		return errors.New("vpat: invalid report identity")
	}
	if len(r.Rows) != len(Catalog()) {
		return fmt.Errorf("vpat: got %d rows, want all %d WCAG 2.2 A/AA criteria", len(r.Rows), len(Catalog()))
	}
	for i, c := range Catalog() {
		row := r.Rows[i]
		if row.ID != c.ID || row.Name != c.Name || row.Level != c.Level {
			return fmt.Errorf("vpat: criterion row %d is missing, reordered, or altered", i)
		}
		if !validRating(row.Rating) || strings.TrimSpace(row.Remarks) == "" {
			return fmt.Errorf("vpat: criterion %s has invalid rating or empty remarks", row.ID)
		}
	}
	if r.SourceRun.ID == "" || r.SourceRun.At.IsZero() || r.SourceRun.At.Location() != time.UTC || r.SourceRun.Sequence == 0 || r.SourceRun.JournalDigest == "" || r.SourceRun.ResultsDigest == "" || r.SourceRun.EvidenceDigest == "" || r.SourceRun.GateScope != "SSR primary release route only" {
		return errors.New("vpat: source UX-003 run identity is required")
	}
	if r.GeneratedAt.IsZero() || r.GeneratedAt.Before(r.SourceRun.At) {
		return errors.New("vpat: invalid generated time")
	}
	resultsDigest, err := wcag.ResultsDigest(r.SourceResults)
	if err != nil {
		return err
	}
	if resultsDigest != r.SourceRun.ResultsDigest {
		return errors.New("vpat: embedded scorecard results do not match source UX-003 run")
	}
	want, err := r.DigestValue()
	if err != nil {
		return err
	}
	if r.Digest != want {
		return errors.New("vpat: digest mismatch")
	}
	return nil
}

// ValidateCurrent loads and verifies the local append-only UX-003 run journal,
// then rejects any report that does not reference its latest execution.
func (r Report) ValidateCurrent() error {
	latest, err := wcag.LatestRunRecord()
	if err != nil {
		return fmt.Errorf("vpat: load current UX-003 run journal: %w", err)
	}
	return r.validateAgainst(latest)
}

// LoadCurrentReport loads the versioned report artifact from this repository
// and verifies its digest and source against the latest UX-003 run journal.
func LoadCurrentReport() (Report, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return Report{}, errors.New("vpat: cannot locate report artifact")
	}
	path := filepath.Join(filepath.Dir(file), "reports", "wcag-2.2-aa-v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return Report{}, fmt.Errorf("vpat: decode current report: %w", err)
	}
	if err := report.ValidateCurrent(); err != nil {
		return Report{}, err
	}
	return report, nil
}

// DigestValue returns the deterministic SHA-256 of the report excluding its
// digest field, making the artifact suitable for versioned procurement refs.
func (r Report) DigestValue() (string, error) {
	r.Digest = ""
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// Reference is the stable procurement reference for a generated report.
type Reference struct {
	Version             string    `json:"version"`
	Digest              string    `json:"sha256"`
	SourceRunID         string    `json:"source_run_id"`
	SourceRunSequence   uint64    `json:"source_run_sequence"`
	SourceRunGatePassed bool      `json:"source_run_gate_passed"`
	SourceRunJournalSHA string    `json:"source_run_journal_sha256"`
	SourceRunAt         time.Time `json:"source_run_at"`
}

func (r Report) Reference() Reference {
	return Reference{Version: r.Version, Digest: r.Digest, SourceRunID: r.SourceRun.ID, SourceRunSequence: r.SourceRun.Sequence, SourceRunGatePassed: r.SourceRun.GatePassed, SourceRunJournalSHA: r.SourceRun.JournalDigest, SourceRunAt: r.SourceRun.At}
}

// ValidateCurrent confirms the reference exactly identifies the checked-in
// report and that this report still matches the latest UX-003 journal entry.
func (ref Reference) ValidateCurrent() error {
	report, err := LoadCurrentReport()
	if err != nil {
		return err
	}
	current := report.Reference()
	if ref.Version != current.Version || ref.Digest != current.Digest || ref.SourceRunID != current.SourceRunID || ref.SourceRunSequence != current.SourceRunSequence || ref.SourceRunGatePassed != current.SourceRunGatePassed || ref.SourceRunJournalSHA != current.SourceRunJournalSHA || !ref.SourceRunAt.Equal(current.SourceRunAt) {
		return errors.New("vpat: procurement reference does not identify the current accessibility report")
	}
	return nil
}

// WriteJSON validates and writes a stable, human-readable report artifact.
func (r Report) WriteJSON(path string) error {
	if err := r.ValidateCurrent(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

// Markdown renders the WCAG edition's per-criterion table for human review.
func (r Report) Markdown() string {
	var out strings.Builder
	gateStatus := "failed"
	if r.SourceRun.GatePassed {
		gateStatus = "passed"
	}
	gwcPassed, gwcTotal, gwcFailures := 0, 0, make([]string, 0)
	for _, result := range r.SourceResults {
		if result.Surface != "GWC" {
			continue
		}
		gwcTotal++
		if result.Pass {
			gwcPassed++
		} else {
			gwcFailures = append(gwcFailures, result.Name)
		}
	}
	gwcStatus := fmt.Sprintf("%d/%d checks passed", gwcPassed, gwcTotal)
	if len(gwcFailures) > 0 {
		gwcStatus += "; findings: " + strings.Join(gwcFailures, ", ")
	}
	fmt.Fprintf(&out, "# Accessibility Evidence Report\n\nEdition: %s  \nVersion: %s  \nStandard: [%s](%s)  \nSource UX-003 run: %s (sequence %d, %s)  \nSSR primary release route gate: %s (%s)  \nGWC auxiliary checks: %s  \nOverall WCAG conformance: Not established  \nRun journal SHA-256: %s  \nReport SHA-256: %s\n\n", r.Edition, r.Version, r.Standard, r.StandardURI, r.SourceRun.ID, r.SourceRun.Sequence, r.SourceRun.At.UTC().Format(time.RFC3339), gateStatus, r.SourceRun.GateScope, gwcStatus, r.SourceRun.JournalDigest, r.Digest)
	fmt.Fprintf(&out, "Scope: %s\n\nMethodology: %s\n\n", r.Scope, r.Methodology)
	out.WriteString("| Success Criterion | Conformance level / assessment status | Remarks |\n| --- | --- | --- |\n")
	for _, row := range r.Rows {
		fmt.Fprintf(&out, "| %s %s (Level %s) | %s | %s |\n", row.ID, row.Name, row.Level, row.Rating, escapeTable(row.Remarks))
	}
	return out.String()
}

// WriteMarkdown validates and writes the human-readable report table.
func (r Report) WriteMarkdown(path string) error {
	if err := r.ValidateCurrent(); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(r.Markdown()), 0o600)
}

func escapeTable(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "|", "\\|"), "\n", " ")
}

// CanonicalReport emits a stable text view used by human review and golden
// tests. It includes all criteria in normative order.
func CanonicalReport(r Report) string {
	lines := make([]string, 0, len(r.Rows))
	for _, row := range r.Rows {
		lines = append(lines, fmt.Sprintf("%s %s (%s) :: %s :: %s", row.ID, row.Name, row.Level, row.Rating, row.Remarks))
	}
	return strings.Join(lines, "\n")
}
