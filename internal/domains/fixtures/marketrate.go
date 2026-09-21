package fixtures

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Static market-rate anchors for tests and local runs. These are demo data,
// not market facts: each anchor sits near its demo band midpoint so the
// high-performer variant exercises the market-informed raise floor without
// pretending a vendor answered. A test that needs a different market uses
// Add or builds its own rewards.MarketRateSource.
type staticMarketAnchor struct {
	scope    payband.Scope
	currency string
	p25      string
	p50      string
	p75      string
	asOf     string
}

// stubMarketAnchors covers the demo promotion scopes in USD.
var stubMarketAnchors = []staticMarketAnchor{
	{scope: payband.Scope{JobCode: "CARE-CC2", Grade: "P2", PayZone: "US-EAST"}, currency: "USD", p25: "62000.00", p50: "70000.00", p75: "78000.00", asOf: "2026-01-01"},
	{scope: payband.Scope{JobCode: "CARE-CC3", Grade: "P3", PayZone: "US-EAST"}, currency: "USD", p25: "74000.00", p50: "83000.00", p75: "92000.00", asOf: "2026-01-01"},
	{scope: payband.Scope{JobCode: "OPS-HRBP3", Grade: "P3", PayZone: "US-EAST"}, currency: "USD", p25: "104000.00", p50: "112000.00", p75: "120000.00", asOf: "2026-01-01"},
}

// MemoryMarketRateCatalog is an in-memory rewards.MarketRateSource over the
// static stub table.
type MemoryMarketRateCatalog struct {
	anchors []staticMarketAnchor
	// Fail, when set, is returned instead of a lookup result. It exists so a
	// test can prove that a source fault is an error rather than a silent
	// "no market".
	Fail error
}

// NewMemoryMarketRateCatalog builds the in-memory source over the stub table.
func NewMemoryMarketRateCatalog() *MemoryMarketRateCatalog {
	return &MemoryMarketRateCatalog{anchors: stubMarketAnchors}
}

// Add appends one anchor to the table. It exists for tests that need a
// market outside the stub scopes.
func (c *MemoryMarketRateCatalog) Add(anchor staticMarketAnchor) {
	c.anchors = append(c.anchors, anchor)
}

// LookupMarketRate implements rewards.MarketRateSource.
func (c *MemoryMarketRateCatalog) LookupMarketRate(_ context.Context, q rewards.MarketQuery) (rewards.MarketRecord, error) {
	if c.Fail != nil {
		return rewards.MarketRecord{}, c.Fail
	}
	if err := q.Validate(); err != nil {
		return rewards.MarketRecord{}, err
	}
	for _, entry := range c.anchors {
		if entry.scope != q.Scope() || entry.currency != q.Currency {
			continue
		}
		p25, err := Money(entry.p25, entry.currency)
		if err != nil {
			return rewards.MarketRecord{}, err
		}
		p50, err := Money(entry.p50, entry.currency)
		if err != nil {
			return rewards.MarketRecord{}, err
		}
		p75, err := Money(entry.p75, entry.currency)
		if err != nil {
			return rewards.MarketRecord{}, err
		}
		asOf, err := values.ParseLocalDate(entry.asOf)
		if err != nil {
			return rewards.MarketRecord{}, err
		}
		recorded, err := values.NewRecordedAt(mustMarketInstant())
		if err != nil {
			return rewards.MarketRecord{}, err
		}
		return rewards.MarketRecord{
			Scope:         entry.scope,
			Anchor:        rewards.MarketAnchor{P25: p25, P50: p50, P75: p75, AsOf: asOf},
			SourceVersion: "rewards.market.stub/2026.1",
			Authority: evidence.SourceAuthority{
				Kind:      evidence.AuthorityExternalObservation,
				System:    "hcmnext.rewards.market.stub",
				PolicyRef: "rewards.market_source/2026.1",
			},
			Provenance: evidence.Provenance{
				Source:      "hcmnext.rewards.market.stub",
				EvidenceRef: "evd_market_stub_" + strings.ToLower(entry.scope.JobCode) + "_" + strings.ToLower(entry.scope.Grade),
				RecordedAt:  recorded,
			},
		}, nil
	}
	return rewards.MarketRecord{}, fmt.Errorf("%w: %s/%s/%s in %s",
		rewards.ErrMarketRateNotFound, q.JobCode, q.Grade, q.PayZone, q.Currency)
}

// mustMarketInstant is the stub's own publication instant. It is fixed so
// the table is deterministic; a test that needs time to move builds its own
// source.
func mustMarketInstant() (t values.Instant) {
	t, err := instant("2026-01-02T00:00:00Z")
	if err != nil {
		panic(err)
	}
	return t
}
