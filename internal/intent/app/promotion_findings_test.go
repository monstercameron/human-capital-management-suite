package app

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

func TestTodo_PROMOUX_009TypedFindingIdentityRemovesOnlyKernelBridge(t *testing.T) {
	domainFinding := promotion.Finding{
		Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory,
		Field: "budget_authority", Message: "the observed budget authority is not a reservation",
	}
	unrelated := intent.Finding{
		Code: intent.FindingMissingRequired, FieldPath: "manager", Detail: "manager is required",
		Status: intent.PreflightNeedsData,
	}
	got := findingsProto(
		intent.PreflightResult{Findings: []intent.Finding{domainFindingKernelProjection(domainFinding), unrelated}},
		promotion.PreflightResult{Findings: []promotion.Finding{domainFinding}},
	)
	if len(got) != 2 {
		t.Fatalf("findings = %d, want the domain finding and unrelated kernel finding", len(got))
	}
	if got[0].GetCode() != string(unrelated.Code) || got[1].GetCode() != promotion.CodeBudgetObservationOnly {
		t.Fatalf("finding codes = [%s %s], want unrelated then stable domain code", got[0].GetCode(), got[1].GetCode())
	}
	if got[1].GetMessage() != "Finance confirmed the current budget baseline. Funds are reserved only when the promotion is recorded." {
		t.Fatalf("budget copy = %q", got[1].GetMessage())
	}
}

func TestTodo_PROMOUX_009PreservesIndependentlySourcedCorroboration(t *testing.T) {
	findings := []promotion.Finding{
		{Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory, Field: "budget_a", Message: "source a"},
		{Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory, Field: "budget_b", Message: "source b"},
	}
	got := findingsProto(intent.PreflightResult{Findings: domainFindings(promotion.PreflightResult{Findings: findings})}, promotion.PreflightResult{Findings: findings})
	if len(got) != 2 {
		t.Fatalf("findings = %d, want both independently sourced domain findings", len(got))
	}
}
