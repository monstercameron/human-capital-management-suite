package projectstore

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
)

func TestTodo_PM_017_FilterBeforeBoardPageLimit(t *testing.T) {
	store, _ := projectFixture(t)
	ctx := context.Background()
	fields, err := json.Marshal(map[string]any{"team": map[string]string{"FieldID": "team", "Type": "ENUM", "CanonicalValue": `"design"`}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if err := store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES($1,'p1','owner','Board','UTC')`, tenant); err != nil {
				return err
			}
			for _, task := range []struct {
				id, status, priority, assignee string
				archived                       bool
			}{
				{"01", "todo", "NORMAL", "bob", false},
				{"02", "doing", "HIGH", "alice", false},
				{"03", "doing", "HIGH", "alice", true},
				{"04", "doing", "HIGH", "alice", false},
				{"05", "doing", "HIGH", "alice", false},
			} {
				if _, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,priority,assignee_id,fields_json,archived) VALUES($1,$2,'p1','Task',$3,$4,$5,$6::jsonb,$7)`, tenant, task.id, task.status, task.priority, task.assignee, fields, task.archived); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	query := TaskQuery{StatusIDs: []string{"doing"}, RequireStatusScope: true, ExcludeArchived: true, Filter: projectboard.Filter{AssigneeID: "alice", Priorities: []string{"HIGH"}, EnumFields: map[string][]string{"team": {"design"}}}}
	first, err := store.ListTasksFiltered(ctx, "tenant-a", "p1", "", 2, query)
	if err != nil || len(first) != 2 || first[0].ID != "02" || first[1].ID != "04" {
		t.Fatalf("first filtered page=%+v err=%v", first, err)
	}
	second, err := store.ListTasksFiltered(ctx, "tenant-a", "p1", first[1].ID, 2, query)
	if err != nil || len(second) != 1 || second[0].ID != "05" {
		t.Fatalf("second filtered page=%+v err=%v", second, err)
	}
	empty, err := store.ListTasksFiltered(ctx, "tenant-a", "p1", "", 2, TaskQuery{RequireStatusScope: true})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty board status scope=%+v err=%v", empty, err)
	}
	list, err := store.ListTasksFiltered(ctx, "tenant-a", "p1", "", 2, TaskQuery{})
	if err != nil || len(list) != 2 || list[0].ID != "01" || list[1].ID != "02" {
		t.Fatalf("task list page=%+v err=%v", list, err)
	}
	other, err := store.ListTasksFiltered(ctx, "tenant-b", "p1", "", 1, query)
	if err != nil || len(other) != 1 || other[0].TenantID != "tenant-b" {
		t.Fatalf("tenant filtering=%+v err=%v", other, err)
	}
}

func TestTodo_PM_017_StableBoardOrderAcrossPages(t *testing.T) {
	store, _ := projectFixture(t)
	ctx := context.Background()
	if err := store.RunTenantTx(ctx, "tenant-order", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES('tenant-order','p1','owner','Board','UTC')`); err != nil {
			return err
		}
		for _, row := range []struct{ id, title, priority, due string }{
			{"a", "zulu", "NORMAL", "2025-01-03"},
			{"b", "Alpha", "HIGH", "2025-01-02"},
			{"c", "alpha", "LOW", ""},
			{"d", "bravo", "HIGH", "2025-01-01"},
		} {
			var due any
			if row.due != "" {
				due = row.due
			}
			if _, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,priority,due_date) VALUES('tenant-order',$1,'p1',$2,'todo',$3,$4)`, row.id, row.title, row.priority, due); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		order projectboard.OrderField
		desc  bool
		want  []string
	}{
		{projectboard.OrderTitle, false, []string{"b", "c", "d", "a"}},
		{projectboard.OrderTitle, true, []string{"a", "d", "c", "b"}},
		{projectboard.OrderPriority, false, []string{"b", "d", "c", "a"}},
		{projectboard.OrderPriority, true, []string{"a", "c", "d", "b"}},
		{projectboard.OrderDueDate, false, []string{"c", "d", "b", "a"}},
		{projectboard.OrderDueDate, true, []string{"a", "b", "d", "c"}},
		{projectboard.OrderTaskID, true, []string{"d", "c", "b", "a"}},
	} {
		var got []string
		value, id := "", ""
		for {
			rows, err := store.ListBoardTasksFiltered(ctx, "tenant-order", "p1", value, id, tc.order, tc.desc, 2, TaskQuery{})
			if err != nil {
				t.Fatalf("%s desc=%v page: %v", tc.order, tc.desc, err)
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				got = append(got, row.ID)
				id = row.ID
				switch tc.order {
				case projectboard.OrderTitle:
					value = projectboard.FoldTitleForOrder(row.Title)
				case projectboard.OrderPriority:
					value = row.Priority
				case projectboard.OrderDueDate:
					if row.DueDate != nil {
						value = row.DueDate.Format("2006-01-02")
					} else {
						value = ""
					}
				}
			}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s desc=%v got %v want %v", tc.order, tc.desc, got, tc.want)
		}
	}
}

func TestTodo_PM_017_UnicodeTitleCursorConsistency(t *testing.T) {
	store, _ := projectFixture(t)
	ctx := context.Background()
	if err := store.RunTenantTx(ctx, "tenant-unicode", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES('tenant-unicode','p1','owner','Board','UTC')`); err != nil {
			return err
		}
		for _, row := range []struct{ id, title string }{{"a", "I"}, {"b", "i"}, {"c", "Z"}, {"d", "İ"}} {
			if _, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id) VALUES('tenant-unicode',$1,'p1',$2,'todo')`, row.id, row.title); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var got []string
	value, id := "", ""
	for {
		rows, err := store.ListBoardTasksFiltered(ctx, "tenant-unicode", "p1", value, id, projectboard.OrderTitle, false, 1, TaskQuery{})
		if err != nil {
			t.Fatalf("page after %q/%q: %v", value, id, err)
		}
		if len(rows) == 0 {
			break
		}
		row := rows[0]
		got = append(got, row.ID)
		value, id = projectboard.FoldTitleForOrder(row.Title), row.ID
	}
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Unicode title pages got %v want %v", got, want)
	}
}

func TestTodo_PM_018_DueDatePredicatesApplyBeforeLimit(t *testing.T) {
	store, _ := projectFixture(t)
	ctx := context.Background()
	if err := store.RunTenantTx(ctx, "tenant-search", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES('tenant-search','p1','owner','Search','UTC')`); err != nil {
			return err
		}
		for _, row := range []struct{ id, due string }{{"01", "2026-02-28"}, {"02", "2026-03-01"}, {"03", "2026-03-05"}, {"04", "2026-03-06"}} {
			if _, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,due_date) VALUES('tenant-search',$1,'p1','Task','todo',$2)`, row.id, row.due); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	query := TaskQuery{DueDateFrom: "2026-03-01", DueDateTo: "2026-03-05"}
	first, err := store.ListTasksFiltered(ctx, "tenant-search", "p1", "", 2, query)
	if err != nil || len(first) != 2 || first[0].ID != "02" || first[1].ID != "03" {
		t.Fatalf("inclusive bounded due-date page=%+v err=%v", first, err)
	}
	second, err := store.ListTasksFiltered(ctx, "tenant-search", "p1", first[1].ID, 2, query)
	if err != nil || len(second) != 0 {
		t.Fatalf("due-date continuation=%+v err=%v", second, err)
	}
}
