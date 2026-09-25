package app

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// demoBandCatalog layers HarborCare's explicitly versioned sample-company
// ranges over the fixed conformance corpus. The override is tenant-scoped;
// another tenant with a coincidentally matching job code cannot inherit a
// demo company's salary policy.
//
// Every shipped demo company publishes its own ranges, keyed by its own
// tenant: a company's bands answer only that company's tenant.
type demoBandCatalog struct {
	base    rewards.PayBandCatalog
	tenants map[string]map[payband.Scope]rewards.BandRecord
}

func newDemoBandCatalog(base rewards.PayBandCatalog) (*demoBandCatalog, error) {
	if base == nil {
		return nil, fmt.Errorf("app: demo band catalog requires a base catalog")
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(time.Date(2026, time.September, 1, 14, 0, 0, 0, time.UTC)))
	if err != nil {
		return nil, err
	}
	c := &demoBandCatalog{base: base, tenants: map[string]map[payband.Scope]rewards.BandRecord{}}
	for _, pack := range demoworkforce.Packs() {
		bands, err := demoPackBands(pack, recordedAt)
		if err != nil {
			return nil, err
		}
		c.tenants[pack.Key] = bands
	}
	return c, nil
}

// demoPackBands prices one demo company's published ranges.
func demoPackBands(pack *demoworkforce.Pack, recordedAt values.RecordedAt) (map[payband.Scope]rewards.BandRecord, error) {
	specs, err := pack.PayBandSpecs()
	if err != nil {
		return nil, err
	}
	version := pack.PayBandPolicyVersion()
	bands := make(map[payband.Scope]rewards.BandRecord, len(specs))
	for _, spec := range specs {
		scope := payband.Scope{JobCode: spec.JobCode, Grade: spec.Grade, PayZone: spec.PayZone}
		// Annual amounts: the simulation compares annualized pay, so an
		// hourly band is published at its annual equivalent.
		band := payband.Band{ID: spec.ID, Version: spec.Version, Scope: scope,
			Minimum: spec.AnnualMinimum, Midpoint: spec.AnnualMidpoint, Maximum: spec.AnnualMaximum}
		record := rewards.BandRecord{
			Band: band, CatalogVersion: version, Blocking: true,
			Authority: evidence.SourceAuthority{Kind: evidence.AuthorityLocal,
				System: "hcmnext.demo-seed", PolicyRef: version},
			Provenance: evidence.Provenance{Source: "hcmnext.demo-seed",
				EvidenceRef: spec.ID + "@" + spec.Version, RecordedAt: recordedAt},
		}
		if err := record.Validate(); err != nil {
			return nil, fmt.Errorf("app: demo pay band %s: %w", spec.ID, err)
		}
		if _, exists := bands[scope]; exists {
			return nil, fmt.Errorf("app: duplicate demo pay band scope %s", spec.ID)
		}
		bands[scope] = record
	}
	return bands, nil
}

func (c *demoBandCatalog) LookupBand(ctx context.Context, q rewards.BandQuery) (rewards.BandRecord, error) {
	if err := q.Validate(); err != nil {
		return rewards.BandRecord{}, err
	}
	own := c.tenants[string(q.Tenant)]
	if own != nil && q.Currency == "USD" {
		if record, ok := own[q.Scope()]; ok {
			return record, nil
		}
	}
	// The fixture base also contains demo pay bands for its own conformance
	// suite. Do not let that globally keyed fallback expose a demo-company
	// policy to a different tenant.
	for tenant, bands := range c.tenants {
		if tenant == string(q.Tenant) {
			continue
		}
		if _, demoScope := bands[q.Scope()]; demoScope {
			return rewards.BandRecord{}, fmt.Errorf("%w: demo pay band outside company tenant", rewards.ErrBandNotFound)
		}
	}
	return c.base.LookupBand(ctx, q)
}

var _ rewards.PayBandCatalog = (*demoBandCatalog)(nil)
