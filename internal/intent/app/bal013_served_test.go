package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/balance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_BAL_013_Served(t *testing.T) {
	cell, err := NewCell(CellConfig{
		Store:    newMemLifecycleStore(),
		Audience: "hcm-next-api",
		Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) {
			return nil, errors.New("verification bypassed")
		}),
	})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	if cell.BalanceIndex == nil {
		t.Fatal("served cell has no balance dependency index")
	}

	component := balance.PlanComponent{
		PlanID: "plan-served", ComponentID: "paid-time", Sources: []string{"bucket:vacation"},
		Available: values.MustDecimal("80.00", 2, values.RoundingExactRequired),
		Paid:      values.MustDecimal("32.00", 2, values.RoundingExactRequired),
		Unpaid:    values.MustDecimal("8.00", 2, values.RoundingExactRequired), Revision: "rev-1",
	}
	if err := cell.BalanceIndex.Register(component); err != nil {
		t.Fatalf("Register: %v", err)
	}
	findings, err := cell.BalanceIndex.Invalidate(balance.AvailabilityEvent{
		Kind: balance.EventDebit, SourceID: "bucket:vacation", OldRevision: "rev-1", NewRevision: "rev-2",
		OldAvailable: values.MustDecimal("80.00", 2, values.RoundingExactRequired),
		NewAvailable: values.MustDecimal("72.00", 2, values.RoundingExactRequired),
	})
	if err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if len(findings) != 1 || findings[0].Finding != "REPLAN_REQUIRED" || findings[0].PlanID != "plan-served" {
		t.Fatalf("findings = %+v, want one served REPLAN_REQUIRED finding", findings)
	}
}
