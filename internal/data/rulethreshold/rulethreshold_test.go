package rulethreshold_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rulethreshold"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func seedTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'rulethreshold test', 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		tenantID, "rulethreshold-"+tenantID.String()[:8])
	return tenantID
}

func inTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func frozenInput(t *testing.T) rules.PromotionApprovalInput {
	t.Helper()
	percent, err := values.NewDecimal("12.50", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return rules.PromotionApprovalInput{
		IncreasePercent: percent,
		BandPosition:    rules.BandPositionInBand,
		BudgetAuthority: rules.BudgetAuthoritySufficient,
		GradeChange:     true,
	}
}

func frozenDecision(tenantID, intentID uuid.UUID) rulethreshold.Decision {
	return rulethreshold.Decision{
		TenantID: tenantID, IntentID: intentID, Revision: 3, Attempt: 1,
		InstanceID:   uuid.New(),
		Tier:         string(rules.ApprovalTierFinanceRequired),
		MatchedRow:   "row-finance-1",
		TableID:      rules.PromotionApprovalTableID,
		TableVersion: rules.PromotionApprovalTableVersion,
		TableDigest:  "sha256:table",
		InputDigest:  "sha256:inputs",
		RecordedAt:   time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
}

// TestThresholdDecisionRoundtrip proves the frozen decision survives the
// store byte-for-byte: tier, row, table identity, input digest and the
// exact typed inputs come back, latest attempt wins, and an unknown
// revision reads back absent rather than zero.
func TestThresholdDecisionRoundtrip(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db)
	intentID := uuid.New()
	want := frozenDecision(tenantID, intentID)
	want.Input = frozenInput(t)

	inTx(t, db, tenantID, func(tx dbport.Tx) error { return rulethreshold.Record(context.Background(), tx, want) })
	// Identical re-record is a no-op, not a second row.
	inTx(t, db, tenantID, func(tx dbport.Tx) error { return rulethreshold.Record(context.Background(), tx, want) })

	var got rulethreshold.Decision
	var found bool
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		got, found, err = rulethreshold.Latest(context.Background(), tx, tenantID, intentID, 3)
		return err
	})
	if !found {
		t.Fatal("no decision found")
	}
	if got.Tier != want.Tier || got.MatchedRow != want.MatchedRow || got.TableVersion != want.TableVersion ||
		got.TableDigest != want.TableDigest || got.InputDigest != want.InputDigest || got.Attempt != 1 ||
		got.InstanceID != want.InstanceID || !got.RecordedAt.Equal(want.RecordedAt) {
		t.Fatalf("decision = %+v, want %+v", got, want)
	}
	if !got.Input.IncreasePercent.Equal(want.Input.IncreasePercent) ||
		got.Input.IncreasePercent.String() != want.Input.IncreasePercent.String() ||
		got.Input.BandPosition != want.Input.BandPosition ||
		got.Input.BudgetAuthority != want.Input.BudgetAuthority ||
		got.Input.GradeChange != want.Input.GradeChange {
		t.Fatalf("inputs = %+v, want %+v", got.Input, want.Input)
	}

	// A second attempt supersedes the first on read.
	second := want
	second.Attempt = 2
	second.Tier = string(rules.ApprovalTierExecutiveRequired)
	inTx(t, db, tenantID, func(tx dbport.Tx) error { return rulethreshold.Record(context.Background(), tx, second) })
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		got, found, err = rulethreshold.Latest(context.Background(), tx, tenantID, intentID, 3)
		return err
	})
	if !found || got.Attempt != 2 || got.Tier != string(rules.ApprovalTierExecutiveRequired) {
		t.Fatalf("latest = %+v %v", got, found)
	}

	// Unknown revision reads absent.
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		_, found, err = rulethreshold.Latest(context.Background(), tx, tenantID, intentID, 9)
		return err
	})
	if found {
		t.Fatal("unknown revision resolved")
	}
}

// TestThresholdDecisionConflict proves a second, different decision for one
// key is refused and the frozen one stands.
func TestThresholdDecisionConflict(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db)
	intentID := uuid.New()
	first := frozenDecision(tenantID, intentID)
	first.Input = frozenInput(t)
	inTx(t, db, tenantID, func(tx dbport.Tx) error { return rulethreshold.Record(context.Background(), tx, first) })

	moved := first
	moved.Tier = string(rules.ApprovalTierStandard)
	moved.InputDigest = "sha256:other"
	var conflict error
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		conflict = rulethreshold.Record(context.Background(), tx, moved)
		return nil
	})
	if !errors.Is(conflict, rulethreshold.ErrConflict) {
		t.Fatalf("conflict = %v, want %v", conflict, rulethreshold.ErrConflict)
	}
	var got rulethreshold.Decision
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		got, _, err = rulethreshold.Latest(context.Background(), tx, tenantID, intentID, 3)
		return err
	})
	if got.Tier != first.Tier || got.InputDigest != first.InputDigest {
		t.Fatalf("frozen decision moved: %+v", got)
	}
}
