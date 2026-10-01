package agentcharter

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func agent010Input() Input {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	allowOrg := []OrganizationID{"org-branch-7", "org-entity-2"}
	allTools := []CapabilityID{"people.read", "policy.read", "workflow.write"}
	base := Settings{
		Organizations:  Constraint[OrganizationID]{Specified: true, Allow: allowOrg},
		Jurisdictions:  Constraint[JurisdictionID]{Specified: true, Allow: []JurisdictionID{"jurisdiction-us"}},
		Sources:        Constraint[CapabilityID]{Specified: true, Allow: []CapabilityID{"handbook"}},
		Tools:          Constraint[CapabilityID]{Specified: true, Allow: allTools},
		DataCategories: Constraint[DataCategory]{Specified: true, Allow: []DataCategory{"public", "personnel"}},
		Decisions:      Constraint[DecisionID]{Specified: true, Allow: []DecisionID{"answer", "draft"}},
	}
	layers := []Layer{
		{Kind: LayerPlatform, ID: "platform", Version: 3, EffectiveFrom: at.Add(-24 * time.Hour), Settings: base},
		{Kind: LayerTenant, ID: "tenant-acme", Version: 4, EffectiveFrom: at.Add(-24 * time.Hour), Settings: base},
		{Kind: LayerEntity, ID: "org-branch-7", Version: 2, EffectiveFrom: at.Add(-24 * time.Hour), Settings: base},
		{Kind: LayerAgent, ID: "agent-policy", Version: 8, EffectiveFrom: at.Add(-24 * time.Hour), Settings: base},
		{Kind: LayerInstallation, ID: "install-1", Version: 5, EffectiveFrom: at.Add(-24 * time.Hour), Settings: base},
		{Kind: LayerRun, ID: "run-1", Version: 1, EffectiveFrom: at.Add(-time.Hour), Settings: base},
	}
	organization := OrganizationRef{ID: "org-branch-7", EffectiveFrom: at.Add(-365 * 24 * time.Hour)}
	return Input{Context: Context{TenantID: "tenant-acme", Organizations: []OrganizationRef{organization}, RunID: "run-1", EffectiveAt: at}, Layers: layers}
}

func TestTodo_AGENT_010(t *testing.T) {
	result, err := Resolve(agent010Input())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !reflect.DeepEqual(result.Organizations.Allowed, []OrganizationID{"org-branch-7"}) {
		t.Fatalf("effective organizations = %v", result.Organizations.Allowed)
	}
	if !reflect.DeepEqual(result.Tools.Allowed, []CapabilityID{"people.read", "policy.read", "workflow.write"}) {
		t.Fatalf("effective tools = %v", result.Tools.Allowed)
	}
	if len(result.Tools.Sources) != 6 || result.Tools.Sources[0] != (Source{Layer: LayerPlatform, ID: "platform", Version: 3}) {
		t.Fatalf("tool provenance = %#v", result.Tools.Sources)
	}
}

func TestTodo_AGENT_010_Property(t *testing.T) {
	input := agent010Input()
	baseline, err := Resolve(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range baseline.Tools.Allowed {
		lower := input
		lower.Layers = append([]Layer(nil), input.Layers...)
		last := lower.Layers[len(lower.Layers)-1]
		last.Settings.Tools = Constraint[CapabilityID]{Specified: true, Allow: []CapabilityID{tool}}
		lower.Layers[len(lower.Layers)-1] = last
		resolved, err := Resolve(lower)
		if err != nil {
			t.Fatalf("narrowing to %q failed: %v", tool, err)
		}
		if len(resolved.Tools.Allowed) != 1 || resolved.Tools.Allowed[0] != tool {
			t.Fatalf("lower layer widened or ignored %q: %v", tool, resolved.Tools.Allowed)
		}
	}
	for _, tool := range []CapabilityID{"unknown.tool", "platform.denied"} {
		lower := input
		lower.Layers = append([]Layer(nil), input.Layers...)
		last := lower.Layers[len(lower.Layers)-1]
		last.Settings.Tools = Constraint[CapabilityID]{Specified: true, Allow: []CapabilityID{tool}}
		lower.Layers[len(lower.Layers)-1] = last
		resolved, err := Resolve(lower)
		if err == nil && contains(resolved.Tools.Allowed, tool) {
			t.Fatalf("lower layer expanded tools with %q", tool)
		}
	}
}

func TestTodo_AGENT_010_Security(t *testing.T) {
	t.Run("lower template and installation cannot override mandatory denial", func(t *testing.T) {
		input := agent010Input()
		input.Layers[0].Settings.Tools = Constraint[CapabilityID]{Specified: true, Allow: []CapabilityID{"people.read", "workflow.write"}, Deny: []CapabilityID{"workflow.write"}}
		input.Layers[3].Settings.Tools = Constraint[CapabilityID]{Specified: true, Allow: []CapabilityID{"people.read", "workflow.write"}}
		input.Layers[4].Settings.Tools = Constraint[CapabilityID]{Specified: true, Allow: []CapabilityID{"people.read", "workflow.write"}}
		input.Layers[5].Settings.Tools = Constraint[CapabilityID]{Specified: true, Allow: []CapabilityID{"people.read", "workflow.write"}}
		result, err := Resolve(input)
		if err != nil {
			t.Fatal(err)
		}
		if contains(result.Tools.Allowed, "workflow.write") || !contains(result.Tools.Denied, "workflow.write") {
			t.Fatalf("deny was not decisive: %#v", result.Tools)
		}
	})
	t.Run("denial provenance does not duplicate repeated mandatory denial", func(t *testing.T) {
		input := agent010Input()
		for _, index := range []int{0, 1} {
			input.Layers[index].Settings.Tools = Constraint[CapabilityID]{Specified: true, Allow: []CapabilityID{"people.read", "policy.read"}, Deny: []CapabilityID{"workflow.write"}}
		}
		result, err := Resolve(input)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Tools.Denied, []CapabilityID{"workflow.write"}) {
			t.Fatalf("duplicate deny entries were not canonicalized: %v", result.Tools.Denied)
		}
	})
	t.Run("name-like other entity never substitutes for authorized stable ID", func(t *testing.T) {
		input := agent010Input()
		input.Context.Organizations = []OrganizationRef{{ID: "org-other-entity", EffectiveFrom: input.Context.EffectiveAt.Add(-time.Hour)}}
		if _, err := Resolve(input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("mismatched entity layer accepted, error = %v", err)
		}
	})
	t.Run("unknown or malformed IDs fail closed", func(t *testing.T) {
		input := agent010Input()
		input.Layers[1].Settings.Tools = Constraint[CapabilityID]{Allow: []CapabilityID{"tool.without_specification"}}
		if _, err := Resolve(input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unspecified grant accepted, error = %v", err)
		}
	})
}

func TestTodo_AGENT_010_Golden(t *testing.T) {
	result, err := Resolve(agent010Input())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"effective_at":"2026-09-29T12:00:00Z","organizations":{"allowed":["org-branch-7"],"denied":[],"sources":[{"layer":"PLATFORM","id":"platform","version":3},{"layer":"TENANT","id":"tenant-acme","version":4},{"layer":"ENTITY","id":"org-branch-7","version":2},{"layer":"AGENT","id":"agent-policy","version":8},{"layer":"INSTALLATION","id":"install-1","version":5},{"layer":"RUN","id":"run-1","version":1}]},"jurisdictions":{"allowed":["jurisdiction-us"],"denied":[],"sources":[{"layer":"PLATFORM","id":"platform","version":3},{"layer":"TENANT","id":"tenant-acme","version":4},{"layer":"ENTITY","id":"org-branch-7","version":2},{"layer":"AGENT","id":"agent-policy","version":8},{"layer":"INSTALLATION","id":"install-1","version":5},{"layer":"RUN","id":"run-1","version":1}]},"sources":{"allowed":["handbook"],"denied":[],"sources":[{"layer":"PLATFORM","id":"platform","version":3},{"layer":"TENANT","id":"tenant-acme","version":4},{"layer":"ENTITY","id":"org-branch-7","version":2},{"layer":"AGENT","id":"agent-policy","version":8},{"layer":"INSTALLATION","id":"install-1","version":5},{"layer":"RUN","id":"run-1","version":1}]},"tools":{"allowed":["people.read","policy.read","workflow.write"],"denied":[],"sources":[{"layer":"PLATFORM","id":"platform","version":3},{"layer":"TENANT","id":"tenant-acme","version":4},{"layer":"ENTITY","id":"org-branch-7","version":2},{"layer":"AGENT","id":"agent-policy","version":8},{"layer":"INSTALLATION","id":"install-1","version":5},{"layer":"RUN","id":"run-1","version":1}]},"data_categories":{"allowed":["personnel","public"],"denied":[],"sources":[{"layer":"PLATFORM","id":"platform","version":3},{"layer":"TENANT","id":"tenant-acme","version":4},{"layer":"ENTITY","id":"org-branch-7","version":2},{"layer":"AGENT","id":"agent-policy","version":8},{"layer":"INSTALLATION","id":"install-1","version":5},{"layer":"RUN","id":"run-1","version":1}]},"decisions":{"allowed":["answer","draft"],"denied":[],"sources":[{"layer":"PLATFORM","id":"platform","version":3},{"layer":"TENANT","id":"tenant-acme","version":4},{"layer":"ENTITY","id":"org-branch-7","version":2},{"layer":"AGENT","id":"agent-policy","version":8},{"layer":"INSTALLATION","id":"install-1","version":5},{"layer":"RUN","id":"run-1","version":1}]},"escalation_owner":{"set":false},"success_measures":{"set":false}}`
	if string(encoded) != want {
		t.Fatalf("resolved charter changed\n got: %s\nwant: %s", encoded, want)
	}
}

func TestResolve_EffectiveDatesAndLayerOrder(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input)
	}{
		{"expired layer", func(input *Input) { input.Layers[2].EffectiveUntil = input.Context.EffectiveAt }},
		{"expired organization reference", func(input *Input) { input.Context.Organizations[0].EffectiveUntil = input.Context.EffectiveAt }},
		{"reordered layers", func(input *Input) { input.Layers[0], input.Layers[1] = input.Layers[1], input.Layers[0] }},
		{"missing required layer", func(input *Input) { input.Layers = append(input.Layers[:2], input.Layers[3:]...) }},
		{"run identity mismatch", func(input *Input) { input.Layers[5].ID = "different-run" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := agent010Input()
			tc.mutate(&input)
			if _, err := Resolve(input); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid input accepted, error = %v", err)
			}
		})
	}
}

func TestResolve_HighestPrecedenceDefaultAndSource(t *testing.T) {
	input := agent010Input()
	input.Layers[0].Settings.EscalationOwner = Override[PrincipalID]{Set: true, Value: "platform-hr"}
	input.Layers[1].Settings.EscalationOwner = Override[PrincipalID]{Set: true, Value: "tenant-hr"}
	input.Layers[2].Settings.SuccessMeasures = Override[string]{Set: true, Value: "answer-with-citations"}
	result, err := Resolve(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.EscalationOwner.Value != "platform-hr" || result.EscalationOwner.Source.ID != "platform" {
		t.Fatalf("higher-precedence owner not retained: %#v", result.EscalationOwner)
	}
	if result.SuccessMeasures.Value != "answer-with-citations" || result.SuccessMeasures.Source.ID != "org-branch-7" {
		t.Fatalf("entity success measure provenance lost: %#v", result.SuccessMeasures)
	}
}

func TestResolve_OptionalPackLayer(t *testing.T) {
	input := agent010Input()
	at := input.Context.EffectiveAt
	pack := Layer{Kind: LayerPack, ID: "pack.healthcare", Version: 2, EffectiveFrom: at.Add(-24 * time.Hour), Settings: Settings{
		Tools: Constraint[CapabilityID]{Specified: true, Allow: []CapabilityID{"policy.read"}},
	}}
	input.Layers = append(input.Layers[:3], append([]Layer{pack}, input.Layers[3:]...)...)
	result, err := Resolve(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Tools.Allowed, []CapabilityID{"policy.read"}) || result.Tools.Sources[3] != (Source{Layer: LayerPack, ID: "pack.healthcare", Version: 2}) {
		t.Fatalf("pack did not narrow with provenance: %#v", result.Tools)
	}
}

func TestResolve_UnspecifiedGrantIsCanonicalEmpty(t *testing.T) {
	input := agent010Input()
	for i := range input.Layers {
		input.Layers[i].Settings.Tools = Constraint[CapabilityID]{}
	}
	result, err := Resolve(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tools.Allowed == nil || len(result.Tools.Allowed) != 0 || result.Tools.Denied == nil || len(result.Tools.Denied) != 0 || result.Tools.Sources == nil || len(result.Tools.Sources) != 0 {
		t.Fatalf("unspecified tool policy must be represented as an empty, source-free ceiling: %#v", result.Tools)
	}
}

func TestResolve_DenyOnlyLayerPreservesEarlierAllowCeiling(t *testing.T) {
	input := agent010Input()
	input.Layers[1].Settings.Tools = Constraint[CapabilityID]{
		Specified: true,
		Deny:      []CapabilityID{"workflow.write"},
	}
	result, err := Resolve(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []CapabilityID{"people.read", "policy.read"}
	if !reflect.DeepEqual(result.Tools.Allowed, want) {
		t.Fatalf("deny-only layer replaced earlier ceiling: got %v, want %v", result.Tools.Allowed, want)
	}
	if !reflect.DeepEqual(result.Tools.Denied, []CapabilityID{"workflow.write"}) {
		t.Fatalf("deny-only layer lost denial: %v", result.Tools.Denied)
	}
}

func TestResolve_ExplicitEmptyAllowCeilingRemainsEmpty(t *testing.T) {
	input := agent010Input()
	input.Layers[1].Settings.Tools = Constraint[CapabilityID]{
		Specified: true,
		Allow:     []CapabilityID{},
	}
	result, err := Resolve(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools.Allowed) != 0 {
		t.Fatalf("explicit empty ceiling widened unexpectedly: %v", result.Tools.Allowed)
	}
}

func contains[T comparable](values []T, value T) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func TestResolve_ErrorTextIsSafe(t *testing.T) {
	input := agent010Input()
	input.Context.TenantID = " "
	_, err := Resolve(input)
	if err == nil || !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
