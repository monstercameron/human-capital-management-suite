package projectsearch

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
)

// StoreRepository adapts the canonical project task store to exact search.
// All predicates, including due-date bounds, are applied in SQL before LIMIT.
type StoreRepository struct{ Store *projectstore.Store }

func (r StoreRepository) EventSequence(ctx context.Context, tenantID, projectID string) (uint64, error) {
	if r.Store == nil {
		return 0, ErrUnavailable
	}
	var sequence int64
	err := r.Store.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT event_sequence FROM project WHERE tenant_id=$1 AND id=$2`, tenantID, projectID).Scan(&sequence)
	})
	if err != nil {
		return 0, err
	}
	if sequence < 0 {
		return 0, ErrInvalidCursor
	}
	return uint64(sequence), nil
}

func (r StoreRepository) ListExact(ctx context.Context, tenantID, projectID, afterID string, f Filter, limit int) ([]Task, error) {
	if r.Store == nil {
		return nil, ErrUnavailable
	}
	fields := make(map[string][]string, len(f.Fields))
	for id, values := range f.Fields {
		fields[id] = append([]string(nil), values...)
	}
	rows, err := r.Store.ListTasksFiltered(ctx, tenantID, projectID, afterID, int32(limit), projectstore.TaskQuery{
		StatusIDs:       f.StatusIDs,
		DueDateFrom:     f.DueDateFrom,
		DueDateTo:       f.DueDateTo,
		Filter:          projectboard.Filter{AssigneeID: f.AssigneeID, TypeIDs: f.TypeIDs, EnumFields: fields},
		ExcludeArchived: true,
	})
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(rows))
	for _, row := range rows {
		fields := map[string]project.TaskFieldEdit{}
		if len(row.Fields) > 0 {
			if err := json.Unmarshal(row.Fields, &fields); err != nil {
				return nil, err
			}
		}
		dueDate := ""
		if row.DueDate != nil {
			dueDate = row.DueDate.Format("2006-01-02")
		}
		tasks = append(tasks, Task{ID: row.ID, TenantID: row.TenantID, ProjectID: row.ProjectID, Title: row.Title, Description: row.Description, StatusID: row.StatusID, TypeID: row.TypeID, Priority: row.Priority, AssigneeID: row.AssigneeID, DueDate: dueDate, Revision: uint64(row.Revision), Archived: row.Archived, Fields: fields})
	}
	return tasks, nil
}
