package timeclockstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func TestSelfProjectionStoreReadsDurableRevisionAndTimestamp(t *testing.T) {
	store, tenant := adapterFixture(t)
	if err := store.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO time_self_clock_projection (tenant_id,worker_ref,assignment_ref,revision,status_code,last_event_at) VALUES ($1,$2,$3,4,'ON_BREAK',$4)`, tenant, "worker-1", "assignment-1", time.Unix(200, 0).UTC())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	labels := CatalogClockLabels{Locale: workspace.ResolveLocale("de-DE"), WorkerLabel: func(string) string { return "Ada" }, ScheduleLabel: func(string) string { return "Day" }}
	got, err := (SelfProjectionStore{Store: store, Labels: labels}).ReadSelfClock(context.Background(), tenant, "worker-1", "assignment-1")
	if err != nil || got.Revision != 4 || got.StatusLabel != "In Pause" || got.LastEventLabel != time.Unix(200, 0).UTC().Format(time.RFC3339) {
		t.Fatalf("projection=%+v err=%v", got, err)
	}
}

func TestSelfProjectionStoreRejectsUnknownStatusAndMissingLabels(t *testing.T) {
	store, tenant := adapterFixture(t)
	if err := store.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO time_self_clock_projection (tenant_id,worker_ref,assignment_ref,revision,status_code) VALUES ($1,$2,$3,1,'UNKNOWN')`, tenant, "worker-1", "assignment-1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	labels := CatalogClockLabels{Locale: workspace.ResolveLocale("en-US"), WorkerLabel: func(string) string { return "Ada" }, ScheduleLabel: func(string) string { return "Day" }}
	if _, err := (SelfProjectionStore{Store: store, Labels: labels}).ReadSelfClock(context.Background(), tenant, "worker-1", "assignment-1"); err != clockservice.ErrUnavailable {
		t.Fatalf("unknown status err=%v", err)
	}
	if _, err := (SelfProjectionStore{Store: store}).ReadSelfClock(context.Background(), tenant, "worker-1", "assignment-1"); err != clockservice.ErrUnavailable {
		t.Fatalf("missing labels err=%v", err)
	}
	if _, err := (SelfProjectionStore{Store: store}).ReadSelfClock(context.Background(), tenant, "worker-fresh", "assignment-1"); err != clockservice.ErrUnavailable {
		t.Fatalf("fresh missing-label err=%v", err)
	}
	if err := store.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref=$2`, tenant, "worker-fresh").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("missing-label read inserted projection count=%d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
