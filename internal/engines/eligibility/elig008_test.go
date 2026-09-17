package eligibility_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errParityBoom = errors.New("parity: injected fact failure")

// ELIG-008 RED: the four-domain parity gate does not exist yet. Everything
// below must fail to compile until parity.go lands.

func elig008Case(t *testing.T, domain, matter, purpose, authority, rule string) eligibility.DomainCase {
	t.Helper()
	req := validRequest()
	req.SubjectMatter = eligibility.SubjectMatterRef{
		Kind: eligibility.SubjectMatterProgram, ID: matter, Revision: "2026.1",
	}
	req.Purpose = purpose
	req.Authority = authority
	criteria := eligibility.Criteria{Root: and(
		factEquals("grade", "P3"),
		ruleCondition(rule, testRuleCatalog()[rule].Version),
	)}
	plan := mustPlan(t, criteria)
	facts := newFakeFacts().with(subject(1), "grade", values.Value("P3"))
	rules := newFakeRules().with(subject(1), rule, testRuleCatalog()[rule].Version, eligibility.RuleOutcomePass)
	return eligibility.DomainCase{
		Name: domain, Request: req, Plan: plan, Facts: facts, Rules: rules,
	}
}

func elig008Cases(t *testing.T) []eligibility.DomainCase {
	t.Helper()
	return []eligibility.DomainCase{
		elig008Case(t, "benefits", "benefit-health", "benefits_eligibility", "authz:role:benefits_admin", "performance-rating"),
		elig008Case(t, "leave", "leave-medical", "leave_eligibility", "authz:role:leave_admin", "manager-attestation"),
		elig008Case(t, "learning", "learning-course", "learning_eligibility", "authz:role:learning_admin", "performance-rating"),
		elig008Case(t, "rewards", "rewards-bonus", "reward_eligibility", "authz:role:rewards_admin", "manager-attestation"),
	}
}

func TestTodo_ELIG_008(t *testing.T) {
	report, err := eligibility.CheckDomainParity(context.Background(), elig008Cases(t))
	if err != nil {
		t.Fatalf("CheckDomainParity: %v", err)
	}
	if len(report.Domains) != 4 {
		t.Fatalf("domains=%v", report.Domains)
	}
	for _, domain := range []string{"benefits", "leave", "learning", "rewards"} {
		status, ok := report.Statuses[domain]
		if !ok || !status.Valid() {
			t.Fatalf("domain %s status=%v valid=%v", domain, status, ok && status.Valid())
		}
	}
	if report.Digest == "" {
		t.Fatal("parity report has no seal")
	}
	// RED: a fifth domain blocks publication.
	fifth := append(elig008Cases(t), elig008Case(t, "payroll", "payroll-run", "payroll_eligibility", "authz:role:payroll_admin", "manager-attestation"))
	if _, err := eligibility.CheckDomainParity(context.Background(), fifth); err == nil {
		t.Fatal("five-domain parity published")
	}
	// RED: a missing domain blocks publication.
	if _, err := eligibility.CheckDomainParity(context.Background(), elig008Cases(t)[:3]); err == nil {
		t.Fatal("three-domain parity published")
	}
	// RED: two domains sharing one subject matter block publication:
	// domain-specific rules must be retained.
	cloned := elig008Cases(t)
	cloned[1].Request.SubjectMatter.ID = "benefit-health"
	if _, err := eligibility.CheckDomainParity(context.Background(), cloned); err == nil {
		t.Fatal("cloned subject matter published")
	}
	// Shared unknown semantics: a fact-reader failure in one domain
	// resolves UNKNOWN through the same vocabulary, never denial, and the
	// parity still seals over the named unknown.
	broken := elig008Cases(t)
	broken[2].Facts = newFakeFacts().withErr(subject(1), "grade", errParityBoom)
	unknown, err := eligibility.CheckDomainParity(context.Background(), broken)
	if err != nil {
		t.Fatalf("unknown parity refused: %v", err)
	}
	if unknown.Statuses["learning"] != eligibility.StatusUnknown {
		t.Fatalf("learning status=%v, want UNKNOWN", unknown.Statuses["learning"])
	}
	// RED: an invalid request in one domain blocks publication.
	invalid := elig008Cases(t)
	invalid[0].Request.Jurisdiction = ""
	if _, err := eligibility.CheckDomainParity(context.Background(), invalid); err == nil {
		t.Fatal("invalid request published")
	}
}

func TestTodo_ELIG_008_Property(t *testing.T) {
	first, err := eligibility.CheckDomainParity(context.Background(), elig008Cases(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := eligibility.CheckDomainParity(context.Background(), elig008Cases(t))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("parity report is not deterministic")
	}
}

func TestTodo_ELIG_008_Conformance(t *testing.T) {
	report, err := eligibility.CheckDomainParity(context.Background(), elig008Cases(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Verify(elig008Cases(t)); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// A restated request no longer matches the sealed report.
	restated := elig008Cases(t)
	restated[0].Request.Snapshots.FactSnapshotRef = "fact-snap-restated"
	if err := report.Verify(restated); err == nil {
		t.Fatal("restated cases verified")
	}
}
