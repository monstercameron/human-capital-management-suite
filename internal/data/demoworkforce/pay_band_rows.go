package demoworkforce

// The published salary ranges, recorded as compensation_band rows.
//
// migrations/00013 has carried a compensation_band table since P1A, but the
// demo seed never wrote one: [PayBandSpecs] lived only in this process's
// memory, so the served band catalog answered from a compiled-in map that no
// tenant owned and no operator could inspect. This file records the same
// authored specs as rows, under deterministic identities, so the served
// catalog can read a tenant's own bands (internal/data/bandfacts) instead.
//
// Nothing here invents a number. The minimum, midpoint and maximum are the
// values PayBandSpecs already computes from the staffing catalog's published
// base pay, which is also the pay every seeded worker is recorded at -- so a
// worker's base pay is their band's midpoint by construction, never merely
// near it.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PayBandID is the deterministic compensation_band identity of one published
// band spec, derived from the spec id the policy already names.
func PayBandID(specID string) uuid.UUID { return deterministicID("compensation-band", specID) }

// PayBandSummary counts what one [SeedPayBands] call did.
type PayBandSummary struct {
	Planned, Inserted, Skipped int
}

// SeedPayBands records every published pay band inside tx, which the caller
// owns and has already scoped to tenant.
//
// It is replay-safe by deterministic identity: a band whose row already
// exists at [CatalogEffectiveFrom] is left untouched and counted as skipped,
// so a second boot writes nothing rather than superseding a live row with an
// identical one.
func SeedPayBands(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordedAt time.Time) (PayBandSummary, error) {
	return HarborCarePack.SeedPayBands(ctx, tx, tenant, recordedAt)
}

// SeedPayBands records this company's published pay bands inside tx.
func (p *Pack) SeedPayBands(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordedAt time.Time) (PayBandSummary, error) {
	if tx == nil || tenant == uuid.Nil {
		return PayBandSummary{}, fmt.Errorf("demoworkforce: seed pay bands: a transaction and tenant are required")
	}
	recorded := recordedAt.UTC()
	if recorded.IsZero() {
		return PayBandSummary{}, fmt.Errorf("demoworkforce: seed pay bands: recorded_at is required")
	}
	specs, err := p.PayBandSpecs()
	if err != nil {
		return PayBandSummary{}, err
	}
	comp := aggregates.CompensationStore{}
	summary := PayBandSummary{Planned: len(specs)}
	for _, spec := range specs {
		id := PayBandID(spec.ID)
		existing, readErr := comp.CurrentCompensationBand(ctx, tx, tenant, id, CatalogEffectiveFrom)
		switch {
		case readErr == nil:
			if existing.JobCode != spec.JobCode || existing.Grade != spec.Grade || existing.PayZone != spec.PayZone {
				return PayBandSummary{}, fmt.Errorf(
					"demoworkforce: existing compensation_band %s scopes %s/%s/%s, not the published %s/%s/%s",
					id, existing.JobCode, existing.Grade, existing.PayZone, spec.JobCode, spec.Grade, spec.PayZone)
			}
			summary.Skipped++
			continue
		case errors.Is(readErr, aggregates.ErrNotFound):
		default:
			return PayBandSummary{}, fmt.Errorf("demoworkforce: read pay band %s: %w", spec.ID, readErr)
		}
		// The stored band is annual, which is the unit the governed
		// simulation compares annualized pay in; an hourly band's rates are
		// its annual equivalent over standard hours (for a salaried band the
		// two are the same amounts).
		band, buildErr := aggregates.NewCompensationBand(tenant, id, CatalogEffectiveFrom, nil, recorded,
			spec.JobCode, spec.Grade, spec.PayZone, spec.AnnualMinimum, spec.AnnualMidpoint, spec.AnnualMaximum)
		if buildErr != nil {
			return PayBandSummary{}, fmt.Errorf("demoworkforce: pay band %s: %w", spec.ID, buildErr)
		}
		if _, putErr := comp.PutCompensationBand(ctx, tx, band); putErr != nil {
			return PayBandSummary{}, fmt.Errorf("demoworkforce: pay band %s: %w", spec.ID, putErr)
		}
		summary.Inserted++
	}
	return summary, nil
}
