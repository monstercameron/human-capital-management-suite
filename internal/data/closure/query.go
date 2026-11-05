package closure

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func checkQuery(hierarchy, node string, effective, known time.Time) error {
	if strings.TrimSpace(hierarchy) == "" || strings.TrimSpace(node) == "" {
		return fmt.Errorf("%w: hierarchy and node are required", ErrClosureInvalid)
	}
	if effective.IsZero() || known.IsZero() {
		return fmt.Errorf("%w: effective and known times are required", ErrClosureInvalid)
	}
	return nil
}

func collectIDs(rows dbport.Rows) ([]string, error) {
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

const pairDedup = `AND NOT EXISTS (SELECT 1 FROM hierarchy_closure n
    WHERE n.tenant_id = c.tenant_id AND n.hierarchy_key = c.hierarchy_key
      AND n.ancestor_id = c.ancestor_id AND n.descendant_id = c.descendant_id
      AND n.known_at <= $5
      AND (n.effective_from < c.effective_to OR c.effective_to IS NULL)
      AND (c.effective_from < n.effective_to OR n.effective_to IS NULL)
      AND (n.known_at > c.known_at OR (n.known_at = c.known_at AND n.event_id > c.event_id)))`

func Ancestors(ctx context.Context, q dbport.Querier, tenant uuid.UUID, hierarchy, node string, effective, known time.Time) ([]string, error) {
	if err := checkQuery(hierarchy, node, effective, known); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT c.ancestor_id FROM hierarchy_closure c
WHERE c.tenant_id = $1 AND c.hierarchy_key = $2 AND c.descendant_id = $3
  AND c.effective_from <= $4::date AND (c.effective_to IS NULL OR $4::date < c.effective_to)
  AND c.known_at <= $5
  `+pairDedup+`
ORDER BY c.depth, c.ancestor_id`, tenant, hierarchy, node, effective, known)
	if err != nil {
		return nil, err
	}
	return collectIDs(rows)
}

func Descendants(ctx context.Context, q dbport.Querier, tenant uuid.UUID, hierarchy, node string, effective, known time.Time) ([]string, error) {
	if err := checkQuery(hierarchy, node, effective, known); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT c.descendant_id FROM hierarchy_closure c
WHERE c.tenant_id = $1 AND c.hierarchy_key = $2 AND c.ancestor_id = $3
  AND c.effective_from <= $4::date AND (c.effective_to IS NULL OR $4::date < c.effective_to)
  AND c.known_at <= $5
  `+pairDedup+`
ORDER BY c.depth, c.descendant_id`, tenant, hierarchy, node, effective, known)
	if err != nil {
		return nil, err
	}
	return collectIDs(rows)
}

func Related(ctx context.Context, q dbport.Querier, tenant uuid.UUID, hierarchy, principal, owner string, effective, known time.Time) (bool, error) {
	if strings.TrimSpace(principal) == "" || strings.TrimSpace(owner) == "" || principal == owner {
		return false, nil
	}
	if err := checkQuery(hierarchy, owner, effective, known); err != nil {
		return false, err
	}
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hierarchy_closure c
WHERE c.tenant_id = $1 AND c.hierarchy_key = $2 AND c.descendant_id = $3 AND c.ancestor_id = $6
  AND c.effective_from <= $4::date AND (c.effective_to IS NULL OR $4::date < c.effective_to)
  AND c.known_at <= $5
  `+pairDedup+`)`,
		tenant, hierarchy, owner, effective, known, principal).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func CountClosure(ctx context.Context, q dbport.Querier, tenant uuid.UUID, hierarchy string) (int64, error) {
	var n int64
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM hierarchy_closure WHERE tenant_id = $1 AND hierarchy_key = $2`, tenant, hierarchy).Scan(&n)
	return n, err
}
