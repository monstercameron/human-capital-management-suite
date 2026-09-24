package procurement

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant/govauth"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/securityassurance/pentest"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/vpat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

func procurementTestKey() (ed25519.PrivateKey, []byte) {
	private := ed25519.NewKeyFromSeed([]byte(strings.Repeat("p", ed25519.SeedSize)))
	return private, append([]byte(nil), private.Public().(ed25519.PublicKey)...)
}

func procurementFixture(t *testing.T, date time.Time) govauth.GovernmentAuthorizationProfile {
	t.Helper()
	answers := make([]govauth.ProcurementAnswer, 0, len(govauth.QuestionnaireItems))
	for _, question := range govauth.QuestionnaireItems {
		answers = append(answers, govauth.ProcurementAnswer{Question: question, Answer: "Reviewed source evidence.", Owner: "security", Date: date, ArtifactRef: "artifact/" + string(question)})
	}
	profile, err := govauth.NewGovernmentAuthorizationProfile(govauth.GovernmentAuthorizationProfile{
		Revision: 1, TenantID: "tenant-test", IntegrationID: "integration-test",
		ApplicablePrograms: []govauth.Program{govauth.ProgramFISMA}, SystemBoundary: "declared application boundary",
		AssessorStatus: govauth.AssessorAccepted, ReviewDate: date, Evidence: []govauth.EvidencePointer{{ArtifactRef: "artifact/profile", Owner: "security", Date: date}},
		ProcurementAnswers: answers,
	})
	if err != nil {
		t.Fatalf("construct profile fixture: %v", err)
	}
	return profile
}

func currentNoEngagementAnswer(t *testing.T, date time.Time) pentest.ProcurementAnswer {
	t.Helper()
	private, _ := procurementTestKey()
	answer, err := pentest.NoEngagementAnswerSigned(date, private)
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

func TestTodo_REV_099_04(t *testing.T) {
	date := time.Date(2026, 9, 23, 23, 59, 59, 0, time.UTC)
	profile := procurementFixture(t, date)
	_, trustedKeys := procurementTestKey()
	pack, err := GeneratePackTrusted(profile, currentNoEngagementAnswer(t, date), [][]byte{trustedKeys})
	if err != nil {
		t.Fatalf("generate current pack: %v", err)
	}
	if pack.Assurance == nil || pack.Assurance.Accessibility.Version != vpat.ReportVersion || pack.Assurance.Accessibility.Digest == "" {
		t.Fatalf("pack does not pin current VPAT report: %+v", pack.Assurance)
	}
	if _, err := GeneratePack(profile, currentNoEngagementAnswer(t, date)); !errors.Is(err, ErrTrustedSignerRequired) {
		t.Fatalf("legacy adapter did not fail closed: %v", err)
	}
	staleAnswer := currentNoEngagementAnswer(t, date.Add(-24*time.Hour))
	if _, err := GeneratePackFromReportTrusted(profile, staleAnswer, mustCurrentReport(t), [][]byte{trustedKeys}); !errors.Is(err, ErrInvalidPentestAnswer) {
		t.Fatalf("adapter accepted dated answer from a prior assurance date: %v", err)
	}
	if pack.Assurance.PenetrationTest.Status != "no_completed_engagement" || pack.Assurance.PenetrationTest.AnswerDigest == "" {
		t.Fatalf("pack did not preserve typed pentest answer: %+v", pack.Assurance.PenetrationTest)
	}
	accessibilityAnswerFound := false
	for _, item := range pack.Answers {
		if item.Question == govauth.QuestionAccessibility {
			accessibilityAnswerFound = true
			if !strings.HasPrefix(item.Answer, "Interim accessibility evidence report "+vpat.ReportVersion+" SHA-256 ") || strings.Contains(item.Answer, "VPAT") || strings.Contains(item.Answer, "ACR") {
				t.Fatalf("accessibility procurement answer overstates interim evidence: %+v", item)
			}
		}
	}
	if !accessibilityAnswerFound {
		t.Fatal("procurement pack is missing its accessibility evidence answer")
	}
}

func mustCurrentReport(t *testing.T) vpat.Report {
	t.Helper()
	report, err := vpat.LoadCurrentReport()
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestTodo_REV_099_04_EngagementProvenance(t *testing.T) {
	private, public := procurementTestKey()
	trusted := [][]byte{public}
	start := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	e, err := pentest.NewEngagement(pentest.EngagementInput{ID: "buyer-proof-1", Version: 1, Scope: []string{"public API", "tenant boundaries"}, Tester: "Independent Security Labs", StartedAt: start, EndedAt: start.Add(8 * time.Hour), ReportSHA256: strings.Repeat("e", 64), Cadence: 365 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Sign(private); err != nil {
		t.Fatal(err)
	}
	closure, err := pentest.CloseEngagement(e, nil, start.Add(9*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := closure.Sign(private); err != nil {
		t.Fatal(err)
	}
	report, err := vpat.LoadCurrentReport()
	if err != nil {
		t.Fatal(err)
	}
	date := report.Reference().SourceRunAt.UTC()
	answer, err := pentest.AnswerForEngagementTrusted(e, closure, nil, date, trusted, private)
	if err != nil {
		t.Fatal(err)
	}
	profile := procurementFixture(t, date)
	pack, err := GeneratePackFromReportTrusted(profile, answer, report, trusted)
	if err != nil {
		t.Fatalf("signed engagement → procurement pack: %v", err)
	}
	if pack.Assurance.PenetrationTest.Status != "current" || !strings.Contains(pack.Assurance.PenetrationTest.Answer, date.Format("2006-01-02")) || pack.Assurance.PenetrationTest.EvidenceDigest == "" {
		t.Fatalf("pack lost signed engagement answer: %+v", pack.Assurance.PenetrationTest)
	}
	_, otherPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	substitutedEngagement := e
	if err := substitutedEngagement.Sign(otherPrivate); err != nil {
		t.Fatal(err)
	}
	if _, err := pentest.AnswerForEngagementTrusted(substitutedEngagement, closure, nil, date, trusted, private); err == nil {
		t.Fatal("source engagement signed by a substituted key was accepted")
	}
	forged, err := pentest.NoEngagementAnswerSigned(date, otherPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GeneratePackFromReportTrusted(profile, forged, report, trusted); !errors.Is(err, ErrInvalidPentestAnswer) {
		t.Fatalf("substituted signer was accepted: %v", err)
	}
	unsigned, err := pentest.AnswerForEngagement(e, closure, nil, date)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GeneratePackFromReportTrusted(profile, unsigned, report, trusted); !errors.Is(err, ErrInvalidPentestAnswer) {
		t.Fatalf("unsigned caller-built current answer was accepted: %v", err)
	}
}

func TestTodo_REV_099_04_Conformance(t *testing.T) {
	date := time.Date(2026, 9, 23, 23, 59, 59, 0, time.UTC)
	profile := procurementFixture(t, date)
	report, err := vpat.LoadCurrentReport()
	if err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	journal, err := os.ReadFile(filepath.Join(repoRoot, "definitions", "ux", "wcag", "ux-003-run-journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	if err := os.WriteFile(journalPath, journal, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UX003_RUN_JOURNAL", journalPath)
	results := []qual.CriterionResult{{Name: "Next UX-003 run", Pass: true, Detail: "integration test run"}}
	if _, err := wcag.RecordUX003Run(results, results, wcag.Evidence{Todo: "UX-003", Standard: "WCAG 2.2 AA", Artifact: "test", Scenarios: []wcag.Scenario{{ID: "manual", Kind: "manual", Status: "PASS", Method: "integration"}}}, true); err != nil {
		t.Fatalf("record next UX-003 run: %v", err)
	}
	_, trustedKeys := procurementTestKey()
	if _, err := GeneratePackFromReportTrusted(profile, currentNoEngagementAnswer(t, date), report, [][]byte{trustedKeys}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("adapter accepted report made stale by next run: %v", err)
	}
	if err := os.Setenv("UX003_RUN_JOURNAL", ""); err != nil {
		t.Fatal(err)
	}
	profile = procurementFixture(t, date)
	report, err = vpat.LoadCurrentReport()
	if err != nil {
		t.Fatal(err)
	}
	report.Rows[0].Remarks += " tampered"
	if _, err := GeneratePackFromReportTrusted(profile, currentNoEngagementAnswer(t, date), report, [][]byte{trustedKeys}); err == nil {
		t.Fatal("adapter accepted tampered VPAT report")
	}
}

func TestTodo_REV_099_04_Mutation(t *testing.T) {
	date := time.Date(2026, 9, 23, 23, 59, 59, 0, time.UTC)
	profile := procurementFixture(t, date)
	answer := currentNoEngagementAnswer(t, date)
	report, err := vpat.LoadCurrentReport()
	if err != nil {
		t.Fatal(err)
	}
	ref := report.Reference()
	ref.Digest = strings.Repeat("f", 64)
	fabricated := report
	fabricated.Digest = ref.Digest
	_, trustedKeys := procurementTestKey()
	if _, err := GeneratePackFromReportTrusted(profile, answer, fabricated, [][]byte{trustedKeys}); err == nil {
		t.Fatal("adapter accepted fabricated VPAT digest reference")
	}
	answer.Answer = "fabricated pentest status"
	if _, err := GeneratePackFromReportTrusted(profile, answer, report, [][]byte{trustedKeys}); !errors.Is(err, ErrInvalidPentestAnswer) {
		t.Fatalf("adapter accepted modified typed pentest answer: %v", err)
	}
}
