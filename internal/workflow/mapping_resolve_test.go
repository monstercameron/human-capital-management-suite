package workflow

import "testing"

// fakeMappingSource is a minimal, in-memory [MappingSource] for exercising
// [ResolveMappings] without a SIMULATE run or a durable artifact store.
type fakeMappingSource struct {
	inputs  map[string]TypedValue
	ran     map[string]bool
	outputs map[string]map[string]TypedValue
	context map[string]map[string]TypedValue
}

func (f fakeMappingSource) WorkflowInput(path string) (TypedValue, bool) {
	v, ok := f.inputs[path]
	return v, ok
}

func (f fakeMappingSource) NodeOutput(nodeID, path string) (TypedValue, bool, bool) {
	if !f.ran[nodeID] {
		return TypedValue{}, false, false
	}
	v, ok := f.outputs[nodeID][path]
	return v, true, ok
}

func (f fakeMappingSource) Context(kind, path string) (TypedValue, bool) {
	v, ok := f.context[kind][path]
	return v, ok
}

func stringType() ValueType { return ValueType{Kind: KindString} }

func TestResolveMappings_WorkflowInput(t *testing.T) {
	node := CompiledNode{ID: "n1", Mappings: []CompiledMapping{
		{Target: "worker_id", TargetType: stringType(), SourceKind: SourceWorkflowInput, SourcePath: "worker_id"},
	}}
	src := fakeMappingSource{inputs: map[string]TypedValue{"worker_id": {Type: stringType(), Text: "w-1"}}}
	got, err := ResolveMappings(node, src)
	if err != nil {
		t.Fatalf("ResolveMappings: %v", err)
	}
	if got["worker_id"].Text != "w-1" {
		t.Fatalf("resolved worker_id = %+v, want text w-1", got["worker_id"])
	}
}

func TestResolveMappings_WorkflowInputMissing(t *testing.T) {
	node := CompiledNode{ID: "n1", Mappings: []CompiledMapping{
		{Target: "worker_id", TargetType: stringType(), SourceKind: SourceWorkflowInput, SourcePath: "worker_id"},
	}}
	_, err := ResolveMappings(node, fakeMappingSource{})
	assertMappingCode(t, err, CodeUnresolvedWorkflowInput)
}

func TestResolveMappings_NodeOutput(t *testing.T) {
	node := CompiledNode{ID: "apply", Mappings: []CompiledMapping{
		{Target: "score", TargetType: ValueType{Kind: KindDecimal}, SourceKind: SourceNodeOutput, SourceNode: "compute", SourcePath: "score"},
	}}
	src := fakeMappingSource{
		ran: map[string]bool{"compute": true},
		outputs: map[string]map[string]TypedValue{
			"compute": {"score": {Type: ValueType{Kind: KindDecimal}, Text: "1.5000"}},
		},
	}
	got, err := ResolveMappings(node, src)
	if err != nil {
		t.Fatalf("ResolveMappings: %v", err)
	}
	if got["score"].Text != "1.5000" {
		t.Fatalf("resolved score = %+v, want 1.5000", got["score"])
	}
}

func TestResolveMappings_NodeOutput_SourceNodeNotRun(t *testing.T) {
	node := CompiledNode{ID: "apply", Mappings: []CompiledMapping{
		{Target: "score", TargetType: ValueType{Kind: KindDecimal}, SourceKind: SourceNodeOutput, SourceNode: "compute", SourcePath: "score"},
	}}
	_, err := ResolveMappings(node, fakeMappingSource{})
	assertMappingCode(t, err, CodeSourceNodeNotRun)
}

func TestResolveMappings_NodeOutput_SourceFieldNotProduced(t *testing.T) {
	node := CompiledNode{ID: "apply", Mappings: []CompiledMapping{
		{Target: "score", TargetType: ValueType{Kind: KindDecimal}, SourceKind: SourceNodeOutput, SourceNode: "compute", SourcePath: "score"},
	}}
	src := fakeMappingSource{ran: map[string]bool{"compute": true}}
	_, err := ResolveMappings(node, src)
	assertMappingCode(t, err, CodeSourceFieldNotProduced)
}

func TestResolveMappings_Context(t *testing.T) {
	node := CompiledNode{ID: "n1", Mappings: []CompiledMapping{
		{Target: "grade", TargetType: stringType(), SourceKind: SourceContext, SourceCtx: "employment", SourcePath: "grade"},
	}}
	src := fakeMappingSource{context: map[string]map[string]TypedValue{"employment": {"grade": {Type: stringType(), Text: "E4"}}}}
	got, err := ResolveMappings(node, src)
	if err != nil {
		t.Fatalf("ResolveMappings: %v", err)
	}
	if got["grade"].Text != "E4" {
		t.Fatalf("resolved grade = %+v, want E4", got["grade"])
	}
}

func TestResolveMappings_ContextMissing(t *testing.T) {
	node := CompiledNode{ID: "n1", Mappings: []CompiledMapping{
		{Target: "grade", TargetType: stringType(), SourceKind: SourceContext, SourceCtx: "employment", SourcePath: "grade"},
	}}
	_, err := ResolveMappings(node, fakeMappingSource{})
	assertMappingCode(t, err, CodeUnresolvedContext)
}

func TestResolveMappings_Constant(t *testing.T) {
	node := CompiledNode{ID: "n1", Mappings: []CompiledMapping{
		{Target: "kind", TargetType: stringType(), SourceKind: SourceConstant, Constant: "PROMOTION"},
	}}
	got, err := ResolveMappings(node, fakeMappingSource{})
	if err != nil {
		t.Fatalf("ResolveMappings: %v", err)
	}
	if got["kind"].Text != "PROMOTION" {
		t.Fatalf("resolved kind = %+v, want PROMOTION", got["kind"])
	}
}

func TestResolveMappings_UnknownSourceKind(t *testing.T) {
	node := CompiledNode{ID: "n1", Mappings: []CompiledMapping{
		{Target: "x", TargetType: stringType(), SourceKind: SourceKind("BOGUS")},
	}}
	_, err := ResolveMappings(node, fakeMappingSource{})
	assertMappingCode(t, err, CodeUnknownSourceKind)
}

func TestResolveMappings_TypeMismatch(t *testing.T) {
	node := CompiledNode{ID: "n1", Mappings: []CompiledMapping{
		{Target: "x", TargetType: ValueType{Kind: KindDecimal}, SourceKind: SourceWorkflowInput, SourcePath: "x"},
	}}
	src := fakeMappingSource{inputs: map[string]TypedValue{"x": {Type: stringType(), Text: "not-a-decimal"}}}
	_, err := ResolveMappings(node, src)
	assertMappingCode(t, err, CodeTypeMismatch)
}

func TestResolveMappings_MultipleMappings(t *testing.T) {
	node := CompiledNode{ID: "apply", Mappings: []CompiledMapping{
		{Target: "score", TargetType: ValueType{Kind: KindDecimal}, SourceKind: SourceNodeOutput, SourceNode: "compute", SourcePath: "score"},
		{Target: "worker_id", TargetType: stringType(), SourceKind: SourceWorkflowInput, SourcePath: "worker_id"},
		{Target: "kind", TargetType: stringType(), SourceKind: SourceConstant, Constant: "PROMOTION"},
	}}
	src := fakeMappingSource{
		inputs: map[string]TypedValue{"worker_id": {Type: stringType(), Text: "w-1"}},
		ran:    map[string]bool{"compute": true},
		outputs: map[string]map[string]TypedValue{
			"compute": {"score": {Type: ValueType{Kind: KindDecimal}, Text: "2.0000"}},
		},
	}
	got, err := ResolveMappings(node, src)
	if err != nil {
		t.Fatalf("ResolveMappings: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("resolved %d values, want 3: %+v", len(got), got)
	}
}

func assertMappingCode(t *testing.T, err error, want string) {
	t.Helper()
	var merr *MappingError
	if !errorsAsMappingError(err, &merr) {
		t.Fatalf("error = %v, want a *MappingError with code %s", err, want)
	}
	if merr.Code != want {
		t.Fatalf("error code = %s, want %s (%v)", merr.Code, want, err)
	}
}

func errorsAsMappingError(err error, target **MappingError) bool {
	if e, ok := err.(*MappingError); ok {
		*target = e
		return true
	}
	return false
}
