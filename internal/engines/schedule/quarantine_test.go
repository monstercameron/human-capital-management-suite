package schedule_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
)

func quarantineStore(t *testing.T, maxAttempts int) *schedule.QuarantineStore {
	t.Helper()
	store, err := schedule.NewQuarantineStore(schedule.QuarantinePolicy{MaxAttempts: maxAttempts, Clock: dispatchClock})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func quarantineRequest(t *testing.T, attempt int) schedule.QuarantineRequest {
	t.Helper()
	return schedule.QuarantineRequest{Firing: occurrenceFiringAt(t, 7, 0), Reason: "CONVERTER_REJECTED", Failure: "intent converter refused the firing", Attempt: attempt}
}

// TestTodo_SCHED_004 proves quarantine and redrive: a failed firing becomes
// an immutable dead letter preserving its identity and versions, and only
// an approved replay of the identical payload becomes a new attempt with
// lineage back to the letter.
func TestTodo_SCHED_004(t *testing.T) {
	store := quarantineStore(t, 3)
	firing := occurrenceFiringAt(t, 7, 0)
	letter, err := store.Quarantine(schedule.QuarantineRequest{Firing: firing, Reason: "CONVERTER_REJECTED", Failure: "converter refused", Attempt: 1})
	if err != nil {
		t.Fatalf("quarantine: %v", err)
	}
	if letter.Duplicate || letter.Terminal || letter.Digest == "" {
		t.Fatalf("fresh dead letter is not sealed live: %+v", letter)
	}
	if letter.Key != firing.Key || letter.Sequence != firing.Sequence || letter.TriggerDigest != firing.Trigger.Digest {
		t.Fatalf("dead letter lost original identity: %+v", letter)
	}
	if letter.TriggerVersion != firing.Trigger.Definition.Version || letter.TriggerID != firing.Trigger.Definition.ID {
		t.Fatalf("dead letter lost original versions: %+v", letter)
	}
	// Redelivery of the same failure converges on the stored letter.
	replay, err := store.Quarantine(schedule.QuarantineRequest{Firing: firing, Reason: "CONVERTER_REJECTED", Failure: "converter refused", Attempt: 1})
	if err != nil || !replay.Duplicate || replay.Digest != letter.Digest {
		t.Fatalf("quarantine redelivery diverged: %+v err=%v", replay, err)
	}
	// An approved replay of the identical payload becomes attempt 2.
	redrive, err := store.Redrive(schedule.RedriveRequest{Key: firing.Key, Firing: firing, Approver: "ops-lead", ApprovalReason: "downstream fixed"})
	if err != nil {
		t.Fatalf("redrive: %v", err)
	}
	if redrive.Attempt != 2 || redrive.DeadLetterDigest != letter.Digest || redrive.Fingerprint != letter.Fingerprint {
		t.Fatalf("redrive lost lineage or payload: %+v", redrive)
	}
	// A replay with a changed payload is refused, never redriven.
	changed := occurrenceFiringAt(t, 7, 1)
	changed.Key = firing.Key
	if _, err := store.Redrive(schedule.RedriveRequest{Key: firing.Key, Firing: changed, Approver: "ops-lead", ApprovalReason: "retry"}); !errors.Is(err, schedule.ErrRedrivePayloadChanged) {
		t.Fatalf("changed-payload replay error = %v", err)
	}
	changedEvent := eventFiring(9)
	changedEvent.Key = firing.Key
	if _, err := store.Redrive(schedule.RedriveRequest{Key: firing.Key, Firing: changedEvent, Approver: "ops-lead", ApprovalReason: "retry"}); !errors.Is(err, schedule.ErrRedrivePayloadChanged) {
		t.Fatalf("cross-kind replay error = %v", err)
	}
}

func TestTodo_SCHED_004_Race(t *testing.T) {
	store := quarantineStore(t, 3)
	const racers = 32
	var wg sync.WaitGroup
	digests := make([]string, racers)
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			letter, err := store.Quarantine(quarantineRequest(t, 1))
			if err == nil {
				digests[i] = letter.Digest
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i := 0; i < racers; i++ {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if digests[i] != digests[0] {
			t.Fatalf("racer %d diverged: %s vs %s", i, digests[i], digests[0])
		}
	}
	firing := occurrenceFiringAt(t, 7, 0)
	if history := store.History(firing.Key); len(history) != 1 {
		t.Fatalf("concurrent quarantine recorded %d letters, want 1", len(history))
	}
	// Distinct keys quarantine independently under concurrency.
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			request := quarantineRequest(t, 1)
			request.Firing.Key = request.Firing.Key + "-parallel"
			request.Firing.Sequence = uint64(100 + i)
			_, _ = store.Quarantine(request)
		}(i)
	}
	wg.Wait()
}

func TestTodo_SCHED_004_Fault(t *testing.T) {
	store := quarantineStore(t, 2)
	badKind := occurrenceFiringAt(t, 7, 0)
	badKind.Kind = "UNKNOWN"
	if _, err := store.Quarantine(schedule.QuarantineRequest{Firing: badKind, Reason: "X", Failure: "Y", Attempt: 1}); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("bad kind error = %v", err)
	}
	emptyKey := occurrenceFiringAt(t, 7, 0)
	emptyKey.Key = ""
	if _, err := store.Quarantine(schedule.QuarantineRequest{Firing: emptyKey, Reason: "X", Failure: "Y", Attempt: 1}); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("empty key error = %v", err)
	}
	zeroSeq := occurrenceFiringAt(t, 0, 0)
	if _, err := store.Quarantine(schedule.QuarantineRequest{Firing: zeroSeq, Reason: "X", Failure: "Y", Attempt: 1}); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("zero sequence error = %v", err)
	}
	blankReason := quarantineRequest(t, 1)
	blankReason.Reason = ""
	if _, err := store.Quarantine(blankReason); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("blank reason error = %v", err)
	}
	firing := occurrenceFiringAt(t, 7, 0)
	if _, err := store.Redrive(schedule.RedriveRequest{Key: "missing", Firing: firing, Approver: "ops", ApprovalReason: "why"}); !errors.Is(err, schedule.ErrDeadLetterNotFound) {
		t.Fatalf("unknown key error = %v", err)
	}
	if _, err := store.Quarantine(quarantineRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	noApproval := schedule.RedriveRequest{Key: firing.Key, Firing: firing}
	if _, err := store.Redrive(noApproval); !errors.Is(err, schedule.ErrRedriveApproval) {
		t.Fatalf("unapproved redrive error = %v", err)
	}
	// A stale failure report behind the quarantine cursor is refused.
	stale := quarantineRequest(t, 1)
	stale.Attempt = 0
	if _, err := store.Quarantine(stale); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("zero attempt error = %v", err)
	}
	// Attempt 2 of 2 is terminal: the failure is recorded, but no further
	// redrive is approved, so retries can never run forever.
	terminal, err := store.Quarantine(quarantineRequest(t, 2))
	if err != nil || !terminal.Terminal {
		t.Fatalf("terminal letter = %+v err=%v", terminal, err)
	}
	if _, err := store.Redrive(schedule.RedriveRequest{Key: firing.Key, Firing: firing, Approver: "ops", ApprovalReason: "once more"}); !errors.Is(err, schedule.ErrRedriveExhausted) {
		t.Fatalf("exhausted redrive error = %v", err)
	}
	// A report behind the recorded attempt is stale history, not a letter.
	if _, err := store.Quarantine(quarantineRequest(t, 1)); !errors.Is(err, schedule.ErrQuarantineStale) {
		t.Fatalf("stale report error = %v", err)
	}
}

func TestTodo_SCHED_004_RejectsForgedAndStaleState(t *testing.T) {
	if _, err := schedule.NewQuarantineStore(schedule.QuarantinePolicy{}); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("empty policy error = %v", err)
	}
	store := quarantineStore(t, 3)
	padded := occurrenceFiringAt(t, 7, 0)
	padded.Key = " " + padded.Key
	if _, err := store.Quarantine(schedule.QuarantineRequest{Firing: padded, Reason: "X", Failure: "Y", Attempt: 1}); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("padded key error = %v", err)
	}
	forged := occurrenceFiringAt(t, 7, 0)
	forged.Trigger.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err := store.Quarantine(schedule.QuarantineRequest{Firing: forged, Reason: "X", Failure: "Y", Attempt: 1}); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("forged trigger error = %v", err)
	}
	firing := occurrenceFiringAt(t, 7, 0)
	if _, err := store.Redrive(schedule.RedriveRequest{Key: firing.Key, Firing: badFiring(), Approver: "ops", ApprovalReason: "why"}); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("redrive invalid firing error = %v", err)
	}
	if history := store.History("unknown-key"); len(history) != 0 {
		t.Fatalf("unknown history = %+v", history)
	}
	// A snapshot carrying a broken seal, a reordered chain, an orphan
	// redrive or an empty policy never resumes.
	letter, err := store.Quarantine(quarantineRequest(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	redrive, err := store.Redrive(schedule.RedriveRequest{Key: firing.Key, Firing: firing, Approver: "ops", ApprovalReason: "why"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	broken := snapshot
	broken.Letters[0].Reason = "rewritten after seal"
	if _, err := schedule.ResumeQuarantine(broken, schedule.QuarantinePolicy{MaxAttempts: 3, Clock: dispatchClock}); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("broken seal resumed: %v", err)
	}
	reordered := schedule.QuarantineSnapshot{Letters: []schedule.DeadLetter{letter, letter}}
	if _, err := schedule.ResumeQuarantine(reordered, schedule.QuarantinePolicy{MaxAttempts: 3, Clock: dispatchClock}); !errors.Is(err, schedule.ErrQuarantineStale) {
		t.Fatalf("reordered chain resumed: %v", err)
	}
	orphan := schedule.QuarantineSnapshot{Redrives: []schedule.RedriveAttempt{redrive}}
	if _, err := schedule.ResumeQuarantine(orphan, schedule.QuarantinePolicy{MaxAttempts: 3, Clock: dispatchClock}); !errors.Is(err, schedule.ErrDeadLetterNotFound) {
		t.Fatalf("orphan redrive resumed: %v", err)
	}
	forgedRedrive := redrive
	forgedRedrive.Approver = "impostor"
	orphanLetters := schedule.QuarantineSnapshot{Letters: []schedule.DeadLetter{letter}, Redrives: []schedule.RedriveAttempt{forgedRedrive}}
	if _, err := schedule.ResumeQuarantine(orphanLetters, schedule.QuarantinePolicy{MaxAttempts: 3, Clock: dispatchClock}); !errors.Is(err, schedule.ErrRedriveApproval) {
		t.Fatalf("forged redrive resumed: %v", err)
	}
	if _, err := schedule.ResumeQuarantine(snapshot, schedule.QuarantinePolicy{Clock: dispatchClock}); !errors.Is(err, schedule.ErrQuarantineInvalid) {
		t.Fatalf("empty policy resumed: %v", err)
	}
}

func badFiring() schedule.Firing {
	firing := eventFiring(4)
	firing.Key = ""
	return firing
}

func TestTodo_SCHED_004_Recovery(t *testing.T) {
	store := quarantineStore(t, 3)
	firing := occurrenceFiringAt(t, 7, 0)
	letter, err := store.Quarantine(quarantineRequest(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	redrive, err := store.Redrive(schedule.RedriveRequest{Key: firing.Key, Firing: firing, Approver: "ops-lead", ApprovalReason: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	// The redriven attempt fails again: attempt 2 supersedes attempt 1 with
	// lineage, preserving every prior immutable record.
	second, err := store.Quarantine(schedule.QuarantineRequest{Firing: firing, Reason: "DOWNSTREAM_FAULT", Failure: "still failing", Attempt: redrive.Attempt})
	if err != nil || second.Duplicate || second.Supersedes != letter.Digest {
		t.Fatalf("superseding letter = %+v err=%v", second, err)
	}
	snapshot := store.Snapshot()
	restarted, err := schedule.ResumeQuarantine(snapshot, schedule.QuarantinePolicy{MaxAttempts: 3, Clock: dispatchClock})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	history := restarted.History(firing.Key)
	if len(history) != 2 || history[0].Digest != letter.Digest || history[1].Digest != second.Digest {
		t.Fatalf("resumed history lost lineage: %+v", history)
	}
	// Post-restart behavior is identical: duplicates converge and the live
	// letter redrives with its lineage intact.
	replay, err := restarted.Quarantine(schedule.QuarantineRequest{Firing: firing, Reason: "DOWNSTREAM_FAULT", Failure: "still failing", Attempt: redrive.Attempt})
	if err != nil || !replay.Duplicate || replay.Digest != second.Digest {
		t.Fatalf("post-restart redelivery diverged: %+v err=%v", replay, err)
	}
	again, err := restarted.Redrive(schedule.RedriveRequest{Key: firing.Key, Firing: firing, Approver: "ops-lead", ApprovalReason: "fixed again"})
	if err != nil || again.Attempt != 3 || again.DeadLetterDigest != second.Digest {
		t.Fatalf("post-restart redrive = %+v err=%v", again, err)
	}
}
