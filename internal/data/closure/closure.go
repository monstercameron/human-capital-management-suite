// Package closure maintains effective-dated hierarchy closure tables (WF-DATA-020).
//
// Each hierarchy is a declared relationship type whose ancestor queries answer
// at any effective and known time. The maintainer applies edge events
// incrementally inside the caller transaction: cycle, depth-limit and
// single-parent refusals change nothing, and event replay is idempotent.
//
// Bitemporal rule: rows are versioned by effective range and known time.
// Newer knowledge supersedes older knowledge only where their effective
// ranges overlap, so an ended generation and its replacement coexist and
// each answers its own time. Events sharing one known time order by
// event id: a causally later event must use a greater id.
//
// Scope: one parent per child per effective time (reporting, legal,
// financial, location and tenant self-links are all trees). Matrix and
// secondary relationships are out of scope for this maintainer.
package closure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrClosureInvalid = errors.New("closure: invalid hierarchy request")
	ErrDepthExceeded  = errors.New("closure: edge exceeds the hierarchy depth limit")
	ErrCycle          = errors.New("closure: edge would close a cycle")
)

const DefaultDepthLimit = 32

func DeclareHierarchy(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, hierarchy string, depthLimit int, at time.Time) (int, error) {
	if strings.TrimSpace(hierarchy) == "" || depthLimit < 1 || at.IsZero() {
		return 0, fmt.Errorf("%w: hierarchy, positive depth limit and time are required", ErrClosureInvalid)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO hierarchy_type (tenant_id, hierarchy_key, depth_limit, declared_at)
VALUES ($1, $2, $3, $4) ON CONFLICT (tenant_id, hierarchy_key) DO NOTHING`, tenant, hierarchy, depthLimit, at.UTC()); err != nil {
		return 0, err
	}
	var existing int
	if err := tx.QueryRow(ctx, `SELECT depth_limit FROM hierarchy_type WHERE tenant_id = $1 AND hierarchy_key = $2`, tenant, hierarchy).Scan(&existing); err != nil {
		return 0, err
	}
	if existing != depthLimit {
		return 0, fmt.Errorf("%w: %q declares depth %d, want %d", ErrClosureInvalid, hierarchy, existing, depthLimit)
	}
	return existing, nil
}

func ensureHierarchy(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, hierarchy string, at time.Time) (int, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO hierarchy_type (tenant_id, hierarchy_key, depth_limit, declared_at)
VALUES ($1, $2, $3, $4) ON CONFLICT (tenant_id, hierarchy_key) DO NOTHING`, tenant, hierarchy, DefaultDepthLimit, at.UTC()); err != nil {
		return 0, err
	}
	var limit int
	if err := tx.QueryRow(ctx, `SELECT depth_limit FROM hierarchy_type WHERE tenant_id = $1 AND hierarchy_key = $2`, tenant, hierarchy).Scan(&limit); err != nil {
		return 0, err
	}
	return limit, nil
}

// AddEdge applies one open-ended edge event.
func AddEdge(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, hierarchy, parent, child string, from, known time.Time, eventID string) (bool, error) {
	return applyEdge(ctx, tx, tenant, hierarchy, parent, child, from, nil, known, eventID)
}

// EndEdge supersedes one edge with an ended version. The edge must hold
// at the end time with the given start, or the end is refused.
func EndEdge(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, hierarchy, parent, child string, from, to, known time.Time, eventID string) (bool, error) {
	if to.IsZero() || !to.After(from) {
		return false, fmt.Errorf("%w: edge end must be after its start", ErrClosureInvalid)
	}
	var found bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hierarchy_edge e
WHERE e.tenant_id = $1 AND e.hierarchy_key = $2 AND e.parent_id = $3 AND e.child_id = $4
  AND e.effective_from = $5::date AND e.effective_from <= $6::date AND (e.effective_to IS NULL OR $6::date < e.effective_to)
  AND e.known_at <= $7
  AND NOT EXISTS (SELECT 1 FROM hierarchy_edge n
    WHERE n.tenant_id = e.tenant_id AND n.hierarchy_key = e.hierarchy_key
      AND n.parent_id = e.parent_id AND n.child_id = e.child_id
      AND n.known_at <= $7
      AND (n.effective_from < e.effective_to OR e.effective_to IS NULL)
      AND (e.effective_from < n.effective_to OR n.effective_to IS NULL)
      AND (n.known_at > e.known_at OR (n.known_at = e.known_at AND n.event_id > e.event_id))))`,
		tenant, hierarchy, parent, child, from, to, known).Scan(&found)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("%w: edge %s->%s holds no version starting %v through %v", ErrClosureInvalid, parent, child, from, to)
	}
	return applyEdge(ctx, tx, tenant, hierarchy, parent, child, from, &to, known, eventID)
}

func applyEdge(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, hierarchy, parent, child string, from time.Time, to *time.Time, known time.Time, eventID string) (bool, error) {
	if strings.TrimSpace(hierarchy) == "" || strings.TrimSpace(parent) == "" || strings.TrimSpace(child) == "" || parent == child {
		return false, fmt.Errorf("%w: hierarchy and two distinct node ids are required", ErrClosureInvalid)
	}
	if from.IsZero() || known.IsZero() || strings.TrimSpace(eventID) == "" {
		return false, fmt.Errorf("%w: effective, known and event id are required", ErrClosureInvalid)
	}
	limit, err := ensureHierarchy(ctx, tx, tenant, hierarchy, known)
	if err != nil {
		return false, err
	}
	if to == nil {
		var conflict bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hierarchy_edge e
WHERE e.tenant_id = $1 AND e.hierarchy_key = $2 AND e.child_id = $3 AND e.parent_id <> $4
  AND (e.effective_to IS NULL OR e.effective_to > $5::date)
  AND e.known_at <= $6
  AND NOT EXISTS (SELECT 1 FROM hierarchy_edge n
    WHERE n.tenant_id = e.tenant_id AND n.hierarchy_key = e.hierarchy_key
      AND n.parent_id = e.parent_id AND n.child_id = e.child_id
      AND n.known_at <= $6
      AND (n.effective_from < e.effective_to OR e.effective_to IS NULL)
      AND (e.effective_from < n.effective_to OR n.effective_to IS NULL)
      AND (n.known_at > e.known_at OR (n.known_at = e.known_at AND n.event_id > e.event_id))))`,
			tenant, hierarchy, child, parent, from, known).Scan(&conflict)
		if err != nil {
			return false, err
		}
		if conflict {
			return false, fmt.Errorf("%w: %s already has a parent over %v", ErrClosureInvalid, child, from)
		}
	}
	ancestors, err := Ancestors(ctx, tx, tenant, hierarchy, parent, from, known)
	if err != nil {
		return false, err
	}
	for _, a := range ancestors {
		if a == child {
			return false, fmt.Errorf("%w: %s reaches %s", ErrCycle, child, parent)
		}
	}
	var maxDepth int
	depthSQL := `WITH a(depth) AS (
  SELECT c.depth FROM hierarchy_closure c
  WHERE c.tenant_id = $1 AND c.hierarchy_key = $2 AND c.descendant_id = $3 AND c.known_at <= $5
    AND NOT EXISTS (SELECT 1 FROM hierarchy_closure n
      WHERE n.tenant_id = c.tenant_id AND n.hierarchy_key = c.hierarchy_key
        AND n.ancestor_id = c.ancestor_id AND n.descendant_id = c.descendant_id
        AND n.known_at <= $5
        AND (n.effective_from < c.effective_to OR c.effective_to IS NULL)
        AND (c.effective_from < n.effective_to OR n.effective_to IS NULL)
        AND (n.known_at > c.known_at OR (n.known_at = c.known_at AND n.event_id > c.event_id)))
  UNION ALL SELECT 0
), d(depth) AS (
  SELECT c.depth FROM hierarchy_closure c
  WHERE c.tenant_id = $1 AND c.hierarchy_key = $2 AND c.ancestor_id = $4 AND c.known_at <= $5
    AND NOT EXISTS (SELECT 1 FROM hierarchy_closure n
      WHERE n.tenant_id = c.tenant_id AND n.hierarchy_key = c.hierarchy_key
        AND n.ancestor_id = c.ancestor_id AND n.descendant_id = c.descendant_id
        AND n.known_at <= $5
        AND (n.effective_from < c.effective_to OR c.effective_to IS NULL)
        AND (c.effective_from < n.effective_to OR n.effective_to IS NULL)
        AND (n.known_at > c.known_at OR (n.known_at = c.known_at AND n.event_id > c.event_id)))
  UNION ALL SELECT 0
)
SELECT COALESCE(MAX(a.depth + 1 + d.depth), 1) FROM a CROSS JOIN d`
	if err := tx.QueryRow(ctx, depthSQL, tenant, hierarchy, parent, child, known).Scan(&maxDepth); err != nil {
		return false, err
	}
	if maxDepth > limit {
		return false, fmt.Errorf("%w: depth %d over limit %d", ErrDepthExceeded, maxDepth, limit)
	}
	var toArg any
	if to != nil {
		t := *to
		toArg = t
	}
	affected, err := tx.Exec(ctx, `INSERT INTO hierarchy_edge
(tenant_id, hierarchy_key, parent_id, child_id, effective_from, effective_to, known_at, event_id)
VALUES ($1, $2, $3, $4, $5::date, $6::date, $7, $8)
ON CONFLICT (tenant_id, hierarchy_key, event_id) DO NOTHING`,
		tenant, hierarchy, parent, child, from, toArg, known, eventID)
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	closureSQL := `INSERT INTO hierarchy_closure
(tenant_id, hierarchy_key, ancestor_id, descendant_id, depth, effective_from, effective_to, known_at, event_id)
WITH a(node, depth, eff_from, eff_to) AS (
  SELECT c.ancestor_id, c.depth, c.effective_from, c.effective_to FROM hierarchy_closure c
  WHERE c.tenant_id = $1 AND c.hierarchy_key = $2 AND c.descendant_id = $3 AND c.known_at <= $7
    AND NOT EXISTS (SELECT 1 FROM hierarchy_closure n
      WHERE n.tenant_id = c.tenant_id AND n.hierarchy_key = c.hierarchy_key
        AND n.ancestor_id = c.ancestor_id AND n.descendant_id = c.descendant_id
        AND n.known_at <= $7
        AND (n.effective_from < c.effective_to OR c.effective_to IS NULL)
        AND (c.effective_from < n.effective_to OR n.effective_to IS NULL)
        AND (n.known_at > c.known_at OR (n.known_at = c.known_at AND n.event_id > c.event_id)))
  UNION ALL SELECT $3::text, 0, $5::date, $6::date
), d(node, depth, eff_from, eff_to) AS (
  SELECT c.descendant_id, c.depth, c.effective_from, c.effective_to FROM hierarchy_closure c
  WHERE c.tenant_id = $1 AND c.hierarchy_key = $2 AND c.ancestor_id = $4 AND c.known_at <= $7
    AND NOT EXISTS (SELECT 1 FROM hierarchy_closure n
      WHERE n.tenant_id = c.tenant_id AND n.hierarchy_key = c.hierarchy_key
        AND n.ancestor_id = c.ancestor_id AND n.descendant_id = c.descendant_id
        AND n.known_at <= $7
        AND (n.effective_from < c.effective_to OR c.effective_to IS NULL)
        AND (c.effective_from < n.effective_to OR n.effective_to IS NULL)
        AND (n.known_at > c.known_at OR (n.known_at = c.known_at AND n.event_id > c.event_id)))
  UNION ALL SELECT $4::text, 0, $5::date, $6::date
)
SELECT $1, $2, a.node, d.node, a.depth + 1 + d.depth,
  GREATEST(a.eff_from, d.eff_from, $5::date),
  LEAST(a.eff_to, d.eff_to, $6::date),
  $7, $8
FROM a CROSS JOIN d
WHERE GREATEST(a.eff_from, d.eff_from, $5::date) < LEAST(a.eff_to, d.eff_to, $6::date)
  OR LEAST(a.eff_to, d.eff_to, $6::date) IS NULL`
	if _, err := tx.Exec(ctx, closureSQL, tenant, hierarchy, parent, child, from, toArg, known, eventID); err != nil {
		return false, err
	}
	return true, nil
}
