package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type reclaimerTenants []uuid.UUID

func (t reclaimerTenants) ActiveTenants(context.Context) ([]uuid.UUID, error) { return t, nil }

type reclaimerStore struct {
	seen   []string
	cutoff []time.Time
}

func (s *reclaimerStore) ExpireTenant(tenant string, cutoff time.Time) (int64, error) {
	s.seen = append(s.seen, tenant)
	s.cutoff = append(s.cutoff, cutoff)
	return 1, nil
}

func TestIdempotencyReclaimerScopesEveryActiveTenant(t *testing.T) {
	tenantA, tenantB := uuid.New(), uuid.New()
	cutoff := time.Date(2026, 9, 24, 12, 0, 0, 0, time.FixedZone("test", -4*60*60))
	store := &reclaimerStore{}
	count, err := reclaimIdempotency(context.Background(), reclaimerTenants{tenantA, uuid.Nil, tenantB}, store, cutoff)
	if err != nil || count != 2 || len(store.seen) != 2 {
		t.Fatalf("reclamation = (%d, %v), tenants=%v", count, err, store.seen)
	}
	if store.seen[0] != tenantA.String() || store.seen[1] != tenantB.String() || !store.cutoff[0].Equal(cutoff.UTC()) || !store.cutoff[1].Equal(cutoff.UTC()) {
		t.Fatalf("tenant/cutoff calls = %v / %v", store.seen, store.cutoff)
	}
}
