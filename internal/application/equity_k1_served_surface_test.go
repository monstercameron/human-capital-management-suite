package application_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/equity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func servedEquityDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	value, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func servedEquityDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	value, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func servedEquityInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(parsed)
}

// TestTodo_EQUITY_001_Served proves the equity contract is reachable through
// the application package composed by shipped commands, not only by a direct
// domain-package import.
func TestTodo_EQUITY_001_Served(t *testing.T) {
	var app application.App
	surface := app.Equity()
	if surface.Version == nil || surface.NewPlanRevision == nil || surface.NewGrant == nil || surface.NewAcceptance == nil || surface.ValidateGrant == nil || surface.ComputeVesting == nil || surface.Accept == nil || surface.Activate == nil || surface.Cancel == nil || surface.Settle == nil || surface.Explain == nil {
		t.Fatal("served equity surface is incomplete")
	}

	plan, err := surface.NewPlanRevision(equity.EquityPlanRevision{
		PlanID: "plan-served", Revision: 1, Name: "Served Incentive Plan", PoolRef: "pool-served",
		AuthorizedQuantity: servedEquityDecimal(t, "1000.00"), Currency: "USD",
		InstrumentKinds: []equity.InstrumentKind{equity.InstrumentOption}, ApprovalRef: "approval-served",
	})
	if err != nil || surface.Version() != 1 {
		t.Fatalf("served plan construction: plan=%+v err=%v version=%d", plan, err, surface.Version())
	}
	grant, err := surface.NewGrant(equity.EquityGrant{
		GrantID: "grant-served", Revision: 1, PlanDigest: plan.CanonicalDigest, PlanRevision: plan.Revision,
		PoolRef: plan.PoolRef, WorkerRef: "worker-served", Instrument: equity.InstrumentOption,
		Quantity: servedEquityDecimal(t, "100.00"), GrantDate: servedEquityDate(t, "2026-01-31"),
		StrikePrice: servedEquityDecimal(t, "10.00"), Currency: "USD",
		Vesting: equity.VestingSchedule{CalendarRule: equity.CalendarGregorian, CliffMonths: 12, PeriodicMonths: 3, TrancheCount: 4}, State: equity.GrantProposed,
	})
	if err != nil {
		t.Fatalf("served grant construction: %v", err)
	}
	if err := surface.ValidateGrant(plan, grant); err != nil {
		t.Fatalf("served plan/grant binding: %v", err)
	}
	tranches, err := surface.ComputeVesting(grant.Vesting, grant.GrantDate, grant.Quantity)
	if err != nil || len(tranches) != 4 || tranches[0].VestsOn.String() != "2027-01-31" {
		t.Fatalf("served vesting: tranches=%+v err=%v", tranches, err)
	}

	event, err := surface.NewAcceptance(equity.AcceptanceEvent{
		EventID: "acceptance-served", GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision,
		AcceptedBy: "worker-served", AcceptedAt: servedEquityInstant(t, "2026-02-01T12:00:00Z"), EvidenceRef: "signed-served",
	})
	if err != nil {
		t.Fatalf("served acceptance construction: %v", err)
	}
	accepted, err := surface.Accept(grant, event)
	if err != nil || accepted.State != equity.GrantAccepted {
		t.Fatalf("served acceptance: grant=%+v err=%v", accepted, err)
	}
	active, err := surface.Activate(accepted, "activation-served")
	if err != nil || active.State != equity.GrantActive {
		t.Fatalf("served activation: grant=%+v err=%v", active, err)
	}
	settled, err := surface.Settle(active, "settlement-served")
	if err != nil || settled.State != equity.GrantSettled {
		t.Fatalf("served settlement: grant=%+v err=%v", settled, err)
	}
	if _, err := surface.Cancel(grant, "invalid-transition"); !errors.Is(err, equity.ErrGrantTransition) {
		t.Fatalf("served invalid transition=%v, want ErrGrantTransition", err)
	}
	explanation, err := surface.Explain(settled)
	if err != nil || explanation.Digest != settled.CanonicalDigest || explanation.State != equity.GrantSettled {
		t.Fatalf("served explanation=%+v err=%v", explanation, err)
	}

	var nilApp *application.App
	if exposed := nilApp.Equity(); exposed.Version != nil || exposed.NewGrant != nil {
		t.Fatal("nil application exposed equity capabilities")
	}
}
