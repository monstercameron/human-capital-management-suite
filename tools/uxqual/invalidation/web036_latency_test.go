//go:build !race

// Wall-clock interaction budgets are verified by the ordinary test and
// coverage sweep. Race instrumentation deliberately distorts these timings,
// so the race job exercises WEB-036's functional, fault, and security tests
// while leaving product latency measurement to an uninstrumented process.

package invalidation

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

func TestTodo_WEB_036_Latency(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000109")
	raw := testMessage(t, 12, subject)
	budget := latencygate.Budget{Name: "invalidation reconnect catch-up", P95: 2 * time.Millisecond, Warmups: 3, Samples: 25}
	result, err := latencygate.Measure(budget, func() error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
		if err != nil {
			return err
		}
		err = client.RunReconnect(ctx, func(context.Context, Cursor) (CloseStream, error) {
			return &cancelAfterStream{value: raw, cancel: cancel}, nil
		}, successfulCatchUp, ReconnectOptions{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
		if !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatalf("%v (%s)", err, result)
	}
	t.Logf("%s", result)
}

func BenchmarkSequenceReconnectCatchUp(b *testing.B) {
	subject := testSubject("00000000-0000-4000-8000-000000000110")
	raw := testMessage(b, 12, subject)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
		if err != nil {
			b.Fatal(err)
		}
		err = client.RunReconnect(ctx, func(context.Context, Cursor) (CloseStream, error) {
			return &cancelAfterStream{value: raw, cancel: cancel}, nil
		}, successfulCatchUp, ReconnectOptions{MaxAttempts: 1, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
		if !errors.Is(err, context.Canceled) {
			b.Fatal(err)
		}
	}
}

type cancelAfterStream struct {
	value  []byte
	cancel context.CancelFunc
}

func (s *cancelAfterStream) Recv() ([]byte, error) {
	if s.value != nil {
		value := append([]byte(nil), s.value...)
		s.value = nil
		return value, nil
	}
	s.cancel()
	return nil, io.EOF
}

func (*cancelAfterStream) Close() error { return nil }
