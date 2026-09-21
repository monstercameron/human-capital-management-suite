//go:build !race

// Wall-clock budgets describe production behavior. Race instrumentation adds
// synchronization and allocation overhead that makes those measurements
// intentionally unrepresentative; the ordinary coverage sweep still runs
// this latency test, benchmark, and fuzz seed.

package invalidation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

func TestTodo_WEB_035_Latency(t *testing.T) {
	budget := latencygate.Budget{Name: "invalidation validation", P95: 2 * time.Millisecond, Warmups: 3, Samples: 25}
	subject := testSubject("00000000-0000-4000-8000-000000000019")
	raw := testMessage(t, 11, subject)
	result, err := latencygate.Measure(budget, func() error {
		client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
		if err != nil {
			return err
		}
		return client.Run(context.Background(), &sliceStream{values: [][]byte{raw}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatalf("%v (%s)", err, result)
	}
	t.Logf("%s", result)
}

func BenchmarkInvalidationValidation(b *testing.B) {
	subject := testSubject("00000000-0000-4000-8000-000000000020")
	raw := testMessage(b, 11, subject)
	scope := testScope(subject)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		client, err := New(scope, func(context.Context, Refresh) error { return nil }, Options{})
		if err != nil {
			b.Fatal(err)
		}
		if err := client.Run(context.Background(), &sliceStream{values: [][]byte{raw}}); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzDecode(f *testing.F) {
	subject := testSubject("00000000-0000-4000-8000-000000000021")
	f.Add(testMessage(f, 11, subject))
	f.Add([]byte(`{"unknown":"00000000-0000-4000-8000-000000000099"}`))
	f.Add([]byte{0xff, 0xfe, 0xfd})
	f.Fuzz(func(t *testing.T, raw []byte) {
		message, err := Decode(raw, DefaultMaxMessageBytes)
		if err != nil {
			if err.Error() != ErrInvalidMessage.Error() {
				t.Fatalf("Decode error = %q, want deterministic sentinel", err)
			}
			return
		}
		if validateErr := message.Validate(); validateErr != nil {
			t.Fatalf("Decode accepted invalid message: %v", validateErr)
		}
		encoded, encodeErr := json.Marshal(message)
		if encodeErr != nil || len(encoded) > DefaultMaxMessageBytes {
			t.Fatalf("accepted message encoding len=%d error=%v", len(encoded), encodeErr)
		}
	})
}
