package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
)

// TestTodo_JOB_003_Race proves one holder wins each fencing generation: 16
// concurrent acquirers elect exactly one lease, concurrent renewals converge
// on its token, and after a lapse the next generation fences the old holder
// out on every goroutine.
func TestTodo_JOB_003_Race(t *testing.T) {
	t.Parallel()
	fx := newJob003Fixture(t, "job003-race")

	now := fixedInstant
	mgr, err := jobs.NewLeaseManager(5*time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLeaseManager: %v", err)
	}
	const racers = 16
	var wg sync.WaitGroup
	leases := make([]jobs.Lease, racers)
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			leases[i], errs[i] = mgr.Acquire(fx.tenant, fx.partition.PartitionID, fmt.Sprintf("racer-%d", i))
		}(i)
	}
	wg.Wait()
	winners, held := 0, 0
	var winner jobs.Lease
	for i := range errs {
		switch {
		case errs[i] == nil:
			winners++
			winner = leases[i]
		case errors.Is(errs[i], jobs.ErrLeaseHeld):
			held++
		default:
			t.Fatalf("racer %d err = %v, want nil or ErrLeaseHeld", i, errs[i])
		}
	}
	if winners != 1 || held != racers-1 {
		t.Fatalf("winners = %d held = %d, want 1 winner and %d held", winners, held, racers-1)
	}
	if winner.Token != 1 {
		t.Fatalf("winner token = %d, want 1", winner.Token)
	}

	// Concurrent renewals of the winning lease all succeed on its token.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			renewed, err := mgr.Renew(winner)
			if err != nil {
				t.Errorf("renew %d: %v", i, err)
				return
			}
			if renewed.Token != winner.Token {
				t.Errorf("renew %d token = %d, want %d", i, renewed.Token, winner.Token)
			}
		}(i)
	}
	wg.Wait()

	// After the lapse the next generation has exactly one winner and the
	// fenced-out holder is refused on every goroutine.
	now = now.Add(6 * time.Minute)
	second := make([]jobs.Lease, racers)
	secondErrs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			second[i], secondErrs[i] = mgr.Acquire(fx.tenant, fx.partition.PartitionID, fmt.Sprintf("racer-%d", i))
		}(i)
	}
	wg.Wait()
	secondWinners := 0
	var winner2 jobs.Lease
	for i := range secondErrs {
		if secondErrs[i] == nil {
			secondWinners++
			winner2 = second[i]
		} else if !errors.Is(secondErrs[i], jobs.ErrLeaseHeld) {
			t.Fatalf("second generation racer %d err = %v", i, secondErrs[i])
		}
	}
	if secondWinners != 1 {
		t.Fatalf("second generation winners = %d, want 1", secondWinners)
	}
	if winner2.Token != winner.Token+1 {
		t.Fatalf("second generation token = %d, want %d", winner2.Token, winner.Token+1)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mgr.Check(winner); !errors.Is(err, jobs.ErrStaleFence) {
				t.Errorf("fenced-out Check err = %v, want ErrStaleFence", err)
			}
		}()
	}
	wg.Wait()
}

// TestTodo_JOB_003_Fault proves lease expiry is fail-closed and crash resume
// is durable: renewal or checkpoint past expiry is refused with zero writes,
// and a partition with no checkpoints replays from zero while a duplicate
// checkpoint sequence keeps its ErrDuplicate refusal through the fence.
func TestTodo_JOB_003_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newJob003Fixture(t, "job003-fault")

	now := fixedInstant
	mgr, err := jobs.NewLeaseManager(5*time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLeaseManager: %v", err)
	}
	lease, err := mgr.Acquire(fx.tenant, fx.partition.PartitionID, "worker-a")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	fencedCheckpoint(t, ctx, fx, mgr, lease, 1)

	// The exact expiry instant is already expired: fail-closed boundary.
	now = fixedInstant.Add(5 * time.Minute)
	if err := mgr.Check(lease); !errors.Is(err, jobs.ErrLeaseExpired) {
		t.Fatalf("boundary Check err = %v, want ErrLeaseExpired", err)
	}
	if _, err := mgr.Renew(lease); !errors.Is(err, jobs.ErrLeaseExpired) {
		t.Fatalf("post-expiry Renew err = %v, want ErrLeaseExpired", err)
	}
	before := checkpointCount(t, ctx, fx)
	err = inTenantTxErr(fx.conn, fx.tenant, func(tx dbport.Tx) error {
		_, err := mgr.Checkpoint(ctx, tx, lease, jobs.JobCheckpoint{
			TenantID:         fx.tenant,
			PartitionID:      fx.partition.PartitionID,
			Sequence:         2,
			StateDigest:      digestOf("expired-write"),
			PartitionVersion: fx.partition.Version,
			TakenAt:          now,
		})
		return err
	})
	if !errors.Is(err, jobs.ErrLeaseExpired) {
		t.Fatalf("expired checkpoint err = %v, want ErrLeaseExpired", err)
	}
	if after := checkpointCount(t, ctx, fx); after != before {
		t.Fatalf("expired write persisted: checkpoints %d -> %d", before, after)
	}

	// A live renewal extends the fence: the original deadline passes with
	// the lease still current.
	now = fixedInstant
	mgr2, err := jobs.NewLeaseManager(5*time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLeaseManager: %v", err)
	}
	live, err := mgr2.Acquire(fx.tenant, fx.partition.PartitionID, "worker-b")
	if err != nil {
		t.Fatalf("Acquire worker-b: %v", err)
	}
	now = fixedInstant.Add(4 * time.Minute)
	live, err = mgr2.Renew(live)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	now = fixedInstant.Add(8 * time.Minute)
	if err := mgr2.Check(live); err != nil {
		t.Fatalf("renewed Check at +8m err = %v, want nil", err)
	}

	// A duplicate checkpoint sequence keeps its store refusal through the
	// fence rather than surfacing as a fence error.
	now = fixedInstant
	mgr3, err := jobs.NewLeaseManager(time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLeaseManager: %v", err)
	}
	fresh, err := mgr3.Acquire(fx.tenant, fx.partition.PartitionID, "worker-c")
	if err != nil {
		t.Fatalf("Acquire worker-c: %v", err)
	}
	err = inTenantTxErr(fx.conn, fx.tenant, func(tx dbport.Tx) error {
		_, err := mgr3.Checkpoint(ctx, tx, fresh, jobs.JobCheckpoint{
			TenantID:         fx.tenant,
			PartitionID:      fx.partition.PartitionID,
			Sequence:         1,
			StateDigest:      digestOf("duplicate-seq"),
			PartitionVersion: fx.partition.Version,
			TakenAt:          now,
		})
		return err
	})
	if !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("duplicate sequence err = %v, want ErrDuplicate", err)
	}

	// A partition with no checkpoints resumes from zero: full replay.
	emptyPartition := uuid.New()
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID:     fx.tenant,
			PartitionID:  emptyPartition,
			RunID:        fx.run.RunID,
			PartitionKey: "part-empty",
			CreatedAt:    fixedInstant,
		})
		return err
	})
	var watermark uint64
	var ok bool
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		watermark, ok, err = jobs.LoadWatermark(ctx, tx, fx.tenant, emptyPartition)
		return err
	})
	if ok || watermark != 0 {
		t.Fatalf("empty LoadWatermark = (%d, %v), want (0, false)", watermark, ok)
	}
	var items []jobs.WorkItem
	for i := uint64(0); i < 3; i++ {
		items = append(items, jobs.WorkItem{Index: i, Key: fmt.Sprintf("cold-%d", i)})
	}
	plan, err := jobs.PlanResume(fx.tenant, emptyPartition, items, watermark, ok)
	if err != nil {
		t.Fatalf("PlanResume cold: %v", err)
	}
	if len(plan.Pending) != 3 {
		t.Fatalf("cold resume pending = %d, want full replay of 3", len(plan.Pending))
	}
}

// TestTodo_JOB_003_Mutation kills near-miss mutants: token, holder and
// partition off-by-ones are fenced, watermark arithmetic is exact, and
// malformed leases or items are refused before any write.
func TestTodo_JOB_003_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newJob003Fixture(t, "job003-mutation")

	now := fixedInstant
	mgr, err := jobs.NewLeaseManager(5*time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLeaseManager: %v", err)
	}
	lease, err := mgr.Acquire(fx.tenant, fx.partition.PartitionID, "worker-a")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	stale := lease
	stale.Token--
	if err := mgr.Check(stale); !errors.Is(err, jobs.ErrStaleFence) {
		t.Fatalf("token-1 Check err = %v, want ErrStaleFence", err)
	}
	wrongHolder := lease
	wrongHolder.Holder = "worker-b"
	if err := mgr.Check(wrongHolder); !errors.Is(err, jobs.ErrStaleFence) {
		t.Fatalf("wrong-holder Check err = %v, want ErrStaleFence", err)
	}
	wrongPartition := lease
	wrongPartition.PartitionID = uuid.New()
	if err := mgr.Check(wrongPartition); !errors.Is(err, jobs.ErrStaleFence) {
		t.Fatalf("wrong-partition Check err = %v, want ErrStaleFence", err)
	}
	if _, err := mgr.Acquire(fx.tenant, fx.partition.PartitionID, "  "); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("blank holder Acquire err = %v, want ErrInvalid", err)
	}
	if _, err := mgr.Acquire(uuid.Nil, fx.partition.PartitionID, "worker-a"); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("nil tenant Acquire err = %v, want ErrInvalid", err)
	}
	if _, err := jobs.NewLeaseManager(0, nil); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("zero TTL NewLeaseManager err = %v, want ErrInvalid", err)
	}

	var items []jobs.WorkItem
	for i := uint64(0); i < 6; i++ {
		items = append(items, jobs.WorkItem{Index: i, Key: fmt.Sprintf("m-%d", i)})
	}
	plusOne, err := jobs.PlanResume(fx.tenant, fx.partition.PartitionID, items, 4, true)
	if err != nil {
		t.Fatalf("PlanResume +1: %v", err)
	}
	if len(plusOne.Pending) != 2 || plusOne.Pending[0].Index != 4 {
		t.Fatalf("watermark 4 pending starts at %+v, want index 4 (no skip)", plusOne.Pending)
	}
	minusOne, err := jobs.PlanResume(fx.tenant, fx.partition.PartitionID, items, 2, true)
	if err != nil {
		t.Fatalf("PlanResume -1: %v", err)
	}
	if len(minusOne.Pending) != 4 || minusOne.Pending[0].Index != 2 {
		t.Fatalf("watermark 2 pending starts at %+v, want index 2 (no duplicate)", minusOne.Pending)
	}
	duped := append(append([]jobs.WorkItem{}, items...), jobs.WorkItem{Index: 2, Key: "m-dup"})
	if _, err := jobs.PlanResume(fx.tenant, fx.partition.PartitionID, duped, 0, false); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("duplicate indexes err = %v, want ErrInvalid", err)
	}
	plan, err := jobs.PlanResume(fx.tenant, fx.partition.PartitionID, items, 0, false)
	if err != nil {
		t.Fatalf("PlanResume: %v", err)
	}
	if _, err := jobs.ExecuteFromWatermark(plan, nil); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("nil apply err = %v, want ErrInvalid", err)
	}

	before := checkpointCount(t, ctx, fx)
	err = inTenantTxErr(fx.conn, fx.tenant, func(tx dbport.Tx) error {
		_, err := mgr.Checkpoint(ctx, tx, lease, jobs.JobCheckpoint{
			TenantID:         fx.tenant,
			PartitionID:      fx.partition.PartitionID,
			Sequence:         0,
			StateDigest:      digestOf("seq-zero"),
			PartitionVersion: fx.partition.Version,
			TakenAt:          now,
		})
		return err
	})
	if !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("sequence-0 fence err = %v, want ErrInvalid", err)
	}
	if after := checkpointCount(t, ctx, fx); after != before {
		t.Fatalf("invalid checkpoint persisted: %d -> %d", before, after)
	}
}
