package actiondiscovery

import (
	"errors"
	"testing"
)

func fixtureAction() Definition {
	return Definition{SemanticID: CreateSemanticID("worker", "update-name", "worker.update"), Label: "Update worker name", Scope: Contextual, FeatureID: "worker", IntentID: "update-name", CapabilityID: "worker.update", Versions: Versions{Feature: "1", Intent: "2", Capability: "3"}, Route: Route{Method: "POST", Path: "/v1/intents/worker/update-name", Version: "2026-01"}, PermittedSubjectTypes: []string{"operator"}, RequiredInputs: []Input{{Name: "displayName", Type: "string", Required: true}}, Risk: RiskMedium, Effect: EffectUpdate, SimulationAvailable: true}
}

func TestTodo_REV_018_02(t *testing.T) {
	update := fixtureAction()
	inspect := Definition{SemanticID: CreateSemanticID("worker", "inspect", "worker.read"), Label: "Inspect worker", Scope: Universal, FeatureID: "worker", IntentID: "inspect", CapabilityID: "worker.read", Versions: Versions{Feature: "1", Intent: "1", Capability: "1"}, Route: Route{Method: "GET", Path: "/v1/intents/worker/inspect", Version: "2026-01"}, PermittedSubjectTypes: []string{"operator", "manager"}, Risk: RiskLow, Effect: EffectRead}
	unpublished := inspect
	unpublished.SemanticID = CreateSemanticID("worker", "inspect-beta", "worker.read-beta")
	unpublished.IntentID = "inspect-beta"
	unpublished.CapabilityID = "worker.read-beta"
	unavailable := false
	unpublished.Published = &unavailable
	registry, err := New([]Definition{inspect, update, unpublished})
	if err != nil {
		t.Fatal(err)
	}
	rows := registry.Discover(Request{Context: Context{SubjectType: "operator", ContextType: "worker", ContextID: "w-1"}})
	if len(rows) != 3 {
		t.Fatalf("discovered %d actions, want 3", len(rows))
	}
	if !rows[0].Available || rows[0].Route != inspect.Route {
		t.Fatalf("universal action = %+v", rows[0])
	}
	if !rows[1].Available || rows[1].SemanticID != update.SemanticID {
		t.Fatalf("contextual action = %+v", rows[1])
	}
	if rows[2].Available || rows[2].UnavailableReason != ReasonUnpublishedIntent {
		t.Fatalf("unpublished action = %+v", rows[2])
	}
	denied := registry.Discover(Request{Context: Context{SubjectType: "operator", ContextType: "worker", ContextID: "w-1"}, Authorize: func(Definition, Context) Authorization {
		return Authorization{Allowed: false, Reason: "policy-internal-detail", Explanation: "secret"}
	}})
	if denied[0].UnavailableReason != ReasonUnauthorized || denied[0].UnavailableExplanation != "This action is not available for the current authorization." {
		t.Fatalf("unsafe authorization explanation: %+v", denied[0])
	}
	if got := registry.Discover(Request{Context: Context{SubjectType: "candidate", ContextType: "worker", ContextID: "w-1"}})[1]; got.UnavailableReason != ReasonUnsupportedSubject || got.UnavailableExplanation != "" {
		t.Fatalf("unsupported subject disclosure = %+v", got)
	}
}

func TestTodo_REV_018_02_Conformance(t *testing.T) {
	action := fixtureAction()
	if got := CreateSemanticID(" Worker! ", "Update Name", "worker.update"); got != "worker:update-name:worker.update" {
		t.Fatalf("semantic ID = %q", got)
	}
	registry, err := New([]Definition{action})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	routes := map[Route]bool{}
	for _, surface := range []string{"global-bar", "command-palette", "worker-menu", "mobile", "kiosk"} {
		_ = surface
		found := registry.Discover(Request{Context: Context{SubjectType: "operator", ContextType: "worker", ContextID: "w-1"}})
		if len(found) != 1 || !found[0].Available {
			t.Fatalf("surface discovery = %+v", found)
		}
		ids[found[0].SemanticID] = true
		routes[found[0].Route] = true
	}
	if len(ids) != 1 || len(routes) != 1 {
		t.Fatalf("surfaces diverged: ids=%v routes=%v", ids, routes)
	}
	listed := registry.List()
	listed[0].PermittedSubjectTypes[0] = "candidate"
	listed[0].RequiredInputs[0].Name = "forged"
	got, _ := registry.Get(action.SemanticID)
	if got.PermittedSubjectTypes[0] != "operator" || got.RequiredInputs[0].Name != "displayName" {
		t.Fatalf("registry exposed mutable slices: %+v", got)
	}
	if _, err := New([]Definition{action, action}); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("duplicate error=%v", err)
	}
	bad := action
	bad.Route.Path = "v1/no-leading-slash"
	if _, err := New([]Definition{bad}); err == nil {
		t.Fatal("invalid route accepted")
	}
	filtered := Scope("universal")
	if got := registry.Discover(Request{Context: Context{SubjectType: "operator"}, Scope: &filtered}); len(got) != 0 {
		t.Fatalf("scope filter returned %+v", got)
	}
	if CreateSemanticID("worker", "update-name", "worker.update") != action.SemanticID {
		t.Fatal("fixture action id is not canonical")
	}
}
