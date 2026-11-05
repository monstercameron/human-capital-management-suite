package closure

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type currentEdge struct {
	parent string
	from   time.Time
}

func currentParents(ctx context.Context, q dbport.Querier, tenant uuid.UUID, hierarchy, child string, effective, known time.Time) ([]currentEdge, error) {
	rows, err := q.Query(ctx, `SELECT e.parent_id, e.effective_from FROM hierarchy_edge e
WHERE e.tenant_id = $1 AND e.hierarchy_key = $2 AND e.child_id = $3
  AND e.effective_from <= $4::date AND (e.effective_to IS NULL OR $4::date < e.effective_to)
  AND e.known_at <= $5
  AND NOT EXISTS (SELECT 1 FROM hierarchy_edge n
    WHERE n.tenant_id = e.tenant_id AND n.hierarchy_key = e.hierarchy_key
      AND n.parent_id = e.parent_id AND n.child_id = e.child_id
      AND n.known_at <= $5
      AND (n.effective_from < e.effective_to OR e.effective_to IS NULL)
      AND (e.effective_from < n.effective_to OR n.effective_to IS NULL)
      AND (n.known_at > e.known_at OR (n.known_at = e.known_at AND n.event_id > e.event_id)))`,
		tenant, hierarchy, child, effective, known)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []currentEdge
	for rows.Next() {
		var edge currentEdge
		if err := rows.Scan(&edge.parent, &edge.from); err != nil {
			return nil, err
		}
		out = append(out, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func Reparent(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, hierarchy, child, newParent string, effective, known time.Time, eventBase string) error {
	if strings.TrimSpace(child) == "" || strings.TrimSpace(newParent) == "" || child == newParent {
		return fmt.Errorf("%w: child and a distinct new parent are required", ErrClosureInvalid)
	}
	if strings.TrimSpace(eventBase) == "" {
		return fmt.Errorf("%w: event base is required", ErrClosureInvalid)
	}
	parents, err := currentParents(ctx, tx, tenant, hierarchy, child, effective, known)
	if err != nil {
		return err
	}
	for i, edge := range parents {
		if edge.parent == newParent {
			continue
		}
		if _, err := EndEdge(ctx, tx, tenant, hierarchy, edge.parent, child, edge.from, effective, known, fmt.Sprintf("%s-a-%d", eventBase, i)); err != nil {
			return err
		}
	}
	_, err = AddEdge(ctx, tx, tenant, hierarchy, newParent, child, effective, known, eventBase+"-b")
	return err
}

type openEdge struct {
	parent string
	child  string
	from   time.Time
}

func Rebuild(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, hierarchy string, known time.Time, eventBase string) error {
	if strings.TrimSpace(hierarchy) == "" || known.IsZero() || strings.TrimSpace(eventBase) == "" {
		return fmt.Errorf("%w: hierarchy, known time and event base are required", ErrClosureInvalid)
	}
	limit, err := ensureHierarchy(ctx, tx, tenant, hierarchy, known)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT e.parent_id, e.child_id, e.effective_from FROM hierarchy_edge e
WHERE e.tenant_id = $1 AND e.hierarchy_key = $2 AND e.known_at <= $3 AND e.effective_to IS NULL
  AND NOT EXISTS (SELECT 1 FROM hierarchy_edge n
    WHERE n.tenant_id = e.tenant_id AND n.hierarchy_key = e.hierarchy_key
      AND n.parent_id = e.parent_id AND n.child_id = e.child_id
      AND n.known_at <= $3
      AND (n.effective_from < e.effective_to OR e.effective_to IS NULL)
      AND (e.effective_from < n.effective_to OR n.effective_to IS NULL)
      AND (n.known_at > e.known_at OR (n.known_at = e.known_at AND n.event_id > e.event_id)))`,
		tenant, hierarchy, known)
	if err != nil {
		return err
	}
	var edges []openEdge
	collect := func() error {
		defer rows.Close()
		for rows.Next() {
			var edge openEdge
			if err := rows.Scan(&edge.parent, &edge.child, &edge.from); err != nil {
				return err
			}
			edges = append(edges, edge)
		}
		return rows.Err()
	}
	if err := collect(); err != nil {
		return err
	}
	parentsOf := make(map[string]string)
	for _, edge := range edges {
		if edge.parent == edge.child {
			return fmt.Errorf("%w: %s parents itself", ErrCycle, edge.parent)
		}
		if prior, ok := parentsOf[edge.child]; ok && prior != edge.parent {
			return fmt.Errorf("%w: %s has parents %s and %s", ErrClosureInvalid, edge.child, prior, edge.parent)
		}
		parentsOf[edge.child] = edge.parent
	}
	type pair struct {
		ancestor   string
		descendant string
		from       time.Time
	}
	children := make(map[string][]openEdge)
	for _, edge := range edges {
		children[edge.parent] = append(children[edge.parent], edge)
	}
	knownPairs := make(map[[2]string]bool)
	var frontier []pair
	for _, edge := range edges {
		key := [2]string{edge.parent, edge.child}
		if knownPairs[key] {
			continue
		}
		knownPairs[key] = true
		frontier = append(frontier, pair{ancestor: edge.parent, descendant: edge.child, from: edge.from})
	}
	depth := 1
	for len(frontier) > 0 {
		if depth > limit {
			return fmt.Errorf("%w: rebuild passes depth %d over limit %d", ErrDepthExceeded, depth, limit)
		}
		var next []pair
		for _, p := range frontier {
			if p.ancestor == p.descendant {
				return fmt.Errorf("%w: rebuild reaches %s from itself", ErrCycle, p.ancestor)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO hierarchy_closure
(tenant_id, hierarchy_key, ancestor_id, descendant_id, depth, effective_from, effective_to, known_at, event_id)
VALUES ($1, $2, $3, $4, $5, $6::date, NULL, $7, $8)
ON CONFLICT (tenant_id, hierarchy_key, event_id, ancestor_id, descendant_id) DO NOTHING`,
				tenant, hierarchy, p.ancestor, p.descendant, depth, p.from, known, eventBase); err != nil {
				return err
			}
			for _, edge := range children[p.descendant] {
				key := [2]string{p.ancestor, edge.child}
				if knownPairs[key] {
					continue
				}
				knownPairs[key] = true
				from := p.from
				if edge.from.After(from) {
					from = edge.from
				}
				next = append(next, pair{ancestor: p.ancestor, descendant: edge.child, from: from})
			}
		}
		frontier = next
		depth++
	}
	return nil
}
