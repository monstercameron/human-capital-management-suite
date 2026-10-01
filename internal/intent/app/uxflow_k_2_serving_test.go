package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/draftflow"
	recoveryoutcome "github.com/monstercameron/human-capital-management-suite/internal/experience/outcome"
	flowexperience "github.com/monstercameron/human-capital-management-suite/internal/flow"
)

func TestTodo_UXFLOW_005_Browser(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	mechanics := (&IntentService{}).Experience()
	scope := mechanics.NewScope(func() time.Time { return now })
	draft, err := scope.Drafts.Autosave(draftflow.SaveRequest{
		ID: "draft-serving", FlowID: "promotion", Owner: "alice",
		Requested: map[string]string{"title": "Director"}, At: now,
	})
	if err != nil {
		t.Fatalf("autosave: %v", err)
	}
	validation := mechanics.Validate(draft, func(values map[string]string) []draftflow.ValidationError {
		if values["title"] == "" {
			return []draftflow.ValidationError{{Field: "title", Message: "title is required"}}
		}
		return nil
	})
	if !validation.Valid || validation.Requested["title"] != "Director" {
		t.Fatalf("validation lost requested input: %+v", validation)
	}
	simulation := mechanics.Simulate(draft, draftflow.TruthSnapshot{
		Values:     map[string]string{"title": "Manager"},
		Provenance: map[string]string{"title": "hris:v4"},
		Revision:   "snapshot-7",
	}, func(draftflow.Resolved) ([]string, []string) {
		return []string{"title: Manager -> Director"}, nil
	})
	if !simulation.Effects.IsZero() || simulation.Proposal.Digest != draftflow.Digest(simulation.Proposal) {
		t.Fatalf("simulation was not zero-effect and exact: %+v", simulation)
	}
	confirmed, err := mechanics.Confirm(scope, draftflow.ConfirmRequest{
		ID: "intent-serving", IdempotencyKey: "idem-serving", ProposalDigest: simulation.Proposal.Digest,
	}, simulation.Proposal)
	if err != nil || confirmed.ProposalDigest != simulation.Proposal.Digest || confirmed.Idempotent {
		t.Fatalf("confirm: %+v err=%v", confirmed, err)
	}
	replay, err := mechanics.Confirm(scope, draftflow.ConfirmRequest{
		ID: "intent-serving", IdempotencyKey: "idem-serving", ProposalDigest: simulation.Proposal.Digest,
	}, simulation.Proposal)
	if err != nil || !replay.Idempotent {
		t.Fatalf("confirmation replay was not idempotent: %+v err=%v", replay, err)
	}
}

func TestTodo_UXFLOW_006_Browser(t *testing.T) {
	mechanics := (&IntentService{}).Experience()
	scopeA := mechanics.NewScope(nil)
	scopeB := mechanics.NewScope(nil)
	if _, err := scopeA.Resume.SaveDraft("flow-serving", "alice", "stage-1", 0, map[string]string{"name": "Alice", "salary": "100000"}, time.Now().UTC()); err != nil {
		t.Fatalf("save resume draft: %v", err)
	}
	if _, ok := scopeB.Resume.GetDraft("flow-serving"); ok {
		t.Fatal("resume state leaked across scopes")
	}
	if _, err := scopeA.Resume.SaveDraft("flow-serving", "alice", "stage-1", 1, map[string]string{"name": "Alice 2", "salary": "100000"}, time.Now().UTC()); err != nil {
		t.Fatalf("save current version: %v", err)
	}
	if _, err := scopeA.Resume.SaveDraft("flow-serving", "alice", "stage-1", 1, map[string]string{"name": "stale"}, time.Now().UTC()); !errors.Is(err, flowexperience.ErrConflict) {
		t.Fatalf("stale resume write error=%v", err)
	}
	draft, ok := scopeA.Resume.GetDraft("flow-serving")
	if !ok || draft.Masked["salary"] != "***" || draft.Version != 2 {
		t.Fatalf("resume projection=%+v", draft)
	}
}

func TestTodo_UXFLOW_007_Browser(t *testing.T) {
	mechanics := (&IntentService{}).Experience()
	recovery, err := mechanics.Recover(recoveryoutcome.Request{
		Kind:              recoveryoutcome.Unknown,
		LastSafeOperation: "operation-serving",
		Now:               time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !slices.Equal(recovery.Actions, []recoveryoutcome.Action{recoveryoutcome.Refresh, recoveryoutcome.RequestHelp}) {
		t.Fatalf("unsafe recovery actions=%v", recovery.Actions)
	}
}
