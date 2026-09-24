package presentation

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"slices"
	"testing"
)

func base() Input {
	return Input{Dimensions: lifecycle.Dimensions{Request: lifecycle.RequestDraft, Execution: lifecycle.ExecutionNotPlanned, Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyNotApplicable, Obligation: lifecycle.ObligationNotApplicable}, Authorization: AuthorizationAllowed, Freshness: FreshnessFresh, Operational: OperationalReady, HasResult: true, ExternalOutcomeKnown: true}
}

func TestFlowStatePresentationReturnsExactTruthfulStatusAndSafeActions(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Input)
		want    State
		actions []Action
	}{
		{"loading", func(i *Input) { i.Operational = OperationalLoading }, StateLoading, []Action{ActionWait, ActionCancelIfSafe}},
		{"operational waiting", func(i *Input) { i.Operational = OperationalWaiting }, StateWaiting, []Action{ActionWait, ActionRefresh, ActionRequestHelp}},
		{"operational degraded", func(i *Input) { i.Operational = OperationalDegraded }, StatePartialDegraded, []Action{ActionRefresh, ActionOpenRepair, ActionRequestHelp}},
		{"empty", func(i *Input) { i.HasResult = false }, StateEmpty, []Action{ActionRequestHelp}},
		{"draft", func(i *Input) {}, StateDraft, []Action{ActionSaveDraft, ActionSubmit}},
		{"validation", func(i *Input) { i.HasValidationErrors = true }, StateValidationBlocked, []Action{ActionCorrect, ActionSaveDraft}},
		{"simulation", func(i *Input) { i.HasSimulation = true }, StateSimulationReady, []Action{ActionSubmit, ActionReview}},
		{"running", func(i *Input) { i.Dimensions.Execution = lifecycle.ExecutionExecuting }, StateRunning, []Action{ActionWait, ActionCancelIfSafe, ActionReview}},
		{"waiting", func(i *Input) { i.Dimensions.Consistency = lifecycle.ConsistencyPendingObservation }, StateWaiting, []Action{ActionWait, ActionRefresh, ActionRequestHelp}},
		{"stale", func(i *Input) { i.Freshness = FreshnessStale }, StateStaleReplan, []Action{ActionRefresh, ActionReplan, ActionRequestHelp}},
		{"partial", func(i *Input) { i.Dimensions.Consistency = lifecycle.ConsistencyDegraded }, StatePartialDegraded, []Action{ActionRefresh, ActionOpenRepair, ActionRequestHelp}},
		{"unknown", func(i *Input) { i.Dimensions.Business = lifecycle.BusinessUnknown }, StateUnknownAmbiguous, []Action{ActionInvestigate, ActionRefresh, ActionRequestHelp}},
		{"repair", func(i *Input) { i.Dimensions.Execution = lifecycle.ExecutionRepairRequired }, StateRepairRequired, []Action{ActionOpenRepair, ActionReview, ActionRequestHelp}},
		{"completed", func(i *Input) {
			i.Dimensions.Request = lifecycle.RequestClosed
			i.Dimensions.Business = lifecycle.BusinessCompleted
			i.Dimensions.Consistency = lifecycle.ConsistencyConsistent
			i.Dimensions.Obligation = lifecycle.ObligationSatisfied
			i.Dimensions.Execution = lifecycle.ExecutionCommitted
			i.HasResult = true
		}, StateCompleted, []Action{ActionReview, ActionCorrect}},
		{"cancelled", func(i *Input) { i.Dimensions.Request = lifecycle.RequestCancelled }, StateCancelled, []Action{ActionReview, ActionRequestHelp}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := base()
			tc.mutate(&i)
			p := Resolve(i)
			if p.State != tc.want {
				t.Fatalf("state=%s want %s", p.State, tc.want)
			}
			got := make([]Action, len(p.Actions))
			for n, a := range p.Actions {
				got[n] = a.Action
				if !a.Enabled && !(tc.name == "running" && a.Action == ActionCancelIfSafe) {
					t.Errorf("%s unexpectedly disabled", a.Action)
				}
				if tc.name == "running" && a.Action == ActionCancelIfSafe && a.Enabled {
					t.Error("cancel was enabled after execution started")
				}
			}
			if !slices.Equal(got, tc.actions) {
				t.Fatalf("actions=%v want %v", got, tc.actions)
			}
		})
	}
}

func TestTodo_UXFLOW_004_Property(t *testing.T) {
	p := Resolve(base())
	p.EvidenceRefs = append(p.EvidenceRefs, "forged")
	if len(Resolve(base()).EvidenceRefs) != 0 {
		t.Fatal("evidence leaked between results")
	}
}
func TestTodo_UXFLOW_004_Golden(t *testing.T) {
	i := base()
	i.Dimensions.Request = lifecycle.RequestClosed
	i.Dimensions.Business = lifecycle.BusinessCompleted
	i.Dimensions.Consistency = lifecycle.ConsistencyConsistent
	i.Dimensions.Obligation = lifecycle.ObligationSatisfied
	if got := Resolve(i).State; got != StateCompleted {
		t.Fatalf("got %s", got)
	}
}
func TestTodo_UXFLOW_004_Fault(t *testing.T) {
	i := base()
	i.Dimensions.Execution = lifecycle.ExecutionCommitted
	i.ExternalOutcomeKnown = false
	if got := Resolve(i).State; got != StateUnknownAmbiguous {
		t.Fatalf("commit timeout became %s", got)
	}
}
func TestTodo_UXFLOW_004_Security(t *testing.T) {
	i := base()
	i.Authorization = AuthorizationDenied
	i.EvidenceRefs = []string{"secret"}
	p := Resolve(i)
	if p.State != StateGovernanceDenied || len(p.Actions) != 0 || len(p.EvidenceRefs) != 0 || p.Dimensions != (lifecycle.Dimensions{}) {
		t.Fatalf("denied projection disclosed unsafe actions: %+v", p)
	}
}
func TestTodo_UXFLOW_004_Conformance(t *testing.T) {
	i := base()
	i.Dimensions.Business = lifecycle.BusinessCompleted
	i.Dimensions.Consistency = lifecycle.ConsistencyPendingObservation
	if got := Resolve(i).State; got == StateCompleted {
		t.Fatal("flattened open consistency into completed")
	}
}
func TestTodo_UXFLOW_004_Browser(t *testing.T) {
	i := base()
	i.Freshness = FreshnessStale
	p := Resolve(i)
	for _, a := range p.Actions {
		if a.Action == ActionSubmit && a.Enabled {
			t.Fatal("stale browser action enabled")
		}
	}
}
func TestTodo_UXFLOW_004_Mutation(t *testing.T) {
	i := base()
	i.Dimensions.Execution = lifecycle.ExecutionCommitted
	p := Resolve(i)
	for _, a := range p.Actions {
		if a.Action == ActionCancelIfSafe && a.Enabled {
			t.Fatal("cancel after commit enabled")
		}
	}
}

func TestTodo_UXFLOW_004_TruthAndLocalization(t *testing.T) {
	t.Run("closed request with unfinished business is not complete", func(t *testing.T) {
		i := base()
		i.Dimensions.Request = lifecycle.RequestClosed
		p := Resolve(i)
		if p.State != StateUnknownAmbiguous {
			t.Fatalf("closed request with unfinished business presented as %s", p.State)
		}
	})
	t.Run("completed business keeps open obligations visible", func(t *testing.T) {
		i := base()
		i.Dimensions.Request = lifecycle.RequestClosed
		i.Dimensions.Business = lifecycle.BusinessCompleted
		i.Dimensions.Execution = lifecycle.ExecutionCommitted
		i.Dimensions.Consistency = lifecycle.ConsistencyConsistent
		i.Dimensions.Obligation = lifecycle.ObligationPending
		p := Resolve(i)
		if p.State != StateWaiting || p.Dimensions.Obligation != lifecycle.ObligationPending {
			t.Fatalf("open obligation flattened: %+v", p)
		}
	})
	t.Run("unknown authorization hides state and evidence", func(t *testing.T) {
		i := base()
		i.Authorization = AuthorizationUnknown
		i.EvidenceRefs = []string{"private-ref"}
		p := Resolve(i)
		if p.State != StateGovernanceDenied || len(p.EvidenceRefs) != 0 || p.Dimensions != (lifecycle.Dimensions{}) {
			t.Fatalf("unknown authorization disclosed state: %+v", p)
		}
	})
	t.Run("locale is compiled into participant text", func(t *testing.T) {
		i := base()
		i.Freshness = FreshnessStale
		i.Locale = "de-DE"
		p := Resolve(i)
		if p.Locale != "de-DE" || p.StateLabel != "Aktualisierung erforderlich" || p.Description == "" {
			t.Fatalf("missing localized state: %+v", p)
		}
	})
}
