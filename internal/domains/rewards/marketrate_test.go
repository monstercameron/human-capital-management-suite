package rewards_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// scriptMarketSource is a programmable rewards.MarketRateSource. Answer is
// returned verbatim so a test can prove the governed lookup checks the
// answer rather than trusting the source.
type scriptMarketSource struct {
	answer rewards.MarketRecord
	err    error
}

func (s scriptMarketSource) LookupMarketRate(context.Context, rewards.MarketQuery) (rewards.MarketRecord, error) {
	if s.err != nil {
		return rewards.MarketRecord{}, s.err
	}
	return s.answer, nil
}

func marketMoney(t *testing.T, text string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func marketQuery() rewards.MarketQuery {
	asOf, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		panic(err)
	}
	return rewards.MarketQuery{
		Tenant:   fixtures.Tenant,
		JobCode:  "CARE-CC3",
		Grade:    "P3",
		PayZone:  "US-EAST",
		Currency: "USD",
		AsOf:     asOf,
	}
}

func marketRecord() rewards.MarketRecord {
	asOf, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		panic(err)
	}
	mustMoney := func(text string) values.Money {
		m, err := values.NewMoney(text, "USD", 2, values.RoundingHalfEven)
		if err != nil {
			panic(err)
		}
		return m
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		panic(err)
	}
	return rewards.MarketRecord{
		Scope:         payband.Scope{JobCode: "CARE-CC3", Grade: "P3", PayZone: "US-EAST"},
		Anchor:        rewards.MarketAnchor{P25: mustMoney("74000.00"), P50: mustMoney("83000.00"), P75: mustMoney("92000.00"), AsOf: asOf},
		SourceVersion: "rewards.market.stub/2026.1",
		Authority: evidence.SourceAuthority{
			Kind:      evidence.AuthorityExternalObservation,
			System:    "hcmnext.rewards.market.stub",
			PolicyRef: "rewards.market_source/2026.1",
		},
		Provenance: evidence.Provenance{
			Source:      "hcmnext.rewards.market.stub",
			EvidenceRef: "evd_market_stub_care-cc3_p3",
			RecordedAt:  recorded,
		},
	}
}

// TestTodo_HIPERF_001 proves the market-rate read port: a covering source
// yields a citable evaluation with stable digests, while a miss, a fault, a
// wrong-scope answer, a mixed-currency leg, unordered legs and an unpinned
// answer each fail loudly with a matchable sentinel.
func TestTodo_HIPERF_001(t *testing.T) {
	ctx := context.Background()
	query := marketQuery()

	t.Run("covering source yields a citable evaluation", func(t *testing.T) {
		got, err := rewards.LookupMarketRate(ctx, scriptMarketSource{answer: marketRecord()}, query)
		if err != nil {
			t.Fatalf("LookupMarketRate: %v", err)
		}
		if got.IntentType != rewards.MarketRateIntentType || got.IntentVersion != rewards.MarketRateIntentVersion {
			t.Fatalf("intent = %s/%s, want %s/%s", got.IntentType, got.IntentVersion, rewards.MarketRateIntentType, rewards.MarketRateIntentVersion)
		}
		if got.Anchor.P50.String() != marketMoney(t, "83000.00").String() {
			t.Fatalf("p50 = %s, want 83000.00 USD", got.Anchor.P50)
		}
		if got.InputsDigest == "" || got.ResultDigest == "" {
			t.Fatal("evaluation carries no digests")
		}
		if got.Effects != evidence.ZeroEffects() {
			t.Fatalf("a read-only lookup records effects: %+v", got.Effects)
		}
		again, err := rewards.LookupMarketRate(ctx, scriptMarketSource{answer: marketRecord()}, query)
		if err != nil {
			t.Fatalf("second LookupMarketRate: %v", err)
		}
		if again.ResultDigest != got.ResultDigest || again.InputsDigest != got.InputsDigest {
			t.Fatal("identical lookups digest differently")
		}
		if got.Canonical() == nil {
			t.Fatal("evaluation has no canonical encoding")
		}
	})

	t.Run("miss is a typed miss, not a wrapped fault", func(t *testing.T) {
		_, err := rewards.LookupMarketRate(ctx, scriptMarketSource{err: rewards.ErrMarketRateNotFound}, query)
		if !errors.Is(err, rewards.ErrMarketRateNotFound) {
			t.Fatalf("miss = %v, want ErrMarketRateNotFound", err)
		}
		if errors.Is(err, rewards.ErrMarketSourceFailed) {
			t.Fatalf("miss is wrapped as a source fault: %v", err)
		}
	})

	t.Run("fault wraps as source failure", func(t *testing.T) {
		boom := errors.New("connection refused")
		_, err := rewards.LookupMarketRate(ctx, scriptMarketSource{err: boom}, query)
		if !errors.Is(err, rewards.ErrMarketSourceFailed) || !errors.Is(err, boom) {
			t.Fatalf("fault = %v, want ErrMarketSourceFailed wrapping the cause", err)
		}
	})

	t.Run("wrong-scope answer is refused", func(t *testing.T) {
		record := marketRecord()
		record.Scope = payband.Scope{JobCode: "CARE-CC2", Grade: "P2", PayZone: "US-EAST"}
		_, err := rewards.LookupMarketRate(ctx, scriptMarketSource{answer: record}, query)
		if !errors.Is(err, rewards.ErrMarketScopeMismatch) {
			t.Fatalf("wrong scope = %v, want ErrMarketScopeMismatch", err)
		}
	})

	t.Run("mixed-currency leg is refused", func(t *testing.T) {
		record := marketRecord()
		eur, err := values.NewMoney("83000.00", "EUR", 2, values.RoundingHalfEven)
		if err != nil {
			t.Fatal(err)
		}
		anchor := record.Anchor
		anchor.P50 = eur
		record.Anchor = anchor
		_, err = rewards.LookupMarketRate(ctx, scriptMarketSource{answer: record}, query)
		if !errors.Is(err, rewards.ErrMarketScopeMismatch) {
			t.Fatalf("mixed currency = %v, want ErrMarketScopeMismatch", err)
		}
	})

	t.Run("unordered legs are refused", func(t *testing.T) {
		record := marketRecord()
		anchor := record.Anchor
		anchor.P25, anchor.P75 = anchor.P75, anchor.P25
		record.Anchor = anchor
		_, err := rewards.LookupMarketRate(ctx, scriptMarketSource{answer: record}, query)
		if !errors.Is(err, rewards.ErrMarketAnchorInvalid) {
			t.Fatalf("unordered legs = %v, want ErrMarketAnchorInvalid", err)
		}
	})

	t.Run("unpinned answer is refused", func(t *testing.T) {
		record := marketRecord()
		record.SourceVersion = ""
		_, err := rewards.LookupMarketRate(ctx, scriptMarketSource{answer: record}, query)
		if !errors.Is(err, rewards.ErrMarketSourceUnpinned) {
			t.Fatalf("unpinned = %v, want ErrMarketSourceUnpinned", err)
		}
	})

	t.Run("nil source is refused", func(t *testing.T) {
		if _, err := rewards.LookupMarketRate(ctx, nil, query); !errors.Is(err, rewards.ErrMarketQueryInvalid) {
			t.Fatalf("nil source = %v, want ErrMarketQueryInvalid", err)
		}
	})

	t.Run("malformed query is refused", func(t *testing.T) {
		bad := query
		bad.JobCode = ""
		if _, err := rewards.LookupMarketRate(ctx, scriptMarketSource{answer: marketRecord()}, bad); !errors.Is(err, rewards.ErrMarketQueryInvalid) {
			t.Fatalf("empty job code = %v, want ErrMarketQueryInvalid", err)
		}
		if bad.Canonical() != nil {
			t.Fatal("an invalid query has a canonical encoding")
		}
	})
}
