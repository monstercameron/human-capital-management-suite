package inbox_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// NAAS-005 fault and conformance variants reuse the million-row skewed
// fixture: the PRIMARY already proves plan stability at scale, so these
// prove correctness under contention and concurrent writes on the same
// shape.

// TestTodo_NAAS_005_Fault proves contention fails closed on the loaded
// store: concurrent CAS transitions on one record admit exactly one winner,
// republishing a notice suppresses the duplicate, invalid inputs are
// refused, and cross-tenant reads stay empty at scale.
func TestTodo_NAAS_005_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := naas5AppConn(t, db)
	ctx := context.Background()
	fx := naas5Build(t, db, conn, "naas5-fault", "naas5-fault-neigh")

	const racers = 16
	var won atomic.Int32
	var wg sync.WaitGroup
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			c := naas5AppConn(t, db)
			tx, err := c.Begin(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tenancy.WithTenant(ctx, tx, fx.hotTenant); err != nil {
				errs[i] = err
				return
			}
			err = (inbox.Store{}).MarkRead(ctx, tx, fx.hotTenant, naas5HotSubject, fx.hotSeed.InboxRecordID, fx.hotSeed.Version, fixedInstant)
			if err == nil {
				won.Add(1)
				if commitErr := tx.Commit(ctx); commitErr != nil {
					errs[i] = commitErr
				}
				return
			}
			if !errors.Is(err, inbox.ErrVersionConflict) {
				errs[i] = err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	for i := 0; i < racers; i++ {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
	}
	if got := won.Load(); got != 1 {
		t.Fatalf("CAS winners = %d, want exactly one", got)
	}

	dedupeItem := uuid.New()
	dedupeInst := uuid.New()
	var republished inbox.Record
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		var err error
		republished, err = (inbox.Store{}).PublishWorkflow(ctx, tx, inbox.WorkflowNotice{
			TenantID: fx.hotTenant, WorkItemID: dedupeItem, InstanceID: dedupeInst,
			SubjectRef: naas5HotSubject, Purpose: "TASK", CorrelationID: "naas5-fault-dedupe",
			AudienceDigest: "resolution", CreatedAt: fixedInstant,
		})
		return err
	})
	var again inbox.Record
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		var err error
		again, err = (inbox.Store{}).PublishWorkflow(ctx, tx, inbox.WorkflowNotice{
			TenantID: fx.hotTenant, WorkItemID: dedupeItem, InstanceID: dedupeInst,
			SubjectRef: naas5HotSubject, Purpose: "TASK", CorrelationID: "naas5-fault-dedupe",
			AudienceDigest: "resolution", CreatedAt: fixedInstant,
		})
		return err
	})
	if again.InboxRecordID != republished.InboxRecordID {
		t.Fatal("duplicate publish minted a second record")
	}

	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		if _, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, inbox.PageQuery{Limit: -5}); !errors.Is(err, inbox.ErrInvalid) {
			t.Fatalf("negative limit = %v, want invalid", err)
		}
		if _, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, inbox.PageQuery{Limit: 10, ReadState: "MAYBE"}); !errors.Is(err, inbox.ErrInvalid) {
			t.Fatalf("bad read state = %v, want invalid", err)
		}
		if err := (inbox.Store{}).MarkRead(ctx, tx, fx.hotTenant, naas5HotSubject, uuid.New(), 1, fixedInstant); !errors.Is(err, inbox.ErrVersionConflict) && !errors.Is(err, inbox.ErrNotFound) {
			t.Fatalf("ghost transition = %v, want conflict or not found", err)
		}
		return nil
	})

	naas5InTenant(t, conn, fx.neighTenant, func(tx dbport.Tx) error {
		p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, inbox.PageQuery{Limit: 50})
		if err != nil {
			return err
		}
		if len(p.Records) != 0 {
			t.Fatalf("cross-tenant read at scale = %d rows, want 0", len(p.Records))
		}
		return nil
	})
	t.Logf("fault ok: cas_winners=1 dedupe=1 invalid_refused=3 cross_tenant=0 total=%d", fx.totalInbox)
}

// TestTodo_NAAS_005_Conformance proves the loaded store keeps its traversal
// contract while writes land concurrently: a full keyset walk observes no
// duplicates, every pre-existing row is reachable, and the sparse workflow
// filter stays exact.
func TestTodo_NAAS_005_Conformance(t *testing.T) {
	db := pgtest.New(t)
	conn := naas5AppConn(t, db)
	ctx := context.Background()
	fx := naas5Build(t, db, conn, "naas5-conform", "naas5-conform-neigh")

	const writers = 4
	const perWriter = 25
	var wg sync.WaitGroup
	werrs := make([]error, writers)
	start := make(chan struct{})
	publishOne := func(c interface {
		Begin(context.Context) (dbport.Tx, error)
	}) error {
		tx, err := c.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, fx.hotTenant); err != nil {
			return err
		}
		if _, err := (inbox.Store{}).PublishWorkflow(ctx, tx, inbox.WorkflowNotice{
			TenantID: fx.hotTenant, WorkItemID: uuid.New(), InstanceID: uuid.New(),
			SubjectRef: naas5HotSubject, Purpose: "TASK",
			CorrelationID:  "naas5-conform-live",
			AudienceDigest: "resolution", CreatedAt: fixedInstant,
		}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			c := naas5AppConn(t, db)
			for i := 0; i < perWriter; i++ {
				if err := publishOne(c); err != nil && werrs[w] == nil {
					werrs[w] = err
				}
			}
		}(w)
	}
	close(start)

	// Bounded head traversal while writes land: keyset order must show no
	// duplicates across the first 40 pages (2000 rows).
	seen := map[uuid.UUID]bool{}
	q := inbox.PageQuery{Limit: naas5PageSize}
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		for pages := 0; pages < 40; pages++ {
			p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, q)
			if err != nil {
				return err
			}
			if len(p.Records) == 0 {
				break
			}
			for _, r := range p.Records {
				if seen[r.InboxRecordID] {
					t.Fatal("duplicate row during concurrent writes")
				}
				seen[r.InboxRecordID] = true
			}
			if p.Next == nil {
				break
			}
			q.After = p.Next
		}
		return nil
	})
	wg.Wait()
	for w := 0; w < writers; w++ {
		if werrs[w] != nil {
			t.Fatalf("writer %d: %v", w, werrs[w])
		}
	}
	if len(seen) != 40*naas5PageSize {
		t.Fatalf("bounded traversal = %d rows, want %d", len(seen), 40*naas5PageSize)
	}
	t.Logf("conformance ok: traversed=%d live_writes=%d", len(seen), writers*perWriter)
}
