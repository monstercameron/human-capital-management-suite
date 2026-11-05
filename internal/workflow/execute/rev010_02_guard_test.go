package execute

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_REV_010_02_Guard proves the RULE-004 re-evaluation runs the
// tenant own published table: an approval frozen under tenant thresholds
// re-confirms against the same tenant table, while a malformed published
// resolution blocks under the closed CURRENCY_RULE_INVALIDATED reason
// instead of re-running the reference table.
func TestTodo_REV_010_02_Guard(t *testing.T) {
	tenantTable := func(t *testing.T) rules.Table {
		t.Helper()
		table, err := rules.PromotionApprovalThresholdTableForTenant(
			rules.TenantThresholdScope{Tenant: "tenant-a"},
			rules.TenantThresholdResolution{FinanceText: "5.0000", ExecutiveText: "15.0000", Revision: 7, DefinitionVersion: "tenant-params/v7", Found: true},
		)
		if err != nil {
			t.Fatal(err)
		}
		return table
	}
	check := func(t *testing.T, stub *stubRuleFacts) CurrencyVerdict {
		t.Helper()
		rev := baseCurrencyRevision()
		rev.Tenant = "tenant-a"
		guard := CurrencyGuard{
			Proposal: runtime.MemoryProposalFacts{},
			Approval: approvedApprovalFacts(rev),
			Rules:    stub,
		}
		verdict, err := guard.Check(context.Background(), nil, CurrencyCheckRequest{
			TenantID: uuid.New(), InstanceID: uuid.New(),
			Proposal: runtime.ProposalBinding{Revision: rev}, CheckedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		return verdict
	}

	t.Run("tenant approval reconfirms against the tenant table", func(t *testing.T) {
		approved, err := rules.NewApprovedPlan(tenantTable(t), rev01001Input(t, "12.0000"), "sha256:governance-approval")
		if err != nil {
			t.Fatal(err)
		}
		stub := &stubRuleFacts{approval: RuleApproval{
			Resolved: true, Approved: approved, Current: rev01001Input(t, "12.0000"),
			Thresholds: rules.TenantThresholdResolution{FinanceText: "5.0000", ExecutiveText: "15.0000", Revision: 7, DefinitionVersion: "tenant-params/v7", Found: true},
		}}
		verdict := check(t, stub)
		if verdict.Blocked {
			t.Fatalf("verdict = %+v, want the continue path", verdict)
		}
		if verdict.RuleReevaluation == nil || verdict.RuleReevaluation.Verdict != rules.VerdictConfirmed {
			t.Fatalf("verdict carries %+v, want a CONFIRMED reevaluation", verdict.RuleReevaluation)
		}
	})

	t.Run("malformed tenant resolution blocks closed", func(t *testing.T) {
		stub := &stubRuleFacts{approval: RuleApproval{
			Resolved: true, Approved: rev01001Approved(t, "5.0000"), Current: rev01001Input(t, "5.0000"),
			Thresholds: rules.TenantThresholdResolution{FinanceText: "30.0000", ExecutiveText: "20.0000", Revision: 3, DefinitionVersion: "tenant-params/v3", Found: true},
		}}
		verdict := check(t, stub)
		if !verdict.Blocked || verdict.Reason != ReasonCurrencyRuleInvalidated {
			t.Fatalf("verdict = %+v, want Blocked with reason %q", verdict, ReasonCurrencyRuleInvalidated)
		}
	})
}
