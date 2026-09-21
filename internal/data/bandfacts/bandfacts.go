// Package bandfacts is the database-backed
// internal/domains/rewards.PayBandCatalog adapter: the tenant's own published
// salary ranges, read from the DB-010 compensation_band table through
// internal/data/aggregates' schema rather than from a compiled-in map.
//
// Until this package existed the served catalog answered pay-band lookups out
// of a Go map built at process start, so a tenant could not see, scope or
// revise the ranges its own workers were judged against, and a band recorded
// in the database was never read. The reader closes that: a lookup is
// answered by the tenant's live row for the queried scope.
//
// There is no judgment here. A scope no live row governs is reported as not
// found (or delegated to a declared fallback catalog), never guessed at; what
// an out-of-band amount means stays with internal/domains/rewards.
package bandfacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// bandAuthority names the source authority of the stored band rows this
// reader discloses, parallel to the compfacts precedent for stored
// compensation rows.
const bandAuthority = "compensation.pay_band.source_authority/2026.1"

// storedMoneyScale is the exact-decimal scale compensation_band declares its
// bounds at (numeric(19, 4)). The bounds are read back as text at that scale,
// never through a float.
const storedMoneyScale = 4

// DefaultMoneyScale is the scale a returned band's bounds are expressed at
// when a catalog declares none.
//
// It is two, not the column's four, and that is load-bearing:
// internal/engines/payband refuses to place an amount in a band whose bounds
// declare a different scale (payband.ErrScaleMismatch), and every served
// amount in this platform -- a worker's base pay, a proposed base -- is money
// at two. A reader that published the column's own scale made every band
// lookup for a stored band fail with a scale mismatch, so every promotion
// proposed against one was refused as "the operation could not be completed".
// A bound that cannot be expressed exactly at this scale is reported rather
// than rounded: a band silently widened by a rounding step is not the band
// the tenant published.
const DefaultMoneyScale int32 = 2

// Catalog is the rewards.PayBandCatalog adapter over compensation_band.
type Catalog struct {
	// DB opens the tenant-scoped, read-only transaction each lookup runs
	// inside. Every lookup opens its own transaction and rolls it back: this
	// port never writes.
	DB dbport.Beginner
	// TenantUUID resolves the domain's logical tenant key to the physical
	// tenant UUID compensation_band keys on.
	TenantUUID func(values.TenantId) uuid.UUID
	// CatalogVersion pins the published catalog a returned band was read
	// from. A record cannot be cited without one, so a catalog composed
	// without it refuses rather than inventing a version.
	CatalogVersion string
	// Blocking says whether an out-of-band amount blocks or merely advises.
	// It is a published property of the catalog, recorded here rather than
	// assumed by the engine.
	Blocking bool
	// Source names the system the stored rows came from, for the authority
	// and provenance a returned record carries.
	Source string
	// MoneyScale is the declared decimal scale a returned band's bounds are
	// expressed at. Zero means [DefaultMoneyScale], which is the scale every
	// served amount in this platform already declares; see that constant for
	// why publishing the column's own scale instead is a defect.
	MoneyScale int32
	// Fallback answers scopes this tenant records no band for. It is how a
	// deployment whose database has no bands yet keeps the behaviour it had;
	// a nil fallback reports rewards.ErrBandNotFound instead.
	Fallback rewards.PayBandCatalog
}

var _ rewards.PayBandCatalog = Catalog{}

// LookupBand implements rewards.PayBandCatalog over the tenant's live band
// for the queried scope and currency at the query's as-of date.
func (c Catalog) LookupBand(ctx context.Context, q rewards.BandQuery) (rewards.BandRecord, error) {
	if err := q.Validate(); err != nil {
		return rewards.BandRecord{}, err
	}
	if c.DB == nil || c.TenantUUID == nil {
		return c.fallback(ctx, q)
	}
	if c.CatalogVersion == "" {
		return rewards.BandRecord{}, fmt.Errorf("bandfacts: catalog was composed with no catalog version")
	}
	tenantID := c.TenantUUID(q.Tenant)
	if tenantID == uuid.Nil {
		return c.fallback(ctx, q)
	}
	businessAt := localDateToTime(q.AsOf)

	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return rewards.BandRecord{}, fmt.Errorf("bandfacts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return rewards.BandRecord{}, fmt.Errorf("bandfacts: scope tenant: %w", err)
	}

	var (
		entityID                    uuid.UUID
		minimum, midpoint, maximum  string
		currency, digest            string
		recordedAt, effectiveFromAt time.Time
	)
	// The scope index (compensation_band_scope) is on exactly these columns;
	// the newest live row wins when a tenant has recorded a successor that
	// takes effect on or before the queried date.
	err = tx.QueryRow(ctx, `
		SELECT entity_id, minimum::text, midpoint::text, maximum::text, currency, digest, recorded_at, effective_from
		FROM compensation_band
		WHERE tenant_id = $1 AND job_code = $2 AND grade = $3 AND pay_zone = $4 AND currency = $5
		  AND superseded_at IS NULL
		  AND effective_from <= $6 AND (effective_to IS NULL OR effective_to > $6)
		ORDER BY effective_from DESC, recorded_at DESC
		LIMIT 1`,
		tenantID, q.JobCode, q.Grade, q.PayZone, q.Currency, businessAt).
		Scan(&entityID, &minimum, &midpoint, &maximum, &currency, &digest, &recordedAt, &effectiveFromAt)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return c.fallback(ctx, q)
		}
		return rewards.BandRecord{}, fmt.Errorf("bandfacts: read the pay band for %s/%s/%s: %w", q.JobCode, q.Grade, q.PayZone, err)
	}

	scale := c.MoneyScale
	if scale == 0 {
		scale = DefaultMoneyScale
	}
	bounds := make([]values.Money, 0, 3)
	for _, amount := range []string{minimum, midpoint, maximum} {
		// Parsed at the column's declared scale, then expressed at the scale
		// the served amounts declare. Both steps are exact: a bound that
		// would have to be rounded is a band this reader refuses to publish
		// rather than quietly move.
		stored, decErr := values.NewDecimal(amount, storedMoneyScale, values.RoundingExactRequired)
		if decErr != nil {
			return rewards.BandRecord{}, fmt.Errorf("bandfacts: pay band %s bound %q: %w", entityID, amount, decErr)
		}
		exact, quantErr := stored.Quantize(scale, values.RoundingExactRequired)
		if quantErr != nil {
			return rewards.BandRecord{}, fmt.Errorf(
				"bandfacts: pay band %s bound %q cannot be expressed at scale %d: %w", entityID, amount, scale, quantErr)
		}
		money, moneyErr := values.NewMoney(exact.String(), currency, scale, values.RoundingExactRequired)
		if moneyErr != nil {
			return rewards.BandRecord{}, fmt.Errorf("bandfacts: pay band %s bound %q: %w", entityID, amount, moneyErr)
		}
		bounds = append(bounds, money)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(recordedAt.UTC()))
	if err != nil {
		return rewards.BandRecord{}, fmt.Errorf("bandfacts: pay band %s recorded_at: %w", entityID, err)
	}
	source := c.Source
	if source == "" {
		source = "hcmnext.compensation"
	}
	record := rewards.BandRecord{
		Band: payband.Band{
			// The stored row's own identity is the citable band id; the
			// version is the published catalog the row was read under, which
			// is what pins the meaning a later reader reproduces.
			ID: entityID.String(), Version: c.CatalogVersion,
			Scope:   payband.Scope{JobCode: q.JobCode, Grade: q.Grade, PayZone: q.PayZone},
			Minimum: bounds[0], Midpoint: bounds[1], Maximum: bounds[2],
		},
		CatalogVersion: c.CatalogVersion, Blocking: c.Blocking,
		Authority: evidence.SourceAuthority{
			Kind: evidence.AuthorityLocal, System: source, PolicyRef: bandAuthority,
		},
		Provenance: evidence.Provenance{Source: source, EvidenceRef: digest, RecordedAt: recorded},
	}
	if err := record.Validate(); err != nil {
		return rewards.BandRecord{}, fmt.Errorf("bandfacts: stored pay band %s: %w", entityID, err)
	}
	return record, nil
}

// fallback delegates to the declared fallback catalog, or reports the scope
// as governed by no band.
func (c Catalog) fallback(ctx context.Context, q rewards.BandQuery) (rewards.BandRecord, error) {
	if c.Fallback == nil {
		return rewards.BandRecord{}, fmt.Errorf("%w: %s/%s/%s", rewards.ErrBandNotFound, q.JobCode, q.Grade, q.PayZone)
	}
	return c.Fallback.LookupBand(ctx, q)
}

func localDateToTime(d values.LocalDate) time.Time {
	if !d.IsSet() {
		return time.Time{}
	}
	return time.Date(int(d.Year()), d.Month(), int(d.Day()), 0, 0, 0, 0, time.UTC)
}
