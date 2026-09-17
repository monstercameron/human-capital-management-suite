package legal

// LEGAL-ST-HI-001 verification tests.
//
// The Hawaii draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the matrix row matched, the
// personnel-file resolution carried as a visible VERIFY ?-emission rather
// than a silent absence — and the guardrail that keeps the pack out of
// evaluation until counsel approves it. They do not, and must not, mark
// the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hawaiiPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "HI").Candidate()
	if err != nil {
		t.Fatalf("Candidate(HI): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_HI_001 verifies the Hawaii draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_HI_001(t *testing.T) {
	def := loadStateDraft(t, "HI")
	if def.PackID != "us-hi-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Hawaii draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "HI" {
		t.Fatalf("subdivision=%q, want HI", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := hawaiiPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $16.00/hr (2026) on a schedule to $18.00/hr (2028),
	// HRS § 387-2.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "16.00 USD" {
		t.Fatalf("floor=%q, want the $16.00/hr floor", got)
	}
	if floor.Basis != "HOURLY" || floor.Indexation != "SCHEDULE" {
		t.Fatalf("floor=%+v, want HOURLY on a SCHEDULE", floor)
	}
	if !strings.Contains(floor.Citation.Section, "387-2") {
		t.Fatalf("section=%q, want HRS § 387-2", floor.Citation.Section)
	}
	if !strings.Contains(floor.Citation.Note, "18.00") {
		t.Fatalf("note=%q, want the scheduled $18.00 step carried", floor.Citation.Note)
	}
	// FIELD_RESTRICTION: salary-history ban, all employers, HRS § 378-2.4.
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "378-2.4") {
		t.Fatalf("section=%q, want HRS § 378-2.4", pack.FieldRestrictions[0].Citation.Section)
	}
	// PAY_TRANSPARENCY: salary-range posting for 50+ employers, HRS
	// § 378-2.3 as amended by Act 203.
	if len(pack.PayTransparencyDuties) != 1 {
		t.Fatalf("pay transparency duties=%d, want one", len(pack.PayTransparencyDuties))
	}
	pt := pack.PayTransparencyDuties[0]
	if pt.Trigger != "internal_promotion" {
		t.Fatalf("trigger=%q, want internal_promotion", pt.Trigger)
	}
	if !strings.Contains(pt.RequiredDisclosure, "50+") {
		t.Fatalf("disclosure=%q, want the 50-employer threshold carried", pt.RequiredDisclosure)
	}
	// PAY_FREQUENCY: semi-monthly, HRS § 388-2.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("pay frequency=%+v, want the semi-monthly floor", pack.PayFrequencyConstraints)
	}
	// FINAL_PAY_DEADLINE: at discharge, or next working day if immediate
	// payment is impossible, HRS § 388-3.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the discharge deadline", pack.FinalPayDeadlines)
	}
	if !strings.Contains(pack.FinalPayDeadlines[0].DeadlineDescription, "next working day") {
		t.Fatalf("deadline=%q, want the next-working-day fallback", pack.FinalPayDeadlines[0].DeadlineDescription)
	}
	// LEAVE_INTERACTION: HFLL 4-week unpaid leave for 100+ employers
	// (? cell carried at VERIFY), plus the TDI wage-replacement note.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	if !strings.Contains(pack.LeaveInteractions[0].InteractionRule, "HFLL") {
		t.Fatalf("leave=%q, want the HFLL rule", pack.LeaveInteractions[0].InteractionRule)
	}
	if pack.LeaveInteractions[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("leave must stay VERIFY: the matrix marks LEAVE uncertain in part")
	}
	// NON_COMPETE: technology-worker exemption and the void-absent-purpose
	// rule (? cell with evidence, carried at VERIFY with the pay-change
	// recheck).
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	if pack.NonCompeteThresholds[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("non-compete must stay VERIFY: no research item states a section")
	}
	// MINI_WARN: 50+-employee 60-day notice plus severance, HRS § 394B.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	mini := pack.MiniWARNTriggers[0]
	if mini.EmployeeThreshold != 50 || mini.NoticeDays != 60 {
		t.Fatalf("mini-warn=%+v, want the 50-employee 60-day trigger", mini)
	}
	if !strings.Contains(mini.Citation.Section, "394B") {
		t.Fatalf("section=%q, want HRS § 394B", mini.Citation.Section)
	}
	// NOTICE (? with evidence) and PAY_STATEMENT (Y) each appear once.
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written-notice ? emission", pack.Notices)
	}
	if !hasString(pack.Notices[0].ContentFields, "pay_rate") {
		t.Fatalf("notice=%+v, want the pay-rate content carried", pack.Notices[0])
	}
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	if !strings.Contains(pack.PayStatements[0].Citation.Section, "388-6") {
		t.Fatalf("section=%q, want HRS § 388-6", pack.PayStatements[0].Citation.Section)
	}
	// PERSONNEL_FILE: the LEGAL-018 resolution — no private-sector
	// statutory right (HRS § 89-16.5 is public-sector-union-only) —
	// carried as a visible VERIFY ?-emission, correctly granting nothing.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want the resolution carried, not dropped", len(pack.PersonnelFileRules))
	}
	if pack.PersonnelFileRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("personnel file must stay VERIFY: there is no private-sector right to confirm")
	}
	if !strings.Contains(pack.PersonnelFileRules[0].Citation.Section, "89-16.5") {
		t.Fatalf("section=%q, want the public-sector-only section cited", pack.PersonnelFileRules[0].Citation.Section)
	}
	// RETENTION, PAY_EQUITY (Y cell held at VERIFY by the research's own
	// "verify exact section number" marker), ANTI_RETALIATION (?),
	// DRUG_TESTING (? with federal-baseline evidence) and BREACH each
	// appear once.
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want one", len(pack.RetentionRules))
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "388-4") {
		t.Fatalf("section=%q, want HRS § 388-4", pack.RetentionRules[0].Citation.Section)
	}
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want the ?-cell emission", len(pack.PayEquityReviews))
	}
	if len(pack.AntiRetaliationRules) != 1 || !hasString(pack.AntiRetaliationRules[0].ProtectedActivities, "whistleblower_report") {
		t.Fatalf("anti-retaliation=%+v, want the whistleblower rule", pack.AntiRetaliationRules)
	}
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the ?-cell emission", len(pack.DrugTestingRules))
	}
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	// F cells stay absent: classifications, e-verify, separation
	// filings, job security, automated decisions, monitoring consents.
	if len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_HI_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_HI_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-hi.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "hawaii.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var digest string
	for _, line := range strings.Split(string(want), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		digest = line
	}
	if got != digest {
		t.Fatalf("draft bytes moved:\n got=%q\nwant=%q", got, digest)
	}
}

// TestTodo_LEGAL_ST_HI_001_Conformance checks the Hawaii matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_HI_001_Conformance(t *testing.T) {
	pack := hawaiiPack(t)
	// Table A Y cells: PAY_TRANSPARENCY, FIELD_RESTRICTION, WAGE_FLOOR,
	// PAY_FREQUENCY, PAY_STATEMENT, LEAVE, NON_COMPETE, PAY_EQUITY,
	// RETENTION. Table B Y cells: FINAL_PAY, MINI_WARN,
	// ANTI_RETALIATION, BREACH.
	if len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 || len(pack.WageFloors) == 0 ||
		len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 || len(pack.LeaveInteractions) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: classifications. Table B F cells stay
	// absent: e-verify, separation filings, job security, automated
	// decisions.
	if len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_HI_001_Mutation seeds mutants into the Hawaii draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_HI_001_Mutation(t *testing.T) {
	mutants := []struct {
		name   string
		mutate func(*PackDefinition)
	}{
		{"schema version drift", func(d *PackDefinition) { d.SchemaVersion = 99 }},
		{"missing pack id", func(d *PackDefinition) { d.PackID = "" }},
		{"unknown jurisdiction level", func(d *PackDefinition) { d.Jurisdiction.Level = "GALAXY" }},
		{"unknown obligation kind", func(d *PackDefinition) { d.Obligations[0].Kind = "VIBES" }},
		{"citation without a section", func(d *PackDefinition) { d.Obligations[0].Citation.Section = "" }},
		{"inverted effective window", func(d *PackDefinition) { d.Window.End = "2025-01-01" }},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			def := loadStateDraft(t, "HI")
			def.Obligations = append([]ObligationJSON(nil), def.Obligations...)
			def.Preemptions = append([]PreemptionJSON(nil), def.Preemptions...)
			m.mutate(&def)
			encoded, err := json.Marshal(def)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			reloaded, err := LoadPackDefinition(encoded)
			if err == nil {
				_, err = reloaded.Candidate()
			}
			if err == nil {
				t.Fatal("mutant survived: the loader accepted it")
			}
		})
	}
}
