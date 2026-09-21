package invalidation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var fastReconnect = ReconnectOptions{MaxAttempts: 2, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond}

func TestTodo_WEB_036(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000101")
	stream := newChannelStream(1)
	requests := make(chan CatchUpRequest, 1)
	var refreshed Refresh
	client, err := New(testScope(subject), func(_ context.Context, refresh Refresh) error {
		refreshed = refresh
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- client.RunReconnect(context.Background(), func(_ context.Context, cursor Cursor) (CloseStream, error) {
			if cursor.Sequence() != 10 || cursor.Watermark() != 10 {
				t.Errorf("initial checkpoint = %d/%d, want 10/10", cursor.Sequence(), cursor.Watermark())
			}
			return stream, nil
		}, func(_ context.Context, request CatchUpRequest) (CatchUpResult, error) {
			requests <- request
			return CatchUpResult{SourceSequence: request.ToSequence, Watermark: request.ToSequence}, nil
		}, fastReconnect)
	}()
	stream.messages <- testMessage(t, 12, subject)

	select {
	case request := <-requests:
		if request.From.Sequence() != 10 || request.From.Watermark() != 10 || request.ToSequence != 11 || request.Tenant != testTenant || request.Projection != "worker_summary" {
			t.Fatalf("catch-up request = %+v, want exact missing range 11", request)
		}
	case <-time.After(time.Second):
		t.Fatal("sequence gap did not invoke catch-up")
	}
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Refetched == 1 })
	if got := client.Snapshot(); got.LastSourceSequence != 12 || got.CatchUps != 1 || got.CatchUpErrors != 0 {
		t.Fatalf("snapshot = %+v, want catch-up then authoritative hint commit", got)
	}
	if refreshed.SourceSequence != 12 || len(refreshed.Subjects) != 1 || refreshed.Subjects[0] != subject {
		t.Fatalf("authoritative refetch = %+v", refreshed)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal = %v, want cancellation", err)
	}
	if stream.closeCalls.Load() != 1 {
		t.Fatalf("stream Close calls = %d, want 1", stream.closeCalls.Load())
	}
}

func TestTodo_WEB_036_Golden(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000102")
	stream := newChannelStream(1)
	events := make(chan Event, 8)
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{Observe: func(event Event) { events <- event }})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
			return stream, nil
		}, successfulCatchUp, fastReconnect)
	}()
	stream.messages <- testMessage(t, 12, subject)
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Refetched == 1 })
	_ = client.Close()
	if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal = %v, want cancellation", err)
	}

	got := receiveEvents(t, events, 5)
	want := []Event{
		{Kind: EventReconnected, SourceSequence: 10},
		{Kind: EventCaughtUp, SourceSequence: 11},
		{Kind: EventAccepted, Items: 1, QueueLength: 1, SourceSequence: 12},
		{Kind: EventRefetched, QueueLength: 0, SourceSequence: 12},
		{Kind: EventClosed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want deterministic %#v", got, want)
	}
}

func TestTodo_WEB_036_Conformance(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000103")
	normalizedOptions := normalizeReconnectOptions(ReconnectOptions{})
	if normalizedOptions.MaxAttempts != defaultReconnectAttempts || normalizedOptions.InitialBackoff != defaultReconnectBackoff || normalizedOptions.MaxBackoff != maxReconnectBackoff {
		t.Fatalf("default reconnect options = %+v", normalizedOptions)
	}
	var cursors []Cursor
	var requests []CatchUpRequest
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = client.RunReconnect(context.Background(), func(_ context.Context, cursor Cursor) (CloseStream, error) {
		cursors = append(cursors, cursor)
		if len(cursors) == 1 {
			return &sliceStream{values: [][]byte{
				testMessage(t, 11, subject),
				testMessage(t, 11, subject),
				testMessage(t, 13, subject),
				testMessage(t, 12, subject),
			}}, nil
		}
		return &sliceStream{}, nil
	}, func(_ context.Context, request CatchUpRequest) (CatchUpResult, error) {
		requests = append(requests, request)
		return CatchUpResult{SourceSequence: request.ToSequence, Watermark: request.ToSequence}, nil
	}, ReconnectOptions{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
	if !errors.Is(err, ErrReconnectExhausted) {
		t.Fatalf("RunReconnect = %v, want bounded exhaustion", err)
	}
	if len(cursors) != 2 || cursors[0].Sequence() != 10 || cursors[1].Sequence() != 13 {
		t.Fatalf("resume checkpoints = %+v, want 10 then last committed 13", cursors)
	}
	if len(requests) != 1 || requests[0].From.Sequence() != 11 || requests[0].ToSequence != 12 {
		t.Fatalf("catch-up requests = %+v, want only exact gap at 12", requests)
	}
	if got := client.Snapshot(); got.LastSourceSequence != 13 || got.CatchUps != 1 || got.Refetched != 2 || got.Rejected != 2 {
		t.Fatalf("duplicate/out-of-order snapshot = %+v", got)
	}

	//lint:ignore SA9005 deliberate: this test proves Cursor exposes no serializable wire fields.
	encoded, err := json.Marshal(cursors[0])
	if err != nil || string(encoded) != "{}" {
		t.Fatalf("checkpoint wire form = %q, %v; want no serializable fields", encoded, err)
	}
	var forged Cursor
	//lint:ignore SA9005 deliberate: this test proves foreign cursor fields decode to the zero value.
	if err := json.Unmarshal([]byte(`{"source_sequence":999,"watermark":999}`), &forged); err != nil {
		t.Fatal(err)
	}
	if forged.Sequence() != 0 || forged.Watermark() != 0 {
		t.Fatalf("JSON forged checkpoint = %d/%d", forged.Sequence(), forged.Watermark())
	}
	zeroScope := testScope(subject)
	zeroScope.SourceSequence = 0
	zeroScope.Watermark = 0
	zeroClient, err := New(zeroScope, func(context.Context, Refresh) error { return nil }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	zeroOpens := 0
	err = zeroClient.RunReconnect(context.Background(), func(_ context.Context, cursor Cursor) (CloseStream, error) {
		zeroOpens++
		if zeroOpens == 1 {
			if cursor.Sequence() != 0 || cursor.Watermark() != 0 {
				t.Fatalf("initial empty checkpoint = %d/%d", cursor.Sequence(), cursor.Watermark())
			}
			return &sliceStream{values: [][]byte{testMessage(t, 1, subject)}}, nil
		}
		return &sliceStream{}, nil
	}, successfulCatchUp, ReconnectOptions{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
	if !errors.Is(err, ErrReconnectExhausted) || zeroClient.Snapshot().LastSourceSequence != 1 || zeroClient.Snapshot().CatchUps != 0 {
		t.Fatalf("initial empty checkpoint result = %v snapshot=%+v", err, zeroClient.Snapshot())
	}

	maxScope := testScope(subject)
	maxScope.SourceSequence = math.MaxUint64
	maxScope.Watermark = math.MaxUint64
	maxClient, err := New(maxScope, func(context.Context, Refresh) error { return nil }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	catchUps := 0
	err = maxClient.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
		return &sliceStream{values: [][]byte{testMessage(t, math.MaxUint64, subject)}}, nil
	}, func(context.Context, CatchUpRequest) (CatchUpResult, error) {
		catchUps++
		return CatchUpResult{}, nil
	}, ReconnectOptions{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
	if !errors.Is(err, ErrReconnectExhausted) || catchUps != 0 || maxClient.Snapshot().LastSourceSequence != math.MaxUint64 {
		t.Fatalf("maximum cursor result = %v, catch-ups=%d snapshot=%+v", err, catchUps, maxClient.Snapshot())
	}
}

func TestTodo_WEB_036_Security(t *testing.T) {
	oldSubject := testSubject("00000000-0000-4000-8000-000000000104")
	newSubject := testSubject("00000000-0000-4000-8000-000000000105")
	foreign := testSubject("00000000-0000-4000-8000-000000000106")
	foreign.Tenant = values.TenantId("other")
	stream := newChannelStream(3)
	events := make(chan Event, 16)
	var refreshed Refresh
	var catchUps atomic.Int32
	client, err := New(testScope(oldSubject), func(_ context.Context, refresh Refresh) error {
		refreshed = refresh
		return nil
	}, Options{Observe: func(event Event) { events <- event }})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
			return stream, nil
		}, func(_ context.Context, request CatchUpRequest) (CatchUpResult, error) {
			catchUps.Add(1)
			next := testScope(newSubject)
			next.SourceSequence = request.ToSequence
			next.Watermark = request.ToSequence
			return CatchUpResult{SourceSequence: request.ToSequence, Watermark: request.ToSequence, Scope: &next}, nil
		}, fastReconnect)
	}()

	stream.messages <- testMessage(t, 12, oldSubject)
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.CatchUps == 1 && snapshot.Rejected == 1 })
	stream.messages <- testMessage(t, 12, newSubject)
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Refetched == 1 })
	stream.messages <- testMessageRevision(t, foreign.Tenant, "worker_summary", 14, 14, foreign)
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Rejected == 2 })
	_ = client.Close()
	terminal := waitReconnectResult(t, result)
	if !errors.Is(terminal, context.Canceled) || catchUps.Load() != 1 {
		t.Fatalf("terminal = %v, catch-ups=%d", terminal, catchUps.Load())
	}
	if refreshed.SourceSequence != 12 || len(refreshed.Subjects) != 1 || refreshed.Subjects[0] != newSubject {
		t.Fatalf("refetch after authorization change = %+v, want only new subject", refreshed)
	}
	if got := client.Snapshot(); got.LastSourceSequence != 12 || got.Accepted != 1 || got.CatchUps != 1 {
		t.Fatalf("security snapshot = %+v", got)
	}
	observed := receiveEvents(t, events, 7)
	for _, event := range observed {
		if event.Kind == EventRejected && event.SourceSequence != 0 {
			t.Fatalf("rejected event disclosed attacker sequence: %+v", event)
		}
	}
	for _, secret := range []string{oldSubject.String(), newSubject.String(), foreign.String()} {
		if strings.Contains(fmt.Sprint(terminal, observed), secret) {
			t.Fatalf("identifier %q leaked through terminal or observability", secret)
		}
	}
}

func TestTodo_WEB_036_Integration(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000107")
	var mu sync.Mutex
	var cursors []uint64
	var streams []*reconnectTestStream
	var applied []uint64
	fourthOpened := make(chan struct{})
	client, err := New(testScope(subject), func(_ context.Context, refresh Refresh) error {
		mu.Lock()
		applied = append(applied, refresh.SourceSequence)
		mu.Unlock()
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- client.RunReconnect(context.Background(), func(_ context.Context, cursor Cursor) (CloseStream, error) {
			mu.Lock()
			cursors = append(cursors, cursor.Sequence())
			var stream *reconnectTestStream
			if cursor.Sequence() < 13 {
				stream = newReconnectTestStream(false, testMessage(t, cursor.Sequence()+1, subject))
			} else {
				stream = newReconnectTestStream(true)
				select {
				case <-fourthOpened:
				default:
					close(fourthOpened)
				}
			}
			streams = append(streams, stream)
			mu.Unlock()
			return stream, nil
		}, func(context.Context, CatchUpRequest) (CatchUpResult, error) {
			return CatchUpResult{}, errors.New("contiguous delivery must not catch up")
		}, ReconnectOptions{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
	}()
	select {
	case <-fourthOpened:
	case <-time.After(time.Second):
		t.Fatal("useful progress did not reset the one-failure budget")
	}
	_ = client.Close()
	if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal = %v, want cancellation", err)
	}
	mu.Lock()
	gotCursors := append([]uint64(nil), cursors...)
	gotApplied := append([]uint64(nil), applied...)
	gotStreams := append([]*reconnectTestStream(nil), streams...)
	mu.Unlock()
	if !reflect.DeepEqual(gotCursors, []uint64{10, 11, 12, 13}) || !reflect.DeepEqual(gotApplied, []uint64{11, 12, 13}) {
		t.Fatalf("integration cursors/applied = %v/%v", gotCursors, gotApplied)
	}
	for i, stream := range gotStreams {
		waitCloseCalls(t, stream.closeCalls.Load, 1)
		if stream.closeCalls.Load() != 1 {
			t.Fatalf("generation %d Close calls = %d", i, stream.closeCalls.Load())
		}
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{testMessage(t, 14, subject)}}); err != nil {
		t.Fatalf("client not retryable after reconnect Close: %v", err)
	}
	if client.Snapshot().LastSourceSequence != 14 {
		t.Fatalf("restart snapshot = %+v", client.Snapshot())
	}
}

func TestTodo_WEB_036_Fault(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000108")

	t.Run("failed catch-up retries unchanged checkpoint", func(t *testing.T) {
		client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
		if err != nil {
			t.Fatal(err)
		}
		second := newChannelStream(1)
		second.messages <- testMessage(t, 12, subject)
		var cursors []uint64
		var calls int
		result := make(chan error, 1)
		go func() {
			result <- client.RunReconnect(context.Background(), func(_ context.Context, cursor Cursor) (CloseStream, error) {
				cursors = append(cursors, cursor.Sequence())
				if len(cursors) == 1 {
					return &sliceStream{values: [][]byte{testMessage(t, 12, subject)}}, nil
				}
				return second, nil
			}, func(_ context.Context, request CatchUpRequest) (CatchUpResult, error) {
				calls++
				if calls == 1 {
					return CatchUpResult{}, errors.New("private upstream secret")
				}
				return CatchUpResult{SourceSequence: request.ToSequence, Watermark: request.ToSequence}, nil
			}, fastReconnect)
		}()
		waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Refetched == 1 })
		_ = client.Close()
		if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) {
			t.Fatalf("terminal = %v", err)
		}
		if !reflect.DeepEqual(cursors, []uint64{10, 10}) {
			t.Fatalf("retry checkpoints = %v, want unchanged 10", cursors)
		}
		if got := client.Snapshot(); got.LastSourceSequence != 12 || got.CatchUpErrors != 1 || got.CatchUps != 1 {
			t.Fatalf("catch-up retry snapshot = %+v", got)
		}
	})

	t.Run("failed refetch retries from catch-up only", func(t *testing.T) {
		var refetches atomic.Int32
		client, err := New(testScope(subject), func(context.Context, Refresh) error {
			if refetches.Add(1) == 1 {
				return errors.New("temporary read failure")
			}
			return nil
		}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		second := newChannelStream(1)
		second.messages <- testMessage(t, 12, subject)
		var mu sync.Mutex
		var cursors []uint64
		result := make(chan error, 1)
		go func() {
			result <- client.RunReconnect(context.Background(), func(_ context.Context, cursor Cursor) (CloseStream, error) {
				mu.Lock()
				cursors = append(cursors, cursor.Sequence())
				attempt := len(cursors)
				mu.Unlock()
				if attempt == 1 {
					return &sliceStream{values: [][]byte{testMessage(t, 12, subject)}}, nil
				}
				return second, nil
			}, successfulCatchUp, fastReconnect)
		}()
		waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Refetched == 1 })
		_ = client.Close()
		_ = waitReconnectResult(t, result)
		mu.Lock()
		gotCursors := append([]uint64(nil), cursors...)
		mu.Unlock()
		if len(gotCursors) < 2 || gotCursors[0] != 10 || gotCursors[1] != 11 {
			t.Fatalf("refetch retry checkpoints = %v, want 10 then catch-up-only 11", gotCursors)
		}
		if got := client.Snapshot(); got.LastSourceSequence != 12 || got.RefetchErrors != 1 || got.Refetched != 1 {
			t.Fatalf("refetch retry snapshot = %+v", got)
		}
	})

	t.Run("cursor mismatches are refused", func(t *testing.T) {
		wrongScope := testScope(subject)
		wrongScope.Projection = "different_projection"
		wrongScope.SourceSequence = 11
		wrongScope.Watermark = 11
		tests := []struct {
			name   string
			result CatchUpResult
		}{
			{name: "sequence regression", result: CatchUpResult{SourceSequence: 10, Watermark: 10}},
			{name: "sequence overshoot", result: CatchUpResult{SourceSequence: 12, Watermark: 11}},
			{name: "watermark regression", result: CatchUpResult{SourceSequence: 11, Watermark: 9}},
			{name: "watermark ahead", result: CatchUpResult{SourceSequence: 11, Watermark: 12}},
			{name: "scope mismatch", result: CatchUpResult{SourceSequence: 11, Watermark: 11, Scope: &wrongScope}},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
				if err != nil {
					t.Fatal(err)
				}
				err = client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
					return &sliceStream{values: [][]byte{testMessage(t, 12, subject)}}, nil
				}, func(context.Context, CatchUpRequest) (CatchUpResult, error) {
					return tc.result, nil
				}, ReconnectOptions{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
				if !errors.Is(err, ErrReconnectExhausted) || client.Snapshot().LastSourceSequence != 10 || client.Snapshot().CatchUpErrors != 1 {
					t.Fatalf("result = %v snapshot=%+v", err, client.Snapshot())
				}
			})
		}
	})

	t.Run("factory partial failures close and panics are contained", func(t *testing.T) {
		stream := newReconnectTestStream(false)
		client, _ := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
		err := client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
			return stream, errors.New("private factory failure")
		}, successfulCatchUp, ReconnectOptions{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
		if !errors.Is(err, ErrReconnectExhausted) || stream.closeCalls.Load() != 1 {
			t.Fatalf("partial factory failure = %v close calls=%d", err, stream.closeCalls.Load())
		}

		client, _ = New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
		err = client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
			panic("private factory panic")
		}, successfulCatchUp, ReconnectOptions{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
		if !errors.Is(err, ErrReconnectExhausted) || strings.Contains(err.Error(), "private") {
			t.Fatalf("factory panic result = %v", err)
		}
	})

	t.Run("cancellation interrupts every phase", func(t *testing.T) {
		t.Run("open", func(t *testing.T) {
			client, _ := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
			started := make(chan struct{})
			result := make(chan error, 1)
			go func() {
				result <- client.RunReconnect(context.Background(), func(ctx context.Context, _ Cursor) (CloseStream, error) {
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				}, successfulCatchUp, fastReconnect)
			}()
			<-started
			if !client.Snapshot().Running {
				t.Fatal("reconnect open phase not reported running")
			}
			if _, err := client.Start(context.Background(), &sliceStream{}); !errors.Is(err, ErrAlreadyRunning) {
				t.Fatalf("concurrent Start = %v, want %v", err, ErrAlreadyRunning)
			}
			if err := client.UpdateScope(testScope(subject)); !errors.Is(err, ErrAlreadyRunning) {
				t.Fatalf("concurrent UpdateScope = %v, want %v", err, ErrAlreadyRunning)
			}
			_ = client.Close()
			if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("open cancellation = %v", err)
			}
		})

		t.Run("backoff", func(t *testing.T) {
			client, _ := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
			called := make(chan struct{}, 1)
			var calls atomic.Int32
			result := make(chan error, 1)
			go func() {
				result <- client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
					calls.Add(1)
					called <- struct{}{}
					return nil, errors.New("refused")
				}, successfulCatchUp, ReconnectOptions{MaxAttempts: 4, InitialBackoff: time.Second, MaxBackoff: time.Second})
			}()
			<-called
			_ = client.Close()
			if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) || calls.Load() != 1 {
				t.Fatalf("backoff cancellation = %v calls=%d", err, calls.Load())
			}
		})

		t.Run("recv", func(t *testing.T) {
			client, _ := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
			stream := newBlockingStream()
			opened := make(chan struct{})
			result := make(chan error, 1)
			go func() {
				result <- client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
					close(opened)
					return stream, nil
				}, successfulCatchUp, fastReconnect)
			}()
			<-opened
			waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Running })
			_ = client.Close()
			if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("recv cancellation = %v", err)
			}
			if stream.closeCalls.Load() != 1 {
				t.Fatalf("recv stream Close calls = %d", stream.closeCalls.Load())
			}
		})

		t.Run("catch-up", func(t *testing.T) {
			client, _ := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
			stream := newChannelStream(1)
			stream.messages <- testMessage(t, 12, subject)
			started := make(chan struct{})
			result := make(chan error, 1)
			go func() {
				result <- client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
					return stream, nil
				}, func(ctx context.Context, _ CatchUpRequest) (CatchUpResult, error) {
					close(started)
					<-ctx.Done()
					return CatchUpResult{}, ctx.Err()
				}, fastReconnect)
			}()
			<-started
			_ = client.Close()
			if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("catch-up cancellation = %v", err)
			}
			if got := client.Snapshot(); got.LastSourceSequence != 10 || got.CatchUpErrors != 0 {
				t.Fatalf("canceled catch-up snapshot = %+v", got)
			}
		})

		t.Run("refetch", func(t *testing.T) {
			started := make(chan struct{})
			client, _ := New(testScope(subject), func(ctx context.Context, _ Refresh) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}, Options{})
			stream := newChannelStream(1)
			stream.messages <- testMessage(t, 11, subject)
			result := make(chan error, 1)
			go func() {
				result <- client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) { return stream, nil }, successfulCatchUp, fastReconnect)
			}()
			<-started
			_ = client.Close()
			if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("refetch cancellation = %v", err)
			}
			if got := client.Snapshot(); got.LastSourceSequence != 10 || got.Refetched != 0 || got.RefetchErrors != 0 {
				t.Fatalf("canceled refetch snapshot = %+v", got)
			}
		})
	})

	t.Run("permanent refusal is bounded and redacted", func(t *testing.T) {
		events := make(chan Event, 4)
		client, _ := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{Observe: func(event Event) { events <- event }})
		var calls int
		err := client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
			calls++
			return nil, errors.New("tenant-secret refusal")
		}, successfulCatchUp, ReconnectOptions{MaxAttempts: 3, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
		if !errors.Is(err, ErrReconnectExhausted) || calls != 3 || strings.Contains(err.Error(), "secret") {
			t.Fatalf("permanent refusal = %v calls=%d", err, calls)
		}
		for _, event := range receiveEvents(t, events, 3) {
			if event != (Event{Kind: EventReconnectErr}) {
				t.Fatalf("refusal event = %+v, want identifier-free generic event", event)
			}
		}
	})
}

func successfulCatchUp(_ context.Context, request CatchUpRequest) (CatchUpResult, error) {
	return CatchUpResult{SourceSequence: request.ToSequence, Watermark: request.ToSequence}, nil
}

func waitReconnectResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("reconnect did not terminate")
		return nil
	}
}

type reconnectTestStream struct {
	mu         sync.Mutex
	values     [][]byte
	blockAfter bool
	closed     chan struct{}
	closeOnce  sync.Once
	closeCalls atomic.Int32
}

func newReconnectTestStream(blockAfter bool, values ...[]byte) *reconnectTestStream {
	return &reconnectTestStream{values: values, blockAfter: blockAfter, closed: make(chan struct{})}
}

func (s *reconnectTestStream) Recv() ([]byte, error) {
	s.mu.Lock()
	if len(s.values) > 0 {
		value := append([]byte(nil), s.values[0]...)
		s.values = s.values[1:]
		s.mu.Unlock()
		return value, nil
	}
	block := s.blockAfter
	s.mu.Unlock()
	if !block {
		return nil, io.EOF
	}
	<-s.closed
	return nil, io.EOF
}

func (s *reconnectTestStream) Close() error {
	s.closeCalls.Add(1)
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func waitCloseCalls(t *testing.T, current func() int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if current() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
