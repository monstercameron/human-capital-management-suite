package workflow_test

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

type wfData038Resolver struct {
	declarations map[string]workflow.ParameterDeclaration
	calls        map[string]int
}

func (r *wfData038Resolver) ResolveParameter(key string) (workflow.ParameterDeclaration, bool) {
	if r.calls == nil {
		r.calls = make(map[string]int)
	}
	r.calls[key]++
	declaration, ok := r.declarations[key]
	return declaration, ok
}

func wfData038Fixture(t *testing.T, mode workflow.ParameterBindingMode) (workflow.Definition, workflow.Options, *wfData038Resolver) {
	t.Helper()
	definition := workflow.PromotionReferenceDefinition()
	resolver := &wfData038Resolver{declarations: map[string]workflow.ParameterDeclaration{
		"compensation.band": {
			Key: "compensation.band", Type: workflow.ValueType{Kind: workflow.KindString},
			Classification:   "CONFIDENTIAL_HR",
			AllowedConsumers: []workflow.ParameterConsumer{{Kind: "WORKFLOW", ID: definition.WorkflowID}},
			PublishValue:     "EXCELLENT", HasPublishValue: true,
			DefinitionName: "tenant-parameters", DefinitionVersion: "v12", Revision: 3,
		},
	}}
	for i := range definition.Nodes {
		if definition.Nodes[i].ID != workflow.PromotionNodeBuildProposal {
			continue
		}
		for j := range definition.Nodes[i].InputMappings {
			if definition.Nodes[i].InputMappings[j].Target == "band_position" {
				definition.Nodes[i].InputMappings[j].Source = workflow.Source{
					Kind: workflow.SourceParameter, ParameterKey: "compensation.band", ParameterMode: mode,
				}
			}
		}
	}
	options := promotionOptions(t)
	options.Parameters = resolver
	return definition, options, resolver
}

func parameterMapping(t *testing.T, plan *workflow.CompiledWorkflow, nodeID, target string) workflow.CompiledMapping {
	t.Helper()
	for _, node := range plan.Nodes {
		if node.ID != nodeID {
			continue
		}
		for _, mapping := range node.Mappings {
			if mapping.Target == target {
				return mapping
			}
		}
	}
	t.Fatalf("compiled mapping %s.%s is missing", nodeID, target)
	return workflow.CompiledMapping{}
}

// TestTodo_WF_DATA_038 proves declared modes resolve only typed, allowlisted,
// classification-compatible parameters and reject a live decision input.
func TestTodo_WF_DATA_038(t *testing.T) {
	for _, mode := range []workflow.ParameterBindingMode{
		workflow.ParameterPinnedAtPublish,
		workflow.ParameterPinnedAtStart,
		workflow.ParameterLive,
	} {
		t.Run(string(mode), func(t *testing.T) {
			definition, options, _ := wfData038Fixture(t, mode)
			if mode == workflow.ParameterLive {
				for i := range definition.Nodes {
					if definition.Nodes[i].ID != workflow.PromotionNodeBuildProposal {
						continue
					}
					for j := range definition.Nodes[i].InputMappings {
						if definition.Nodes[i].InputMappings[j].Target == "band_position" {
							definition.Nodes[i].InputMappings[j].Source.MaxAgeSeconds = 300
						}
					}
				}
			}
			plan, err := workflow.Compile(definition, options)
			if err != nil {
				t.Fatalf("compile %s binding: %v", mode, err)
			}
			mapping := parameterMapping(t, plan, workflow.PromotionNodeBuildProposal, "band_position")
			if mapping.ParameterMode != mode || mapping.ParameterKey != "compensation.band" {
				t.Fatalf("compiled mapping lost its declaration: %+v", mapping)
			}
			if mode == workflow.ParameterPinnedAtPublish && (mapping.ParameterValue != "EXCELLENT" || mapping.ParameterRevision != 3 || mapping.ParameterDefinitionVersion != "v12") {
				t.Fatalf("publish binding did not pin its value revision: %+v", mapping)
			}
			if mode == workflow.ParameterLive && (mapping.PinnedInput || mapping.ParameterMaxAgeSeconds != 300) {
				t.Fatalf("live binding must preserve its freshness bound and remain unpinned: %+v", mapping)
			}
		})
	}

	t.Run("rejects_unauthorized_consumer", func(t *testing.T) {
		definition, options, resolver := wfData038Fixture(t, workflow.ParameterPinnedAtStart)
		declaration := resolver.declarations["compensation.band"]
		declaration.AllowedConsumers = []workflow.ParameterConsumer{{Kind: "WORKFLOW", ID: "workflow:other"}}
		resolver.declarations[declaration.Key] = declaration
		if plan, err := workflow.Compile(definition, options); plan != nil || err == nil || !diagnostics(t, err).Has(workflow.CodeUnresolvedRef) {
			t.Fatalf("unauthorized parameter consumer should be refused, plan=%v err=%v", plan, err)
		}
	})

	t.Run("rejects_type_mismatch", func(t *testing.T) {
		definition, options, resolver := wfData038Fixture(t, workflow.ParameterPinnedAtStart)
		declaration := resolver.declarations["compensation.band"]
		declaration.Type = workflow.ValueType{Kind: workflow.KindBool}
		resolver.declarations[declaration.Key] = declaration
		if plan, err := workflow.Compile(definition, options); plan != nil || err == nil || !diagnostics(t, err).Has(workflow.CodeTypeMismatch) {
			t.Fatalf("incompatible parameter type should be refused, plan=%v err=%v", plan, err)
		}
	})

	t.Run("rejects_classification_flow", func(t *testing.T) {
		definition, options, resolver := wfData038Fixture(t, workflow.ParameterPinnedAtStart)
		resolver.declarations["compensation.band"] = workflow.ParameterDeclaration{
			Key: "compensation.band", Type: workflow.ValueType{Kind: workflow.KindString}, Classification: "PUBLIC",
			AllowedConsumers: []workflow.ParameterConsumer{{Kind: "WORKFLOW", ID: definition.WorkflowID}},
		}
		if plan, err := workflow.Compile(definition, options); plan != nil || err == nil || !diagnostics(t, err).Has(workflow.CodeTypeMismatch) {
			t.Fatalf("disallowed classification flow should be refused, plan=%v err=%v", plan, err)
		}
	})

	t.Run("live_requires_bounded_age", func(t *testing.T) {
		definition, options, _ := wfData038Fixture(t, workflow.ParameterLive)
		if plan, err := workflow.Compile(definition, options); plan != nil || err == nil || !diagnostics(t, err).Has(workflow.CodeUnresolvedRef) {
			t.Fatalf("unbounded LIVE read should be refused, plan=%v err=%v", plan, err)
		}
	})

	definition, options, resolver := wfData038Fixture(t, workflow.ParameterPinnedAtStart)
	declaration := resolver.declarations["compensation.band"]
	declaration.Type = workflow.ValueType{Kind: workflow.KindDecimal}
	resolver.declarations[declaration.Key] = declaration
	for i := range definition.Nodes {
		if definition.Nodes[i].ID == workflow.PromotionNodeRaiseThreshold {
			definition.Nodes[i].InputMappings[0].Source = workflow.Source{
				Kind: workflow.SourceParameter, ParameterKey: "compensation.band", ParameterMode: workflow.ParameterLive, MaxAgeSeconds: 60,
			}
		}
	}
	plan, err := workflow.Compile(definition, options)
	if plan != nil || err == nil {
		t.Fatal("a DECISION must reject a LIVE parameter even when its age is bounded")
	}
	if !diagnostics(t, err).Has(workflow.CodeMutableDecisionInput) {
		t.Fatalf("expected mutable decision input diagnostic, got %v", diagnostics(t, err).Codes())
	}

	delete(resolver.declarations, "compensation.band")
	plan, err = workflow.Compile(definition, options)
	if plan != nil || err == nil {
		t.Fatal("an absent parameter must prevent compilation")
	}
}

// TestTodo_WF_DATA_038_Property proves a published plan retains its pinned
// parameter revision after the source declaration advances.
func TestTodo_WF_DATA_038_Property(t *testing.T) {
	definition, options, resolver := wfData038Fixture(t, workflow.ParameterPinnedAtPublish)
	plan, err := workflow.Compile(definition, options)
	if err != nil {
		t.Fatalf("compile publish-pinned plan: %v", err)
	}
	digest := plan.Digest()
	declaration := resolver.declarations["compensation.band"]
	declaration.PublishValue = "NEEDS_IMPROVEMENT"
	declaration.Revision++
	resolver.declarations[declaration.Key] = declaration
	mapping := parameterMapping(t, plan, workflow.PromotionNodeBuildProposal, "band_position")
	if mapping.ParameterValue != "EXCELLENT" || mapping.ParameterRevision != 3 || plan.Digest() != digest {
		t.Fatalf("compiled plan changed after parameter revision advanced: mapping=%+v digest=%s", mapping, plan.Digest())
	}
	if resolver.calls["compensation.band"] != 1 {
		t.Fatalf("compiler read a stateful parameter resolver %d times for one key", resolver.calls["compensation.band"])
	}
}

// TestTodo_WF_DATA_038_Golden pins the canonical parameter mapping bytes
// published into the compiled workflow plan.
func TestTodo_WF_DATA_038_Golden(t *testing.T) {
	definition, options, _ := wfData038Fixture(t, workflow.ParameterPinnedAtPublish)
	plan, err := workflow.Compile(definition, options)
	if err != nil {
		t.Fatalf("compile publish-pinned plan: %v", err)
	}
	mapping := parameterMapping(t, plan, workflow.PromotionNodeBuildProposal, "band_position")
	got, err := json.Marshal(mapping)
	if err != nil {
		t.Fatalf("marshal compiled parameter mapping: %v", err)
	}
	want := `{"target":"band_position","target_type":{"kind":"STRING"},"source_kind":"PARAMETER","source_type":{"kind":"STRING"},"pinned_input":true,"parameter_key":"compensation.band","parameter_mode":"PINNED_AT_PUBLISH","parameter_value":"EXCELLENT","parameter_definition_name":"tenant-parameters","parameter_definition_version":"v12","parameter_revision":3}`
	if string(got) != want {
		t.Fatalf("compiled parameter mapping changed\n got: %s\nwant: %s", got, want)
	}
}
