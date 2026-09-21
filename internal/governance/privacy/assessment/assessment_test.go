package assessment

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rev005Assessment() *Assessment {
	return &Assessment{
		TenantID:        "tenant-acme",
		ActivityID:      "automated-risk-scoring",
		Version:         2,
		DecidedAt:       time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		ReviewInterval:  90 * 24 * time.Hour,
		Necessity:       "scoring is necessary to triage pilot support load",
		Proportionality: "only workforce-role fields are scored, never raw identifiers",
		Risks: []Risk{
			{Description: "false positive flag delays a legitimate promotion", Severity: "medium"},
			{Description: "scoring drift across locales", Severity: "low"},
		},
		Mitigations: []Mitigation{
			{Control: "consent", Reference: "TRUST-024"},
			{Control: "field-mask", Reference: "TRUST-010"},
			{Control: "dlp", Reference: "TRUST-018"},
		},
		Reviewer:          "privacy-reviewer",
		ReviewerSignature: "ed25519:reviewer-signature",
	}
}

var rev005Now = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

// TestTodo_REV_005_02 is the REV-005-02 primary: a signed fresh assessment
// authorizes activation; missing, unsigned, stale, mismatched and malformed
// assessments block it with typed errors; low-risk processing needs none.
func TestTodo_REV_005_02(t *testing.T) {
	if err := AuthorizeActivation(rev005Assessment(), "tenant-acme", "automated-risk-scoring", true, false, rev005Now); err != nil {
		t.Errorf("valid assessment blocked: %v", err)
	}
	if err := AuthorizeActivation(rev005Assessment(), "tenant-acme", "automated-risk-scoring", false, true, rev005Now); err != nil {
		t.Errorf("valid assessment blocked for restricted processing: %v", err)
	}
	if err := AuthorizeActivation(nil, "tenant-acme", "automated-risk-scoring", true, false, rev005Now); !errors.Is(err, ErrAssessmentMissing) {
		t.Errorf("missing assessment err = %v, want ErrAssessmentMissing", err)
	}

	unsigned := rev005Assessment()
	unsigned.ReviewerSignature = "  "
	if err := AuthorizeActivation(unsigned, "tenant-acme", "automated-risk-scoring", true, false, rev005Now); !errors.Is(err, ErrAssessmentUnsigned) {
		t.Errorf("unsigned assessment err = %v, want ErrAssessmentUnsigned", err)
	}

	stale := rev005Assessment()
	stale.DecidedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := AuthorizeActivation(stale, "tenant-acme", "automated-risk-scoring", true, false, rev005Now); !errors.Is(err, ErrAssessmentStale) {
		t.Errorf("stale assessment err = %v, want ErrAssessmentStale", err)
	}

	if err := AuthorizeActivation(rev005Assessment(), "tenant-other", "automated-risk-scoring", true, false, rev005Now); !errors.Is(err, ErrAssessmentMismatch) {
		t.Errorf("cross-tenant assessment err = %v, want ErrAssessmentMismatch", err)
	}
	if err := AuthorizeActivation(rev005Assessment(), "tenant-acme", "other-activity", true, false, rev005Now); !errors.Is(err, ErrAssessmentMismatch) {
		t.Errorf("cross-activity assessment err = %v, want ErrAssessmentMismatch", err)
	}

	malformed := rev005Assessment()
	malformed.Version = 0
	if err := AuthorizeActivation(malformed, "tenant-acme", "automated-risk-scoring", true, false, rev005Now); !errors.Is(err, ErrAssessmentInvalid) {
		t.Errorf("malformed assessment err = %v, want ErrAssessmentInvalid", err)
	}

	if err := AuthorizeActivation(nil, "tenant-acme", "routine-report", false, false, rev005Now); err != nil {
		t.Errorf("low-risk processing blocked without assessment: %v", err)
	}
}

// TestTodo_REV_005_02_Golden pins the canonical assessment bytes.
func TestTodo_REV_005_02_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "rev00502.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := rev005Assessment().Canonical(); got != string(want) {
		t.Errorf("golden mismatch:\n got: %q\nwant: %q", got, string(want))
	}
}

// TestTodo_REV_005_02_Security proves assessments never cross tenants and
// that a stripped signature is indistinguishable from unsigned.
func TestTodo_REV_005_02_Security(t *testing.T) {
	victim := rev005Assessment()
	if err := AuthorizeActivation(victim, "tenant-acme", "automated-risk-scoring", true, false, rev005Now); err != nil {
		t.Fatalf("control assessment blocked: %v", err)
	}
	if err := AuthorizeActivation(victim, "tenant-acme", "automated-risk-scoring", true, true, rev005Now.Add(100*24*time.Hour)); !errors.Is(err, ErrAssessmentStale) {
		t.Errorf("expired assessment still authorizes: %v", err)
	}
	stripped := rev005Assessment()
	stripped.ReviewerSignature = ""
	if err := AuthorizeActivation(stripped, "tenant-acme", "automated-risk-scoring", false, true, rev005Now); !errors.Is(err, ErrAssessmentUnsigned) {
		t.Errorf("stripped signature authorized restricted processing: %v", err)
	}
}
