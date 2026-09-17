package legal

// LEGAL-ST-TX-001 verification tests.
//
// The Texas draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the matrix row matched, the
// preemption-plus-litigation-history shape carried as data — and the
// guardrail that keeps the pack out of evaluation until counsel
// approves it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func texasPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "TX").Candidate()
	if err != nil {
		t.Fatalf("Candidate(TX): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_TX_001 verifies the Texas draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is
// still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_TX_001(t *testing.T) {
	def := loadStateDraft(t, "TX")
	if def.PackID != "us-tx-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Texas draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "TX" {
		t.Fatalf("subdivision=%q, want TX", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := texasPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only (F cell). No state figure above the
	// federal $7.25/hr floor (§ 62.151) may appear.
	for _, floor := range pack.WageFloors {
		if got := floor.FloorAmount.String(); got != "" {
			t.Fatalf("floor=%q, Texas defers to the federal rate", got)
		}
	}
	// PAY_FREQUENCY: semi-monthly for non-exempt, monthly for exempt
	// (§§ 61.011, 61.012).
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	if section := pack.PayFrequencyConstraints[0].Citation.Section; !strings.Contains(section, "61.011") {
		t.Fatalf("section=%q, want §§ 61.011, 61.012", section)
	}
	// FINAL_PAY_DEADLINE: within 6 calendar days on discharge, next
	// regular payday on resignation (§ 61.014).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "6 days") {
		t.Fatalf("deadline=%q, want the 6-day discharge limb carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "61.014") {
		t.Fatalf("section=%q, want § 61.014", finalPay.Citation.Section)
	}
	// NON_COMPETE: ancillary-to-enforceable-agreement requirement
	// (§ 15.50), with the SB 1318 healthcare-practitioner caps —
	// 1-year/5-mile/buyout, eff. 2025-09-01 — carried in the rule.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	rule := pack.NonCompeteThresholds[0].Rule
	if !strings.Contains(rule, "ancillary") {
		t.Fatalf("rule=%q, want the ancillary-agreement requirement", rule)
	}
	if !strings.Contains(rule, "1-year") || !strings.Contains(rule, "5-mile") || !strings.Contains(rule, "buyout") {
		t.Fatalf("rule=%q, want the SB 1318 1-year/5-mile/buyout caps carried", rule)
	}
	if !strings.Contains(rule, "2025-09-01") {
		t.Fatalf("rule=%q, want the SB 1318 effective date carried", rule)
	}
	// PreemptionAssertion: LEAVE_INTERACTION and WAGE_FLOOR preempted at
	// locality level statewide (HB 2127, the preemption-plus-litigation
	// shape: Austin/Dallas/San Antonio ordinances enjoined since
	// 2018-2021). Data for LEGAL-013, never an inline exception.
	if len(pack.PreemptionAssertions) != 2 {
		t.Fatalf("preemption assertions=%d, want LEAVE_INTERACTION and WAGE_FLOOR", len(pack.PreemptionAssertions))
	}
	for _, a := range pack.PreemptionAssertions {
		if a.Scope != "LOCALITY_ONLY" {
			t.Fatalf("preemption scope=%q, want LOCALITY_ONLY", a.Scope)
		}
		if !strings.Contains(a.Citation.Section, "HB 2127") {
			t.Fatalf("section=%q, want HB 2127", a.Citation.Section)
		}
	}
	// BREACH_NOTIFICATION: 60 days to individuals, 30 days to the AG for
	// 250+-resident breaches (§ 521.053).
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if pack.BreachNotifications[0].SubjectDeadlineDays != 60 {
		t.Fatalf("breach=%+v, want the 60-day individual deadline", pack.BreachNotifications[0])
	}
	if note := pack.BreachNotifications[0].Citation.Note; !strings.Contains(note, "30-day") {
		t.Fatalf("note=%q, want the 30-day AG timeline carried", note)
	}
	// ANTI_RETALIATION: workers-compensation retaliation, 2-year
	// limitation (§ 451.001).
	if len(pack.AntiRetaliationRules) != 1 || !hasString(pack.AntiRetaliationRules[0].ProtectedActivities, "workers_compensation_claim") {
		t.Fatalf("anti-retaliation=%+v, want the workers-compensation rule", pack.AntiRetaliationRules)
	}
	if !strings.Contains(pack.AntiRetaliationRules[0].Citation.Section, "451.001") {
		t.Fatalf("section=%q, want § 451.001", pack.AntiRetaliationRules[0].Citation.Section)
	}
	// MONITORING_CONSENT: matrix Y cell (contract § 5.2 prose list:
	// Texas is one of the five named states). Carried once.
	if len(pack.MonitoringConsents) != 1 {
		t.Fatalf("monitoring consents=%d, want the named-state duty", len(pack.MonitoringConsents))
	}
	if !hasString(pack.MonitoringConsents[0].DataCategories, "biometric_identifiers") {
		t.Fatalf("monitoring=%+v, want the biometric category carried", pack.MonitoringConsents[0])
	}
	// PAY_STATEMENT (? with evidence) and DRUG_TESTING (?) each appear
	// once at VERIFY.
	if len(pack.PayStatements) != 1 || pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("pay statements=%+v, want the VERIFY ?-emission", pack.PayStatements)
	}
	if len(pack.DrugTestingRules) != 1 || pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("drug testing=%+v, want the VERIFY ?-emission", pack.DrugTestingRules)
	}
	// F cells stay absent: notices, pay transparency, field
	// restrictions, leave interactions (P: preempted, asserted above),
	// classifications, pay equity, personnel files, mini-warn,
	// e-verify, separation filings (dropped ?), job security,
	// automated decisions. (WAGE_FLOOR is federal-only: no state
	// figure, asserted above.)
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 || len(pack.PayEquityReviews) != 0 ||
		len(pack.PersonnelFileRules) != 0 || len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F (or preempted/dropped) cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_TX_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_TX_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-tx.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "texas.golden.txt"))
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

// TestTodo_LEGAL_ST_TX_001_Conformance checks the Texas matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_TX_001_Conformance(t *testing.T) {
	pack := texasPack(t)
	// Table A Y cells: PAY_FREQUENCY, NON_COMPETE. Table A ? cell with
	// evidence: PAY_STATEMENT. Table A P cell: LEAVE (preempted,
	// asserted, never an obligation). Table B Y cells: FINAL_PAY,
	// ANTI_RETALIATION, BREACH_NOTIFICATION, MONITORING_CONSENT
	// (§ 5.2 prose list). Table B ? cells with evidence: DRUG_TESTING.
	// Table B ? cell without evidence: SEP_FILING (dropped).
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.PayStatements) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 ||
		len(pack.MonitoringConsents) == 0 || len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix Y (or evidenced ?) cell has no pack obligation")
	}
	if len(pack.PreemptionAssertions) == 0 {
		t.Fatal("the HB 2127 preemption assertion is missing")
	}
	// Table A F cells stay absent: notices, pay transparency, field
	// restrictions, wage floor (federal-only), classifications, pay
	// equity, retention, personnel files. Table B F cells stay absent:
	// mini-warn, e-verify, job security, automated decisions.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PayEquityReviews) != 0 || len(pack.RetentionRules) != 0 ||
		len(pack.PersonnelFileRules) != 0 || len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	for _, a := range pack.PreemptionAssertions {
		if a.Scope != "LOCALITY_ONLY" {
			t.Fatalf("preemption scope=%q, want LOCALITY_ONLY", a.Scope)
		}
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_TX_001_Mutation seeds mutants into the Texas
// draft's guards and asserts the loader notices. A surviving mutant
// means the guard is decorative.
func TestTodo_LEGAL_ST_TX_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "TX")
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
