package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// WF-EXT-004: one mapping resolver shared by the SIMULATE interpreter
// (internal/workflow/simulate) and the durable EXECUTE path
// (internal/workflow/execute). Both walk the same [CompiledMapping] list a
// node's compiled plan declares; only where the typed values live at
// resolution time differs -- an in-memory bag for SIMULATE, durable input and
// output artifacts for EXECUTE -- and [MappingSource] is exactly that seam.

// TypedValue is one typed value flowing through a resolved mapping, carried
// as its canonical text rather than an `any`: a MONEY value names its own
// currency in its own text, so a mapping can never coerce a value into a
// type its declared source never claimed. internal/workflow/simulate.Value
// holds the identical shape for the SIMULATE walk; this is the durable
// path's own copy so that package does not import simulate.
type TypedValue struct {
	Type ValueType `json:"type"`
	Text string    `json:"text"`
}

// TypedOutput is one typed value a node attempt produced, or a workflow
// input document supplies, named by its declared field path.
type TypedOutput struct {
	Path  string     `json:"path"`
	Value TypedValue `json:"value"`
}

// OutputDocument wraps the typed values a step runner reports it produced
// (WF-EXT-004), so [frontier.NodeOutcome] can carry them behind a pointer
// field ([]TypedOutput alone would make NodeOutcome uncomparable with ==,
// which existing callers rely on) while keeping the same (Path, Value) shape
// a workflow-input document and a node-output artifact both use.
type OutputDocument struct {
	Values []TypedOutput
}

// MappingSource answers the four questions a [CompiledMapping] can ask:
// the workflow's own declared input document, a predecessor node's declared
// output, a pinned context snapshot, and a workflow variable field. It never
// resolves a CONSTANT mapping -- a constant carries its own value in the
// compiled plan and needs no source at all.
//
// A NODE_OUTPUT lookup distinguishes "the node has not produced this output"
// from "the node has not run at all": nodeProduced is false only for the
// latter, so [ResolveMappings] can tell [CodeSourceNodeNotRun] apart from
// [CodeSourceFieldNotProduced].
//
// A VARIABLE lookup returns the field's canonical JSON text, not a
// [TypedValue]: variable revisions store untyped JSON, and the mapping's
// declared source type is what types it -- exactly as a CONSTANT pairs its
// carried text with a plan type. The lookup reads the revision current when
// the attempt's inputs resolved; a revision written later is invisible to it.
type MappingSource interface {
	WorkflowInput(path string) (TypedValue, bool)
	NodeOutput(nodeID, path string) (value TypedValue, nodeProduced bool, ok bool)
	Context(kind, path string) (TypedValue, bool)
	Variable(name, path string) (raw string, ok bool)
}

// Stable mapping-resolution refusal codes. They are runtime codes, not
// compile diagnostics: a definition that reaches [ResolveMappings] at all
// has already passed [checkMappings], so these report a source that resolved
// to nothing (or something the wrong shape) at the exact attempt the mapping
// was evaluated, never a structural defect in the plan itself.
const (
	// CodeUnresolvedWorkflowInput reports a WORKFLOW_INPUT mapping reading a
	// path the run's input document does not carry.
	CodeUnresolvedWorkflowInput = "UNRESOLVED_WORKFLOW_INPUT"
	// CodeSourceNodeNotRun reports a NODE_OUTPUT mapping reading a node that
	// has not produced any output at all.
	CodeSourceNodeNotRun = "SOURCE_NODE_NOT_RUN"
	// CodeSourceFieldNotProduced reports a NODE_OUTPUT mapping reading a
	// field its source node ran but did not produce.
	CodeSourceFieldNotProduced = "SOURCE_FIELD_NOT_PRODUCED"
	// CodeUnresolvedContext reports a CONTEXT mapping reading a snapshot or
	// field path the run was not given.
	CodeUnresolvedContext = "UNRESOLVED_CONTEXT"
	// CodeUnresolvedVariable reports a VARIABLE mapping reading a variable
	// with no revision current at the attempt, or a field path that revision
	// does not carry.
	CodeUnresolvedVariable = "UNRESOLVED_VARIABLE"
	// CodeUnknownSourceKind reports a compiled mapping whose source kind this
	// resolver does not implement. [checkMappings] already refuses this at
	// compile time; a plan that reaches here is fabricated or corrupted.
	CodeUnknownSourceKind = "UNKNOWN_SOURCE_KIND"
)

// MappingError is one runtime mapping-resolution refusal: a stable code, the
// node and target field it happened at, and a human-readable detail.
type MappingError struct {
	Code   string
	NodeID string
	Target string
	Detail string
}

func (e *MappingError) Error() string {
	return fmt.Sprintf("%s [node=%s field=%s]: %s", e.Code, e.NodeID, e.Target, e.Detail)
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *MappingError) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func mappingErrorf(code, nodeID, target, format string, args ...any) *MappingError {
	return &MappingError{Code: code, NodeID: nodeID, Target: target, Detail: fmt.Sprintf(format, args...)}
}

// ResolveMappings turns node's compiled mappings into the typed values its
// step runner receives, reading every non-constant source through src.
//
// It is the one resolver [internal/workflow/simulate] and the EXECUTE driver
// (internal/workflow/execute) both call: SIMULATE adapts its in-memory node
// output bags and supplied inputs to [MappingSource], and EXECUTE adapts
// durable workflow-input and node-output artifacts the same way, so a
// mapping resolves identically -- same refusal, same code -- on both paths.
func ResolveMappings(node CompiledNode, src MappingSource) (map[string]TypedValue, error) {
	out := make(map[string]TypedValue, len(node.Mappings))
	for _, m := range node.Mappings {
		v, err := resolveOneMapping(node.ID, m, src)
		if err != nil {
			return nil, err
		}
		out[m.Target] = v
	}
	return out, nil
}

func resolveOneMapping(nodeID string, m CompiledMapping, src MappingSource) (TypedValue, error) {
	var v TypedValue
	switch m.SourceKind {
	case SourceWorkflowInput:
		found, ok := src.WorkflowInput(m.SourcePath)
		if !ok {
			return TypedValue{}, mappingErrorf(CodeUnresolvedWorkflowInput, nodeID, m.Target,
				"mapping reads workflow input %q, which was not supplied", m.SourcePath)
		}
		v = found
	case SourceNodeOutput:
		found, nodeProduced, ok := src.NodeOutput(m.SourceNode, m.SourcePath)
		if !nodeProduced {
			return TypedValue{}, mappingErrorf(CodeSourceNodeNotRun, nodeID, m.Target,
				"mapping reads node %q, which has not run", m.SourceNode)
		}
		if !ok {
			return TypedValue{}, mappingErrorf(CodeSourceFieldNotProduced, nodeID, m.Target,
				"mapping reads %s.%s, which that node did not produce", m.SourceNode, m.SourcePath)
		}
		v = found
	case SourceContext:
		found, ok := src.Context(m.SourceCtx, m.SourcePath)
		if !ok {
			return TypedValue{}, mappingErrorf(CodeUnresolvedContext, nodeID, m.Target,
				"mapping reads context %s.%s, which was not supplied", m.SourceCtx, m.SourcePath)
		}
		v = found
	case SourceVariable:
		raw, ok := src.Variable(m.SourceVariable, m.SourcePath)
		if !ok {
			return TypedValue{}, mappingErrorf(CodeUnresolvedVariable, nodeID, m.Target,
				"mapping reads variable %q field %q, which has no revision current at this attempt",
				m.SourceVariable, m.SourcePath)
		}
		text, ok := variableFieldText(raw, m.SourceType)
		if !ok {
			return TypedValue{}, mappingErrorf(CodeTypeMismatch, nodeID, m.Target,
				"variable %q field %q does not carry %s", m.SourceVariable, m.SourcePath, m.SourceType)
		}
		v = TypedValue{Type: m.SourceType, Text: text}
	case SourceConstant:
		v = TypedValue{Type: m.TargetType, Text: m.Constant}
	default:
		return TypedValue{}, mappingErrorf(CodeUnknownSourceKind, nodeID, m.Target,
			"mapping declares source kind %q", string(m.SourceKind))
	}
	if err := v.Type.AssignableTo(m.TargetType); err != nil {
		return TypedValue{}, mappingErrorf(CodeTypeMismatch, nodeID, m.Target,
			"%s -> %s: %v", v.Type, m.TargetType, err)
	}
	return v, nil
}

// variableFieldText renders a variable field's canonical JSON text as the
// canonical text of its declared source type: strings verbatim, numbers and
// booleans in their JSON spelling, messages and lists as compact JSON. A
// field whose JSON shape the type cannot carry -- a number for a STRING, an
// explicit null, trailing bytes -- is not a value of that type.
func variableFieldText(raw string, t ValueType) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	if dec.More() {
		return "", false
	}
	switch t.Kind {
	case KindString, KindInstant, KindLocalDate, KindEnum:
		s, ok := v.(string)
		return s, ok
	case KindMoney:
		// Money text names its own currency, so a money field is stored as
		// a JSON string already in canonical text form ("USD 12.50").
		s, ok := v.(string)
		return s, ok
	case KindInteger:
		n, ok := v.(json.Number)
		if !ok || strings.ContainsAny(string(n), ".eE") {
			return "", false
		}
		return string(n), true
	case KindDecimal:
		n, ok := v.(json.Number)
		if !ok {
			return "", false
		}
		return string(n), true
	case KindBool:
		b, ok := v.(bool)
		if !ok {
			return "", false
		}
		if b {
			return "true", true
		}
		return "false", true
	case KindMessage:
		obj, ok := v.(map[string]any)
		if !ok {
			return "", false
		}
		return marshalVariableField(obj)
	case KindList:
		arr, ok := v.([]any)
		if !ok {
			return "", false
		}
		return marshalVariableField(arr)
	default:
		return "", false
	}
}

// marshalVariableField renders a composite field as compact JSON. Like the
// revision store's own canonicalization, object keys sort and numbers keep
// their stored spelling.
func marshalVariableField(v any) (string, bool) {
	rendered, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(rendered), true
}
