package workitem_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// TestTodo_REV_090_02_WorkItemBatch proves ListForInstances and
// LoadTransitionsForItems answer what ListForInstance and LoadTransitions
// answer, for several instances at once, in the same order, and never return
// another tenant's rows (REV-090-02).
func TestTodo_REV_090_02_WorkItemBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "rev09002-workitem")
	other := insertTenant(t, db, "rev09002-workitem-other")
	store := workitem.Store{}
	conn := appConn(t, db)

	instanceWith := func(tenantID uuid.UUID, items int) uuid.UUID {
		instance := uuid.New()
		insertInstance(t, db, tenantID, instance)
		for index := 0; index < items; index++ {
			in, err := workitem.NewWorkItem(newTaskInput(tenantID, instance))
			if err != nil {
				t.Fatalf("NewWorkItem: %v", err)
			}
			inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
				_, err := store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
				return err
			})
		}
		return instance
	}
	busy, single, idle := instanceWith(tenant, 2), instanceWith(tenant, 1), instanceWith(tenant, 0)
	foreign := instanceWith(other, 1)
	ids := []uuid.UUID{busy, single, idle, foreign}

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		batched, err := store.ListForInstances(ctx, tx, tenant, ids)
		if err != nil {
			t.Fatalf("ListForInstances: %v", err)
		}
		if _, leaked := batched[foreign]; leaked {
			t.Fatal("ListForInstances disclosed another tenant's work items")
		}
		if len(batched[busy]) != 2 || len(batched[single]) != 1 || len(batched[idle]) != 0 {
			t.Fatalf("batched item counts = %d/%d/%d, want 2/1/0", len(batched[busy]), len(batched[single]), len(batched[idle]))
		}
		var itemIDs []uuid.UUID
		for _, instance := range []uuid.UUID{busy, single} {
			want, err := store.ListForInstance(ctx, tx, tenant, instance)
			if err != nil {
				t.Fatalf("ListForInstance: %v", err)
			}
			if !reflect.DeepEqual(batched[instance], want) {
				t.Fatalf("batched items for %s = %+v, single read = %+v", instance, batched[instance], want)
			}
			for _, item := range want {
				itemIDs = append(itemIDs, item.WorkItemID)
			}
		}
		transitions, err := store.LoadTransitionsForItems(ctx, tx, tenant, append(itemIDs, uuid.New()))
		if err != nil {
			t.Fatalf("LoadTransitionsForItems: %v", err)
		}
		for _, id := range itemIDs {
			want, err := store.LoadTransitions(ctx, tx, tenant, id)
			if err != nil {
				t.Fatalf("LoadTransitions: %v", err)
			}
			if len(want) == 0 {
				t.Fatalf("fixture assumption broken: item %s recorded no transition", id)
			}
			if !reflect.DeepEqual(transitions[id], want) {
				t.Fatalf("batched transitions for %s = %+v, single read = %+v", id, transitions[id], want)
			}
		}
		empty, err := store.ListForInstances(ctx, tx, tenant, nil)
		if err != nil || len(empty) != 0 {
			t.Fatalf("ListForInstances(nil) = %v, %v; want empty", empty, err)
		}
		none, err := store.LoadTransitionsForItems(ctx, tx, tenant, nil)
		if err != nil || len(none) != 0 {
			t.Fatalf("LoadTransitionsForItems(nil) = %v, %v; want empty", none, err)
		}
		return nil
	})
}
