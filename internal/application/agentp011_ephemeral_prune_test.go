package application

import (
	"context"
	"errors"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_AGENTP_011_ExpiredPrivateAnswersAreDeleted runs the served sweep
// over a real chat store. Reads already hide an expired private answer; the
// sweep is what removes its text from the database.
func TestTodo_AGENTP_011_ExpiredPrivateAnswersAreDeleted(t *testing.T) {
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	store, err := chatstore.New(context.Background(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	adapter := chatstore.NewAdapter(store)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		room := "room-" + tenant
		if _, err := adapter.CreateConversation(ctx, chatcore.Conversation{ID: room, TenantID: tenant, Kind: chatcore.PublicChannel, Name: room, OwnerID: "alice", Revision: 1}, []chatcore.Membership{
			{TenantID: tenant, HomeTenantID: tenant, ConversationID: room, SubjectID: "alice", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
		}, ""); err != nil {
			t.Fatal(err)
		}
	}
	put := func(tenant, id string, created time.Time) {
		t.Helper()
		if _, err := adapter.PutEphemeral(ctx, chatcore.EphemeralPost{
			ID: id, TenantID: tenant, ConversationID: "room-" + tenant, ThreadID: "root-post",
			RecipientHomeTenantID: tenant, RecipientSubjectID: "alice", Body: "private answer " + id,
			OnlyVisibleToYou: true, CreatedAt: created, ExpiresAt: created.Add(chatcore.EphemeralLifetime),
			DurableCopyConversationID: "alice-agent-dm", DurableCopyPostID: "copy-" + id, ThreadLink: "/chat/share/" + id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Two answers a day and a half old, one from an hour ago, and an old answer
	// in a tenant this process does not serve.
	put("tenant-a", "old-1", now.Add(-36*time.Hour))
	put("tenant-a", "old-2", now.Add(-36*time.Hour))
	put("tenant-a", "fresh", now.Add(-time.Hour))
	put("tenant-b", "old-other-tenant", now.Add(-36*time.Hour))
	remaining := func(tenant string) []string {
		t.Helper()
		var ids []string
		if err := store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			rows, err := tx.Query(ctx, `SELECT id FROM chat_ephemeral_post WHERE tenant_id=$1 ORDER BY id`, tenant)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					return err
				}
				ids = append(ids, id)
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	if got := remaining("tenant-a"); len(got) != 3 {
		t.Fatalf("seeded private answers = %v", got)
	}

	workload := agentEphemeralPruneWorkload(store, []string{"tenant-a"}, func() time.Time { return now }, &recordingLogger{})
	if workload == nil || workload.Name != "chat-ephemeral-prune" {
		t.Fatalf("served sweep = %+v", workload)
	}
	runCtx, stop := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() { finished <- workload.Run(runCtx) }()
	// The first sweep runs as soon as the workload starts.
	deadline := time.Now().Add(20 * time.Second)
	for len(remaining("tenant-a")) != 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	if err := <-finished; err != nil {
		t.Fatalf("sweep ended with %v", err)
	}
	if got := remaining("tenant-a"); len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("private answers left after the sweep = %v, want only the unexpired one", got)
	}
	if got := remaining("tenant-b"); len(got) != 1 {
		t.Fatalf("the sweep touched a tenant it was not given: %v", got)
	}
}

type agentEphemeralPrunerFake struct {
	batches []int64
	err     error
	calls   []string
}

func (f *agentEphemeralPrunerFake) PruneExpiredEphemeral(_ context.Context, tenant string, _ time.Time, limit int) (int64, error) {
	f.calls = append(f.calls, tenant)
	if limit != agentEphemeralPruneBatch {
		return 0, errors.New("unexpected batch size")
	}
	if len(f.batches) == 0 {
		return 0, f.err
	}
	batch := f.batches[0]
	f.batches = f.batches[1:]
	return batch, nil
}

func TestTodo_AGENTP_011_EphemeralPruneIsBounded(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	// A backlog is drained batch by batch until one comes back short.
	backlog := &agentEphemeralPrunerFake{batches: []int64{agentEphemeralPruneBatch, agentEphemeralPruneBatch, 7}}
	removed, err := pruneExpiredAgentEphemeralPosts(context.Background(), backlog, "tenant-a", now)
	if err != nil || removed != 2*agentEphemeralPruneBatch+7 || len(backlog.calls) != 3 {
		t.Fatalf("backlog removed=%d calls=%d err=%v", removed, len(backlog.calls), err)
	}
	// One tenant never takes more than its share of a sweep.
	endless := &agentEphemeralPrunerFake{}
	for range agentEphemeralPruneRounds + 5 {
		endless.batches = append(endless.batches, agentEphemeralPruneBatch)
	}
	if removed, err := pruneExpiredAgentEphemeralPosts(context.Background(), endless, "tenant-a", now); err != nil || removed != agentEphemeralPruneRounds*agentEphemeralPruneBatch || len(endless.calls) != agentEphemeralPruneRounds {
		t.Fatalf("endless backlog removed=%d calls=%d err=%v", removed, len(endless.calls), err)
	}
	failing := &agentEphemeralPrunerFake{err: errors.New("database unavailable")}
	if _, err := pruneExpiredAgentEphemeralPosts(context.Background(), failing, "tenant-a", now); err == nil {
		t.Fatal("a failed deletion was reported as a success")
	}
	// No store, no tenant or an invalid tenant: there is nothing to schedule.
	clock := func() time.Time { return now }
	if agentEphemeralPruneWorkload(nil, []string{"tenant-a"}, clock, nil) != nil ||
		agentEphemeralPruneWorkloadFor(backlog, nil, clock, nil, time.Minute) != nil ||
		agentEphemeralPruneWorkloadFor(backlog, []string{" tenant-a"}, clock, nil, time.Minute) != nil ||
		agentEphemeralPruneWorkloadFor(backlog, []string{"tenant-a"}, nil, nil, time.Minute) != nil {
		t.Fatal("a sweep was scheduled without a store, a tenant or a clock")
	}
	// A failing tenant is logged and the next tenant is still swept.
	logger := &recordingLogger{}
	sweep := agentEphemeralPruneWorkloadFor(failing, []string{"tenant-a", "tenant-b", "tenant-a"}, clock, logger, time.Hour)
	if sweep == nil {
		t.Fatal("no sweep for two tenants")
	}
	failing.calls = nil
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- sweep.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		logger.mu.Lock()
		logged := len(logger.events)
		logger.mu.Unlock()
		if logged >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-finished; err != nil {
		t.Fatalf("sweep ended with %v", err)
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if len(logger.events) != 2 || logger.events[0] != "hcmnext.chat_ephemeral_prune_failed" {
		t.Fatalf("sweep log = %v, want one failure per served tenant", logger.events)
	}
}
