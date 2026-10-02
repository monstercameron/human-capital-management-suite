package agentrunstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// The heartbeat extends the lease of the worker that holds it, without a new
// revision, and only while it holds it.
func TestTodo_AGENTRUN_001_RenewLease(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	tenant := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenant); err != nil {
		t.Fatal(err)
	}
	store := openAgentStore(t, db)
	repository, err := New(store, func(string) uuid.UUID { return tenant })
	if err != nil {
		t.Fatal(err)
	}
	runs, err := repository.ForTenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	admission := testAcceptedAdmission(now)
	if err := insertAcceptedAdmission(ctx, store, tenant, admission); err != nil {
		t.Fatal(err)
	}
	service, err := runstate.New(runs, allowRecheck{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, admission)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker-1", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	until := now.Add(time.Minute)
	if err := runs.RenewLease(ctx, run.ID, "worker-1", claimed.Fence, until, now.Add(time.Millisecond)); err != nil {
		t.Fatalf("renew: %v", err)
	}
	renewed, err := runs.Get(ctx, run.ID)
	if err != nil || !renewed.Lease.Until.Equal(until) || renewed.Version != claimed.Version || renewed.Fence != claimed.Fence {
		t.Fatalf("renewed run=%+v err=%v: want the lease at %v and the same revision %d", renewed, err, until, claimed.Version)
	}
	// The worker's own next write is not disturbed by the heartbeat.
	if _, err := service.Checkpoint(ctx, run.ID, "worker-1", claimed.Fence, claimed.Version, runstate.PhaseContext, 0, "ctx", testDigest("ctx"), now.Add(2*time.Millisecond)); err != nil {
		t.Fatalf("checkpoint after a heartbeat: %v", err)
	}
	// A renewal never shortens a lease.
	if err := runs.RenewLease(ctx, run.ID, "worker-1", claimed.Fence, now.Add(2*time.Second), now.Add(3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if again, _ := runs.Get(ctx, run.ID); !again.Lease.Until.Equal(until) {
		t.Fatalf("a heartbeat shortened the lease to %v", again.Lease.Until)
	}
	for name, err := range map[string]error{
		"another owner":    runs.RenewLease(ctx, run.ID, "worker-2", claimed.Fence, until, now),
		"another fence":    runs.RenewLease(ctx, run.ID, "worker-1", claimed.Fence+1, until, now),
		"an expired lease": runs.RenewLease(ctx, run.ID, "worker-1", claimed.Fence, until.Add(time.Hour), until.Add(time.Second)),
		"no run":           runs.RenewLease(ctx, "missing", "worker-1", claimed.Fence, until, now),
		"no owner":         runs.RenewLease(ctx, run.ID, "", claimed.Fence, until, now),
		"no fence":         runs.RenewLease(ctx, run.ID, "worker-1", 0, until, now),
	} {
		if !errors.Is(err, runstate.ErrLease) {
			t.Fatalf("renewal for %s = %v, want ErrLease", name, err)
		}
	}
	// Taken over by another process: the old holder's heartbeat learns it.
	if _, err := service.InterruptOrphan(ctx, run.ID, mustVersion(t, runs, run.ID), until.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := runs.RenewLease(ctx, run.ID, "worker-1", claimed.Fence, until.Add(time.Hour), now); !errors.Is(err, runstate.ErrLease) {
		t.Fatalf("renewal after takeover = %v, want ErrLease", err)
	}
}

func mustVersion(t *testing.T, runs *TenantStore, id string) uint64 {
	t.Helper()
	run, err := runs.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return run.Version
}
