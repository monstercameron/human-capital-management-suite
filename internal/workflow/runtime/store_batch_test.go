package runtime_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_REV_090_02_RuntimeBatch proves the batched instance and node reads
// answer exactly what the single-instance reads answer, for several instances
// at once, and stay silent about another tenant's rows (REV-090-02).
func TestTodo_REV_090_02_RuntimeBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "rev09002-runtime")
	other := insertTenant(t, db, "rev09002-runtime-other")
	plan := referencePlan(t)
	store := runtime.Store{}
	conn := appConn(t, db)

	create := func(tenantID uuid.UUID, nodes int) runtime.Instance {
		inst := newInstance(t, tenantID, plan)
		var created runtime.Instance
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			created, err = store.CreateInstance(ctx, tx, inst)
			if err != nil {
				return err
			}
			version := created.InstanceVersion
			for attempt := 1; attempt <= nodes; attempt++ {
				node := runtime.NewNodeExecution(tenantID, inst.InstanceID, plan.StartNodeID, attempt,
					workflow.StepCapability, runtime.NodeRunning)
				node.StartedAt = timePtr(fixedInstant)
				if _, version, err = store.RecordNodeExecution(ctx, tx, node, version); err != nil {
					return err
				}
			}
			return nil
		})
		return created
	}
	first, second, empty := create(tenant, 2), create(tenant, 1), create(tenant, 0)
	foreign := create(other, 1)
	missing := uuid.New()
	ids := []uuid.UUID{first.InstanceID, second.InstanceID, empty.InstanceID, foreign.InstanceID, missing}

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		instances, err := store.LoadInstances(ctx, tx, tenant, ids)
		if err != nil {
			t.Fatalf("LoadInstances: %v", err)
		}
		if len(instances) != 3 {
			t.Fatalf("LoadInstances returned %d instances, want the tenant's 3", len(instances))
		}
		if _, leaked := instances[foreign.InstanceID]; leaked {
			t.Fatal("LoadInstances disclosed another tenant's instance")
		}
		nodes, err := store.LoadNodeExecutionsForInstances(ctx, tx, tenant, ids)
		if err != nil {
			t.Fatalf("LoadNodeExecutionsForInstances: %v", err)
		}
		if _, leaked := nodes[foreign.InstanceID]; leaked {
			t.Fatal("LoadNodeExecutionsForInstances disclosed another tenant's nodes")
		}
		for _, id := range []uuid.UUID{first.InstanceID, second.InstanceID, empty.InstanceID} {
			single, err := store.LoadInstance(ctx, tx, tenant, id)
			if err != nil {
				t.Fatalf("LoadInstance(%s): %v", id, err)
			}
			if !reflect.DeepEqual(instances[id], single) {
				t.Fatalf("batched instance %s = %+v, single read = %+v", id, instances[id], single)
			}
			singleNodes, err := store.LoadNodeExecutions(ctx, tx, tenant, id)
			if err != nil {
				t.Fatalf("LoadNodeExecutions(%s): %v", id, err)
			}
			if len(singleNodes) == 0 && len(nodes[id]) == 0 {
				continue
			}
			if !reflect.DeepEqual(nodes[id], singleNodes) {
				t.Fatalf("batched nodes for %s = %+v, single read = %+v", id, nodes[id], singleNodes)
			}
		}
		if got := len(nodes[first.InstanceID]); got != 2 {
			t.Fatalf("first instance has %d batched attempts, want 2", got)
		}
		return nil
	})

	// An empty request is answered without a statement and without error.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		instances, err := store.LoadInstances(ctx, tx, tenant, nil)
		if err != nil || len(instances) != 0 {
			t.Fatalf("LoadInstances(nil) = %v, %v; want empty", instances, err)
		}
		nodes, err := store.LoadNodeExecutionsForInstances(ctx, tx, tenant, nil)
		if err != nil || len(nodes) != 0 {
			t.Fatalf("LoadNodeExecutionsForInstances(nil) = %v, %v; want empty", nodes, err)
		}
		return nil
	})
}
