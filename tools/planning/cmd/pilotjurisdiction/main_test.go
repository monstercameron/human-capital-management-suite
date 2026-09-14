package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
)

// repoRoot is the path from this command's directory to the repository
// root: tools/planning/cmd/pilotjurisdiction is four levels below root,
// exactly like tools/planning/cmd/scopeceiling.
const repoRoot = "../../../.."

func TestRunReportsTheOneKnownViolationAndAValidSignature(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-profile", repoRoot + "/definitions/planning/gates/select-001-jurisdiction-profile.yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error: the checked-in profile carries exactly one known violation (empty reviewer.name)")
	}
	if !strings.Contains(err.Error(), "1 structural violation") {
		t.Errorf("expected the error to report exactly one structural violation, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "VIOLATION: reviewer.name") {
		t.Errorf("expected stderr to name the reviewer.name violation, got: %s", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"signature verified: true", "jurisdiction: US-CA", "obligation_mappings: 18"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, out)
		}
	}
}

func TestRunFailsOnUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-this-flag-does-not-exist"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected a flag-parse error, got nil")
	}
}

// caPresentTestKinds mirrors tools/planning/pilotjurisdiction's own
// caPresentKinds fixture set: the obligation kinds a complete, valid
// profile maps rather than excludes. It is duplicated here (not imported)
// because it is unexported test-only fixture data in that package.
var caPresentTestKinds = map[string]bool{
	"NOTICE": true, "FIELD_RESTRICTION": true, "RETENTION": true, "LEAVE_INTERACTION": true,
	"PAY_FREQUENCY": true, "FINAL_PAY_DEADLINE": true, "PAY_TRANSPARENCY": true, "NON_COMPETE": true,
	"MINI_WARN": true, "WAGE_FLOOR": true, "PAY_EQUITY_REVIEW": true, "PAY_STATEMENT": true,
	"CLASSIFICATION": true, "PERSONNEL_FILE": true, "ANTI_RETALIATION": true, "DRUG_TESTING": true,
	"BREACH_NOTIFICATION": true, "MONITORING_CONSENT": true,
}

// writeCompleteZeroViolationProfile writes a structurally complete profile
// (every RED element present, every obligation kind mapped or excluded) to
// a temp file with a deliberately invalid signature, so run() reaches the
// "signature did not verify" branch instead of returning early on a
// structural violation.
func writeCompleteZeroViolationProfile(t *testing.T) string {
	t.Helper()
	p := pilotjurisdiction.JurisdictionProfile{
		SchemaVersion: 1, TodoID: "SELECT-001", SignedDate: "2026-09-13",
		Jurisdiction: pilotjurisdiction.JurisdictionRef{Country: "US", State: "CA"},
		ReviewStatus: "UNREVIEWED",
		Sources: []pilotjurisdiction.SourcePin{
			{Kind: "RESEARCH_MEMO", Path: "fixture.md", ReviewStatus: "UNREVIEWED", ContentDigest: "d1"},
			{Kind: "RULE_PACK_DEFINITION", Path: "fixture.json", PackID: "fixture-pack", VersionMajor: 1, VocabularyVersion: 2, ReviewStatus: "UNREVIEWED", ContentDigest: "d2"},
		},
		Reviewer:   pilotjurisdiction.Reviewer{Name: "Fixture Reviewer", Qualification: "fixture"},
		ScopeItems: []pilotjurisdiction.ScopeItem{{IntentID: "hcmnext.people.promote_worker/v1", Disposition: "INCLUDE", Rationale: "fixture"}},
		Window:     pilotjurisdiction.EffectiveWindow{EffectiveStart: "2026-01-01", KnownAtStart: "2026-09-13T00:00:00Z"},
		Collective: pilotjurisdiction.CollectiveInteraction{CBAAssumption: "fixture", MultiEntityHandling: "fixture"},
		Uncertainty: pilotjurisdiction.UncertaintyPolicy{
			AmbiguousInputStatus: pilotjurisdiction.UncertaintyHumanReviewRequired,
			OutOfScopeStatus:     pilotjurisdiction.UncertaintyUnknown,
		},
		UpdateSLA:              pilotjurisdiction.UpdateSLA{ReviewCadenceDays: 90, TriggerEvents: []string{"X"}},
		LegalAdviceDisclaimer:  "this is not legal advice",
		StopReselectThresholds: []pilotjurisdiction.StopReselectThreshold{{Metric: "m", Threshold: "t", Action: pilotjurisdiction.ActionProceed}},
		Signature: &pilotjurisdiction.Signature{
			Algorithm: "ed25519",
			PublicKey: strings.Repeat("00", 32),
			Value:     "00",
		},
	}
	for _, k := range legal.AllObligationTypes() {
		if caPresentTestKinds[k.String()] {
			p.ObligationMappings = append(p.ObligationMappings, pilotjurisdiction.ObligationMapping{
				Kind: k.String(), IntentID: "hcmnext.people.promote_worker/v1", EvidencePath: "legal.AppliedObligation.Bindings",
			})
		} else {
			p.Exclusions = append(p.Exclusions, pilotjurisdiction.Exclusion{Kind: pilotjurisdiction.ExclusionKindObligation, Value: k.String(), Reason: "fixture"})
		}
	}
	p.Exclusions = append(p.Exclusions,
		pilotjurisdiction.Exclusion{Kind: pilotjurisdiction.ExclusionKindJurisdiction, Value: "other", Reason: "fixture"},
		pilotjurisdiction.Exclusion{Kind: pilotjurisdiction.ExclusionKindLocality, Value: "other", Reason: "fixture"},
	)

	b, err := yaml.Marshal(p)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestRunReportsSignatureDidNotVerify(t *testing.T) {
	path := writeCompleteZeroViolationProfile(t)
	var stdout, stderr bytes.Buffer
	err := run([]string{"-profile", path}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error: the fixture profile's signature is deliberately invalid")
	}
	if !strings.Contains(err.Error(), "signature did not verify") {
		t.Errorf("expected a signature-did-not-verify error, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "SIGNATURE INVALID") {
		t.Errorf("expected stderr to report SIGNATURE INVALID, got: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "signature verified: false") {
		t.Errorf("expected stdout to report signature verified: false, got: %s", stdout.String())
	}
}

func TestRunFailsOnMissingProfile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-profile", repoRoot + "/definitions/planning/gates/does-not-exist.yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error for a missing profile, got nil")
	}
	if !strings.Contains(err.Error(), "loading") {
		t.Errorf("expected a loading error, got: %v", err)
	}
}
