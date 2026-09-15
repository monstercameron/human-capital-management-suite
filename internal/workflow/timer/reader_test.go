package timer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// Reader is the production execute.TimerReader: what it hands the driver is
// the durable row, field for field, and a timer that was never promised is
// the scheduler's own not-found refusal rather than a zero value.
func TestReaderLoadsTheDurableRowForTheDriver(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newTimerFixture(t, db, "timer-reader")
	var s timer.Scheduler
	var reader execute.TimerReader = timer.Reader{Scheduler: s}

	fireAt := fixedInstant.Add(36 * time.Hour)
	requirement := wakeRequirement(t, "wait.effective_date", fireAt, testDataset)
	var scheduled timer.Scheduled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		scheduled, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.effective_date",
			Kind: timer.KindUntil, Requirement: requirement, CreatedAt: fixedInstant,
		})
		return err
	})

	var loaded execute.FiredTimer
	f.do(t, func(tx dbport.Tx) error {
		var err error
		loaded, err = reader.LoadTimer(ctx, tx, f.tenant, scheduled.Timer.TimerID)
		return err
	})
	want := execute.FiredTimer{
		TimerID: scheduled.Timer.TimerID, InstanceID: f.instance, NodeID: "wait.effective_date",
		Key: requirement.Digest, State: "PENDING", FiresAt: loaded.FiresAt, Kind: "DELAY",
	}
	if loaded != want {
		t.Fatalf("LoadTimer = %+v, want %+v", loaded, want)
	}
	if !loaded.FiresAt.Equal(fireAt) {
		t.Fatalf("fires_at = %s, want %s", loaded.FiresAt, fireAt)
	}

	err := f.try(func(tx dbport.Tx) error {
		_, loadErr := reader.LoadTimer(ctx, tx, f.tenant, uuid.New())
		return loadErr
	})
	if err == nil || !errors.Is(err, timer.ErrNotFound) {
		t.Fatalf("an unknown timer loaded: err = %v, want ErrNotFound", err)
	}
}
