package assessment

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var rev005Now = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func rev005Keys() (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	return privateKey.Public().(ed25519.PublicKey), privateKey
}

func rev005Assessment(t *testing.T) (*Assessment, map[string]ed25519.PublicKey) {
	t.Helper()
	publicKey, privateKey := rev005Keys()
	a := &Assessment{
		TenantID: "tenant-acme", ActivityID: "automated-risk-scoring", ActivityVersion: "2026-09", InventoryDigest: "inventory-fixture", Version: 2,
		DecidedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ReviewInterval: 90 * 24 * time.Hour,
		Necessity:       "scoring is necessary to triage pilot support load",
		Proportionality: "only workforce-role fields are scored, never raw identifiers",
		Risks: []Risk{
			{Description: "false positive delays promotion", AffectedSubjects: "employees evaluated for promotion", Likelihood: "medium", Impact: "high", Severity: "high"},
			{Description: "scoring drift across locales", AffectedSubjects: "employees in supported locales", Likelihood: "low", Impact: "medium", Severity: "medium"},
		},
		Mitigations: []Mitigation{
			{Control: "consent management", Reference: "TRUST-024"},
			{Control: "field masks", Reference: "TRUST-010"},
			{Control: "data loss prevention", Reference: "TRUST-018"},
		},
		Reviewer: "privacy-reviewer",
	}
	if err := SignAssessment(a, privateKey); err != nil {
		t.Fatalf("SignAssessment: %v", err)
	}
	return a, map[string]ed25519.PublicKey{"privacy-reviewer": publicKey}
}

// TestTodo_REV_005_02 is the primary: a reviewer signed fresh assessment
// authorizes activation; missing, invalid, stale, mismatched, or incomplete
// evidence blocks activation; low-risk processing needs no assessment.
func TestTodo_REV_005_02(t *testing.T) {
	a, reviewers := rev005Assessment(t)
	for _, profile := range []struct {
		name                            string
		highRisk, automated, restricted bool
	}{
		{name: "high risk", highRisk: true},
		{name: "automated decision", automated: true},
		{name: "restricted fields", restricted: true},
	} {
		t.Run(profile.name, func(t *testing.T) {
			if err := AuthorizeActivation(a, "tenant-acme", "automated-risk-scoring", profile.highRisk, profile.automated, profile.restricted, rev005Now, reviewers); err != nil {
				t.Fatalf("valid assessment blocked: %v", err)
			}
		})
	}
	if err := AuthorizeActivation(nil, "tenant-acme", "automated-risk-scoring", false, true, false, rev005Now, reviewers); !errors.Is(err, ErrAssessmentMissing) {
		t.Errorf("missing assessment err = %v, want ErrAssessmentMissing", err)
	}
	if err := AuthorizeActivation(a, "tenant-acme", "automated-risk-scoring", false, false, false, rev005Now, nil); err != nil {
		t.Errorf("ordinary processing unexpectedly blocked: %v", err)
	}
	unsigned := *a
	unsigned.ReviewerSignature = ""
	if err := AuthorizeActivation(&unsigned, "tenant-acme", "automated-risk-scoring", true, false, false, rev005Now, reviewers); !errors.Is(err, ErrAssessmentUnsigned) {
		t.Errorf("unsigned assessment err = %v, want ErrAssessmentUnsigned", err)
	}
	stale := *a
	stale.DecidedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := AuthorizeActivation(&stale, "tenant-acme", "automated-risk-scoring", false, true, false, rev005Now, reviewers); !errors.Is(err, ErrAssessmentStale) {
		t.Errorf("stale assessment err = %v, want ErrAssessmentStale", err)
	}
	for _, tc := range []struct {
		name, tenant, activity string
	}{
		{name: "tenant", tenant: "tenant-other", activity: "automated-risk-scoring"},
		{name: "activity", tenant: "tenant-acme", activity: "other-activity"},
	} {
		if err := AuthorizeActivation(a, tc.tenant, tc.activity, false, true, false, rev005Now, reviewers); !errors.Is(err, ErrAssessmentMismatch) {
			t.Errorf("cross-%s assessment err = %v, want ErrAssessmentMismatch", tc.name, err)
		}
	}
	incomplete := *a
	incomplete.Necessity = "  "
	if err := AuthorizeActivation(&incomplete, "tenant-acme", "automated-risk-scoring", false, true, false, rev005Now, reviewers); !errors.Is(err, ErrAssessmentInvalid) {
		t.Errorf("incomplete assessment err = %v, want ErrAssessmentInvalid", err)
	}
	tooLong := *a
	tooLong.ReviewInterval = MaximumReviewInterval + time.Second
	if err := AuthorizeActivation(&tooLong, "tenant-acme", "automated-risk-scoring", false, true, false, rev005Now, reviewers); !errors.Is(err, ErrAssessmentInvalid) {
		t.Errorf("unbounded review interval err = %v, want ErrAssessmentInvalid", err)
	}
}

// TestTodo_REV_005_02_Golden pins canonical assessment bytes used by signatures.
func TestTodo_REV_005_02_Golden(t *testing.T) {
	a, _ := rev005Assessment(t)
	want, err := os.ReadFile(filepath.Join("testdata", "rev00502.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := a.CanonicalBytes(); !bytes.Equal(got, bytes.TrimSpace(want)) {
		t.Errorf("canonical bytes mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// TestTodo_REV_005_02_Security proves a reviewer signature binds the full
// assessment, cannot be replaced by an untrusted key, and never crosses tenants.
func TestTodo_REV_005_02_Security(t *testing.T) {
	a, reviewers := rev005Assessment(t)
	if err := AuthorizeActivation(a, "tenant-acme", "automated-risk-scoring", false, true, true, rev005Now, reviewers); err != nil {
		t.Fatalf("control assessment blocked: %v", err)
	}
	changed := *a
	changed.Proportionality = "use every available field"
	if err := AuthorizeActivation(&changed, "tenant-acme", "automated-risk-scoring", false, true, false, rev005Now, reviewers); !errors.Is(err, ErrAssessmentUnsigned) {
		t.Errorf("tampered assessment authorized: %v", err)
	}
	publicKey, _ := rev005Keys()
	if err := AuthorizeActivation(a, "tenant-acme", "automated-risk-scoring", false, false, true, rev005Now, map[string]ed25519.PublicKey{"different-reviewer": publicKey}); !errors.Is(err, ErrAssessmentUnsigned) {
		t.Errorf("untrusted reviewer identity authorized: %v", err)
	}
	if err := AuthorizeActivation(a, "tenant-acme", "automated-risk-scoring", true, false, false, rev005Now.Add(100*24*time.Hour), reviewers); !errors.Is(err, ErrAssessmentStale) {
		t.Errorf("expired assessment authorized: %v", err)
	}
	if err := AuthorizeActivation(a, "tenant-other", "automated-risk-scoring", false, true, false, rev005Now, reviewers); !errors.Is(err, ErrAssessmentMismatch) {
		t.Errorf("cross-tenant assessment authorized: %v", err)
	}
	if err := AuthorizeActivation(a, "tenant-acme", "automated-risk-scoring", false, true, false, rev005Now, nil); !errors.Is(err, ErrAssessmentUnsigned) {
		t.Errorf("missing trusted reviewer registry authorized: %v", err)
	}
}
