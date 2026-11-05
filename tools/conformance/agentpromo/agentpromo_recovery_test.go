package agentpromo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// TestTodo_AGENT2_024_Recovery proves the promotion journey survives a
// worker killed while it waits: the wait resumes exactly once, duplicate
// signals collapse, stale wakes are ignored and retried submissions run once.
func TestTodo_AGENT2_024_Recovery(t *testing.T) {
	ctx := context.Background()

	t.Run("worker killed while waiting resumes once", func(t *testing.T) {
		promoRunstateResumeOnce(t, promoTask, promoNow)
	})

	t.Run("duplicate workflow signals resume once", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.runReadPhase()
		world.runDraftPhase()
		world.runApprovalPhase()
		world.runSubmitPhase()
		task := world.get()
		condition := *task.Plan.Steps[task.CurrentStep].Wait
		if _, err := world.runtime.ParkStep(ctx, world.taskID, task.Version, condition, world.beat()); err != nil {
			t.Fatal(err)
		}
		event := agentrun.WakeEvent{ID: "wake-signal-1", Kind: agentrun.WakeSignal, Key: promoSignalKey, Correlation: promoCorrelation, OccurredAt: world.beat()}
		first, err := world.runtime.WakeWithRecheck(ctx, world.taskID, event, promoWakeRechecker{world: world})
		if err != nil || !first.Accepted {
			t.Fatalf("first wake=%+v err=%v", first, err)
		}
		second, err := world.runtime.WakeWithRecheck(ctx, world.taskID, event, promoWakeRechecker{world: world})
		if err != nil || !second.Duplicate {
			t.Fatalf("second wake=%+v err=%v, want the dedupe inbox to collapse it", second, err)
		}
		completed, err := world.runtime.CompleteWait(ctx, world.taskID, second.Task.Version, agentrun.StepResult{Ref: "signal:" + promoCorrelation}, world.beat())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := world.runtime.CompleteWait(ctx, world.taskID, completed.Version, agentrun.StepResult{Ref: "signal:" + promoCorrelation}, world.beat()); err == nil {
			t.Fatal("the resumed wait completed twice")
		}
	})

	t.Run("stale and foreign wakes are ignored", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.runReadPhase()
		world.runDraftPhase()
		world.runApprovalPhase()
		world.runSubmitPhase()
		task := world.get()
		condition := *task.Plan.Steps[task.CurrentStep].Wait
		parked, err := world.runtime.ParkStep(ctx, world.taskID, task.Version, condition, world.beat())
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := world.runtime.Wake(ctx, world.taskID, agentrun.WakeEvent{ID: "wake-foreign", Kind: agentrun.WakeSignal, Key: "another-batch", OccurredAt: world.beat()})
		if err != nil || !foreign.Ignored {
			t.Fatalf("foreign wake=%+v err=%v, want it ignored", foreign, err)
		}
		if still := world.get(); still.State != agentrun.StateWaiting || still.Version != parked.Version {
			t.Fatalf("foreign wake moved the task: %+v", still)
		}
		late, err := world.runtime.Wake(ctx, world.taskID, agentrun.WakeEvent{ID: "wake-late", Kind: agentrun.WakeSignal, Key: promoSignalKey, Correlation: promoCorrelation, OccurredAt: parked.ExpiresAt.Add(time.Minute)})
		if err != nil || !late.Ignored || late.Task.State != agentrun.StateExpired {
			t.Fatalf("late wake=%+v err=%v, want the task expired and the wake ignored", late, err)
		}
	})

	t.Run("submit retries submit once", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.runReadPhase()
		world.runDraftPhase()
		world.runApprovalPhase()
		world.runSubmitPhase()
		if len(world.journal.Intents) != 2 {
			t.Fatalf("intents=%+v, want the two submitted promotions", world.journal.Intents)
		}
		replay, err := world.approvals.Submit(ctx, world.batch.ID, promoManager, world.batch.Digest, "promo-submit-1", world.revalidator(), world.submitter())
		if err != nil || !replay.Replayed {
			t.Fatalf("replay=%+v err=%v, want the replayed receipt", replay, err)
		}
		if len(world.journal.Intents) != 2 {
			t.Fatalf("replay submitted again: %+v", world.journal.Intents)
		}
		if _, err := world.approvals.Submit(ctx, world.batch.ID, promoManager, world.batch.Digest, "promo-submit-2", world.revalidator(), world.submitter()); !errors.Is(err, agentapproval.ErrIdempotencyConflict) {
			t.Fatalf("second key=%v, want ErrIdempotencyConflict", err)
		}
	})

	t.Run("lost report fails revalidation before submission", func(t *testing.T) {
		world := newPromoWorld(t, promoTask, promoDirectory(t))
		world.runReadPhase()
		world.runDraftPhase()
		batch := world.createBatch("approval-stale-1")
		world.approveBatch(batch)
		world.directory = promoDirectoryNoorMoved(t)
		world.owner.directory = world.directory
		validation, err := world.revalidator().Revalidate(ctx, batch)
		if err != nil {
			t.Fatal(err)
		}
		if validation.Fresh || !validation.GrantValid || !validation.AuthorityValid {
			t.Fatalf("revalidation=%+v, want only Fresh refused for the moved report", validation)
		}
		if _, err := world.approvals.Submit(ctx, batch.ID, promoManager, batch.Digest, "promo-submit-stale", world.revalidator(), world.submitter()); !errors.Is(err, agentapproval.ErrRevalidation) {
			t.Fatalf("stale submit=%v, want ErrRevalidation", err)
		}
		if len(world.journal.Intents) != 0 {
			t.Fatalf("stale batch submitted: %+v", world.journal.Intents)
		}
	})
}
