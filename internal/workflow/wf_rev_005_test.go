package workflow_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestTodo_WF_REV_005 proves the PRIMARY contract: cancellation decides
// per effect, after observing every unconfirmed one. A reversible effect
// beside an irreversible sibling still gets its COMPENSATE verdict even
// though the run refuses; an in-flight effect the observer confirms
// produced is judged by its declared contract; one confirmed never
// produced needs no verdict; and the run verdict follows from the
// per-effect verdicts.
func TestTodo_WF_REV_005(t *testing.T) {
	t.Run("siblings are decided even when the run refuses", func(t *testing.T) {
		outcome, err := workflow.DecideCancellation(workflow.CancellationRequest{
			RunID: "run-wf-rev-005", Revision: "rev-1", Phase: "RUNNING@execute",
			Effects: []workflow.EffectRecord{
				{ID: "assign#1", Reversible: true, Compensation: "people.assignment.supersede@1"},
				{ID: "reserve#1", Compensation: "release.stock@2"},
				{ID: "payout#1"},
				{ID: "notice#1", Correction: "hr.case.correct@1"},
			},
		})
		if err != nil {
			t.Fatalf("DecideCancellation: %v", err)
		}
		want := []workflow.EffectDisposition{
			{ID: "assign#1", Disposition: "REVERTED", Verdict: workflow.EffectCompensate},
			{ID: "reserve#1", Disposition: "COMPENSATE:release.stock@2", Verdict: workflow.EffectCompensate},
			{ID: "payout#1", Disposition: "IRREVERSIBLE", Verdict: workflow.EffectKeepIrreversible},
			{ID: "notice#1", Disposition: "CORRECT:hr.case.correct@1", Verdict: workflow.EffectKeepAndCorrect},
		}
		if len(outcome.Effects) != len(want) {
			t.Fatalf("effects = %+v, want %+v", outcome.Effects, want)
		}
		for i, w := range want {
			if outcome.Effects[i] != w {
				t.Fatalf("effect %d = %+v, want %+v", i, outcome.Effects[i], w)
			}
		}
		if outcome.Decision != workflow.CannotCancel {
			t.Fatalf("decision = %s, want CANNOT_CANCEL: kept effects refuse the run", outcome.Decision)
		}
	})

	t.Run("reverts ride the cancel transition itself", func(t *testing.T) {
		revertOnly, err := workflow.DecideCancellation(workflow.CancellationRequest{
			RunID: "run-revert", Revision: "rev-1", Phase: "RUNNING@execute",
			Effects: []workflow.EffectRecord{
				{ID: "assign#1", Reversible: true, Compensation: "people.assignment.supersede@1"},
			},
		})
		if err != nil {
			t.Fatalf("DecideCancellation: %v", err)
		}
		if revertOnly.Decision != workflow.Cancelled {
			t.Fatalf("decision = %s, want CANCELLED: a run whose every undo reverts needs no discharge driver", revertOnly.Decision)
		}
		withCompensation, err := workflow.DecideCancellation(workflow.CancellationRequest{
			RunID: "run-revert-comp", Revision: "rev-1", Phase: "RUNNING@execute",
			Effects: []workflow.EffectRecord{
				{ID: "assign#1", Reversible: true, Compensation: "people.assignment.supersede@1"},
				{ID: "reserve#1", Compensation: "release.stock@2"},
			},
		})
		if err != nil {
			t.Fatalf("DecideCancellation: %v", err)
		}
		if withCompensation.Decision != workflow.CompensationRequired {
			t.Fatalf("decision = %s, want COMPENSATION_REQUIRED", withCompensation.Decision)
		}
	})

	t.Run("observation resolves unconfirmed effects before deciding", func(t *testing.T) {
		calls := map[string]int{}
		observe := func(id string) (workflow.EffectObservation, error) {
			calls[id]++
			switch id {
			case "inflight-compensable#2":
				return workflow.EffectObservation{Produced: true}, nil
			case "inflight-never-ran#1":
				return workflow.EffectObservation{Produced: false}, nil
			default:
				return workflow.EffectObservation{}, errors.New("unexpected observation of " + id)
			}
		}
		outcome, err := workflow.DecideCancellation(workflow.CancellationRequest{
			RunID: "run-wf-rev-005-observe", Revision: "rev-1", Phase: "RUNNING@execute",
			Effects: []workflow.EffectRecord{
				{ID: "settled#1", Compensation: "release.stock@2"},
				{ID: "inflight-compensable#2", Ambiguous: true, Compensation: "release.stock@2"},
				{ID: "inflight-never-ran#1", Ambiguous: true, Compensation: "release.stock@2"},
			},
			Observe: observe,
		})
		if err != nil {
			t.Fatalf("DecideCancellation: %v", err)
		}
		want := []workflow.EffectDisposition{
			{ID: "settled#1", Disposition: "COMPENSATE:release.stock@2", Verdict: workflow.EffectCompensate},
			{ID: "inflight-compensable#2", Disposition: "COMPENSATE:release.stock@2", Verdict: workflow.EffectCompensate},
			{ID: "inflight-never-ran#1", Disposition: "NOT_PRODUCED"},
		}
		if len(outcome.Effects) != len(want) {
			t.Fatalf("effects = %+v, want %+v", outcome.Effects, want)
		}
		for i, w := range want {
			if outcome.Effects[i] != w {
				t.Fatalf("effect %d = %+v, want %+v", i, outcome.Effects[i], w)
			}
		}
		if outcome.Decision != workflow.CompensationRequired {
			t.Fatalf("decision = %s, want COMPENSATION_REQUIRED", outcome.Decision)
		}
		for id, n := range calls {
			if n != 1 {
				t.Fatalf("observer called %d times for %s, want exactly once", n, id)
			}
		}
		if len(calls) != 2 {
			t.Fatalf("observer calls = %v, want only the two unconfirmed effects", calls)
		}
	})
}

// TestTodo_WF_REV_005_Property proves every committed effect receives
// exactly one verdict, and that the run verdict is a pure summary of them:
// re-deriving the decision from the recorded per-effect verdicts always
// agrees with the returned decision, for every contract class and every
// confirmation state.
func TestTodo_WF_REV_005_Property(t *testing.T) {
	contracts := []workflow.EffectRecord{
		{ID: "reversible", Reversible: true, Compensation: "people.assignment.supersede@1"},
		{ID: "compensable", Compensation: "release.stock@2"},
		{ID: "correctable", Correction: "hr.case.correct@1"},
		{ID: "irreversible"},
	}
	confirmations := []struct {
		name      string
		observe   workflow.ObserveEffect
		committed bool
	}{
		{"settled", nil, true},
		{"observed-produced", func(id string) (workflow.EffectObservation, error) {
			return workflow.EffectObservation{Produced: true}, nil
		}, true},
		{"observed-absent", func(id string) (workflow.EffectObservation, error) {
			return workflow.EffectObservation{Produced: false}, nil
		}, false},
		{"observation-timeout", func(id string) (workflow.EffectObservation, error) {
			return workflow.EffectObservation{}, errors.New("observation timeout")
		}, true},
		{"no-observer", nil, true},
	}
	for _, contract := range contracts {
		for _, confirmation := range confirmations {
			name := contract.ID + "/" + confirmation.name
			t.Run(name, func(t *testing.T) {
				rec := contract
				rec.ID += "#1"
				observe := confirmation.observe
				if confirmation.name == "settled" {
					observe = nil
				} else {
					rec.Ambiguous = true
					if confirmation.name == "no-observer" {
						observe = nil
					}
				}
				outcome, err := workflow.DecideCancellation(workflow.CancellationRequest{
					RunID: "r", Revision: "v", Phase: "p",
					Effects: []workflow.EffectRecord{rec}, Observe: observe,
				})
				if err != nil {
					t.Fatalf("DecideCancellation: %v", err)
				}
				if len(outcome.Effects) != 1 {
					t.Fatalf("effects = %+v, want exactly one record", outcome.Effects)
				}
				got := outcome.Effects[0]
				if got.ID != rec.ID {
					t.Fatalf("effect id = %q, want %q", got.ID, rec.ID)
				}
				if confirmation.committed {
					if got.Verdict == "" {
						t.Fatalf("committed effect %+v received no verdict", got)
					}
				} else if got.Verdict != "" || got.Disposition != "NOT_PRODUCED" {
					t.Fatalf("absent effect = %+v, want NOT_PRODUCED with no verdict", got)
				}
			})
		}
	}

	t.Run("run verdict is a pure summary of per-effect verdicts", func(t *testing.T) {
		mix := []workflow.EffectRecord{
			{ID: "a#1", Reversible: true, Compensation: "people.assignment.supersede@1"},
			{ID: "b#1", Compensation: "release.stock@2"},
			{ID: "c#1", Ambiguous: true, Compensation: "release.stock@2"},
			{ID: "d#1"},
		}
		observe := func(id string) (workflow.EffectObservation, error) {
			if id == "c#1" {
				return workflow.EffectObservation{}, errors.New("observation timeout")
			}
			return workflow.EffectObservation{}, errors.New("unexpected observation of " + id)
		}
		outcome, err := workflow.DecideCancellation(workflow.CancellationRequest{
			RunID: "r", Revision: "v", Phase: "p", Effects: mix, Observe: observe,
		})
		if err != nil {
			t.Fatalf("DecideCancellation: %v", err)
		}
		seen := map[string]int{}
		for _, e := range outcome.Effects {
			seen[e.ID]++
		}
		for _, rec := range mix {
			if seen[rec.ID] != 1 {
				t.Fatalf("effect %s appears %d times, want exactly one verdict record", rec.ID, seen[rec.ID])
			}
		}
		if got := workflow.SummarizeCancellation(outcome.Effects, false, false); got != outcome.Decision {
			t.Fatalf("summary = %s, decision = %s: run verdict is not a pure summary of per-effect verdicts",
				got, outcome.Decision)
		}
		if outcome.Decision != workflow.RepairRequired {
			t.Fatalf("decision = %s, want REPAIR_REQUIRED: only the timed-out effect is unresolved", outcome.Decision)
		}
	})
}

// TestTodo_WF_REV_005_Fault proves an observation timeout leaves only that
// effect unresolved: its siblings are still decided, and the run needs
// repair for exactly the timed-out effect.
func TestTodo_WF_REV_005_Fault(t *testing.T) {
	timeout := errors.New("observation timeout")
	calls := map[string]int{}
	observe := func(id string) (workflow.EffectObservation, error) {
		calls[id]++
		if id == "flaky#1" {
			return workflow.EffectObservation{}, timeout
		}
		return workflow.EffectObservation{Produced: true}, nil
	}
	outcome, err := workflow.DecideCancellation(workflow.CancellationRequest{
		RunID: "run-wf-rev-005-fault", Revision: "rev-1", Phase: "RUNNING@execute",
		Effects: []workflow.EffectRecord{
			{ID: "steady#1", Compensation: "release.stock@2"},
			{ID: "flaky#1", Ambiguous: true, Compensation: "release.stock@2"},
			{ID: "reverting#1", Ambiguous: true, Reversible: true, Compensation: "people.assignment.supersede@1"},
		},
		Observe: observe,
	})
	if err != nil {
		t.Fatalf("DecideCancellation: %v", err)
	}
	want := []workflow.EffectDisposition{
		{ID: "steady#1", Disposition: "COMPENSATE:release.stock@2", Verdict: workflow.EffectCompensate},
		{ID: "flaky#1", Disposition: "AMBIGUOUS", Verdict: workflow.EffectUnresolved},
		{ID: "reverting#1", Disposition: "REVERTED", Verdict: workflow.EffectCompensate},
	}
	if len(outcome.Effects) != len(want) {
		t.Fatalf("effects = %+v, want %+v", outcome.Effects, want)
	}
	for i, w := range want {
		if outcome.Effects[i] != w {
			t.Fatalf("effect %d = %+v, want %+v", i, outcome.Effects[i], w)
		}
	}
	if outcome.Decision != workflow.RepairRequired {
		t.Fatalf("decision = %s, want REPAIR_REQUIRED", outcome.Decision)
	}
	if len(calls) != 2 {
		t.Fatalf("observer calls = %v, want exactly the two unconfirmed effects, once each", calls)
	}
	for id, n := range calls {
		if n != 1 {
			t.Fatalf("observer called %d times for %s, want exactly once", n, id)
		}
	}
}
