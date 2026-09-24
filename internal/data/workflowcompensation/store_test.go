package workflowcompensation_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowcompensation"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func tenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from)
		VALUES($1,$2,'cell-local','compensation test','ACTIVE',timestamptz '2020-01-01')`, id, "compensation-"+id.String())
	return id
}
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func tenantTx(t *testing.T, db *pgtest.DB, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatal(err)
	}
	if err = fn(tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
func originalScope(tenant uuid.UUID, key string) idempotency.Scope {
	return idempotency.Scope{Tenant: tenant, Capability: "workflow.promotion.execute", EffectScope: "workflow-node:commit", Key: key}
}
func reserveOriginal(t *testing.T, db *pgtest.DB, scope idempotency.Scope) {
	t.Helper()
	store := idempotency.PostgresStore{}
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	tenantTx(t, db, scope.Tenant, func(tx dbport.Tx) error {
		rec, created, err := store.Reserve(context.Background(), tx, scope, hash(scope.Key), idempotency.RetentionPolicy{Retention: time.Hour * 72, RetryWindow: time.Hour * 24}, now)
		if err != nil {
			return err
		}
		if !created || rec.Status != idempotency.StatusReserved {
			return fmt.Errorf("reservation=%+v created=%v", rec, created)
		}
		_, err = store.Complete(context.Background(), tx, scope, idempotency.ResultIdentity{ResultRef: "original-result", EventRef: "original-event"}, now)
		return err
	})
}

func TestTodo_WF_REV_010_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := tenant(t, db)
	scope := originalScope(tenantID, "effect-one")
	reserveOriginal(t, db, scope)
	key := workflowcompensation.Key{Tenant: tenantID, Capability: "compensation.release", Effect: "effect-one", IdempotencyKey: "compensation-one"}
	eventRef := "compensation:v1:" + hash("event-one")
	eventDigest := hash("event-digest-one")
	ctx := context.Background()
	payload := []byte(`{"status":"COMPENSATED"}`)
	// A transaction that loses its caller must leave no event, operation, or
	// closure visible after restart.
	rolledBack, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = tenancy.WithTenant(ctx, rolledBack, tenantID); err != nil {
		t.Fatal(err)
	}
	if err = workflowcompensation.Append(ctx, rolledBack, tenantID, eventRef, eventDigest, payload); err != nil {
		t.Fatal(err)
	}
	if _, _, err = workflowcompensation.Reserve(ctx, rolledBack, key, hash("operation-one")); err != nil {
		t.Fatal(err)
	}
	if _, err = (idempotency.PostgresStore{}).CloseCompensated(ctx, rolledBack, scope, eventRef); err != nil {
		t.Fatal(err)
	}
	if err = rolledBack.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	tenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		original, found, lookupErr := (idempotency.PostgresStore{}).Lookup(ctx, tx, scope)
		if lookupErr != nil {
			return lookupErr
		}
		if !found || original.Status != idempotency.StatusCompleted {
			return fmt.Errorf("rollback left original record %+v found=%v", original, found)
		}
		var count int
		if lookupErr = tx.QueryRow(ctx, `SELECT count(*) FROM workflow_compensation_event WHERE tenant_id=$1 AND event_ref=$2`, tenantID, eventRef).Scan(&count); lookupErr != nil {
			return lookupErr
		}
		if count != 0 {
			return fmt.Errorf("rollback left %d compensation events", count)
		}
		return nil
	})
	tenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		if err := workflowcompensation.Append(ctx, tx, tenantID, eventRef, eventDigest, payload); err != nil {
			return err
		}
		op, created, err := workflowcompensation.Reserve(ctx, tx, key, hash("operation-one"))
		if err != nil {
			return err
		}
		if !created || op.State != "RESERVED" {
			return fmt.Errorf("operation=%+v created=%v", op, created)
		}
		if err = workflowcompensation.Update(ctx, tx, key, hash("operation-one"), "COMPLETED", []byte(`{"event_ref":"`+eventRef+`"}`)); err != nil {
			return err
		}
		_, err = (idempotency.PostgresStore{}).CloseCompensated(ctx, tx, scope, eventRef)
		return err
	})
	// A fresh connection models a process restart: both the event and completed
	// operation are recovered from PostgreSQL, and the old semantic key is closed.
	restarted := db.NewConn(t)
	tx, err := restarted.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	stored, created, err := workflowcompensation.Reserve(ctx, tx, key, hash("operation-one"))
	if err != nil {
		t.Fatal(err)
	}
	var recovered map[string]string
	if err = json.Unmarshal(stored.Payload, &recovered); err != nil {
		t.Fatal(err)
	}
	if created || stored.State != "COMPLETED" || recovered["event_ref"] != eventRef {
		t.Fatalf("restart operation=%+v created=%v", stored, created)
	}
	if err = workflowcompensation.Update(ctx, tx, key, hash("operation-one"), "RESERVED", []byte(`{}`)); err == nil {
		t.Fatal("completed compensation operation was reopened")
	}
	rec, found, err := (idempotency.PostgresStore{}).Lookup(ctx, tx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !found || rec.Status != idempotency.StatusCompensated || rec.CompensatedByRef != eventRef || rec.Identity.EventRef != "original-event" {
		t.Fatalf("closed original record=%+v found=%v", rec, found)
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM workflow_compensation_event WHERE tenant_id=$1 AND event_ref=$2`, tenantID, eventRef).Scan(&count); err != nil || count != 1 {
		t.Fatalf("durable event count=%d err=%v", count, err)
	}
	_ = tx.Rollback(ctx)
	tenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		processed, expireErr := (idempotency.PostgresStore{}).Expire(ctx, tx, tenantID, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
		if expireErr != nil {
			return expireErr
		}
		if processed != 1 {
			return fmt.Errorf("expired record count=%d, want 1", processed)
		}
		compacted, found, lookupErr := (idempotency.PostgresStore{}).Lookup(ctx, tx, scope)
		if lookupErr != nil {
			return lookupErr
		}
		if !found || compacted.Status != idempotency.StatusCompensated || compacted.CompensatedByRef != eventRef || !compacted.Identity.Empty() {
			return fmt.Errorf("expired compensation closure=%+v found=%v", compacted, found)
		}
		return nil
	})
}

func TestTodo_WF_REV_010_Property(t *testing.T) {
	db := pgtest.New(t)
	tenantID := tenant(t, db)
	for i := 0; i < 16; i++ {
		suffix := fmt.Sprintf("effect-%02d", i)
		scope := originalScope(tenantID, suffix)
		reserveOriginal(t, db, scope)
		key := workflowcompensation.Key{Tenant: tenantID, Capability: "compensation.release", Effect: suffix, IdempotencyKey: "compensate-" + suffix}
		ref := "compensation:v1:" + hash(suffix)
		digest := hash("event:" + suffix)
		tenantTx(t, db, tenantID, func(tx dbport.Tx) error {
			if err := workflowcompensation.Append(context.Background(), tx, tenantID, ref, digest, []byte(`{"effect":"`+suffix+`"}`)); err != nil {
				return err
			}
			_, created, err := workflowcompensation.Reserve(context.Background(), tx, key, hash("op:"+suffix))
			if err != nil || !created {
				return fmt.Errorf("reserve created=%v err=%w", created, err)
			}
			if err = workflowcompensation.Update(context.Background(), tx, key, hash("op:"+suffix), "COMPLETED", []byte(`{"done":true}`)); err != nil {
				return err
			}
			_, err = (idempotency.PostgresStore{}).CloseCompensated(context.Background(), tx, scope, ref)
			return err
		})
		tenantTx(t, db, tenantID, func(tx dbport.Tx) error {
			rec, found, err := (idempotency.PostgresStore{}).Lookup(context.Background(), tx, scope)
			if err != nil {
				return err
			}
			if !found || rec.Status != idempotency.StatusCompensated || rec.CompensatedByRef != ref {
				return fmt.Errorf("%s closure=%+v found=%v", suffix, rec, found)
			}
			var executions int
			closed, err := idempotency.Guard(context.Background(), tx, idempotency.PostgresStore{}, scope, hash(scope.Key), idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 24 * time.Hour}, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
				executions++
				return idempotency.ResultIdentity{ResultRef: "should-not-run"}, nil
			})
			if err != nil {
				return err
			}
			if closed.Status != idempotency.StatusCompensated || executions != 0 {
				return fmt.Errorf("%s replay=%+v execution_count=%d", suffix, closed, executions)
			}
			// A new semantic effect scope is minted for the next workflow
			// attempt. It must be executable even though the original key is
			// permanently closed, then replay without repeating its callback.
			fresh := scope
			fresh.EffectScope += ":semantic-revision-2"
			freshDigest := hash("fresh:" + suffix)
			freshRuns := 0
			freshResult, err := idempotency.Guard(context.Background(), tx, idempotency.PostgresStore{}, fresh, freshDigest,
				idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 24 * time.Hour}, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
				func(_ context.Context, tx dbport.Tx) (idempotency.ResultIdentity, error) {
					freshRuns++
					return idempotency.ResultIdentity{ResultRef: "fresh-" + suffix, EventRef: "fresh-event-" + suffix}, nil
				})
			if err != nil || freshResult.Status != idempotency.StatusCompleted || freshRuns != 1 {
				return fmt.Errorf("%s fresh semantic scope=%+v executions=%d err=%v", suffix, freshResult, freshRuns, err)
			}
			freshReplay, err := idempotency.Guard(context.Background(), tx, idempotency.PostgresStore{}, fresh, freshDigest,
				idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 24 * time.Hour}, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					freshRuns++
					return idempotency.ResultIdentity{}, nil
				})
			if err != nil || freshReplay.Status != idempotency.StatusCompleted || freshRuns != 1 {
				return fmt.Errorf("%s fresh semantic replay=%+v executions=%d err=%v", suffix, freshReplay, freshRuns, err)
			}
			again, created, err := workflowcompensation.Reserve(context.Background(), tx, key, hash("op:"+suffix))
			if err != nil {
				return err
			}
			var decoded map[string]any
			if err = json.Unmarshal(again.Payload, &decoded); err != nil {
				return err
			}
			if created || again.State != "COMPLETED" || decoded["done"] != true {
				return fmt.Errorf("%s replay=%+v created=%v", suffix, again, created)
			}
			return nil
		})
	}
}
