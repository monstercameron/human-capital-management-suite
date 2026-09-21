package workforce_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

// TestTodo_REV_070_01_Integration proves the lifecycle tokens are
// durable facts: a terminated contractor row and an active
// employee row commit through the real store and list back with
// their tokens intact, so the directory projection reads state
// the database actually kept.
func TestTodo_REV_070_01_Integration(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "rev070")
	gone := newRow(tenant, "gone")
	gone.LifecycleStatus = "TERMINATED"
	gone.WorkerType = "CONTRACTOR"
	staying := newRow(tenant, "staying")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := (workforce.Store{}).Create(context.Background(), tx, gone); err != nil {
			return err
		}
		_, err := (workforce.Store{}).Create(context.Background(), tx, staying)
		return err
	})
	var listed []workforce.WorkerRow
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		rows, err := (workforce.Store{}).List(context.Background(), tx, tenant)
		if err != nil {
			return err
		}
		listed = rows
		return nil
	})
	byKey := make(map[string]workforce.WorkerRow, len(listed))
	for _, row := range listed {
		byKey[row.WorkerKey] = row
	}
	terminated, ok := byKey["gone"]
	if !ok {
		t.Fatal("terminated row missing from list")
	}
	if terminated.LifecycleStatus != "TERMINATED" || terminated.WorkerType != "CONTRACTOR" {
		t.Fatalf("terminated row lists %+v", terminated)
	}
	active, ok := byKey["staying"]
	if !ok {
		t.Fatal("active row missing from list")
	}
	if active.LifecycleStatus != "active" || active.WorkerType != "employee" {
		t.Fatalf("active row lists %+v", active)
	}
	var _ = uuid.New
	var _ = pgtest.New
}
