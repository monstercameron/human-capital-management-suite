package releaseadmission_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/releaseadmission"
)

// Re-pinned: sbom.cdx.json was regenerated intentionally (mongo driver dependencies added).
const checkedInSBOMDigest = "ef2d7b5063e2815ed9f224216a2605dccf6fd713400fa8c57a4639ba45e676c4"

func signedFixture(t *testing.T) provenance.Statement {
	t.Helper()
	root := repopath.RootDir()
	data, err := os.ReadFile(filepath.Join(root, "definitions", "supply-chain", "provenance.json"))
	if err != nil {
		t.Fatalf("read provenance fixture: %v", err)
	}
	var statement provenance.Statement
	if err := json.Unmarshal(data, &statement); err != nil {
		t.Fatalf("decode provenance fixture: %v", err)
	}
	return statement
}

func policyFor(t *testing.T) releaseadmission.Policy {
	t.Helper()
	statement := signedFixture(t)
	return releaseadmission.Policy{
		TrustedPublicKeys: map[string]bool{statement.Signature.PublicKey: true},
		AllowedBuilders:   map[string]bool{statement.Builder.ID: true},
		SBOMDigest:        checkedInSBOMDigest,
	}
}

// TestCosignAdmissionRejectsWrongSubjectIdentityIssuerOrSBOM is TOOL-023's
// PRIMARY test. The existing provenance contract provides signed subject,
// source, builder and SBOM fields; changing any of those signed identities or
// the required SBOM linkage must reject admission.
func TestCosignAdmissionRejectsWrongSubjectIdentityIssuerOrSBOM(t *testing.T) {
	base := signedFixture(t)
	policy := policyFor(t)

	accepted := releaseadmission.Evaluate(policy, base)
	if !accepted.Admitted || accepted.Status != releaseadmission.StatusAdmit {
		t.Fatalf("valid fixture decision = %+v, want ADMIT", accepted)
	}

	cases := []struct {
		name   string
		mutate func(*provenance.Statement, *releaseadmission.Policy)
	}{
		{"wrong signed subject digest", func(s *provenance.Statement, _ *releaseadmission.Policy) {
			s.Subjects[0].SHA256 = strings.Repeat("0", 64)
		}},
		{"untrusted signer", func(_ *provenance.Statement, p *releaseadmission.Policy) { p.TrustedPublicKeys = map[string]bool{} }},
		{"untrusted builder", func(_ *provenance.Statement, p *releaseadmission.Policy) {
			p.AllowedBuilders = map[string]bool{"other-builder": true}
		}},
		{"detached SBOM", func(s *provenance.Statement, _ *releaseadmission.Policy) { s.SBOM.SHA256 = strings.Repeat("1", 64) }},
		{"wrong required SBOM", func(_ *provenance.Statement, p *releaseadmission.Policy) { p.SBOMDigest = strings.Repeat("2", 64) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statement := base
			statement.Subjects = append([]provenance.Subject(nil), base.Subjects...)
			p := policy
			tc.mutate(&statement, &p)
			decision := releaseadmission.Evaluate(p, statement)
			if decision.Admitted || decision.Status != releaseadmission.StatusReject || decision.Digest == "" {
				t.Fatalf("decision = %+v, want deterministic REJECT", decision)
			}
		})
	}
}

// TestTodo_TOOL_023_Golden pins admission of the checked-in signed
// provenance/SBOM pair and the audit shape of its decision.
func TestTodo_TOOL_023_Golden(t *testing.T) {
	decision := releaseadmission.Evaluate(policyFor(t), signedFixture(t))
	if decision.Status != releaseadmission.StatusAdmit || !decision.Admitted {
		t.Fatalf("status=%s admitted=%v, want ADMIT/true", decision.Status, decision.Admitted)
	}
	if decision.StatementDigest == "" || decision.Digest == "" {
		t.Fatalf("decision lacks canonical evidence digests: %+v", decision)
	}
	if !strings.Contains(decision.Explain(), "release admission ADMIT") {
		t.Fatalf("Explain() = %q, want admission status", decision.Explain())
	}
}

// TestTodo_TOOL_023_Fault proves incomplete policy configuration fails closed.
func TestTodo_TOOL_023_Fault(t *testing.T) {
	decision := releaseadmission.Evaluate(releaseadmission.Policy{}, signedFixture(t))
	if decision.Admitted || decision.Status != releaseadmission.StatusReject {
		t.Fatalf("empty policy decision=%+v, want REJECT", decision)
	}
	if len(decision.Reasons) < 3 {
		t.Fatalf("empty policy reasons=%v, want key/builder/SBOM failures", decision.Reasons)
	}
}

// TestTodo_TOOL_023_Security proves a validly signed statement is not enough
// when its signer is outside the pinned key set.
func TestTodo_TOOL_023_Security(t *testing.T) {
	statement := signedFixture(t)
	policy := policyFor(t)
	policy.TrustedPublicKeys = map[string]bool{strings.Repeat("a", 64): true}
	decision := releaseadmission.Evaluate(policy, statement)
	if decision.Admitted || !strings.Contains(strings.Join(decision.Reasons, "\n"), "unknown builder/signer") {
		t.Fatalf("decision=%+v, want unknown signer rejection", decision)
	}
}

// TestTodo_TOOL_023_Conformance pins the Cosign/Sigstore DEFER record and
// proves no Sigstore module has been admitted to go.mod.
func TestTodo_TOOL_023_Conformance(t *testing.T) {
	record := releaseadmission.Qualification()
	if record.Decision != releaseadmission.QualificationDefer {
		t.Fatalf("qualification decision=%q, want DEFER", record.Decision)
	}
	for _, text := range []string{record.Reason, record.AdoptionTrigger, record.DependencyPolicy} {
		if text == "" {
			t.Fatal("qualification record contains an empty rationale field")
		}
	}
	root := repopath.RootDir()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	lower := strings.ToLower(string(data))
	if strings.Contains(lower, "sigstore") || strings.Contains(lower, "cosign") {
		t.Fatal("go.mod contains a Cosign/Sigstore module despite the DEFER record")
	}
}

func TestDecisionDigestIsStable(t *testing.T) {
	first := releaseadmission.Evaluate(policyFor(t), signedFixture(t))
	second := releaseadmission.Evaluate(policyFor(t), signedFixture(t))
	if first.Digest != second.Digest {
		t.Fatalf("identical decisions differ: %s != %s", first.Digest, second.Digest)
	}
}
