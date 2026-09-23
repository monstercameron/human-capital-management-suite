package evidence

import (
	"context"
	"testing"
	"time"
)

type rev10306ValueKey struct{}

// TestTodo_REV_103_06 proves export jobs run on a request-derived context:
// the job inherits the request's values, carries a timeout deadline, and
// is not aborted when the requesting client disconnects.
func TestTodo_REV_103_06(t *testing.T) {
	dispatcher := &GoDispatcher{}
	defer dispatcher.Drain()

	type seen struct {
		deadline time.Time
		hasValue bool
		hasBound bool
	}
	observed := make(chan seen, 1)
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), rev10306ValueKey{}, "caller:operator"))
	defer cancel()

	dispatcher.Dispatch(parent, exportJob{OperationID: "op-ctx-test"}, func(ctx context.Context, job exportJob) {
		// Capture everything inside the job: the dispatcher's deferred
		// cancel fires as soon as this function returns, so observing
		// from the test goroutine afterwards would race it.
		deadline, ok := ctx.Deadline()
		_, hasValue := ctx.Value(rev10306ValueKey{}).(string)
		observed <- seen{deadline: deadline, hasValue: hasValue, hasBound: ok}
	})

	select {
	case got := <-observed:
		if !got.hasBound {
			t.Fatal("dispatched job has no deadline; an export must be bounded")
		}
		if !got.hasValue {
			t.Fatal("dispatched job lost the request's context values")
		}
		if until := time.Until(got.deadline); until <= 0 || until > defaultExportTimeout {
			t.Fatalf("job deadline is %v away, want within (0, %v]", until, defaultExportTimeout)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dispatched job never ran")
	}
}

// TestTodo_REV_103_06_Fault injects the failure the old code hid behind
// context.Background: the request context is cancelled before the job runs.
// The admitted export must still complete (cancellation detached) under its
// own timeout.
func TestTodo_REV_103_06_Fault(t *testing.T) {
	dispatcher := &GoDispatcher{Timeout: 30 * time.Second}
	defer dispatcher.Drain()

	parent, cancel := context.WithCancel(context.Background())
	cancel() // the client disconnected before the job started

	type outcome struct {
		err      error
		deadline time.Time
		hasBound bool
	}
	done := make(chan outcome, 1)
	dispatcher.Dispatch(parent, exportJob{OperationID: "op-cancelled-parent"}, func(ctx context.Context, job exportJob) {
		// Capture inside the job: the dispatcher's deferred cancel fires
		// when this function returns, so the test goroutine must not
		// inspect the context afterwards.
		deadline, ok := ctx.Deadline()
		done <- outcome{err: ctx.Err(), deadline: deadline, hasBound: ok}
	})

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("job inherited the parent's cancellation: %v", got.err)
		}
		if !got.hasBound {
			t.Fatal("job has no deadline after parent cancellation")
		}
		if until := time.Until(got.deadline); until <= 0 || until > 30*time.Second {
			t.Fatalf("job deadline is %v away, want within (0, 30s]", until)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admitted export never ran after its request context was cancelled")
	}
}

// TestTodo_REV_103_06_Security proves shutdown draining: Drain blocks until
// in-flight exports finish, so a server shutdown cannot abandon an admitted
// export mid-assembly with no terminal operation state.
func TestTodo_REV_103_06_Security(t *testing.T) {
	dispatcher := &GoDispatcher{}
	release := make(chan struct{})

	dispatcher.Dispatch(context.Background(), exportJob{OperationID: "op-drain-test"}, func(ctx context.Context, job exportJob) {
		<-release
	})

	drained := make(chan struct{})
	go func() {
		dispatcher.Drain()
		close(drained)
	}()

	select {
	case <-drained:
		t.Fatal("Drain returned while an export was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("Drain never returned after the in-flight export finished")
	}
}
