// Package promotionladder persists and reads the tenant's own published
// career ladder: the promotion_path_edge rows of migration 00317.
//
// The served promotion-path catalog used to answer out of a compiled-in Go
// map, so the ladder a client was offered belonged to the binary rather than
// to the tenant. This package is the two halves of moving it into the
// database: [Seed] records a published ladder, and [Reader] answers the edges
// a tenant publishes at a business instant.
//
// There is no judgment here. An edge is recorded as authored and read back as
// stored; which edges are worth publishing, and what the ladder means for a
// proposal, stay with the caller.
package promotionladder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// LifecyclePublished is the only lifecycle a served catalog reads.
const LifecyclePublished = "PUBLISHED"

// Edge is one published promotion-path edge.
//
// The job codes, grades and title are display projections recorded alongside
// the profile identities: the edge is the published artifact, so a later
// profile revision must not silently retitle an edge a proposal already
// cited.
type Edge struct {
	PathID, Revision string
	OrgUnit, Kind    string
	// Ordinal is this edge's published rank among the targets its source job
	// offers: 1 is the nearest step. It is recorded because the order a
	// ladder is read back in is part of what was published.
	Ordinal int

	SourceProfileID, SourceJobCode, SourceGrade              string
	TargetProfileID, TargetJobCode, TargetGrade, TargetTitle string

	// MinimumBaseIncrease and MaximumBaseIncrease are exact decimal
	// fractions ("0.0500"), never percentages and never floats.
	MinimumBaseIncrease, MaximumBaseIncrease string

	Lifecycle, PolicyVersion string
}

// Validate reports whether the edge is complete enough to record or publish.
func (e Edge) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"path_id", e.PathID}, {"revision", e.Revision}, {"org_unit", e.OrgUnit}, {"kind", e.Kind},
		{"source_profile_id", e.SourceProfileID}, {"source_job_code", e.SourceJobCode}, {"source_grade", e.SourceGrade},
		{"target_profile_id", e.TargetProfileID}, {"target_job_code", e.TargetJobCode}, {"target_grade", e.TargetGrade},
		{"target_title", e.TargetTitle}, {"lifecycle", e.Lifecycle}, {"policy_version", e.PolicyVersion},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("promotionladder: edge %s: %s is required", e.PathID, field.name)
		}
	}
	if e.Ordinal <= 0 {
		return fmt.Errorf("promotionladder: edge %s: ordinal must be positive", e.PathID)
	}
	if e.SourceProfileID == e.TargetProfileID {
		return fmt.Errorf("promotionladder: edge %s leads to its own source profile", e.PathID)
	}
	minimum, err := values.NewDecimal(e.MinimumBaseIncrease, increaseScale, values.RoundingExactRequired)
	if err != nil {
		return fmt.Errorf("promotionladder: edge %s minimum_base_increase: %w", e.PathID, err)
	}
	maximum, err := values.NewDecimal(e.MaximumBaseIncrease, increaseScale, values.RoundingExactRequired)
	if err != nil {
		return fmt.Errorf("promotionladder: edge %s maximum_base_increase: %w", e.PathID, err)
	}
	if minimum.Sign() < 0 {
		return fmt.Errorf("promotionladder: edge %s minimum_base_increase is negative", e.PathID)
	}
	if minimum.Cmp(maximum) > 0 {
		return fmt.Errorf("promotionladder: edge %s minimum_base_increase exceeds its maximum", e.PathID)
	}
	return nil
}

// increaseScale is the exact-decimal scale the numeric(9, 4) guardrail
// columns declare. Parsing at the column's own scale is what makes "0.05",
// "0.0500" and the value read back compare identically.
const increaseScale = 4

// Summary counts what one [Seed] call did.
type Summary struct {
	Planned, Inserted, Skipped int
}

// Seed records edges inside tx, which the caller owns and has already scoped
// to tenant.
//
// It is replay-safe by published identity: an edge whose (path, revision) is
// already recorded is verified to mean the same thing and counted as skipped.
// A stored edge that means something else is a hard error, never an
// overwrite -- the table is append-only, and a published edge a proposal may
// already have cited is not something a boot-time seeder gets to rewrite.
func Seed(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, edges []Edge, effectiveFrom, knownFrom, recordedAt time.Time) (Summary, error) {
	if tx == nil || tenant == uuid.Nil {
		return Summary{}, fmt.Errorf("promotionladder: seed: a transaction and tenant are required")
	}
	if effectiveFrom.IsZero() || knownFrom.IsZero() || recordedAt.IsZero() {
		return Summary{}, fmt.Errorf("promotionladder: seed: effective_from, known_from and recorded_at are required")
	}
	summary := Summary{Planned: len(edges)}
	for _, edge := range edges {
		if err := edge.Validate(); err != nil {
			return Summary{}, err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO promotion_path_edge (
				row_id, tenant_id, path_id, revision, org_unit, kind, ordinal,
				source_profile_id, source_job_code, source_grade,
				target_profile_id, target_job_code, target_grade, target_title,
				minimum_base_increase, maximum_base_increase,
				lifecycle, policy_version, effective_from, effective_to, known_from, recorded_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::text::numeric,$16::text::numeric,$17,$18,$19,NULL,$20,$21)
			ON CONFLICT (tenant_id, path_id, revision) DO NOTHING`,
			uuid.New(), tenant, edge.PathID, edge.Revision, edge.OrgUnit, edge.Kind, edge.Ordinal,
			edge.SourceProfileID, edge.SourceJobCode, edge.SourceGrade,
			edge.TargetProfileID, edge.TargetJobCode, edge.TargetGrade, edge.TargetTitle,
			edge.MinimumBaseIncrease, edge.MaximumBaseIncrease,
			edge.Lifecycle, edge.PolicyVersion, effectiveFrom.UTC(), knownFrom.UTC(), recordedAt.UTC())
		if err != nil {
			return Summary{}, fmt.Errorf("promotionladder: record edge %s: %w", edge.PathID, err)
		}
		if affected > 0 {
			summary.Inserted++
			continue
		}
		stored, found, readErr := edgeAt(ctx, tx, tenant, edge.PathID, edge.Revision)
		if readErr != nil {
			return Summary{}, readErr
		}
		if !found || !stored.sameMeaning(edge) {
			return Summary{}, fmt.Errorf(
				"promotionladder: edge %s revision %s is already published with a different meaning", edge.PathID, edge.Revision)
		}
		summary.Skipped++
	}
	return summary, nil
}

// sameMeaning reports whether a stored edge publishes what other publishes.
// The guardrails are compared as exact decimal text at the column's scale,
// which both sides have already been normalized to.
func (e Edge) sameMeaning(other Edge) bool {
	return e.OrgUnit == other.OrgUnit && e.Kind == other.Kind && e.Ordinal == other.Ordinal &&
		e.SourceProfileID == other.SourceProfileID && e.SourceJobCode == other.SourceJobCode && e.SourceGrade == other.SourceGrade &&
		e.TargetProfileID == other.TargetProfileID && e.TargetJobCode == other.TargetJobCode && e.TargetGrade == other.TargetGrade &&
		e.TargetTitle == other.TargetTitle && e.Lifecycle == other.Lifecycle && e.PolicyVersion == other.PolicyVersion &&
		sameDecimal(e.MinimumBaseIncrease, other.MinimumBaseIncrease) &&
		sameDecimal(e.MaximumBaseIncrease, other.MaximumBaseIncrease)
}

func sameDecimal(a, b string) bool {
	left, leftErr := values.NewDecimal(a, increaseScale, values.RoundingExactRequired)
	right, rightErr := values.NewDecimal(b, increaseScale, values.RoundingExactRequired)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return left.Cmp(right) == 0
}

const edgeColumns = `path_id, revision, org_unit, kind, ordinal,
	source_profile_id, source_job_code, source_grade,
	target_profile_id, target_job_code, target_grade, target_title,
	minimum_base_increase::text, maximum_base_increase::text, lifecycle, policy_version`

func scanEdge(scan func(...any) error) (Edge, error) {
	var e Edge
	err := scan(&e.PathID, &e.Revision, &e.OrgUnit, &e.Kind, &e.Ordinal,
		&e.SourceProfileID, &e.SourceJobCode, &e.SourceGrade,
		&e.TargetProfileID, &e.TargetJobCode, &e.TargetGrade, &e.TargetTitle,
		&e.MinimumBaseIncrease, &e.MaximumBaseIncrease, &e.Lifecycle, &e.PolicyVersion)
	return e, err
}

func edgeAt(ctx context.Context, ex dbport.Querier, tenant uuid.UUID, pathID, revision string) (Edge, bool, error) {
	row := ex.QueryRow(ctx, `SELECT `+edgeColumns+`
		FROM promotion_path_edge WHERE tenant_id = $1 AND path_id = $2 AND revision = $3`, tenant, pathID, revision)
	edge, err := scanEdge(row.Scan)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Edge{}, false, nil
		}
		return Edge{}, false, fmt.Errorf("promotionladder: read edge %s: %w", pathID, err)
	}
	return edge, true, nil
}

// Reader answers the edges a tenant publishes. It opens its own read-only
// transaction per call and rolls it back: this port never writes.
type Reader struct {
	DB dbport.Beginner
	// TenantUUID resolves the logical tenant key to the physical tenant
	// UUID promotion_path_edge keys on.
	TenantUUID func(values.TenantId) uuid.UUID
}

// Edges returns the tenant's PUBLISHED edges live at businessAt, ordered by
// organization unit, source job code and published ordinal, so the nearest
// step a role can take stays the first target it publishes.
//
// A reader with no database or no tenant mapping, and a tenant this reader
// has no physical mapping for, report no edges rather than an error: an
// absent ladder is a catalog the caller falls back from, not a failure.
func (r Reader) Edges(ctx context.Context, tenant values.TenantId, businessAt time.Time) ([]Edge, error) {
	if r.DB == nil || r.TenantUUID == nil {
		return nil, nil
	}
	tenantID := r.TenantUUID(tenant)
	if tenantID == uuid.Nil {
		return nil, nil
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("promotionladder: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, fmt.Errorf("promotionladder: scope tenant: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT `+edgeColumns+`
		FROM promotion_path_edge
		WHERE tenant_id = $1 AND lifecycle = $2
		  AND effective_from <= $3 AND (effective_to IS NULL OR effective_to > $3)
		ORDER BY org_unit, source_job_code, ordinal, path_id`, tenantID, LifecyclePublished, businessAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("promotionladder: read the published ladder: %w", err)
	}
	defer rows.Close()
	var edges []Edge
	for rows.Next() {
		edge, scanErr := scanEdge(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("promotionladder: scan a published edge: %w", scanErr)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("promotionladder: read the published ladder: %w", err)
	}
	return edges, nil
}
