package qualification

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_QUAL_003_Property: evaluation is deterministic — input order
// never changes outcomes, digests or chains — and every item carries a
// closed-vocabulary outcome with a non-empty exact chain.
func TestTodo_QUAL_003_Property(t *testing.T) {
	requirement := validQualification(t)
	asOf := validityInstant(t)
	held := []HeldCredential{
		{CredentialRef: "credential:temp-permit", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:permit-1", Validity: qualificationInterval(t, 1, 31)},
		held(t, 31, EvidenceVerifiedCredential),
		{SkillRef: "skill:triage", Level: 1, EvidenceKind: EvidenceAssessment, EvidenceRef: "evidence:assessment-1", Validity: qualificationInterval(t, 1, 31)},
	}
	rules := []EquivalenceRule{
		{RuleID: "rule-2", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:temp-permit", Jurisdiction: "us-ny"},
		{RuleID: "rule-1", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:compact-license", Jurisdiction: "us-ny"},
	}
	first, err := EvaluateValidity(requirement, held, rules, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	reversed := append([]HeldCredential(nil), held...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	flipped := append([]EquivalenceRule(nil), rules...)
	flipped[0], flipped[1] = flipped[1], flipped[0]
	second, err := EvaluateValidity(requirement, reversed, flipped, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("input order changed the validity digest")
	}
	for _, item := range first.Items {
		if !item.Outcome.Valid() {
			t.Fatalf("item %q has open outcome %q", item.Ref, item.Outcome)
		}
		if len(item.RuleChain) == 0 {
			t.Fatalf("item %q records no rule chain", item.Ref)
		}
	}
	// A rule from another jurisdiction is never consulted: without a
	// direct credential the item stays unsatisfied, never substituted.
	foreign, err := EvaluateValidity(requirement, []HeldCredential{
		{CredentialRef: "credential:compact-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:compact-1", Validity: qualificationInterval(t, 1, 31)},
	}, []EquivalenceRule{
		{RuleID: "rule-9", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:compact-license", Jurisdiction: "us-ca"},
	}, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if item := validityItemByRef(t, foreign, "credential:nursing-license"); item.Outcome == ValiditySatisfied {
		t.Fatalf("foreign-jurisdiction rule satisfied: %+v", item)
	}
}

// TestTodo_QUAL_003_Fault: every malformed boundary rejects with
// QUAL_003_REJECTED instead of judging.
func TestTodo_QUAL_003_Fault(t *testing.T) {
	requirement := validQualification(t)
	asOf := validityInstant(t)
	good := []HeldCredential{held(t, 31, EvidenceVerifiedCredential)}
	goodRules := []EquivalenceRule{{RuleID: "rule-1", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:compact-license", Jurisdiction: "us-ny"}}
	cases := map[string]func() error{
		"bad requirement": func() error {
			broken := requirement
			broken.PolicyRef = ""
			_, err := EvaluateValidity(broken, good, nil, "us-ny", asOf)
			return err
		},
		"bad held": func() error {
			broken := append([]HeldCredential(nil), good...)
			broken[0].EvidenceKind = "UNKNOWN"
			_, err := EvaluateValidity(requirement, broken, nil, "us-ny", asOf)
			return err
		},
		"bad rule": func() error {
			_, err := EvaluateValidity(requirement, good, []EquivalenceRule{{RuleID: "", RequiredRef: "a", SubstituteRef: "b", Jurisdiction: "us-ny"}}, "us-ny", asOf)
			return err
		},
		"duplicate rule": func() error {
			_, err := EvaluateValidity(requirement, good, append(append([]EquivalenceRule(nil), goodRules...), goodRules...), "us-ny", asOf)
			return err
		},
		"bad as-of": func() error {
			_, err := EvaluateValidity(requirement, good, nil, "us-ny", values.Instant{})
			return err
		},
	}
	for name, run := range cases {
		if err := run(); err == nil {
			t.Fatalf("%s: malformed input evaluated", name)
		} else if rejected, ok := AsValidityRejected(err); !ok || rejected.Code != ValidityRejectedCode || rejected.Field == "" || rejected.Version == 0 {
			t.Fatalf("%s: expected QUAL_003_REJECTED with field/version, got %v", name, err)
		}
	}
}

// TestTodo_QUAL_003_Security: evidence content never reaches the
// evaluation — items carry refs and rule IDs only, and the explanation
// carries counts only.
func TestTodo_QUAL_003_Security(t *testing.T) {
	const secret = "secret-evidence-blob-9f3"
	requirement := validQualification(t)
	asOf := validityInstant(t)
	evaluation, err := EvaluateValidity(requirement, []HeldCredential{
		{CredentialRef: "credential:nursing-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: secret, Validity: qualificationInterval(t, 1, 31)},
		{SkillRef: "skill:triage", Level: 1, EvidenceKind: EvidenceAssessment, EvidenceRef: secret, Validity: qualificationInterval(t, 1, 31)},
	}, []EquivalenceRule{
		{RuleID: "rule-1", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:compact-license", Jurisdiction: "us-ny"},
	}, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range evaluation.Items {
		if strings.Contains(item.Ref, secret) || strings.Contains(item.Reason, secret) {
			t.Fatalf("item leaks evidence content: %+v", item)
		}
		for _, step := range item.RuleChain {
			if strings.Contains(step, secret) {
				t.Fatalf("rule chain leaks evidence content: %+v", item)
			}
		}
	}
	if strings.Contains(evaluation.Explain(), secret) {
		t.Fatalf("explanation leaks evidence content: %q", evaluation.Explain())
	}
	if strings.Contains(evaluation.CanonicalDigest, secret) {
		t.Fatal("digest leaks evidence content")
	}
}

// TestTodo_QUAL_003_Mutation: evaluation edges resolve on the
// documented side.
func TestTodo_QUAL_003_Mutation(t *testing.T) {
	requirement := validQualification(t)
	asOf := validityInstant(t)
	// A self-substituting rule is malformed: reject, never judge.
	if _, err := EvaluateValidity(requirement, nil, []EquivalenceRule{
		{RuleID: "rule-1", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:nursing-license", Jurisdiction: "us-ny"},
	}, "us-ny", asOf); err == nil {
		t.Fatal("self-substituting rule evaluated")
	}
	// A substitute held below the required level cannot satisfy.
	weak, err := EvaluateValidity(requirement, []HeldCredential{
		{CredentialRef: "credential:compact-license", Level: 1, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:compact-1", Validity: qualificationInterval(t, 1, 31)},
	}, []EquivalenceRule{
		{RuleID: "rule-1", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:compact-license", Jurisdiction: "us-ny"},
	}, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if item := validityItemByRef(t, weak, "credential:nursing-license"); item.Outcome == ValiditySatisfied {
		t.Fatalf("weak substitute satisfied: %+v", item)
	}
	// A substitute under a disallowed evidence kind cannot satisfy.
	disallowed, err := EvaluateValidity(requirement, []HeldCredential{
		{CredentialRef: "credential:compact-license", Level: 2, EvidenceKind: EvidenceTrainingRecord, EvidenceRef: "evidence:compact-1", Validity: qualificationInterval(t, 1, 31)},
	}, []EquivalenceRule{
		{RuleID: "rule-1", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:compact-license", Jurisdiction: "us-ny"},
	}, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if item := validityItemByRef(t, disallowed, "credential:nursing-license"); item.Outcome == ValiditySatisfied {
		t.Fatalf("disallowed evidence substituted: %+v", item)
	}
	// Zero revision rejects.
	broken := requirement
	broken.Revision = 0
	if _, err := EvaluateValidity(broken, nil, nil, "us-ny", asOf); err == nil {
		t.Fatal("zero-revision requirement evaluated")
	}
}
