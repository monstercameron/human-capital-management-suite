// Package orggraph adapts the canonical organization aggregate history to the
// verified organization-domain Snapshot contract.
package orggraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/organization"
)

type row struct {
	id, revision string
	name, typ    string
	parent       *uuid.UUID
	from         time.Time
	to           *time.Time
}

// Load reads the tenant's currently recorded effective-dated organization
// unit versions. Parent references become half-open HIERARCHY edges whose
// ranges are limited to the interval when both endpoints exist.
func Load(ctx context.Context, queryer dbport.Querier, tenant uuid.UUID) (organization.Snapshot, error) {
	if tenant == uuid.Nil {
		return organization.Snapshot{}, organization.ErrTenantBoundary
	}
	rows, err := queryer.Query(ctx, `
		SELECT entity_id, code, name, org_type, parent_organization_ref,
		       effective_from, effective_to, digest
		FROM organization_unit
		WHERE tenant_id = $1 AND superseded_at IS NULL
		ORDER BY entity_id, effective_from, row_id`, tenant)
	if err != nil {
		return organization.Snapshot{}, fmt.Errorf("orggraph: load organization units: %w", err)
	}
	defer rows.Close()
	var records []row
	for rows.Next() {
		var entityID uuid.UUID
		var code string
		var unit row
		unit.parent = nil
		if err := rows.Scan(&entityID, &code, &unit.name, &unit.typ, &unit.parent, &unit.from, &unit.to, &unit.revision); err != nil {
			return organization.Snapshot{}, fmt.Errorf("orggraph: scan organization unit: %w", err)
		}
		unit.id = entityID.String()
		// Bind the canonical code into the source revision so changes to an
		// identity used by workforce assignment readers alter the watermark.
		unit.revision += ":" + code
		records = append(records, unit)
	}
	if err := rows.Err(); err != nil {
		return organization.Snapshot{}, fmt.Errorf("orggraph: read organization units: %w", err)
	}
	return snapshotFromRows(tenant, records), nil
}

func snapshotFromRows(tenant uuid.UUID, records []row) organization.Snapshot {
	snapshot := organization.Snapshot{Tenant: tenant.String(), ResolverPolicyVersion: "organization-parent-reference/v1"}
	byID := make(map[string][]row, len(records))
	for _, record := range records {
		byID[record.id] = append(byID[record.id], record)
		snapshot.Units = append(snapshot.Units, organization.OrganizationUnit{
			ID: record.id, Tenant: tenant.String(), Name: record.name, Type: record.typ,
			SourceRevision: record.revision, EffectiveFrom: record.from, EffectiveTo: record.to,
		})
	}
	for _, child := range records {
		if child.parent == nil {
			continue
		}
		parentID := child.parent.String()
		for _, parent := range byID[parentID] {
			from := later(child.from, parent.from)
			to := earlier(child.to, parent.to)
			if to != nil && !from.Before(*to) {
				continue
			}
			snapshot.Edges = append(snapshot.Edges, organization.RelationshipEdge{
				ID:     "hierarchy:" + child.id + ":" + child.revision + ":" + parent.revision,
				Tenant: tenant.String(), Source: parentID, Target: child.id,
				SourceRevision: child.revision, Type: organization.Hierarchy,
				EffectiveFrom: from, EffectiveTo: to,
			})
		}
	}
	sort.Slice(snapshot.Units, func(i, j int) bool {
		if snapshot.Units[i].ID != snapshot.Units[j].ID {
			return snapshot.Units[i].ID < snapshot.Units[j].ID
		}
		return snapshot.Units[i].EffectiveFrom.Before(snapshot.Units[j].EffectiveFrom)
	})
	sort.Slice(snapshot.Edges, func(i, j int) bool { return snapshot.Edges[i].ID < snapshot.Edges[j].ID })
	canonical := make([]byte, 0, len(snapshot.Units)*80+len(snapshot.Edges)*80)
	for _, unit := range snapshot.Units {
		canonical = append(canonical, []byte(fmt.Sprintf("u:%s:%s:%s:%s:%s\n", unit.ID, unit.SourceRevision, unit.EffectiveFrom.UTC().Format(time.RFC3339Nano), formatTime(unit.EffectiveTo), unit.Name))...)
	}
	for _, edge := range snapshot.Edges {
		canonical = append(canonical, []byte(fmt.Sprintf("e:%s:%s:%s:%s\n", edge.ID, edge.Source, edge.Target, formatTime(edge.EffectiveTo)))...)
	}
	digest := sha256.Sum256(canonical)
	snapshot.Watermark = "org:" + hex.EncodeToString(digest[:])
	return snapshot
}

func later(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func earlier(left, right *time.Time) *time.Time {
	if left == nil {
		return right
	}
	if right == nil || left.Before(*right) {
		value := *left
		return &value
	}
	value := *right
	return &value
}

func formatTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
