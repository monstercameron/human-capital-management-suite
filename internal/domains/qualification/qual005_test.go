package qualification

import (
	"testing"
)

// TestTodo_QUAL_005: credential/fact expiry drives exactly one
// reevaluation with a bounded warning/restriction/removal intent —
// never silent continuation and never an automatic irreversible action.
func TestTodo_QUAL_005(t *testing.T) {
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

	// Seeded defect: expiry emits one reevaluation with a bounded warning.
	partial := []HeldCredential{
		{CredentialRef: "credential:nursing-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:license-1", Validity: qualificationInterval(t, 1, 20)},
		{SkillRef: "skill:triage", Level: 1, EvidenceKind: EvidenceAssessment, EvidenceRef: "evidence:assessment-1", Validity: qualificationInterval(t, 1, 31)},
	}
	expired, err := Reevaluate(requirement, prior, partial, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if expired.Trigger != TriggerExpiry {
		t.Fatalf("trigger = %v, want EXPIRY", expired.Trigger)
	}
	if len(expired.ChangedRefs) != 1 || expired.ChangedRefs[0] != "credential:nursing-license" {
		t.Fatalf("changed refs = %v", expired.ChangedRefs)
	}
	if len(expired.Intents) != 1 || expired.Intents[0].Kind != FollowUpWarning {
		t.Fatalf("intents = %+v, want one WARNING", expired.Intents)
	}
	if expired.CanonicalDigest == "" {
		t.Fatal("reevaluation was not digested")
	}

	// Seeded defect: a lapsed credential proposes removal subject to
	// approval instead of silently continuing or auto-removing.
	lapsedPrior, err := requirement.Evaluate(partial)
	if err != nil {
		t.Fatal(err)
	}
	absent := []HeldCredential{
		{SkillRef: "skill:triage", Level: 1, EvidenceKind: EvidenceAssessment, EvidenceRef: "evidence:assessment-1", Validity: qualificationInterval(t, 1, 31)},
	}
	lapsed, err := Reevaluate(requirement, lapsedPrior, absent, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(lapsed.Intents) != 1 || lapsed.Intents[0].Kind != FollowUpRemovalProposal {
		t.Fatalf("intents = %+v, want one REMOVAL_PROPOSAL", lapsed.Intents)
	}
	if !lapsed.Intents[0].RequiresApproval {
		t.Fatal("removal proposal does not require approval")
	}

	// No change means no trigger and no intent — but still one
	// digested reevaluation record, never silent continuation of the
	// prior without a decision.
	steady, err := Reevaluate(requirement, prior, covering, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if steady.Trigger != TriggerNone || len(steady.Intents) != 0 || len(steady.ChangedRefs) != 0 {
		t.Fatalf("steady reevaluation = %+v", steady)
	}

	// A prior from another requirement rejects with QUAL_005_REJECTED
	// naming field/version and persists nothing (pure).
	foreign, err := NewQualificationRequirement(QualificationRequirement{
		RequirementID: "qual-2", Revision: 1,
		SubjectScope:      SubjectScope{Kind: "role", Ref: "registered-nurse"},
		AssignmentContext: "clinical-assignment", AuthorizationRef: "policy:talent.read",
		Experience: "acute-care", EquivalencyRef: "equiv:none", PolicyRef: "talent-policy-v1",
		Availability: qualificationInterval(t, 1, 31), Validity: qualificationInterval(t, 1, 31),
		Credentials:      []CredentialRequirement{{Ref: "credential:other-license", Level: 1}},
		AcceptedEvidence: []EvidenceKind{EvidenceVerifiedCredential},
		Renewal:          RenewalRule{Kind: RenewalNotRequired},
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignPrior, err := foreign.Evaluate([]HeldCredential{
		{CredentialRef: "credential:other-license", Level: 1, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:other-1", Validity: qualificationInterval(t, 1, 31)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Reevaluate(requirement, foreignPrior, covering, asOf); err == nil {
		t.Fatal("foreign prior reevaluated")
	} else if rejected, ok := AsReevaluationRejected(err); !ok || rejected.Code != ReevaluationRejectedCode {
		t.Fatalf("expected QUAL_005_REJECTED, got %v", err)
	} else if rejected.Field == "" || rejected.Version == 0 {
		t.Fatalf("rejection lacks field/version: %+v", rejected)
	}
}
