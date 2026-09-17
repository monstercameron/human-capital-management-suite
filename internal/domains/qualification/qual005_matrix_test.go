package qualification

import (
	"strings"
	"testing"
)

// TestTodo_QUAL_005_Property: reevaluation is idempotent and bounded —
// identical inputs replay to the identical digest, intents never exceed
// one per changed ref, and NONE fires exactly when nothing changed.
func TestTodo_QUAL_005_Property(t *testing.T) {
	requirement := validQualification(t)
	asOf := validityInstant(t)
	covering := []HeldCredential{
		held(t, 31, EvidenceVerifiedCredential),
		{SkillRef: "skill:triage", Level: 1, EvidenceKind: EvidenceAssessment, EvidenceRef: "evidence:assessment-1", Validity: qualificationInterval(t, 1, 31)},
	}
	prior, err := requirement.Evaluate(covering)
	if err != nil {
		t.Fatal(err)
	}
	partial := []HeldCredential{
		{CredentialRef: "credential:nursing-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:license-1", Validity: qualificationInterval(t, 1, 20)},
	}
	first, err := Reevaluate(requirement, prior, partial, asOf)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Reevaluate(requirement, prior, partial, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("identical reevaluation inputs replayed to different digests")
	}
	if len(first.Intents) > len(first.ChangedRefs) || len(first.Intents) > len(first.Evaluation.Results) {
		t.Fatalf("intents exceed the bound: %+v", first)
	}
	for _, intent := range first.Intents {
		if !intent.Kind.Valid() {
			t.Fatalf("intent has open kind: %+v", intent)
		}
	}
	// NONE fires exactly when nothing changed.
	steady, err := Reevaluate(requirement, prior, covering, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if steady.Trigger != TriggerNone || len(steady.ChangedRefs) != 0 || len(steady.Intents) != 0 {
		t.Fatalf("steady reevaluation = %+v", steady)
	}
	// A pure improvement is a fact change, never an expiry, and warns nothing.
	unsatisfiedPrior, err := requirement.Evaluate(partial)
	if err != nil {
		t.Fatal(err)
	}
	improved, err := Reevaluate(requirement, unsatisfiedPrior, covering, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if improved.Trigger == TriggerExpiry {
		t.Fatalf("improvement triggered expiry: %+v", improved)
	}
	for _, intent := range improved.Intents {
		if intent.Kind == FollowUpRemovalProposal && !intent.RequiresApproval {
			t.Fatalf("removal without approval: %+v", intent)
		}
	}
}

// TestTodo_QUAL_005_Security: evidence content never reaches the
// reevaluation record — changed refs, intent reasons and the
// explanation carry refs and stable tokens only.
func TestTodo_QUAL_005_Security(t *testing.T) {
	const secret = "secret-evidence-blob-9f3"
	requirement := validQualification(t)
	asOf := validityInstant(t)
	prior, err := requirement.Evaluate([]HeldCredential{
		{CredentialRef: "credential:nursing-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: secret, Validity: qualificationInterval(t, 1, 31)},
		{SkillRef: "skill:triage", Level: 1, EvidenceKind: EvidenceAssessment, EvidenceRef: secret, Validity: qualificationInterval(t, 1, 31)},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Reevaluate(requirement, prior, []HeldCredential{
		{CredentialRef: "credential:nursing-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: secret, Validity: qualificationInterval(t, 1, 20)},
	}, asOf)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range got.ChangedRefs {
		if strings.Contains(ref, secret) {
			t.Fatalf("changed ref leaks evidence content: %q", ref)
		}
	}
	for _, intent := range got.Intents {
		if strings.Contains(intent.Ref, secret) || strings.Contains(intent.Reason, secret) {
			t.Fatalf("intent leaks evidence content: %+v", intent)
		}
	}
	if strings.Contains(got.Explain(), secret) {
		t.Fatalf("explanation leaks evidence content: %q", got.Explain())
	}
}
