package mapping_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/mapping"
)

func TestTodo_XFORM_008_CutoverConnectivity(t *testing.T) {
	ir := mapping.IR{Version: "v1", Rules: []mapping.Rule{
		{Source: "worker_id", Target: "person.id", Op: mapping.OpIdentity},
		{Target: "person.kind", Op: mapping.OpConstant, Argument: "worker"},
	}}
	normalized, err := mapping.Normalize(ir)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if normalized.Rules[0].Null != "" {
		t.Fatalf("omitted null policy = %q, want the legacy zero value preserved", normalized.Rules[0].Null)
	}
	result, err := mapping.ExecuteShared(ir, map[string]string{"worker_id": "W-1"})
	if err != nil {
		t.Fatalf("ExecuteShared: %v", err)
	}
	if len(result.Fields) != 2 || result.Fields[0].Target != "person.id" || result.Fields[0].Value != "W-1" {
		t.Fatalf("shared fields = %+v", result.Fields)
	}
}
