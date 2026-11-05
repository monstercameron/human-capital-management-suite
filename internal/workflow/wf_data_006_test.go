package workflow_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// wfData006Fixture swaps the BuildProposal band_position mapping to a VARIABLE
// read of the proposal variable's grade field.
func wfData006Fixture(t *testing.T) (workflow.Definition, workflow.Options) {
	t.Helper()
	definition := workflow.PromotionReferenceDefinition()
	for i := range definition.Nodes {
		if definition.Nodes[i].ID != workflow.PromotionNodeBuildProposal {
			continue
		}
		for j := range definition.Nodes[i].InputMappings {
			if definition.Nodes[i].InputMappings[j].Target == "band_position" {
				definition.Nodes[i].InputMappings[j].Source = workflow.Source{
					Kind:         workflow.SourceVariable,
					VariableName: "proposal",
					Path:         "grade",
					Type:         workflow.ValueType{Kind: workflow.KindString},
				}
			}
		}
	}
	return definition, promotionOptions(t)
}

// wfData006Source is a minimal [workflow.MappingSource] serving variable
// fields as canonical JSON text.
type wfData006Source struct {
	variables map[string]map[string]string
}

func (s wfData006Source) WorkflowInput(string) (workflow.TypedValue, bool) {
	return workflow.TypedValue{}, false
}

func (s wfData006Source) NodeOutput(string, string) (workflow.TypedValue, bool, bool) {
	return workflow.TypedValue{}, false, false
}

func (s wfData006Source) Context(string, string) (workflow.TypedValue, bool) {
	return workflow.TypedValue{}, false
}

func (s wfData006Source) Variable(name, path string) (string, bool) {
	raw, ok := s.variables[name][path]
	return raw, ok
}

// wfData006AttemptSource serves VARIABLE mappings from one attempt-pinned
// variable view, proving the runtime seam plugs into [workflow.ResolveMappings].
type wfData006AttemptSource struct {
	attempt runtime.AttemptVariables
}

func (s wfData006AttemptSource) WorkflowInput(string) (workflow.TypedValue, bool) {
	return workflow.TypedValue{}, false
}

func (s wfData006AttemptSource) NodeOutput(string, string) (workflow.TypedValue, bool, bool) {
	return workflow.TypedValue{}, false, false
}

func (s wfData006AttemptSource) Context(string, string) (workflow.TypedValue, bool) {
	return workflow.TypedValue{}, false
}

func (s wfData006AttemptSource) Variable(name, path string) (string, bool) {
	return s.attempt.Variable(name, path)
}

func wfData006MappingCode(t *testing.T, err error, want string) {
	t.Helper()
	mappingErr, ok := err.(*workflow.MappingError)
	if !ok {
		t.Fatalf("error = %v, want a *workflow.MappingError with code %s", err, want)
	}
	if mappingErr.Code != want {
		t.Fatalf("error code = %s, want %s (%v)", mappingErr.Code, want, err)
	}
}

// wfData006VariableNode compiles the fixture and returns a node carrying only
// the compiled VARIABLE mapping, so resolution failures name the variable
// read and never a sibling mapping.
func wfData006VariableNode(t *testing.T) workflow.CompiledNode {
	t.Helper()
	definition, options := wfData006Fixture(t)
	plan, err := workflow.Compile(definition, options)
	if err != nil {
		t.Fatalf("compile VARIABLE mapping: %v", err)
	}
	mapping := parameterMapping(t, plan, workflow.PromotionNodeBuildProposal, "band_position")
	return workflow.CompiledNode{ID: workflow.PromotionNodeBuildProposal, Mappings: []workflow.CompiledMapping{mapping}}
}

var fixedInstant006 = time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)

func proposalWriteFor006(value string) runtime.VariableWrite {
	return runtime.VariableWrite{
		VariableName: "proposal",
		SchemaRef:    "schema.promotion.proposal/v1",
		Value:        json.RawMessage(value),
		WriterNodeID: "build_proposal",
		WriterRef:    "transform-execution:1",
		Reason:       "proposal rebuilt after band evaluation",
		CausationID:  "cause:band-evaluated",
	}
}

// TestTodo_WF_DATA_006 proves a VARIABLE mapping source compiles only with a
// named variable, a named field and a declared type, normalizes into the
// compiled plan, and resolves the revision current at the node's attempt.
func TestTodo_WF_DATA_006(t *testing.T) {
	t.Run("GREEN_compiles_and_pins", func(t *testing.T) {
		definition, options := wfData006Fixture(t)
		plan, err := workflow.Compile(definition, options)
		if err != nil {
			t.Fatalf("compile VARIABLE mapping: %v", err)
		}
		mapping := parameterMapping(t, plan, workflow.PromotionNodeBuildProposal, "band_position")
		if mapping.SourceKind != workflow.SourceVariable || mapping.SourceVariable != "proposal" || mapping.SourcePath != "grade" {
			t.Fatalf("compiled mapping lost its variable declaration: %+v", mapping)
		}
		if mapping.SourceType.Kind != workflow.KindString {
			t.Fatalf("compiled mapping source type = %s, want STRING", mapping.SourceType)
		}
		if !mapping.PinnedInput {
			t.Fatalf("a VARIABLE mapping reads the attempt-pinned revision, so it must be pinned: %+v", mapping)
		}
	})

	t.Run("RED_rejects_undeclared_reads", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Source)
			code   string
		}{
			{"missing_variable_name", func(s *workflow.Source) { s.VariableName = "" }, workflow.CodeUnresolvedRef},
			{"missing_field_path", func(s *workflow.Source) { s.Path = "" }, workflow.CodeUnresolvedRef},
			{"wildcard_path", func(s *workflow.Source) { s.Path = "*" }, workflow.CodeUnrestrictedResultCopy},
			{"missing_type", func(s *workflow.Source) { s.Type = workflow.ValueType{} }, workflow.CodeUnresolvedRef},
			{"unassignable_type", func(s *workflow.Source) {
				s.Type = workflow.ValueType{Kind: workflow.KindDecimal}
			}, workflow.CodeTypeMismatch},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				definition, options := wfData006Fixture(t)
				for i := range definition.Nodes {
					if definition.Nodes[i].ID != workflow.PromotionNodeBuildProposal {
						continue
					}
					for j := range definition.Nodes[i].InputMappings {
						if definition.Nodes[i].InputMappings[j].Target == "band_position" {
							tc.mutate(&definition.Nodes[i].InputMappings[j].Source)
						}
					}
				}
				mustReject(t, definition, options, tc.code)
			})
		}
	})

	t.Run("GREEN_resolves_current_revision", func(t *testing.T) {
		node := wfData006VariableNode(t)
		src := wfData006Source{variables: map[string]map[string]string{
			"proposal": {"grade": `"L5"`},
		}}
		got, err := workflow.ResolveMappings(node, src)
		if err != nil {
			t.Fatalf("ResolveMappings: %v", err)
		}
		if got["band_position"].Text != "L5" || got["band_position"].Type.Kind != workflow.KindString {
			t.Fatalf("resolved band_position = %+v, want STRING L5", got["band_position"])
		}
	})

	t.Run("GREEN_reads_revision_current_at_attempt", func(t *testing.T) {
		node := wfData006VariableNode(t)
		log, err := runtime.NewVariableRevisionLog(uuid.New(), uuid.New(), nil)
		if err != nil {
			t.Fatalf("empty log: %v", err)
		}
		if _, err := log.Append(proposalWriteFor006(`{"grade":"L5"}`), fixedInstant006); err != nil {
			t.Fatalf("append L5: %v", err)
		}
		attempt := log.PinAttempt()
		// A revision written after the attempt began must stay invisible.
		if _, err := log.Append(proposalWriteFor006(`{"grade":"L6"}`), fixedInstant006.Add(time.Minute)); err != nil {
			t.Fatalf("append L6: %v", err)
		}
		got, err := workflow.ResolveMappings(node, wfData006AttemptSource{attempt: attempt})
		if err != nil {
			t.Fatalf("ResolveMappings: %v", err)
		}
		if got["band_position"].Text != "L5" {
			t.Fatalf("resolved band_position = %+v, want the L5 revision current at attempt begin", got["band_position"])
		}
	})

	t.Run("RED_unwritten_variable_is_unresolved", func(t *testing.T) {
		node := wfData006VariableNode(t)
		_, err := workflow.ResolveMappings(node, wfData006Source{})
		wfData006MappingCode(t, err, workflow.CodeUnresolvedVariable)
	})

	t.Run("RED_wrong_shaped_field_is_a_type_mismatch", func(t *testing.T) {
		node := wfData006VariableNode(t)
		src := wfData006Source{variables: map[string]map[string]string{
			"proposal": {"grade": `42`},
		}}
		_, err := workflow.ResolveMappings(node, src)
		wfData006MappingCode(t, err, workflow.CodeTypeMismatch)
	})

	t.Run("GREEN_scalar_kinds_render_canonical_text", func(t *testing.T) {
		integer := workflow.ValueType{Kind: workflow.KindInteger}
		decimal := workflow.ValueType{Kind: workflow.KindDecimal}
		boolean := workflow.ValueType{Kind: workflow.KindBool}
		node := workflow.CompiledNode{ID: "n1", Mappings: []workflow.CompiledMapping{
			{Target: "count", TargetType: integer, SourceKind: workflow.SourceVariable, SourceVariable: "counters", SourcePath: "count", SourceType: integer},
			{Target: "ratio", TargetType: decimal, SourceKind: workflow.SourceVariable, SourceVariable: "counters", SourcePath: "ratio", SourceType: decimal},
			{Target: "flag", TargetType: boolean, SourceKind: workflow.SourceVariable, SourceVariable: "counters", SourcePath: "flag", SourceType: boolean},
		}}
		src := wfData006Source{variables: map[string]map[string]string{
			"counters": {"count": `7`, "ratio": `1.5000`, "flag": `true`},
		}}
		got, err := workflow.ResolveMappings(node, src)
		if err != nil {
			t.Fatalf("ResolveMappings: %v", err)
		}
		if got["count"].Text != "7" || got["ratio"].Text != `1.5000` || got["flag"].Text != "true" {
			t.Fatalf("scalar texts = %+v, want 7/1.5000/true", got)
		}
		if got["count"].Type.Kind != workflow.KindInteger {
			t.Fatalf("count type = %s, want INTEGER", got["count"].Type)
		}
	})
}
