package agentpersonastore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/limits"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func limitPolicy() limits.Policy {
	return limits.Policy{Version: "policy-7", InvokerPerPersona: limits.RateLimit{Max: 30, Window: time.Hour}, InvokerConcurrency: 3,
		Conversation: limits.RateLimit{Max: 120, Window: time.Hour}, PersonaDailySpendMicros: 100}
}

func limitRequest(tenant values.TenantId) limits.Request {
	return limits.Request{TenantID: string(tenant), InvokerID: "user:one", ConversationID: "conversation:one", PersonaID: "persona:one", PersonaVersion: 2, PolicyVersion: "policy-7", EstimatedSpendMicros: 40}
}

func TestTodo_AGENTP_015_Integration_ReserveSettleAndRefund(t *testing.T) {
	f := newFixture(t, "limits-tenant")
	s := f.store(t, "limits-tenant")
	ctx := context.Background()
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	token, err := s.Reserve(ctx, limitPolicy(), limitRequest("limits-tenant"), at)
	if err != nil {
		t.Fatal(err)
	}
	if token.ID == "" || token.PolicyVersion != "policy-7" || token.Estimate != 40 {
		t.Fatalf("token = %+v", token)
	}
	if err := s.Settle(ctx, limitPolicy(), token, 25); err != nil {
		t.Fatal(err)
	}
	if err := s.Settle(ctx, limitPolicy(), token, 1); !errors.Is(err, limits.ErrReservationClosed) {
		t.Fatalf("repeat settle = %v, want closed", err)
	}
	second, err := s.Reserve(ctx, limitPolicy(), limitRequest("limits-tenant"), at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, limitPolicy(), second); err != nil {
		t.Fatal(err)
	}
	var reserved, used, active int64
	if err := f.db.QueryRow(ctx, `SELECT reserved_spend,used_spend,active_count FROM persona_limit_buckets WHERE tenant_id=$1 AND bucket_kind='PERSONA'`, f.ids["limits-tenant"]).Scan(&reserved, &used, &active); err != nil {
		t.Fatal(err)
	}
	if reserved != 0 || used != 25 || active != 0 {
		t.Fatalf("durable counters = reserved %d used %d active %d", reserved, used, active)
	}
}

func TestTodo_AGENTP_015_Security_TenantRLSAndPolicyFence(t *testing.T) {
	f := newFixture(t, "limits-a", "limits-b")
	a, b := f.store(t, "limits-a"), f.store(t, "limits-b")
	ctx := context.Background()
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	token, err := a.Reserve(ctx, limitPolicy(), limitRequest("limits-a"), at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reserve(ctx, limitPolicy(), limitRequest("limits-a"), at); !errors.Is(err, limits.ErrInvalid) {
		t.Fatalf("foreign request = %v, want invalid", err)
	}
	if err := a.Settle(ctx, limits.Policy{Version: "stale", InvokerPerPersona: limitPolicy().InvokerPerPersona, InvokerConcurrency: 3, Conversation: limitPolicy().Conversation, PersonaDailySpendMicros: 100}, token, 1); !errors.Is(err, limits.ErrPolicyVersion) {
		t.Fatalf("stale policy settle = %v, want policy version", err)
	}
	var count int
	// The tenant transaction itself is enough to prove the forced RLS fence.
	tx, beginErr := b.begin(ctx)
	if beginErr != nil {
		t.Fatal(beginErr)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM persona_limit_reservations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)
	if count != 0 {
		t.Fatalf("tenant B saw %d foreign reservations", count)
	}
}

func TestTodo_AGENTP_015_Race_ConcurrencyBoundary(t *testing.T) {
	f := newFixture(t, "limits-race")
	const workers = 12
	stores := make([]*TenantStore, workers)
	for i := range stores {
		stores[i] = f.store(t, "limits-race")
	}
	p := limitPolicy()
	p.InvokerConcurrency = 1
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	// Hold one open reservation while all other sessions race the boundary.
	holder, err := stores[0].Reserve(context.Background(), p, limitRequest("limits-race"), at)
	if err != nil {
		t.Fatal(err)
	}
	defer stores[0].Release(context.Background(), p, holder)
	var denied int
	start := make(chan struct{})
	var attempts sync.WaitGroup
	for _, store := range stores[1:] {
		attempts.Add(1)
		go func(store *TenantStore) {
			defer attempts.Done()
			<-start
			token, err := store.Reserve(context.Background(), p, limitRequest("limits-race"), at)
			if err != nil {
				var denial *limits.Denial
				if errors.As(err, &denial) && denial.Code == limits.DenialInvokerConcurrency {
					mu.Lock()
					denied++
					mu.Unlock()
				}
				return
			}
			_ = store.Release(context.Background(), p, token)
		}(store)
	}
	close(start)
	attempts.Wait()
	if denied != workers-1 {
		t.Fatalf("concurrency denials = %d, want %d", denied, workers-1)
	}
}

func TestTodo_AGENTP_015_Race_ReleaseAdmissionBoundary(t *testing.T) {
	f := newFixture(t, "limits-release-race")
	p := limitPolicy()
	p.InvokerConcurrency = 3
	p.InvokerPerPersona.Max = 10000
	p.Conversation.Max = 10000
	p.PersonaDailySpendMicros = 1_000_000
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	stores := make([]*TenantStore, 8)
	for i := range stores {
		stores[i] = f.store(t, "limits-release-race")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for round := 0; round < 20; round++ {
		token, err := stores[0].Reserve(ctx, p, limitRequest("limits-release-race"), at)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for _, store := range stores[1:] {
			wg.Add(1)
			go func(store *TenantStore) {
				defer wg.Done()
				admitted, reserveErr := store.Reserve(ctx, p, limitRequest("limits-release-race"), at)
				if reserveErr == nil {
					if releaseErr := store.Release(ctx, p, admitted); releaseErr != nil {
						t.Errorf("release admitted token: %v", releaseErr)
					}
					return
				}
				var denial *limits.Denial
				if !errors.As(reserveErr, &denial) {
					t.Errorf("reserve error = %v, want a typed denial", reserveErr)
				}
			}(store)
		}
		if err := stores[0].Release(ctx, p, token); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
	}
}
