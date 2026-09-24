package schedulingstore_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/schedulingstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_REV_045_03_Race_StoreInstances(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	tenantKey := tenantID.String()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, tenantKey, tenantKey)
	tenant := values.TenantId(tenantKey)
	first := schedulingstore.New(db.Conn)
	second := schedulingstore.New(db.NewConn(t))
	const originalDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const changedDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	offer, err := first.CreateShiftOffer(context.Background(), tenant, "revision-1", 1, schedopt.ShiftOffer{
		ID: "offer-race", Kind: schedopt.ShiftOfferOpenClaim, AssignmentID: "assignment-1",
		PublicationDigest: originalDigest, FencingToken: 1, State: schedopt.ShiftOfferActive, Reason: "pickup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if offer.FencingToken != 2 {
		t.Fatalf("durable offer fence = %d, want 2", offer.FencingToken)
	}
	loaded, err := second.LoadShiftOffer(context.Background(), tenant, "revision-1", offer.ID)
	if err != nil || loaded.State != schedopt.ShiftOfferActive || loaded.FencingToken != offer.FencingToken || loaded.PublicationDigest != originalDigest {
		t.Fatalf("second store instance rehydrated offer = %+v, err=%v", loaded, err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	type result struct {
		fence uint64
		err   error
	}
	results := make(chan result, 2)
	for _, store := range []*schedulingstore.Store{first, second} {
		wg.Add(1)
		go func(store *schedulingstore.Store) {
			defer wg.Done()
			<-start
			fence, err := store.ClaimShiftOffer(context.Background(), tenant, "revision-1", offer.ID, offer.FencingToken, changedDigest)
			results <- result{fence: fence, err: err}
		}(store)
	}
	close(start)
	wg.Wait()
	close(results)
	wins, losses := 0, 0
	for got := range results {
		if got.err == nil {
			wins++
			if got.fence != 3 {
				t.Errorf("committed fence = %d, want 3", got.fence)
			}
			continue
		}
		if !errors.Is(got.err, schedulingstore.ErrVersionConflict) {
			t.Errorf("loser error = %v, want durable CAS conflict", got.err)
			continue
		}
		losses++
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("separate store instances must produce one durable winner and one typed loser; wins=%d losses=%d", wins, losses)
	}
	currentFence, err := first.LoadShiftScheduleFence(context.Background(), tenant, "revision-1", changedDigest)
	if err != nil || currentFence != 3 {
		t.Fatalf("current published digest fence = %d, err=%v; want 3", currentFence, err)
	}
	closed, err := second.LoadShiftOffer(context.Background(), tenant, "revision-1", offer.ID)
	if err != nil || closed.State != schedopt.ShiftOfferClaimed {
		t.Fatalf("persisted offer state = %s, err=%v; want CLAIMED", closed.State, err)
	}
}
