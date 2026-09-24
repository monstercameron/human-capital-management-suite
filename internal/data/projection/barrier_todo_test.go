package projection_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

func TestTodo_DATA_021(t *testing.T) {
	db, tenant := newFixture(t)
	ctx := context.Background()
	deadline := time.Now().UTC().Add(time.Second)
	result, err := projection.Check(ctx, db.Conn, projection.ReadRequirement{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, MinimumSequence: 0, Deadline: deadline})
	if err != nil || result.Status != projection.BarrierReady || result.Checkpoint.LastAppliedSequence != 0 {
		t.Fatalf("ready barrier = %+v, %v", result, err)
	}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE projection_checkpoint SET status='REBUILDING' WHERE tenant_id=$1 AND projection_name=$2 AND stream_key=$3`, tenant, projectionName, streamKey); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result, err = projection.Check(ctx, db.Conn, projection.ReadRequirement{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, MinimumSequence: 1, Deadline: deadline})
	var barrierErr projection.BarrierError
	if !errors.As(err, &barrierErr) || barrierErr.Status != projection.BarrierRebuilding || result.Status != projection.BarrierRebuilding {
		t.Fatalf("rebuilding barrier = %+v, %v", result, err)
	}
}

func TestTodo_DATA_021_Race(t *testing.T) {
	db, tenant := newFixture(t)
	req := projection.ReadRequirement{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, MinimumSequence: 0, Deadline: time.Now().UTC().Add(time.Second)}
	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := projection.Check(context.Background(), db.Conn, req)
			if err != nil {
				errs[i] = err
			} else if got.Status != projection.BarrierReady {
				errs[i] = errors.New("concurrent read was not ready")
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("reader %d: %v", i, err)
		}
	}
}

func TestTodo_DATA_021_Fault(t *testing.T) {
	db, tenant := newFixture(t)
	req := projection.ReadRequirement{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, MinimumSequence: 0, Deadline: time.Now().UTC().Add(-time.Second)}
	got, err := projection.Check(context.Background(), db.Conn, req)
	var barrierErr projection.BarrierError
	if !errors.As(err, &barrierErr) || barrierErr.Status != projection.BarrierTimeout || got.Status != projection.BarrierTimeout {
		t.Fatalf("expired barrier = %+v, %v", got, err)
	}
}

func TestTodo_DATA_021_Recovery(t *testing.T) {
	db, tenant := newFixture(t)
	tx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := projection.SetStatus(context.Background(), tx, tenant, projectionName, streamKey, "REBUILDING"); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := projection.SetStatus(context.Background(), tx, tenant, projectionName, streamKey, projection.StatusCurrent); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := projection.Check(context.Background(), db.Conn, projection.ReadRequirement{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, MinimumSequence: 0, Deadline: time.Now().Add(time.Second)})
	if err != nil || got.Status != projection.BarrierReady {
		t.Fatalf("recovered barrier = %+v, %v", got, err)
	}
}

func TestTodo_DATA_021_Mutation(t *testing.T) {
	db, tenant := newFixture(t)
	_, err := projection.Check(context.Background(), db.Conn, projection.ReadRequirement{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, MinimumSequence: -1, Deadline: time.Now().Add(time.Second)})
	if err == nil {
		t.Fatal("negative required sequence was accepted")
	}
}
