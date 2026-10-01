// Package personacompfacts reads the complete persisted compensation package
// used by persona reads and private scenarios. It never authorizes or writes.
package personacompfacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrUnavailable = errors.New("personacompfacts: canonical compensation unavailable")

type Component struct {
	ID         string
	Kind       string
	Amount     values.Money
	Frequency  string
	Effective  values.EffectiveInterval
	KnownAt    values.KnownAt
	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance
}

type Snapshot struct {
	Worker     values.EntityRef
	Components []Component
	Revision   values.RevisionToken
}

type Reader struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
}

// CompensationAt reads both effective and knowledge coordinates at asOf. All
// components are returned, so a consumer cannot assert completeness while
// silently dropping a persisted allowance, bonus or other material amount.
func (r Reader) CompensationAt(ctx context.Context, worker values.EntityRef, asOf values.Instant) (Snapshot, error) {
	if r.DB == nil || r.TenantUUID == nil || worker.Validate() != nil || worker.Kind != people.KindWorker || asOf.Validate() != nil {
		return Snapshot{}, ErrUnavailable
	}
	tenantID, workerID := r.TenantUUID(worker.Tenant), uuid.Nil
	workerID, err := uuid.Parse(worker.Id)
	if tenantID == uuid.Nil || err != nil {
		return Snapshot{}, ErrUnavailable
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("personacompfacts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return Snapshot{}, err
	}
	packages, err := tx.Query(ctx, `SELECT entity_id, currency, digest FROM compensation_package
		WHERE tenant_id=$1 AND worker_ref=$2 AND effective_from <= $3
		AND (effective_to IS NULL OR effective_to > $3) AND recorded_at <= $3
		AND (superseded_at IS NULL OR superseded_at > $3) ORDER BY entity_id`, tenantID, workerID, asOf.Time())
	if err != nil {
		return Snapshot{}, err
	}
	var packageID uuid.UUID
	var currency, packageDigest string
	count := 0
	for packages.Next() {
		count++
		if err := packages.Scan(&packageID, &currency, &packageDigest); err != nil {
			packages.Close()
			return Snapshot{}, err
		}
	}
	err = packages.Err()
	packages.Close()
	if err != nil {
		return Snapshot{}, err
	}
	if count != 1 || packageDigest == "" {
		return Snapshot{}, ErrUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT entity_id, component_type, amount::text, currency, frequency,
		effective_from, effective_to, recorded_at, digest FROM compensation_component
		WHERE tenant_id=$1 AND package_ref=$2 AND effective_from <= $3
		AND (effective_to IS NULL OR effective_to > $3) AND recorded_at <= $3
		AND (superseded_at IS NULL OR superseded_at > $3) ORDER BY entity_id`, tenantID, packageID, asOf.Time())
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	snapshot := Snapshot{Worker: worker}
	seen := map[string]bool{}
	digest := canonicalbytes.New("hcmnext.persona.CompensationSnapshot", 1).Value("worker", worker).Value("as_of", asOf).String("package_digest", packageDigest)
	for rows.Next() {
		var component Component
		var amount, componentCurrency, rowDigest string
		var effectiveFrom, recordedAt time.Time
		var effectiveTo *time.Time
		if err := rows.Scan(&component.ID, &component.Kind, &amount, &componentCurrency, &component.Frequency, &effectiveFrom, &effectiveTo, &recordedAt, &rowDigest); err != nil {
			return Snapshot{}, err
		}
		if seen[component.ID] || rowDigest == "" || componentCurrency != currency {
			return Snapshot{}, ErrUnavailable
		}
		seen[component.ID] = true
		component.Amount, err = values.NewMoney(amount, currency, 4, values.RoundingExactRequired)
		if err != nil {
			return Snapshot{}, err
		}
		if effectiveTo == nil {
			component.Effective, err = values.NewOpenInstantInterval(values.NewInstant(effectiveFrom.UTC()))
		} else {
			component.Effective, err = values.NewInstantInterval(values.NewInstant(effectiveFrom.UTC()), values.NewInstant(effectiveTo.UTC()))
		}
		if err != nil {
			return Snapshot{}, err
		}
		component.KnownAt, err = values.NewKnownAt(values.NewInstant(recordedAt.UTC()))
		if err != nil {
			return Snapshot{}, err
		}
		recorded, err := values.NewRecordedAt(values.NewInstant(recordedAt.UTC()))
		if err != nil {
			return Snapshot{}, err
		}
		component.Authority = evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcmnext.compensation", PolicyRef: "compensation.source_authority/2026.1"}
		component.Provenance = evidence.Provenance{Source: "hcmnext.compensation", EvidenceRef: rowDigest, RecordedAt: recorded}
		digest.String("component_digest", rowDigest)
		snapshot.Components = append(snapshot.Components, component)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, err
	}
	if len(snapshot.Components) == 0 {
		return Snapshot{}, ErrUnavailable
	}
	version, err := digest.Digest()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Revision, err = values.NewOpaqueRevision("aggregate.compensation_package."+packageID.String(), []byte(version))
	return snapshot, err
}
