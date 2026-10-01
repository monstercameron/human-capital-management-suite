package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rulethreshold"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type uxblindRuleFacts struct{ approval execute.RuleApproval }

func (f uxblindRuleFacts) Lookup(context.Context, runtime.Executor, uuid.UUID, intent.ProposalRevision, time.Time) (execute.RuleApproval, error) {
	return f.approval, nil
}

type uxblindProposalFacts struct{}

func (uxblindProposalFacts) Supersession(context.Context, runtime.Executor, uuid.UUID, intent.ProposalRevision) (runtime.ProposalSupersessionFact, error) {
	return runtime.ProposalSupersessionFact{}, nil
}

type uxblindApprovalFacts struct{ digest string }

func (f uxblindApprovalFacts) Decisions(context.Context, runtime.Executor, uuid.UUID, intent.ProposalRevision) ([]runtime.ApprovalDecisionFact, error) {
	return []runtime.ApprovalDecisionFact{{DecisionID: "decision:uxblind-093", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: f.digest}}, nil
}

func uxblindRuleInput(t *testing.T, percent string) rules.PromotionApprovalInput {
	t.Helper()
	increase := values.MustDecimal(percent, 2, values.RoundingExactRequired)
	return rules.PromotionApprovalInput{
		IncreasePercent: increase, BandPosition: rules.BandPositionInBand,
		BudgetAuthority: rules.BudgetAuthoritySufficient, GradeChange: true,
	}
}

func TestTodo_UXBLIND_093_RULE004NumericDriftRemainsBlocking(t *testing.T) {
	table := rules.PromotionApprovalThresholdTable()
	tableDigest, err := table.Digest()
	if err != nil {
		t.Fatal(err)
	}
	frozen := uxblindRuleInput(t, "12.50")
	current := uxblindRuleInput(t, "15.00")
	frozenDigest, err := rules.InputDigest(frozen)
	if err != nil {
		t.Fatal(err)
	}
	intentID := uuid.New()
	material := "sha256:material-uxblind-093"
	guard := execute.CurrencyGuard{
		Proposal: uxblindProposalFacts{}, Approval: uxblindApprovalFacts{digest: material},
		Rules: uxblindRuleFacts{approval: execute.RuleApproval{Resolved: true, Approved: rules.ApprovedPlan{
			Input: frozen, InputDigest: frozenDigest, Tier: rules.ApprovalTierFinanceRequired,
			MatchedRowID: "increase-exceeds-finance-threshold", TableID: table.ID,
			TableVersion: table.Version, TableDigest: tableDigest, ApprovalDigest: "decision:uxblind-093",
		}, Current: current}},
	}
	verdict, err := guard.Check(context.Background(), nil, execute.CurrencyCheckRequest{
		TenantID: uuid.New(), InstanceID: uuid.New(), Proposal: runtime.ProposalBinding{Revision: intent.ProposalRevision{
			IntentID: intentID.String(), Revision: 3, ProposalRevisionID: "revision:uxblind-093",
			MaterialDigest: digest.Reference{Digest: material, AlgorithmID: "sha256"},
		}}, CheckedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Blocked || verdict.Reason != execute.ReasonCurrencyRuleInvalidated {
		t.Fatalf("same authorized proposal with changed live numeric input = %+v, want RULE-004 invalidation", verdict)
	}
	if verdict.RuleReevaluation == nil || !verdict.RuleReevaluation.MovedInput {
		t.Fatalf("re-evaluation = %+v, want moved input from 12.50%% to 15.00%%", verdict.RuleReevaluation)
	}
}

func TestTodo_UXBLIND_093_RULE004StoreRoundTripWithInjectedDrift(t *testing.T) {
	db := pgtest.New(t)
	tenantID := servedFactsTenant(t, db)
	intentID := uuid.New()
	frozen := servedFactsInput(t, "12.50")
	live := servedFactsInput(t, "15.00")
	material := "sha256:" + strings.Repeat("b", 64)
	rev := servedFactsRevision(intentID, 3, material)
	servedFactsTx(t, db, tenantID, func(tx dbport.Tx) error {
		return rulethreshold.Record(context.Background(), tx, servedFactsRecord(t, tenantID, intentID, frozen))
	})
	facts := &ServedRuleFacts{
		Thresholds: &stubRuleDeriver{input: live},
		Approval:   stubServedApprovalFacts{decisions: []runtime.ApprovalDecisionFact{{DecisionID: "decision:post-commit", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: material}}},
	}
	var got execute.RuleApproval
	servedFactsTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		got, err = facts.Lookup(context.Background(), tx, tenantID, rev, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
		return err
	})
	if !got.Resolved {
		t.Fatal("stored threshold decision was not resolved")
	}
	if got.Approved.Input.IncreasePercent.String() != "12.50" || got.Current.IncreasePercent.String() != "15.00" {
		t.Fatalf("RULE-004 inputs frozen=%s current=%s, want 12.50 and 15.00", got.Approved.Input.IncreasePercent, got.Current.IncreasePercent)
	}
}

func TestTodo_UXBLIND_093_RULE004MaterialDigestStillBlocks(t *testing.T) {
	table := rules.PromotionApprovalThresholdTable()
	in := uxblindRuleInput(t, "12.50")
	inputDigest, err := rules.InputDigest(in)
	if err != nil {
		t.Fatal(err)
	}
	material := "sha256:material-uxblind-093"
	guard := execute.CurrencyGuard{Proposal: uxblindProposalFacts{}, Approval: uxblindApprovalFacts{digest: "sha256:other-material"}, Rules: uxblindRuleFacts{approval: execute.RuleApproval{Resolved: true, Approved: rules.ApprovedPlan{Input: in, InputDigest: inputDigest, Tier: rules.ApprovalTierFinanceRequired, MatchedRowID: "increase-exceeds-finance-threshold", TableID: table.ID, TableVersion: table.Version, ApprovalDigest: "decision:uxblind-093"}, Current: in}}}
	verdict, err := guard.Check(context.Background(), nil, execute.CurrencyCheckRequest{TenantID: uuid.New(), InstanceID: uuid.New(), Proposal: runtime.ProposalBinding{Revision: intent.ProposalRevision{IntentID: uuid.New().String(), Revision: 3, MaterialDigest: digest.Reference{Digest: material, AlgorithmID: "sha256"}}}, CheckedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Blocked || verdict.Reason != execute.ReasonCurrencyApprovalBindingMismatch {
		t.Fatalf("changed proposal material digest = %+v, want binding mismatch", verdict)
	}
}
