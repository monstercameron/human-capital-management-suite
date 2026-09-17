package qualification

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func validityInstant(t *testing.T) values.Instant {
	t.Helper()
	return values.NewInstant(time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC))
}

func validityRange(t *testing.T, year int, month time.Month, startDay, endDay int) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(year, month, startDay)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(year, month, endDay)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func validityItemByRef(t *testing.T, evaluation ValidityEvaluation, ref string) ValidityItem {
	t.Helper()
	for _, item := range evaluation.Items {
		if item.Ref == ref {
			return item
		}
	}
	t.Fatalf("no validity item for ref %q in %+v", ref, evaluation)
	return ValidityItem{}
}

// TestTodo_QUAL_003: validity, equivalency and substitution evaluation.
// Direct cover satisfies with an exact rule chain; ambiguous equivalency,
// missing jurisdiction and future/expired credentials resolve to
// CONDITIONAL/UNKNOWN rather than a false SATISFIED.
func TestTodo_QUAL_003(t *testing.T) {
	requirement := validQualification(t)
	asOf := validityInstant(t)

	// Direct cover satisfies and records the exact rule chain.
	direct, err := EvaluateValidity(requirement, []HeldCredential{
		held(t, 31, EvidenceVerifiedCredential),
		{SkillRef: "skill:triage", Level: 1, EvidenceKind: EvidenceAssessment, EvidenceRef: "evidence:assessment-1", Validity: qualificationInterval(t, 1, 31)},
	}, nil, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if item := validityItemByRef(t, direct, "credential:nursing-license"); item.Outcome != ValiditySatisfied {
		t.Fatalf("direct cover outcome = %v, want SATISFIED", item.Outcome)
	} else if len(item.RuleChain) != 1 || item.RuleChain[0] != "direct:cover" {
		t.Fatalf("direct cover rule chain = %v", item.RuleChain)
	}

	// Seeded defect: a single substitution satisfies only through its
	// declared rule, and the chain names that rule.
	substituted, err := EvaluateValidity(requirement, []HeldCredential{
		{CredentialRef: "credential:compact-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:compact-1", Validity: qualificationInterval(t, 1, 31)},
	}, []EquivalenceRule{
		{RuleID: "rule-1", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:compact-license", Jurisdiction: "us-ny"},
	}, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if item := validityItemByRef(t, substituted, "credential:nursing-license"); item.Outcome != ValiditySatisfied {
		t.Fatalf("substitution outcome = %v, want SATISFIED", item.Outcome)
	} else if len(item.RuleChain) != 1 || item.RuleChain[0] != "equiv:rule-1" {
		t.Fatalf("substitution rule chain = %v", item.RuleChain)
	}

	// Seeded defect: ambiguous equivalency must not satisfy.
	ambiguous, err := EvaluateValidity(requirement, []HeldCredential{
		{CredentialRef: "credential:compact-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:compact-1", Validity: qualificationInterval(t, 1, 31)},
		{CredentialRef: "credential:temp-permit", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:permit-1", Validity: qualificationInterval(t, 1, 31)},
	}, []EquivalenceRule{
		{RuleID: "rule-1", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:compact-license", Jurisdiction: "us-ny"},
		{RuleID: "rule-2", RequiredRef: "credential:nursing-license", SubstituteRef: "credential:temp-permit", Jurisdiction: "us-ny"},
	}, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if item := validityItemByRef(t, ambiguous, "credential:nursing-license"); item.Outcome != ValidityConditional {
		t.Fatalf("ambiguous equivalency outcome = %v, want CONDITIONAL", item.Outcome)
	}

	// Seeded defect: missing jurisdiction resolves unknown, never satisfied.
	unknown, err := EvaluateValidity(requirement, []HeldCredential{held(t, 31, EvidenceVerifiedCredential)}, nil, "", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if item := validityItemByRef(t, unknown, "credential:nursing-license"); item.Outcome != ValidityUnknown {
		t.Fatalf("missing jurisdiction outcome = %v, want UNKNOWN", item.Outcome)
	}

	// Seeded defect: a future credential is conditional, not satisfied.
	future, err := EvaluateValidity(requirement, []HeldCredential{
		{CredentialRef: "credential:nursing-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:future-1", Validity: validityRange(t, 2026, time.February, 1, 28)},
	}, nil, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if item := validityItemByRef(t, future, "credential:nursing-license"); item.Outcome != ValidityConditional {
		t.Fatalf("future credential outcome = %v, want CONDITIONAL", item.Outcome)
	}

	// Seeded defect: an expired credential is conditional, not satisfied.
	expired, err := EvaluateValidity(requirement, []HeldCredential{
		{CredentialRef: "credential:nursing-license", Level: 2, EvidenceKind: EvidenceVerifiedCredential, EvidenceRef: "evidence:expired-1", Validity: validityRange(t, 2025, time.December, 1, 31)},
	}, nil, "us-ny", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if item := validityItemByRef(t, expired, "credential:nursing-license"); item.Outcome != ValidityConditional {
		t.Fatalf("expired credential outcome = %v, want CONDITIONAL", item.Outcome)
	}

	// Malformed inputs reject with QUAL_003_REJECTED naming field/version
	// and persist nothing (evaluation is pure).
	if _, err := EvaluateValidity(QualificationRequirement{}, nil, nil, "us-ny", asOf); err == nil {
		t.Fatal("empty requirement evaluated")
	} else if rejected, ok := AsValidityRejected(err); !ok || rejected.Code != ValidityRejectedCode {
		t.Fatalf("expected QUAL_003_REJECTED, got %v", err)
	} else if rejected.Field == "" || rejected.Version == 0 {
		t.Fatalf("rejection lacks field/version: %+v", rejected)
	}
}
