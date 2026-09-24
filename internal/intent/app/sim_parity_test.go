package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// parityCatalog returns the pinned conformance band catalog the served cell
// reads bands from in tests.
func parityCatalog(t *testing.T) *fixtures.MemoryBandCatalog {
	t.Helper()
	c, err := fixtures.NewMemoryBandCatalog()
	if err != nil {
		t.Fatalf("band catalog fixture: %v", err)
	}
	return c
}

// parityInput is the canonical simulation the rewards engine's own tests
// pin: the legacy 93,000 -> 98,000 USD annual raise with a 5% bonus target,
// evaluated against the OPS-HRBP3 band. Reusing the engine's canonical
// input is the point: the served answer must match the engine's answer on
// the engine's own question.
func parityInput(t *testing.T) rewards.SimulateCompensationInput {
	t.Helper()
	mkMoney := func(text string) values.Money {
		m, err := fixtures.Money(text, "USD")
		if err != nil {
			t.Fatalf("money: %v", err)
		}
		return m
	}
	pct, err := fixtures.Percent("0.0500")
	if err != nil {
		t.Fatalf("percent: %v", err)
	}
	asOf, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("date: %v", err)
	}
	mkSnapshot := func(amount string, seq uint64) rewards.CompensationSnapshot {
		rev, err := values.NewSequenceRevision("rewards.package.omar", seq)
		if err != nil {
			t.Fatalf("revision: %v", err)
		}
		return rewards.CompensationSnapshot{
			Base:               values.Value(mkMoney(amount)),
			PayBasis:           rewards.PayBasisAnnualSalary,
			EffectiveDate:      asOf,
			Watermark:          rev,
			Complete:           true,
			BonusTargetPercent: values.Value(pct),
		}
	}
	ref, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("subject: %v", err)
	}
	q := rewards.BandQuery{
		Tenant:   fixtures.Tenant,
		JobCode:  "OPS-HRBP3",
		Grade:    "P3",
		PayZone:  "US-EAST",
		Currency: "USD",
		AsOf:     asOf,
	}
	return rewards.SimulateCompensationInput{
		Tenant:        fixtures.Tenant,
		Subject:       ref,
		Current:       mkSnapshot("93000.00", 11),
		Proposed:      mkSnapshot("98000.00", 11),
		Annualization: rewards.DefaultAnnualization(),
		EffectiveDate: asOf,
		Band:          &q,
	}
}

// TestTodo_COMP_006_Integration proves the served
// simulate_compensation binding answers with the real rewards engine, not a
// stub or an echo: the capability-table handler from handlerFor, driven with
// the engine's canonical input against the served catalog, returns a result
// identical to calling the engine directly, with the same pinned versions,
// result digest, and zero-effect receipt. Compensation simulation takes its
// authoritative facts as explicit pinned snapshots, so this integration is
// the application binding plus the real calculation engine and catalog.
func TestTodo_COMP_006_Integration(t *testing.T) {
	ctx := context.Background()
	catalog := parityCatalog(t)
	served := &domainHandlers{bands: catalog}

	handle := served.handlerFor(rewards.SimulateCompensationIntentType)
	if handle == nil {
		t.Fatal("no handler bound for simulate_compensation on the serve path")
	}
	in := parityInput(t)
	got, err := handle(ctx, in)
	if err != nil {
		t.Fatalf("served simulate_compensation: %v", err)
	}
	answer, ok := got.(compensationAnswer)
	if !ok {
		t.Fatalf("served simulate_compensation returned %T, want compensationAnswer", got)
	}
	servedResult := answer.Simulation
	if answer.PreflightPlan.Digest == "" {
		t.Fatal("served simulate_compensation returned no universal preflight digest")
	}

	direct, err := rewards.SimulateCompensation(ctx, catalog, in)
	if err != nil {
		t.Fatalf("direct engine simulate: %v", err)
	}

	if servedResult.ResultDigest != direct.ResultDigest {
		t.Fatalf("served digest %q != engine digest %q: the serve path is not running the engine's answer", servedResult.ResultDigest, direct.ResultDigest)
	}
	if !reflect.DeepEqual(servedResult, direct) {
		t.Fatal("served result differs from the direct engine result beyond its digest")
	}
	if got := servedResult.Delta.AnnualizedBase.String(); got != "5000.00 USD" {
		t.Fatalf("served annualized base delta = %s, want 5000.00 USD for the 93,000 -> 98,000 raise", got)
	}
	if !servedResult.Effects.IsZero() || servedResult.Receipt.ResultDigest != servedResult.ResultDigest {
		t.Fatalf("served simulation violated zero-effect receipt: effects=%v receipt=%+v", servedResult.Effects.NonZero(), servedResult.Receipt)
	}
}

// TestTodo_Unit4_ServedSimRejectsBadInput proves the served binding does not
// launder invalid input into a plausible answer: what the engine refuses,
// the serve path refuses.
func TestTodo_Unit4_ServedSimRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	served := &domainHandlers{bands: parityCatalog(t)}
	handle := served.handlerFor(rewards.SimulateCompensationIntentType)

	in := parityInput(t)
	in.Proposed.Complete = false
	if _, err := handle(ctx, in); err == nil {
		t.Fatal("served simulate_compensation accepted an incomplete proposed snapshot the engine must refuse")
	}
	if _, err := rewards.SimulateCompensation(ctx, parityCatalog(t), in); err == nil {
		t.Fatal("contract check: the engine itself accepts the incomplete snapshot, the test proves nothing")
	}
}
