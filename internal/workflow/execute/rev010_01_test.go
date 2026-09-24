package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// --- REV-010-01 fixtures ------------------------------------------------------
//
// The PRIMARY, INTEGRATION and GOLDEN cases reuse WF-RUN-029's currency
// doubles (a current proposal revision plus one standing, correctly bound
// approval) and add a stub [RuleFacts] port, so the only new behavior under
// test is RULE-004's execution-time re-evaluation joining
// [CurrencyGuard.Check]'s verdict.

func rev01001Input(t *testing.T, percent string) rules.PromotionApprovalInput {
	t.Helper()
	increase, err := values.NewDecimal(percent, rules.IncreasePercentScale, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return rules.PromotionApprovalInput{
		IncreasePercent: increase,
		BandPosition:    rules.BandPositionInBand,
		BudgetAuthority: rules.BudgetAuthoritySufficient,
	}
}

func rev01001Approved(t *testing.T, percent string) rules.ApprovedPlan {
	t.Helper()
	approved, err := rules.NewApprovedPlan(rules.PromotionApprovalThresholdTable(), rev01001Input(t, percent), "sha256:governance-approval")
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

// stubRuleFacts is the [RuleFacts] double REV-010-01's tests drive
// [CurrencyGuard.Check] through: it reports one canned approval context (or
// one canned lookup failure) for whatever revision it is asked about.
type stubRuleFacts struct {
	approval RuleApproval
	err      error
	calls    int
	tenant   uuid.UUID
}

func (s *stubRuleFacts) Lookup(_ context.Context, _ runtime.Executor, tenantID uuid.UUID, _ intent.ProposalRevision, _ time.Time) (RuleApproval, error) {
	s.calls++
	s.tenant = tenantID
	return s.approval, s.err
}

func rev01001Check(t *testing.T, stub *stubRuleFacts) (CurrencyVerdict, uuid.UUID) {
	t.Helper()
	rev := baseCurrencyRevision()
	var port RuleFacts
	if stub != nil {
		port = stub
	}
	guard := CurrencyGuard{
		Proposal: runtime.MemoryProposalFacts{},
		Approval: approvedApprovalFacts(rev),
		Rules:    port,
	}
	tenantID := uuid.New()
	verdict, err := guard.Check(context.Background(), nil, CurrencyCheckRequest{
		TenantID: tenantID, InstanceID: uuid.New(),
		Proposal: runtime.ProposalBinding{Revision: rev}, CheckedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return verdict, tenantID
}

// --- TestTodo_REV_010_01 --------------------------------------------------------

// TestTodo_REV_010_01 is the PRIMARY test: a CONFIRM result joins the
// existing continue path, while an INVALIDATE or REAPPROVAL_REQUIRED result
// -- or a frozen record that no longer reproduces -- joins the BLOCKED path
// under the closed CURRENCY_RULE_INVALIDATED reason. A plan whose approval
// was not resolved by a decision table, and a guard with no Rules port at
// all, keep the exact pre-REV-010-01 verdict.
func TestTodo_REV_010_01(t *testing.T) {
	t.Run("confirm joins the continue path", func(t *testing.T) {
		approved := rev01001Approved(t, "5.0000")
		stub := &stubRuleFacts{approval: RuleApproval{Resolved: true, Approved: approved, Current: rev01001Input(t, "5.0000")}}
		verdict, tenantID := rev01001Check(t, stub)
		if verdict.Blocked {
			t.Fatalf("verdict = %+v, want the continue path", verdict)
		}
		if stub.calls != 1 {
			t.Fatalf("rule lookups = %d, want 1", stub.calls)
		}
		if stub.tenant != tenantID {
			t.Fatalf("rule lookup saw tenant %s, want the caller's own %s", stub.tenant, tenantID)
		}
		if verdict.RuleReevaluation == nil || verdict.RuleReevaluation.Verdict != rules.VerdictConfirmed {
			t.Fatalf("verdict carries %+v, want a CONFIRMED reevaluation", verdict.RuleReevaluation)
		}
	})

	t.Run("tier move invalidates and blocks", func(t *testing.T) {
		stub := &stubRuleFacts{approval: RuleApproval{Resolved: true, Approved: rev01001Approved(t, "5.0000"), Current: rev01001Input(t, "15.0000")}}
		verdict, _ := rev01001Check(t, stub)
		if !verdict.Blocked || verdict.Reason != ReasonCurrencyRuleInvalidated {
			t.Fatalf("verdict = %+v, want Blocked with reason %q", verdict, ReasonCurrencyRuleInvalidated)
		}
		if verdict.RuleReevaluation == nil || verdict.RuleReevaluation.Verdict != rules.VerdictInvalidated {
			t.Fatalf("verdict carries %+v, want an INVALIDATED reevaluation", verdict.RuleReevaluation)
		}
		if verdict.RuleReevaluation.Tier != rules.ApprovalTierFinanceRequired {
			t.Fatalf("reevaluated tier = %s, want FINANCE_REQUIRED", verdict.RuleReevaluation.Tier)
		}
	})

	t.Run("moved input with held tier still blocks for reapproval", func(t *testing.T) {
		stub := &stubRuleFacts{approval: RuleApproval{Resolved: true, Approved: rev01001Approved(t, "5.0000"), Current: rev01001Input(t, "6.0000")}}
		verdict, _ := rev01001Check(t, stub)
		if !verdict.Blocked || verdict.Reason != ReasonCurrencyRuleInvalidated {
			t.Fatalf("verdict = %+v, want Blocked with reason %q", verdict, ReasonCurrencyRuleInvalidated)
		}
		if verdict.RuleReevaluation == nil || verdict.RuleReevaluation.Verdict != rules.VerdictReapprovalRequired {
			t.Fatalf("verdict carries %+v, want a REAPPROVAL_REQUIRED reevaluation", verdict.RuleReevaluation)
		}
	})

	t.Run("tampered approval record blocks fail-closed", func(t *testing.T) {
		approved := rev01001Approved(t, "5.0000")
		approved.Input.IncreasePercent = rev01001Input(t, "50.0000").IncreasePercent
		stub := &stubRuleFacts{approval: RuleApproval{Resolved: true, Approved: approved, Current: rev01001Input(t, "5.0000")}}
		verdict, _ := rev01001Check(t, stub)
		if !verdict.Blocked || verdict.Reason != ReasonCurrencyRuleInvalidated {
			t.Fatalf("verdict = %+v, want fail-closed Blocked with reason %q", verdict, ReasonCurrencyRuleInvalidated)
		}
	})

	t.Run("unresolved plan keeps the historical verdict", func(t *testing.T) {
		stub := &stubRuleFacts{approval: RuleApproval{Resolved: false, Approved: rev01001Approved(t, "5.0000"), Current: rev01001Input(t, "15.0000")}}
		verdict, _ := rev01001Check(t, stub)
		if verdict.Blocked {
			t.Fatalf("verdict = %+v, want the continue path for a plan no decision table resolved", verdict)
		}
		if verdict.RuleReevaluation != nil {
			t.Fatalf("verdict carries %+v, want no reevaluation for an unresolved plan", verdict.RuleReevaluation)
		}
	})

	t.Run("nil rules port keeps the historical verdict", func(t *testing.T) {
		verdict, _ := rev01001Check(t, nil)
		if verdict.Blocked {
			t.Fatalf("verdict = %+v, want the continue path with no rules port", verdict)
		}
		if verdict.RuleReevaluation != nil {
			t.Fatalf("verdict carries %+v, want no reevaluation with no rules port", verdict.RuleReevaluation)
		}
	})

	t.Run("lookup failure is a call error, not a verdict", func(t *testing.T) {
		rev := baseCurrencyRevision()
		lookupErr := errors.New("rule store down")
		guard := CurrencyGuard{
			Proposal: runtime.MemoryProposalFacts{},
			Approval: approvedApprovalFacts(rev),
			Rules:    &stubRuleFacts{err: lookupErr},
		}
		if _, err := guard.Check(context.Background(), nil, CurrencyCheckRequest{
			TenantID: uuid.New(), InstanceID: uuid.New(),
			Proposal: runtime.ProposalBinding{Revision: rev}, CheckedAt: time.Now().UTC(),
		}); !errors.Is(err, lookupErr) {
			t.Fatalf("Check error = %v, want the lookup failure", err)
		}
	})
}

// --- TestTodo_REV_010_01_Integration ----------------------------------------------

// TestTodo_REV_010_01_Integration proves the re-evaluation runs the real
// rules engine against the real published table across the execute/rules
// package boundary, and that the driver's own Resume-to-terminal path blocks
// durably on a real store when the tier moves -- while a CONFIRM still
// reaches COMPLETE through that same path.
func TestTodo_REV_010_01_Integration(t *testing.T) {
	t.Run("real engine across the package boundary", func(t *testing.T) {
		approved := rev01001Approved(t, "5.0000")
		if approved.TableID != rules.PromotionApprovalTableID ||
			approved.TableVersion != rules.PromotionApprovalTableVersion {
			t.Fatalf("approval cites %s@%s, want the published table", approved.TableID, approved.TableVersion)
		}
		stub := &stubRuleFacts{approval: RuleApproval{Resolved: true, Approved: approved, Current: rev01001Input(t, "15.0000")}}
		verdict, _ := rev01001Check(t, stub)
		if !verdict.Blocked || verdict.Reason != ReasonCurrencyRuleInvalidated {
			t.Fatalf("verdict = %+v, want the real engine's INVALIDATE to block", verdict)
		}
		got := verdict.RuleReevaluation
		if got == nil || got.Verdict != rules.VerdictInvalidated || got.Tier != rules.ApprovalTierFinanceRequired {
			t.Fatalf("reevaluation = %+v, want the real INVALIDATED/FINANCE_REQUIRED verdict", got)
		}
		if got.CurrentTableID != rules.PromotionApprovalTableID ||
			got.CurrentTableVer != rules.PromotionApprovalTableVersion {
			t.Fatalf("reevaluation cites %s@%s, want the published table", got.CurrentTableID, got.CurrentTableVer)
		}
	})

	t.Run("driver blocks durably when the tier moves", func(t *testing.T) {
		f := newWfrun028Fixture(t, "rev01001-integration-block")
		guard := &CurrencyGuard{
			Proposal: runtime.MemoryProposalFacts{},
			Approval: approvedApprovalFacts(f.proposal),
			Rules: &stubRuleFacts{approval: RuleApproval{
				Resolved: true, Approved: rev01001Approved(t, "5.0000"), Current: rev01001Input(t, "15.0000"),
			}},
		}
		if _, err := f.driverWithCurrency(t, guard).Resume(context.Background(), f.resumeRequest()); !errors.Is(err, ErrCurrencyBlocked) {
			t.Fatalf("Resume error = %v, want currency blocked", err)
		}
		var inst runtime.Instance
		work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
			var loadErr error
			inst, loadErr = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
			return loadErr
		})
		if inst.RuntimeStatus != runtime.InstanceBlocked {
			t.Fatalf("instance runtime status = %s, want BLOCKED", inst.RuntimeStatus)
		}
		if inst.InstanceVersion != f.instanceVersion+1 {
			t.Fatalf("instance version after block = %d, want %d", inst.InstanceVersion, f.instanceVersion+1)
		}
	})

	t.Run("driver completes when the rules confirm", func(t *testing.T) {
		f := newWfrun028Fixture(t, "rev01001-integration-confirm")
		guard := &CurrencyGuard{
			Proposal: runtime.MemoryProposalFacts{},
			Approval: approvedApprovalFacts(f.proposal),
			Rules: &stubRuleFacts{approval: RuleApproval{
				Resolved: true, Approved: rev01001Approved(t, "5.0000"), Current: rev01001Input(t, "5.0000"),
			}},
		}
		result, err := f.driverWithCurrency(t, guard).Resume(context.Background(), f.resumeRequest())
		if err != nil {
			t.Fatalf("Resume: %v", err)
		}
		if result.Status != StatusComplete {
			t.Fatalf("result = %+v, want COMPLETE (a CONFIRM must never block)", result)
		}
	})
}

// --- TestTodo_REV_010_01_Golden -----------------------------------------------------

// TestTodo_REV_010_01_Golden pins the exact blocked-verdict evidence a tier
// move reports: the closed reason plus the cited original and current table
// versions. The approval below was frozen under an older published version,
// so the evidence must name both versions distinctly. A drift in any byte is
// a behavior change, not a refactor.
func TestTodo_REV_010_01_Golden(t *testing.T) {
	older := rules.PromotionApprovalThresholdTable()
	older.Version = "2025.4"
	approved, err := rules.NewApprovedPlan(older, rev01001Input(t, "5.0000"), "sha256:governance-approval")
	if err != nil {
		t.Fatal(err)
	}
	stub := &stubRuleFacts{approval: RuleApproval{Resolved: true, Approved: approved, Current: rev01001Input(t, "15.0000")}}
	verdict, _ := rev01001Check(t, stub)
	if !verdict.Blocked || verdict.Reason != ReasonCurrencyRuleInvalidated {
		t.Fatalf("verdict = %+v, want Blocked with reason %q", verdict, ReasonCurrencyRuleInvalidated)
	}
	want := []string{
		"rule INVALIDATED: tier FINANCE_REQUIRED via row increase-exceeds-finance-threshold",
		"original table hcmnext.rules.promotion_approval_threshold@2025.4",
		"current table hcmnext.rules.promotion_approval_threshold@2026.1",
	}
	if len(verdict.Explanation) != len(want) {
		t.Fatalf("explanation = %q, want %q", verdict.Explanation, want)
	}
	for i := range want {
		if verdict.Explanation[i] != want[i] {
			t.Fatalf("explanation[%d] = %q, want %q", i, verdict.Explanation[i], want[i])
		}
	}
	got := verdict.RuleReevaluation
	if got == nil {
		t.Fatal("verdict carries no reevaluation")
	}
	if got.OriginalTableID != rules.PromotionApprovalTableID || got.OriginalTableVer != "2025.4" {
		t.Fatalf("original citation = %s@%s, want the frozen version", got.OriginalTableID, got.OriginalTableVer)
	}
	if got.CurrentTableID != rules.PromotionApprovalTableID || got.CurrentTableVer != rules.PromotionApprovalTableVersion {
		t.Fatalf("current citation = %s@%s, want the published table", got.CurrentTableID, got.CurrentTableVer)
	}
	if !got.MovedInput || !got.MovedTable {
		t.Fatalf("reevaluation = %+v, want both input and table moves recorded", got)
	}
}
