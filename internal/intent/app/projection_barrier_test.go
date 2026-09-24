package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

type projectionBarrierStore struct {
	*memLifecycleStore
	err error
}

func (s projectionBarrierStore) LoadIntent(context.Context, string, string) (IntentRecord, error) {
	return IntentRecord{}, s.err
}

func TestIntentLoadProjectsTypedProjectionBarrierStatus(t *testing.T) {
	cases := []struct {
		status projection.BarrierStatus
		reason string
	}{
		{projection.BarrierStale, "projection.read_barrier.stale"},
		{projection.BarrierDegraded, "projection.read_barrier.degraded"},
		{projection.BarrierUnavailable, "projection.read_barrier.unavailable"},
		{projection.BarrierRebuilding, "projection.read_barrier.rebuilding"},
		{projection.BarrierTimeout, "projection.read_barrier.timeout"},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			store := projectionBarrierStore{
				memLifecycleStore: newMemLifecycleStore(),
				err:               projection.BarrierError{Status: tc.status, RetryAfter: 250 * time.Millisecond},
			}
			service := &IntentService{store: store}
			_, _, got := service.loadInstance(context.Background(), "private-tenant", "private-intent")
			if got == nil || got.Code() != envelope.CodeUnavailable || got.ReasonRef() != tc.reason || got.RetryAfter() != 1 {
				t.Fatalf("loadInstance error = %+v, want UNAVAILABLE %s with retry", got, tc.reason)
			}
			if message := got.Error(); strings.Contains(message, "private-tenant") || strings.Contains(message, "private-intent") {
				t.Fatalf("barrier error leaked tenant or intent identity: %q", message)
			}
		})
	}
}
