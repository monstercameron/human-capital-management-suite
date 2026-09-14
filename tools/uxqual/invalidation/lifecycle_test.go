package invalidation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvalidationRejectsTypedNilAndNonCloseableStreams(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000011")
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var typedNil *sliceStream
	if _, err := client.Start(context.Background(), typedNil); !errors.Is(err, ErrNoStream) {
		t.Fatalf("typed-nil Start error = %v, want %v", err, ErrNoStream)
	}
	if _, err := client.Start(context.Background(), &nonCloseStream{}); !errors.Is(err, ErrStreamNotCloseable) {
		t.Fatalf("non-closeable Start error = %v, want %v", err, ErrStreamNotCloseable)
	}
}

func TestInvalidationConcurrentLifecycleAndRestart(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000012")
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	stream := newBlockingStream()
	done, err := client.Start(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	var attempts sync.WaitGroup
	for i := 0; i < 16; i++ {
		attempts.Add(1)
		go func() {
			defer attempts.Done()
			client.Snapshot()
			if _, startErr := client.Start(context.Background(), &sliceStream{}); !errors.Is(startErr, ErrAlreadyRunning) {
				t.Errorf("concurrent Start error = %v, want %v", startErr, ErrAlreadyRunning)
			}
			if runErr := client.Run(context.Background(), &sliceStream{}); !errors.Is(runErr, ErrAlreadyRunning) {
				t.Errorf("concurrent Run error = %v, want %v", runErr, ErrAlreadyRunning)
			}
		}()
	}
	attempts.Wait()
	var closers sync.WaitGroup
	for i := 0; i < 16; i++ {
		closers.Add(1)
		go func() {
			defer closers.Done()
			_ = client.Close()
			client.Snapshot()
		}()
	}
	closers.Wait()
	select {
	case got := <-done:
		if !errors.Is(got, context.Canceled) {
			t.Fatalf("terminal result = %v, want context cancellation", got)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Recv was not interrupted")
	}
	if stream.closeCalls.Load() != 1 {
		t.Fatalf("transport Close calls = %d, want exactly 1", stream.closeCalls.Load())
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{testMessage(t, 11, subject)}}); err != nil {
		t.Fatalf("restart Run: %v", err)
	}
	if got := client.Snapshot(); got.Running || got.LastSourceSequence != 11 || got.Refetched != 1 {
		t.Fatalf("restart snapshot = %+v", got)
	}
}

func TestInvalidationParentCancellationInterruptsRecvAndRefetch(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000013")
	refetchStarted := make(chan struct{})
	client, err := New(testScope(subject), func(ctx context.Context, _ Refresh) error {
		close(refetchStarted)
		<-ctx.Done()
		return ctx.Err()
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	stream := newChannelStream(1)
	ctx, cancel := context.WithCancel(context.Background())
	done, err := client.Start(ctx, stream)
	if err != nil {
		t.Fatal(err)
	}
	stream.messages <- testMessage(t, 11, subject)
	select {
	case <-refetchStarted:
	case <-time.After(time.Second):
		t.Fatal("refetch did not start")
	}
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got, context.Canceled) {
			t.Fatalf("terminal result = %v, want context cancellation", got)
		}
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not stop subscription")
	}
	if stream.closeCalls.Load() != 1 {
		t.Fatalf("transport Close calls = %d, want 1", stream.closeCalls.Load())
	}
	if got := client.Snapshot(); got.LastSourceSequence != 10 || got.Refetched != 0 {
		t.Fatalf("canceled refetch snapshot = %+v, want no cursor commit", got)
	}
}

func TestInvalidationShutdownDoesNotWaitForRefetchBoundaryThatIgnoresContext(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000022")
	refetchStarted := make(chan struct{})
	releaseRefetch := make(chan struct{})
	client, err := New(testScope(subject), func(context.Context, Refresh) error {
		close(refetchStarted)
		<-releaseRefetch // deliberately violates the cancellation contract
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	stream := newChannelStream(1)
	done, err := client.Start(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	stream.messages <- testMessage(t, 11, subject)
	select {
	case <-refetchStarted:
	case <-time.After(time.Second):
		t.Fatal("refetch did not start")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if !errors.Is(got, context.Canceled) {
			t.Fatalf("terminal result = %v, want context cancellation", got)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for a refetch callback that ignored context")
	}
	close(releaseRefetch)
}

func TestReconnectShutdownDoesNotWaitForCatchUpBoundaryThatIgnoresContext(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000023")
	catchUpStarted := make(chan struct{})
	releaseCatchUp := make(chan struct{})
	stream := &sliceStream{values: [][]byte{testMessage(t, 12, subject)}}
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
			return stream, nil
		}, func(context.Context, CatchUpRequest) (CatchUpResult, error) {
			close(catchUpStarted)
			<-releaseCatchUp // deliberately violates the cancellation contract
			return CatchUpResult{}, nil
		}, fastReconnect)
	}()
	select {
	case <-catchUpStarted:
	case <-time.After(time.Second):
		t.Fatal("catch-up did not start")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if !errors.Is(got, context.Canceled) {
			t.Fatalf("terminal result = %v, want context cancellation", got)
		}
	case <-time.After(time.Second):
		t.Fatal("reconnect shutdown waited for catch-up callback")
	}
	close(releaseCatchUp)
}

func TestInvalidationQueueFullPreservesCatchUpCursor(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000014")
	refetchStarted := make(chan struct{})
	client, err := New(testScope(subject), func(ctx context.Context, refresh Refresh) error {
		if refresh.SourceSequence == 11 {
			close(refetchStarted)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}, Options{MaxQueue: 1})
	if err != nil {
		t.Fatal(err)
	}
	stream := newChannelStream(4)
	done, err := client.Start(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	stream.messages <- testMessageRevision(t, testTenant, "worker_summary", 11, 11, subject)
	select {
	case <-refetchStarted:
	case <-time.After(time.Second):
		t.Fatal("first refetch did not start")
	}
	stream.messages <- testMessageRevision(t, testTenant, "worker_summary", 12, 12, subject)
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Accepted == 2 })
	stream.messages <- testMessageRevision(t, testTenant, "worker_summary", 13, 13, subject)
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.QueueFull == 1 })
	if got := client.Snapshot(); got.LastSourceSequence != 10 || got.Accepted != 2 || got.QueueFull != 1 {
		t.Fatalf("overload snapshot = %+v, want no speculative cursor commit", got)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("overloaded client did not close")
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{testMessageRevision(t, testTenant, "worker_summary", 13, 13, subject)}}); err != nil {
		t.Fatal(err)
	}
	if got := client.Snapshot(); got.LastSourceSequence != 13 || got.Refetched != 1 {
		t.Fatalf("catch-up retry snapshot = %+v, want sequence 13 committed", got)
	}
}

func TestInvalidationUpdateScopeAfterCanceledGeneration(t *testing.T) {
	oldSubject := testSubject("00000000-0000-4000-8000-000000000017")
	newSubject := testSubject("00000000-0000-4000-8000-000000000018")
	started := make(chan struct{})
	var applied atomic.Int32
	client, err := New(testScope(oldSubject), func(ctx context.Context, refresh Refresh) error {
		if refresh.Subjects[0] == oldSubject {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		applied.Add(1)
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	stream := newChannelStream(1)
	done, err := client.Start(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	stream.messages <- testMessageRevision(t, testTenant, "worker_summary", 11, 11, oldSubject)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("old-generation refetch did not start")
	}
	if err := client.UpdateScope(testScope(newSubject)); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("live UpdateScope error = %v, want %v", err, ErrAlreadyRunning)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("old generation did not close")
	}
	newScope := testScope(newSubject)
	newScope.SourceSequence = 20
	newScope.Watermark = 20
	if err := client.UpdateScope(newScope); err != nil {
		t.Fatal(err)
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{testMessageRevision(t, testTenant, "worker_summary", 21, 21, newSubject)}}); err != nil {
		t.Fatal(err)
	}
	if applied.Load() != 1 {
		t.Fatalf("new-generation applications = %d, want 1", applied.Load())
	}
	if got := client.Snapshot(); got.LastSourceSequence != 21 || got.Refetched != 1 {
		t.Fatalf("new-generation snapshot = %+v", got)
	}
}
