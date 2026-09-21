package rewards

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// MarketAnchorInput is the optional market position a compensation
// simulation floors the raise against (HIPERF-003). It is the workflow
// fetch_market_rate node's answer carried into the simulation request: the
// anchor itself, the currency it is denominated in, and the source version
// that lets a reviewer cite it. A nil input means no market floor; every
// encoding of a nil input is absent, so pre-market goldens stay
// byte-identical.
type MarketAnchorInput struct {
	Anchor MarketAnchor
	// Currency is the anchor's denomination. It must match the comparison
	// currency (the proposed currency, or the pinned FX target when the
	// simulation converts): there is no implicit FX in P1A.
	Currency string
	// SourceVersion pins the published source the anchor was read from. An
	// unpinned anchor cannot be cited in a proposal digest.
	SourceVersion string
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (m MarketAnchorInput) Canonical() []byte {
	anchor := m.Anchor.Canonical(m.Currency)
	if anchor == nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.MarketAnchorInput", rewardsSchemaVer).
		Field("anchor", anchor).
		String("currency", m.Currency).
		String("source_version", m.SourceVersion).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// withDefaultAsOf returns the input with an empty anchor as-of date defaulted
// to today, without mutating the receiver.
func (m MarketAnchorInput) withDefaultAsOf(today values.LocalDate) MarketAnchorInput {
	out := m
	if out.Anchor.AsOf.Validate() != nil {
		out.Anchor.AsOf = today
	}
	return out
}

// resolve defaults an empty anchor as-of date to today: the simulation's
// effective date is the business "today" the market position describes. It
// reports whether the input can support a market floor.
func (m MarketAnchorInput) resolve(today values.LocalDate, currency string) (MarketAnchor, error) {
	if m.SourceVersion == "" {
		return MarketAnchor{}, fmt.Errorf("%w: market anchor names no source version", ErrSimulationInputInvalid)
	}
	if m.Currency == "" || m.Currency != currency {
		return MarketAnchor{}, fmt.Errorf("%w: market anchor %s, comparison %s", ErrCurrencyMismatch,
			m.Currency, currency)
	}
	anchor := m.Anchor
	if anchor.AsOf.Validate() != nil {
		anchor.AsOf = today
	}
	if err := anchor.Validate(m.Currency); err != nil {
		return MarketAnchor{}, err
	}
	return anchor, nil
}

// MarketInputFromRecord translates a market-rate fetch answer into the
// simulation request's market anchor (HIPERF-005): the fetch_market_rate
// node's governed answer becomes the simulate_compensation node's market
// floor input. The record must validate in the comparison currency; a
// record that cannot be cited is refused, never carried silently.
func MarketInputFromRecord(record MarketRecord, currency string) (MarketAnchorInput, error) {
	if err := record.Validate(currency); err != nil {
		return MarketAnchorInput{}, err
	}
	return MarketAnchorInput{
		Anchor:        record.Anchor,
		Currency:      currency,
		SourceVersion: record.SourceVersion,
	}, nil
}

// MarketResult is the market-informed raise floor section of a simulation
// result. RaiseFloor is the max of the band floor and the anchor's 25th
// percentile (the market low, compared like-with-like against the band
// minimum); with no band evaluated there is no band floor to compare, so
// the floor is the anchor itself.
type MarketResult struct {
	Anchor MarketAnchor
	// Currency is the denomination the floor was compared in.
	Currency string
	// SourceVersion pins the published source the anchor was read from.
	SourceVersion string
	// RaiseFloor is the market-informed minimum annualized base.
	RaiseFloor values.Money
	// BandFloor is the evaluated band minimum. It is meaningful only when
	// BandFloorKnown: with no band there is no band floor.
	BandFloor values.Money
	// BandFloorKnown reports whether a band floor participated in the max.
	BandFloorKnown bool
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (m MarketResult) Canonical() []byte {
	anchor := m.Anchor.Canonical(m.Currency)
	if anchor == nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.rewards.MarketResult", rewardsSchemaVer).
		Field("anchor", anchor).
		String("currency", m.Currency).
		String("source_version", m.SourceVersion).
		Value("raise_floor", m.RaiseFloor).
		Bool("band_floor?", m.BandFloorKnown)
	if m.BandFloorKnown {
		w.Value("band_floor", m.BandFloor)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// marketFloor returns the raise floor for anchor against an optional band
// floor: the max of the two when the band is known, the anchor otherwise.
// All three amounts must share one currency; anything else is a refusal,
// not a comparison.
func marketFloor(anchor MarketAnchor, bandFloor values.Money, bandKnown bool) (values.Money, error) {
	if !bandKnown {
		return anchor.P25, nil
	}
	cmp, err := bandFloor.Cmp(anchor.P25)
	if err != nil {
		return values.Money{}, fmt.Errorf("%w: band floor against market anchor: %w", ErrCurrencyMismatch, err)
	}
	if cmp >= 0 {
		return bandFloor, nil
	}
	return anchor.P25, nil
}
