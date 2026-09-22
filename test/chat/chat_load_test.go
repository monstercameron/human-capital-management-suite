package chat_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/data/operationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// This local profile compares identical durable operation-store work before and
// during chat load. It is a regression probe, not the signed pilot SLO gate.
func TestTodo_CHAT_052_MixedLoad(t *testing.T) {
	core := pgtest.New(t)
	tenant := uuid.New()
	core.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'chat-load-core', 'cell-chat-load', 'Chat Load', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant)
	connections := make([]*operationstore.Store, 4)
	for i := range connections {
		connections[i] = operationstore.New(core.NewConn(t))
	}
	chat := newFixture(t)
	p := principal("company-load", "load-user")
	conversation, err := chat.service.CreateConversation(t.Context(), chatcore.CreateConversationRequest{Principal: p, TenantID: p.TenantID, Kind: chatcore.PublicChannel, Name: "mixed-load"})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	baseline := runCoreOperations(t, connections, tenant, "baseline")

	admission, err := chatadmission.New(chatadmission.Config{TenantConcurrent: 4, ConversationConcurrent: 4, SendConcurrent: 4, WatchConcurrent: 4})
	if err != nil {
		t.Fatalf("chat admission: %v", err)
	}
	start := make(chan struct{})
	var chatWG sync.WaitGroup
	chatErrors := make(chan error, 64)
	chatLatency := make(chan time.Duration, 64)
	for worker := 0; worker < 4; worker++ {
		chatWG.Add(1)
		go func(worker int) {
			defer chatWG.Done()
			<-start
			for i := 0; i < 16; i++ {
				begin := time.Now()
				lease, err := admission.Acquire(t.Context(), chatadmission.Request{TenantID: p.TenantID, ConversationID: conversation.ID, Lane: chatadmission.LaneSend})
				if err != nil {
					chatErrors <- err
					continue
				}
				_, err = chat.service.SendPost(t.Context(), chatcore.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: conversation.ID, Body: "mixed", IdempotencyKey: fmt.Sprintf("mixed-%d-%d", worker, i)})
				_ = lease.Release()
				if err != nil {
					chatErrors <- err
				} else {
					chatLatency <- time.Since(begin)
				}
			}
		}(worker)
	}
	close(start)
	mixed := runCoreOperations(t, connections, tenant, "mixed")
	chatWG.Wait()
	close(chatErrors)
	close(chatLatency)
	for err := range chatErrors {
		if err != chatadmission.ErrOverloaded {
			t.Fatalf("chat send: %v", err)
		}
	}
	var chatSamples []time.Duration
	for d := range chatLatency {
		chatSamples = append(chatSamples, d)
	}
	if len(chatSamples) == 0 {
		t.Fatal("mixed load admitted no chat posts")
	}
	posts, err := chat.service.ListPosts(t.Context(), chatcore.ListPostsRequest{Principal: p, TenantID: p.TenantID, ConversationID: conversation.ID, Page: chatcore.Page{PageSize: 100}})
	if err != nil || len(posts.Posts) != len(chatSamples) {
		t.Fatalf("durable chat posts=%d, admitted=%d, error=%v", len(posts.Posts), len(chatSamples), err)
	}
	leases := make([]*chatadmission.Lease, 4)
	request := chatadmission.Request{TenantID: p.TenantID, ConversationID: conversation.ID, Lane: chatadmission.LaneSend}
	for i := range leases {
		leases[i], err = admission.Acquire(t.Context(), request)
		if err != nil {
			t.Fatalf("fill chat admission lane: %v", err)
		}
	}
	if _, err := admission.Acquire(t.Context(), request); !errors.Is(err, chatadmission.ErrOverloaded) {
		t.Fatalf("saturated chat lane error = %v, want overload", err)
	}
	saturated := runCoreOperations(t, connections, tenant, "saturated")
	for _, lease := range leases {
		if err := lease.Release(); err != nil {
			t.Fatalf("release chat admission lease: %v", err)
		}
	}
	t.Logf("local load profile: core operation Put+Get baseline p95=%s p99=%s; mixed p95=%s p99=%s; chat-lane saturated core p95=%s p99=%s; chat SendPost p95=%s p99=%s, admitted=%d; overload=1", percentile(baseline, 95), percentile(baseline, 99), percentile(mixed, 95), percentile(mixed, 99), percentile(saturated, 95), percentile(saturated, 99), percentile(chatSamples, 95), percentile(chatSamples, 99), len(chatSamples))
}

func runCoreOperations(t *testing.T, stores []*operationstore.Store, tenant uuid.UUID, phase string) []time.Duration {
	t.Helper()
	const perWorker = 8
	results := make(chan time.Duration, len(stores)*perWorker)
	errs := make(chan error, len(stores)*perWorker)
	var wg sync.WaitGroup
	for worker, store := range stores {
		wg.Add(1)
		go func(worker int, store *operationstore.Store) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("chat-load-%s-%d-%d", phase, worker, i)
				now := time.Now().UTC()
				begin := time.Now()
				err := store.Put(context.Background(), operationstore.Record{OperationID: id, TenantID: tenant.String(), Owner: "principal:load", RequestType: "promotion.propose", State: operationstore.StatePending, MetadataRef: "load:" + id, CreatedAt: now, UpdatedAt: now})
				if err == nil {
					var got operationstore.Record
					got, err = store.Get(context.Background(), tenant.String(), id)
					if err == nil && got.OperationID != id {
						err = fmt.Errorf("operation id = %q, want %q", got.OperationID, id)
					}
				}
				if err != nil {
					errs <- err
				} else {
					results <- time.Since(begin)
				}
			}
		}(worker, store)
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Errorf("%s core operation: %v", phase, err)
	}
	var samples []time.Duration
	for d := range results {
		samples = append(samples, d)
	}
	if len(samples) != len(stores)*perWorker {
		t.Fatalf("%s core samples=%d, want %d", phase, len(samples), len(stores)*perWorker)
	}
	return samples
}

func percentile(samples []time.Duration, pct int) time.Duration {
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	index := (len(samples)*pct+99)/100 - 1
	return samples[index]
}
