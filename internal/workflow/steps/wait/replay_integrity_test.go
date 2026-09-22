package wait_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

func TestTodo_WF_STEP_019(t *testing.T) {
	fireAt := mustInstant(t, "2026-06-17T14:00:00Z")
	req, err := wait.ComputeTimerRequirement(fixedInstantNode(t, fireAt), testDataset)
	if err != nil {
		t.Fatal(err)
	}
	early := mustInstant(t, "2026-06-17T13:59:59Z")
	if got, err := wait.Resolve(req, early, wait.WakeEvent{}); !errors.Is(err, wait.ErrEarlyWake) || got.Digest != "" {
		t.Fatalf("early wake: %+v, %v", got, err)
	}
	for _, kind := range []wait.EventKind{wait.EventWake, wait.EventCancel, wait.EventSupersede} {
		t.Run(string(kind), func(t *testing.T) {
			first, err := wait.Resolve(req, fireAt, wait.WakeEvent{Kind: kind, Reason: "test"})
			if err != nil || first.Digest == "" {
				t.Fatalf("initial resolution: %+v, %v", first, err)
			}
			want := map[wait.EventKind]wait.Outcome{
				wait.EventWake: wait.OutcomeFired, wait.EventCancel: wait.OutcomeCancelled, wait.EventSupersede: wait.OutcomeSuperseded,
			}[kind]
			if first.Outcome != want {
				t.Fatalf("outcome %s, want %s", first.Outcome, want)
			}
			replayed, err := wait.Resolve(req, values.Instant{}, wait.WakeEvent{Prior: &first})
			if err != nil || !reflect.DeepEqual(replayed, first) {
				t.Fatalf("exact replay: %+v, %v; want %+v", replayed, err, first)
			}
		})
	}
}

func TestTodo_WF_STEP_019_Mutation(t *testing.T) {
	fireAt := mustInstant(t, "2026-06-17T14:00:00Z")
	early := mustInstant(t, "2026-06-17T13:00:00Z")
	newRequirement := func(t *testing.T) wait.TimerRequirement {
		t.Helper()
		req, err := wait.ComputeTimerRequirement(fixedInstantNode(t, fireAt), testDataset)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	for name, mutate := range map[string]func(*wait.TimerRequirement){
		"fire_at":  func(r *wait.TimerRequirement) { r.FireAt = early },
		"node":     func(r *wait.TimerRequirement) { r.NodeID = "other-node" },
		"version":  func(r *wait.TimerRequirement) { r.WorkflowVersion++ },
		"review":   func(r *wait.TimerRequirement) { r.ReviewRequired = true },
		"evidence": func(r *wait.TimerRequirement) { r.Evidence.Inputs["fire_at"] = early.String() },
		"dataset":  func(r *wait.TimerRequirement) { r.Dataset.TzdbVersion = "other" },
		"digest":   func(r *wait.TimerRequirement) { r.Digest = "not-a-digest" },
	} {
		t.Run("requirement_"+name, func(t *testing.T) {
			req := newRequirement(t)
			prior, err := wait.Resolve(req, fireAt, wait.WakeEvent{})
			if err != nil {
				t.Fatal(err)
			}
			mutate(&req)
			for _, event := range []wait.WakeEvent{{}, {Kind: wait.EventCancel}, {Prior: &prior}} {
				got, err := wait.Resolve(req, fireAt, event)
				if !errors.Is(err, wait.ErrInvalidRequirement) || !reflect.DeepEqual(got, wait.Resolution{}) {
					t.Fatalf("altered requirement yielded %+v, %v", got, err)
				}
			}
		})
	}
	for name, mutate := range map[string]func(*wait.Resolution){
		"outcome":      func(r *wait.Resolution) { r.Outcome = wait.OutcomeCancelled },
		"resolved_at":  func(r *wait.Resolution) { r.ResolvedAt = early },
		"reason":       func(r *wait.Resolution) { r.Reason = "altered" },
		"binding":      func(r *wait.Resolution) { r.RequirementDigest = "other-requirement" },
		"empty_digest": func(r *wait.Resolution) { r.Digest = "" },
		"wrong_digest": func(r *wait.Resolution) { r.Digest = "not-a-digest" },
	} {
		t.Run("replay_"+name, func(t *testing.T) {
			req := newRequirement(t)
			prior, err := wait.Resolve(req, fireAt, wait.WakeEvent{})
			if err != nil {
				t.Fatal(err)
			}
			mutate(&prior)
			got, err := wait.Resolve(req, fireAt, wait.WakeEvent{Prior: &prior})
			if !errors.Is(err, wait.ErrDigestMismatch) || !reflect.DeepEqual(got, wait.Resolution{}) {
				t.Fatalf("altered replay yielded %+v, %v", got, err)
			}
		})
	}
}
