package application

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_CHATTONE_006_Integration: a provider that stalls once, through the
// served rewrite over the test PostgreSQL. With one place left on the
// person's day and one call bound short, the writer gets one answered rewrite
// (HTTP 200 with a draft); the provider was called twice; and the durable
// usage holds one succeeded rewrite line and one reservation, so the retry
// counted against the daily allowance only by being answered.
func TestTodo_CHATTONE_006_Integration(t *testing.T) {
	s := newChattoneServedStackWith(t, 200, false, 1)
	if err := ChattoneEnableTenant(s.service.Rewrite.Registry, chattoneTenant); err != nil {
		t.Fatal(err)
	}
	s.service.Rewrite.CallTimeout = 150 * time.Millisecond
	var calls atomic.Int32
	s.provider.rewrite = func(system, user string) string {
		if calls.Add(1) == 1 {
			time.Sleep(2 * time.Second)
		}
		return chattoneFakeRewrite(system, user)
	}
	start := time.Now()
	code, reply := s.rewrite("alice", "Please fix the broken deploy pipeline now", "professional")
	elapsed := time.Since(start)
	if code != 200 || reply.Draft == "" {
		t.Fatalf("a served rewrite whose first call stalled: %d %s", code, reply.raw)
	}
	if n := s.provider.calls(); n != 2 {
		t.Fatalf("the provider was called %d times, want the stalled call asked once more", n)
	}
	if elapsed >= 10*time.Second {
		t.Fatalf("a stalled call held the served rewrite %s", elapsed)
	}
	var lines, succeeded, reservations int
	err := s.store.RunTenantTx(context.Background(), chattoneTenant, func(tx dbport.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE succeeded) FROM chattone_usage WHERE operation='rewrite'`).Scan(&lines, &succeeded); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM chattone_usage WHERE operation='reserve'`).Scan(&reservations)
	})
	if err != nil {
		t.Fatal(err)
	}
	if lines != 1 || succeeded != 1 {
		t.Fatalf("the durable usage holds %d rewrite lines (%d succeeded), want one for the answered call", lines, succeeded)
	}
	if reservations != 1 {
		t.Fatalf("the durable usage holds %d reservations, want the one call counted once", reservations)
	}
	if !strings.Contains(reply.Draft, "deploy") {
		t.Fatalf("the answered rewrite lost the draft: %q", reply.Draft)
	}
}
