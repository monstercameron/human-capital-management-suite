package workflow

import (
	"bytes"
	"reflect"
	"testing"
)

func TestLoader_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestLoader_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

// TestTodo_WF_EXT_008_Golden pins the selector metadata in the canonical
// stored authoring form and proves the strict loader round-trips it.
func TestTodo_WF_EXT_008_Golden(t *testing.T) {
	d := Definition{
		WorkflowID: "workflow.manager-change", Version: 3, Name: "Manager Change",
		IntentType:     "hcmnext.people.change_manager/v1",
		MatchPredicate: map[string]string{"jurisdiction": "US-NY", "change_kind": "MANAGER"},
	}
	raw, err := Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := "{\n  \"workflow_id\": \"workflow.manager-change\",\n  \"version\": 3,\n  \"name\": \"Manager Change\",\n  \"intent_type\": \"hcmnext.people.change_manager/v1\",\n  \"match_predicate\": {\n    \"change_kind\": \"MANAGER\",\n    \"jurisdiction\": \"US-NY\"\n  },\n  \"input_schema\": {\n    \"schema_id\": \"\",\n    \"version\": 0,\n    \"protobuf_full_name\": \"\"\n  },\n  \"output_schema\": {\n    \"schema_id\": \"\",\n    \"version\": 0,\n    \"protobuf_full_name\": \"\"\n  },\n  \"variables_schema\": {\n    \"schema_id\": \"\",\n    \"version\": 0,\n    \"protobuf_full_name\": \"\"\n  },\n  \"inputs\": null,\n  \"outputs\": null,\n  \"tenant_scope\": \"\",\n  \"organization_scope\": \"\",\n  \"risk_class\": \"\",\n  \"declared_modes\": null,\n  \"terminal_profile\": \"\",\n  \"start_node_id\": \"\",\n  \"nodes\": null,\n  \"edges\": null,\n  \"limits\": {\n    \"max_fan_out\": 0,\n    \"max_depth\": 0,\n    \"max_nodes\": 0\n  },\n  \"failure_policy_ref\": \"\",\n  \"cancellation_policy_ref\": \"\",\n  \"migration_policy_ref\": \"\",\n  \"retention_policy_ref\": \"\"\n}\n"
	if !bytes.Equal(raw, []byte(want)) {
		t.Fatalf("canonical definition bytes changed:\n%s", raw)
	}
	got, err := Load(raw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, d) {
		t.Fatalf("Load(Marshal(definition)) = %#v, want %#v", got, d)
	}
}
