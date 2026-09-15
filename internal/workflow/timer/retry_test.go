package timer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// TestRetryTimersScheduleTheDurableBackoff pins the production
// execute.RetryTimerFactory (WF-RUN-006): the RETRY_BACKOFF row carries the
// derived identity, key and exact instant; a replay of the same decision
// writes nothing; a replay that disagrees about the instant and a malformed
// request are refused; and the reader reports the kind so the driver can tell
// a retry wake from a WAIT one.
func TestRetryTimersScheduleTheDurableBackoff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newTimerFixture(t, db, "timer-retry")
	retries := timer.RetryTimers{}
	fireAt := fixedInstant.Add(22*time.Second + 272*time.Millisecond)
	req := execute.RetryTimerRequest{
		TenantID: f.tenant, InstanceID: f.instance, NodeID: "observe_payroll", Attempt: 2,
		Key: execute.RetryBackoffKey(2), FiresAt: fireAt, CreatedAt: fixedInstant,
	}

	var handle execute.TimerHandle
	f.do(t, func(tx dbport.Tx) error {
		var err error
		handle, err = retries.ScheduleRetry(ctx, tx, req)
		return err
	})
	wantID := timer.RetryTimerID(f.tenant, f.instance, "observe_payroll", 2)
	if handle.TimerID != wantID || handle.Replay || !handle.RetryBackoff || !handle.FiresAt.Equal(fireAt) || handle.Key != req.Key {
		t.Fatalf("handle = %+v, want a fresh retry timer %s at %s", handle, wantID, fireAt)
	}
	if wantID == timer.RetryTimerID(f.tenant, f.instance, "observe_payroll", 3) {
		t.Fatal("retry timer identity does not depend on the attempt")
	}

	var loaded execute.FiredTimer
	f.do(t, func(tx dbport.Tx) error {
		var err error
		loaded, err = timer.Reader{}.LoadTimer(ctx, tx, f.tenant, wantID)
		return err
	})
	if loaded.Kind != runtimestate.TimerRetryBackoff || loaded.State != runtimestate.TimerPending || !loaded.FiresAt.Equal(fireAt) || loaded.Key != req.Key {
		t.Fatalf("loaded = %+v, want a PENDING RETRY_BACKOFF row at %s", loaded, fireAt)
	}

	f.do(t, func(tx dbport.Tx) error {
		var err error
		handle, err = retries.ScheduleRetry(ctx, tx, req)
		return err
	})
	if !handle.Replay || handle.TimerID != wantID {
		t.Fatalf("replayed schedule = %+v, want Replay of %s", handle, wantID)
	}

	moved := req
	moved.FiresAt = fireAt.Add(time.Second)
	if err := f.try(func(tx dbport.Tx) error {
		_, err := retries.ScheduleRetry(ctx, tx, moved)
		return err
	}); !errors.Is(err, timer.ErrInvalid) {
		t.Fatalf("replay at another instant = %v, want ErrInvalid", err)
	}

	for name, bad := range map[string]func(r *execute.RetryTimerRequest){
		"first attempt": func(r *execute.RetryTimerRequest) { r.Attempt, r.Key = 1, execute.RetryBackoffKey(1) },
		"foreign key":   func(r *execute.RetryTimerRequest) { r.Key = "wake-digest" },
		"no instant":    func(r *execute.RetryTimerRequest) { r.FiresAt = time.Time{} },
		"no tenant":     func(r *execute.RetryTimerRequest) { r.TenantID = uuid.Nil },
	} {
		r := req
		r.Attempt, r.Key = 3, execute.RetryBackoffKey(3)
		bad(&r)
		if err := f.try(func(tx dbport.Tx) error {
			_, err := retries.ScheduleRetry(ctx, tx, r)
			return err
		}); !errors.Is(err, timer.ErrInvalid) {
			t.Errorf("%s: ScheduleRetry = %v, want ErrInvalid", name, err)
		}
	}
}
