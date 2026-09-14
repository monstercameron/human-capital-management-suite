package application

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestProgressWorkloadSweepsImmediatelyKeepsRunningThroughFailuresAndStops
// proves the stuck-workflow sweep starts at once, is never stopped by a
// failed sweep, stops cleanly on cancellation, and refuses a nonsensical
// interval.
func TestProgressWorkloadSweepsImmediatelyKeepsRunningThroughFailuresAndStops(t *testing.T) {
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runProgressSweeps(ctx, func(context.Context) error {
			if calls.Add(1) >= 3 {
				cancel()
			}
			return errors.New("database unavailable")
		}, time.Millisecond)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runProgressSweeps returned %v after cancellation, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runProgressSweeps did not stop after cancellation")
	}
	if calls.Load() < 3 {
		t.Fatalf("swept %d times, want the workload to keep sweeping through failures", calls.Load())
	}
	if err := runProgressSweeps(context.Background(), nil, 0); err == nil {
		t.Fatal("a zero sweep interval was accepted")
	}
	if _, _, err := composeProgressWorkload(ServeConfig{Tenant: "t"}, nil, nil, nil, nil); err == nil {
		t.Fatal("the progress workload composed without a database pool")
	}
	if route := progressRoute(); route.PrimaryOwner == route.SecondaryRoute || route.StormLimit < 1 {
		t.Fatalf("progress route %+v would be refused by the operations store", route)
	}
}
