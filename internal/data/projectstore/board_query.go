package projectstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
)

// TaskQuery applies board/list predicates in PostgreSQL before the page limit.
// Board callers require a nonempty status scope and exclude archived tasks;
// task-list callers may leave StatusIDs empty to query every status.
type TaskQuery struct {
	StatusIDs          []string
	DueDateFrom        string
	DueDateTo          string
	Filter             projectboard.Filter
	RequireStatusScope bool
	ExcludeArchived    bool
}

func (s *Store) ListTasksFiltered(ctx context.Context, tenantID, projectID, afterID string, limit int32, q TaskQuery) ([]TaskRecord, error) {
	return s.listTasksFiltered(ctx, tenantID, projectID, afterID, "", projectboard.OrderTaskID, false, limit, q)
}

// ListBoardTasksFiltered pages in the requested board order. The task ID is
// always the final key so equal sort values have a deterministic position.
func (s *Store) ListBoardTasksFiltered(ctx context.Context, tenantID, projectID string, afterValue, afterID string, order projectboard.OrderField, descending bool, limit int32, q TaskQuery) ([]TaskRecord, error) {
	if order == "" {
		order = projectboard.OrderTaskID
	}
	return s.listTasksFiltered(ctx, tenantID, projectID, afterID, afterValue, order, descending, limit, q)
}

func (s *Store) listTasksFiltered(ctx context.Context, tenantID, projectID, afterID, afterValue string, order projectboard.OrderField, descending bool, limit int32, q TaskQuery) ([]TaskRecord, error) {
	if s == nil || tenantID == "" || projectID == "" || limit < 1 || limit > 101 || len(q.StatusIDs) > 100 || len(q.Filter.Priorities) > 100 || len(q.Filter.TypeIDs) > 100 || len(q.Filter.EnumFields) > 25 {
		return nil, ErrInvalidRecord
	}
	if order != projectboard.OrderTaskID && order != projectboard.OrderTitle && order != projectboard.OrderPriority && order != projectboard.OrderDueDate {
		return nil, ErrInvalidRecord
	}
	if q.RequireStatusScope && len(q.StatusIDs) == 0 {
		return []TaskRecord{}, nil
	}
	args := []any{tenantID, projectID}
	var sql strings.Builder
	sql.WriteString(taskSelect)
	sql.WriteString(` WHERE tenant_id=$1 AND project_id=$2`)
	add := func(clause string, value any) {
		args = append(args, value)
		fmt.Fprintf(&sql, clause, len(args))
	}
	if q.ExcludeArchived {
		sql.WriteString(` AND archived=false`)
	}
	if len(q.StatusIDs) > 0 {
		add(` AND status_id=ANY($%d)`, q.StatusIDs)
	}
	if q.DueDateFrom != "" {
		add(` AND due_date >= $%d::date`, q.DueDateFrom)
	}
	if q.DueDateTo != "" {
		add(` AND due_date <= $%d::date`, q.DueDateTo)
	}
	if q.Filter.AssigneeID != "" {
		add(` AND assignee_id=$%d`, q.Filter.AssigneeID)
	}
	if len(q.Filter.Priorities) > 0 {
		add(` AND priority=ANY($%d)`, q.Filter.Priorities)
	}
	if len(q.Filter.TypeIDs) > 0 {
		add(` AND type_id=ANY($%d)`, q.Filter.TypeIDs)
	}
	keys := make([]string, 0, len(q.Filter.EnumFields))
	for fieldID := range q.Filter.EnumFields {
		keys = append(keys, fieldID)
	}
	sort.Strings(keys)
	for _, fieldID := range keys {
		values := q.Filter.EnumFields[fieldID]
		if len(values) == 0 {
			continue
		}
		if fieldID == "" || len(fieldID) > 64 || len(values) > 100 {
			return nil, ErrInvalidRecord
		}
		canonical := make([]string, 0, len(values))
		for _, value := range values {
			raw, err := json.Marshal(value)
			if err != nil {
				return nil, ErrInvalidRecord
			}
			canonical = append(canonical, string(raw))
		}
		args = append(args, fieldID, canonical)
		fmt.Fprintf(&sql, ` AND fields_json->$%d->>'CanonicalValue'=ANY($%d)`, len(args)-1, len(args))
	}
	orderExpr := "id"
	if order == projectboard.OrderTitle {
		orderExpr = `translate(title,'ABCDEFGHIJKLMNOPQRSTUVWXYZ','abcdefghijklmnopqrstuvwxyz') COLLATE "C"`
	} else if order == projectboard.OrderPriority {
		orderExpr = "COALESCE(priority,'')"
	} else if order == projectboard.OrderDueDate {
		orderExpr = "COALESCE(due_date::text,'')"
	}
	comparison := ">"
	direction := "ASC"
	if descending {
		comparison = "<"
		direction = "DESC"
	}
	if afterID != "" {
		if order == projectboard.OrderTaskID {
			args = append(args, afterID)
			fmt.Fprintf(&sql, ` AND id%s$%d`, comparison, len(args))
		} else {
			args = append(args, afterValue, afterID)
			fmt.Fprintf(&sql, ` AND (%s,id) %s ($%d,$%d)`, orderExpr, comparison, len(args)-1, len(args))
		}
	}
	args = append(args, limit)
	if order == projectboard.OrderTaskID {
		fmt.Fprintf(&sql, ` ORDER BY id %s LIMIT $%d`, direction, len(args))
	} else {
		fmt.Fprintf(&sql, ` ORDER BY %s %s,id %s LIMIT $%d`, orderExpr, direction, direction, len(args))
	}
	out := make([]TaskRecord, 0, limit)
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, sql.String(), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var task TaskRecord
			if err := scanTask(rows, &task); err != nil {
				return err
			}
			out = append(out, task)
		}
		return rows.Err()
	})
	return out, err
}
