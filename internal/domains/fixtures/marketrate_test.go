package fixtures

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func stubMarketQuery(t *testing.T, job, grade, zone, currency string) rewards.MarketQuery {
	t.Helper()
	asOf, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	return rewards.MarketQuery{
		Tenant:   Tenant,
		JobCode:  job,
		Grade:    grade,
		PayZone:  zone,
		Currency: currency,
		AsOf:     asOf,
	}
}

// TestTodo_HIPERF_001 proves the static stub table answers the demo scopes
// with ordered single-currency anchors, misses any other scope with the
// typed miss, and surfaces an injected fault instead of a silent "no
// market".
func TestTodo_HIPERF_001(t *testing.T) {
	ctx := context.Background()
	catalog := NewMemoryMarketRateCatalog()

	t.Run("demo scope resolves with an ordered anchor", func(t *testing.T) {
		record, err := catalog.LookupMarketRate(ctx, stubMarketQuery(t, "CARE-CC3", "P3", "US-EAST", "USD"))
		if err != nil {
			t.Fatalf("LookupMarketRate: %v", err)
		}
		if record.SourceVersion == "" {
			t.Fatal("stub record is unpinned")
		}
		if err := record.Validate("USD"); err != nil {
			t.Fatalf("stub record does not validate: %v", err)
		}
		lo, err := record.Anchor.P25.Cmp(record.Anchor.P50)
		if err != nil || lo > 0 {
			t.Fatalf("stub p25/p50 unordered: %v", err)
		}
		hi, err := record.Anchor.P50.Cmp(record.Anchor.P75)
		if err != nil || hi > 0 {
			t.Fatalf("stub p50/p75 unordered: %v", err)
		}
		eval, err := rewards.LookupMarketRate(ctx, catalog, stubMarketQuery(t, "CARE-CC3", "P3", "US-EAST", "USD"))
		if err != nil {
			t.Fatalf("governed lookup over the stub: %v", err)
		}
		if eval.SourceVersion != record.SourceVersion || eval.ResultDigest == "" {
			t.Fatalf("governed lookup does not cite the stub record: %+v", eval)
		}
	})

	t.Run("unknown scope is a typed miss", func(t *testing.T) {
		_, err := catalog.LookupMarketRate(ctx, stubMarketQuery(t, "NOPE-X9", "P9", "US-EAST", "USD"))
		if !errors.Is(err, rewards.ErrMarketRateNotFound) {
			t.Fatalf("unknown scope = %v, want ErrMarketRateNotFound", err)
		}
	})

	t.Run("wrong currency misses rather than converts", func(t *testing.T) {
		_, err := catalog.LookupMarketRate(ctx, stubMarketQuery(t, "CARE-CC3", "P3", "US-EAST", "EUR"))
		if !errors.Is(err, rewards.ErrMarketRateNotFound) {
			t.Fatalf("wrong currency = %v, want ErrMarketRateNotFound", err)
		}
	})

	t.Run("injected fault is returned, not swallowed", func(t *testing.T) {
		boom := errors.New("stub fault")
		catalog.Fail = boom
		defer func() { catalog.Fail = nil }()
		_, err := catalog.LookupMarketRate(ctx, stubMarketQuery(t, "CARE-CC3", "P3", "US-EAST", "USD"))
		if !errors.Is(err, boom) {
			t.Fatalf("injected fault = %v, want it returned", err)
		}
	})
}
