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
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// stubRuleDeriver is the func-backed ruleInputDeriver the served-facts
// tests drive ServedRuleFacts.Lookup through: it reports one canned live
// input set without standing up step services.
type stubRuleDeriver struct {
	input rules.PromotionApprovalInput
	err   error
	calls int
	last  execute.StepRequest
}

func (s *stubRuleDeriver) thresholdInputs(_ context.Context, req execute.StepRequest) (rules.PromotionApprovalInput, error) {
	s.calls++
	s.last = req
	return s.input, s.err
}

func TestServedRuleFactsCurrentThresholdInputsRejectsZeroCheckedAt(t *testing.T) {
	deriver := &stubRuleDeriver{}
	facts := &ServedRuleFacts{Thresholds: deriver}
	_, err := facts.currentThresholdInputs(context.Background(), rulethreshold.Decision{}, uuid.New(), intent.ProposalRevision{}, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "has no checked_at instant") {
		t.Fatalf("currentThresholdInputs with zero CheckedAt = %v, want fail-closed timestamp diagnostic", err)
	}
	if deriver.calls != 0 {
		t.Fatalf("threshold deriver calls = %d, want no call for zero CheckedAt", deriver.calls)
	}
}

// stubServedApprovalFacts reports one canned standing-approval set for
// whatever revision ServedRuleFacts.Lookup asks about.
type stubServedApprovalFacts struct {
	decisions []runtime.ApprovalDecisionFact
	err       error
}

func (s stubServedApprovalFacts) Decisions(context.Context, runtime.Executor, uuid.UUID, intent.ProposalRevision) ([]runtime.ApprovalDecisionFact, error) {
	return s.decisions, s.err
}

func servedFactsInput(t *testing.T, percent string) rules.PromotionApprovalInput {
	t.Helper()
	increase, err := values.NewDecimal(percent, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return rules.PromotionApprovalInput{
		IncreasePercent: increase,
		BandPosition:    rules.BandPositionInBand,
		BudgetAuthority: rules.BudgetAuthoritySufficient,
		GradeChange:     true,
	}
}

func servedFactsTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'served rule facts test', 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		tenantID, "servedfacts-"+tenantID.String()[:8])
	return tenantID
}

func servedFactsTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	conn := db.NewConn(t)
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func servedFactsRevision(intentID uuid.UUID, revision uint64, materialDigest string) intent.ProposalRevision {
	return intent.ProposalRevision{
		ProposalRevisionID: "revision:" + intentID.String(),
		IntentID:           intentID.String(),
		Revision:           revision,
		MaterialDigest:     digest.Reference{Digest: materialDigest, AlgorithmID: "sha256"},
	}
}

func servedFactsRecord(t *testing.T, tenantID, intentID uuid.UUID, in rules.PromotionApprovalInput) rulethreshold.Decision {
	t.Helper()
	return rulethreshold.Decision{
		TenantID: tenantID, IntentID: intentID, Revision: 3, Attempt: 1,
		InstanceID:   uuid.New(),
		Tier:         string(rules.ApprovalTierFinanceRequired),
		MatchedRow:   "row-finance-1",
		TableID:      rules.PromotionApprovalTableID,
		TableVersion: rules.PromotionApprovalTableVersion,
		TableDigest:  "sha256:table",
		InputDigest:  "sha256:inputs",
		Input:        in,
		RecordedAt:   time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
}

// TestServedRuleFactsLookupResolvesTheFrozenDecision proves the served
// RULE-004 read (REV-010-01): with a frozen threshold decision and a
// standing approval bound to the revision's own digest, Lookup resolves
// the frozen tier/row/table beside the live inputs the deriver reports.
// A revision that never ran the threshold node, or that carries no
// standing approval, resolves to silence so the guard keeps the
// WF-RUN-029 verdict exactly.
func TestServedRuleFactsLookupResolvesTheFrozenDecision(t *testing.T) {
	db := pgtest.New(t)
	tenantID := servedFactsTenant(t, db)
	intentID := uuid.New()
	materialDigest := "sha256:" + strings.Repeat("d", 64)
	frozen := servedFactsInput(t, "12.50")
	live := servedFactsInput(t, "15.00")
	rev := servedFactsRevision(intentID, 3, materialDigest)
	checkedAt := time.Date(2026, 9, 23, 17, 30, 0, 123000000, time.FixedZone("test", -4*60*60))

	servedFactsTx(t, db, tenantID, func(tx dbport.Tx) error {
		return rulethreshold.Record(context.Background(), tx, servedFactsRecord(t, tenantID, intentID, frozen))
	})

	facts := &ServedRuleFacts{
		Thresholds: &stubRuleDeriver{input: live},
		Approval: stubServedApprovalFacts{decisions: []runtime.ApprovalDecisionFact{{
			DecisionID: "decision:served-1", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: materialDigest,
		}}},
	}
	var got execute.RuleApproval
	servedFactsTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		got, err = facts.Lookup(context.Background(), tx, tenantID, rev, checkedAt)
		return err
	})
	if !got.Resolved {
		t.Fatal("Lookup resolved nothing for a frozen, approved revision")
	}
	if got.Approved.Tier != rules.ApprovalTierFinanceRequired || got.Approved.MatchedRowID != "row-finance-1" ||
		got.Approved.TableVersion != rules.PromotionApprovalTableVersion || got.Approved.InputDigest != "sha256:inputs" {
		t.Fatalf("frozen record = %+v, want the recorded tier/row/table", got.Approved)
	}
	if !got.Approved.Input.IncreasePercent.Equal(frozen.IncreasePercent) {
		t.Fatalf("frozen input = %+v, want %+v", got.Approved.Input, frozen)
	}
	if !got.Current.IncreasePercent.Equal(live.IncreasePercent) {
		t.Fatalf("live input = %+v, want %+v", got.Current, live)
	}
	if facts.Thresholds.(*stubRuleDeriver).calls != 1 {
		t.Fatal("Lookup did not re-derive the live inputs through the governed path")
	}
	if gotAt := facts.Thresholds.(*stubRuleDeriver).last.RecordedAt; !gotAt.Equal(checkedAt) || gotAt.Location() != time.UTC {
		t.Fatalf("threshold revalidation RecordedAt = %s (%s), want %s normalized to UTC", gotAt.Format(time.RFC3339Nano), gotAt.Location(), checkedAt.Format(time.RFC3339Nano))
	}
}

// TestServedRuleFactsLookupSilentWithoutDecisionOrApproval proves the
// two silence cases: an unknown revision and a frozen revision with no
// standing approval both resolve to the zero value rather than an
// invented approval context.
func TestServedRuleFactsLookupSilentWithoutDecisionOrApproval(t *testing.T) {
	db := pgtest.New(t)
	tenantID := servedFactsTenant(t, db)
	intentID := uuid.New()
	materialDigest := "sha256:" + strings.Repeat("e", 64)
	rev := servedFactsRevision(intentID, 3, materialDigest)

	// No frozen decision at all.
	silent := &ServedRuleFacts{
		Thresholds: &stubRuleDeriver{input: servedFactsInput(t, "12.50")},
		Approval:   stubServedApprovalFacts{},
	}
	servedFactsTx(t, db, tenantID, func(tx dbport.Tx) error {
		got, err := silent.Lookup(context.Background(), tx, tenantID, rev, time.Date(2026, 9, 23, 16, 0, 0, 0, time.UTC))
		if err != nil {
			return err
		}
		if got.Resolved {
			t.Fatalf("Lookup = %+v, want silence for a revision that never ran the threshold node", got)
		}
		return nil
	})

	// Frozen decision but no standing approval.
	servedFactsTx(t, db, tenantID, func(tx dbport.Tx) error {
		return rulethreshold.Record(context.Background(), tx, servedFactsRecord(t, tenantID, intentID, servedFactsInput(t, "12.50")))
	})
	servedFactsTx(t, db, tenantID, func(tx dbport.Tx) error {
		got, err := silent.Lookup(context.Background(), tx, tenantID, rev, time.Date(2026, 9, 23, 16, 0, 0, 0, time.UTC))
		if err != nil {
			return err
		}
		if got.Resolved {
			t.Fatalf("Lookup = %+v, want silence without a standing approval", got)
		}
		return nil
	})
}

// TestServedRuleFactsLookupRefusesMiscomposition proves the loud
// failures: nil ports are a composition mistake, not an unchanged-inputs
// verdict, and a non-uuid intent id cannot key the threshold table.
func TestServedRuleFactsLookupRefusesMiscomposition(t *testing.T) {
	db := pgtest.New(t)
	tenantID := servedFactsTenant(t, db)
	intentID := uuid.New()
	rev := servedFactsRevision(intentID, 3, "sha256:"+strings.Repeat("f", 64))

	servedFactsTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := (&ServedRuleFacts{Approval: stubServedApprovalFacts{}}).Lookup(context.Background(), tx, tenantID, rev, time.Date(2026, 9, 23, 16, 0, 0, 0, time.UTC)); err == nil {
			t.Fatal("nil threshold inputs resolved")
		}
		if _, err := (&ServedRuleFacts{Thresholds: &stubRuleDeriver{}}).Lookup(context.Background(), tx, tenantID, rev, time.Date(2026, 9, 23, 16, 0, 0, 0, time.UTC)); err == nil {
			t.Fatal("nil approval facts resolved")
		}
		bad := servedFactsRevision(uuid.Nil, 3, "sha256:"+strings.Repeat("a", 64))
		bad.IntentID = "intent:retry:synthetic"
		if _, err := (&ServedRuleFacts{Thresholds: &stubRuleDeriver{}, Approval: stubServedApprovalFacts{}}).Lookup(context.Background(), tx, tenantID, bad, time.Date(2026, 9, 23, 16, 0, 0, 0, time.UTC)); err == nil {
			t.Fatal("a non-uuid intent id keyed the threshold table")
		}
		return nil
	})
}
