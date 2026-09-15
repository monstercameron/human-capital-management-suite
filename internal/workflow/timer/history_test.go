package timer_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// TestScheduler_HistoryKeepsSettledTimers proves History, unlike Pending,
// still returns a timer after it was cancelled: the execution inspector reads
// it to explain a wait or retry backoff that already settled.
func TestScheduler_HistoryKeepsSettledTimers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newTimerFixture(t, db, "timer-history")
	s := timer.Scheduler{}
	fireAt := fixedInstant.Add(24 * time.Hour)

	var first, second timer.Scheduled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		first, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.first",
			Kind: timer.KindDeadline, Requirement: wakeRequirement(t, "wait.first", fireAt, testDataset),
			CreatedAt: fixedInstant,
		})
		if err != nil {
			return err
		}
		second, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.second",
			Kind: timer.KindDeadline, Requirement: wakeRequirement(t, "wait.second", fireAt.Add(time.Hour), testDataset),
			CreatedAt: fixedInstant.Add(time.Minute),
		})
		return err
	})
	f.do(t, func(tx dbport.Tx) error {
		_, err := s.Cancel(ctx, tx, f.tenant, first.Timer.TimerID, fireAt, "superseded")
		return err
	})

	var pending, history []timer.Timer
	f.do(t, func(tx dbport.Tx) error {
		var err error
		if pending, err = s.Pending(ctx, tx, f.tenant, f.instance); err != nil {
			return err
		}
		history, err = s.History(ctx, tx, f.tenant, f.instance)
		return err
	})
	if len(pending) != 1 || pending[0].TimerID != second.Timer.TimerID {
		t.Fatalf("pending = %+v, want only the second timer", pending)
	}
	if len(history) != 2 {
		t.Fatalf("history holds %d timers, want 2: %+v", len(history), history)
	}
	if history[0].TimerID != first.Timer.TimerID || history[0].State != runtimestate.TimerCancelled {
		t.Errorf("history[0] = %s %s, want the cancelled first timer", history[0].TimerID, history[0].State)
	}
	if history[1].TimerID != second.Timer.TimerID || history[1].State != runtimestate.TimerPending {
		t.Errorf("history[1] = %s %s, want the pending second timer", history[1].TimerID, history[1].State)
	}
}
