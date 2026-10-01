package agentmodel

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func contractRequest() ModelRequest {
	return ModelRequest{
		ContractVersion:  ContractVersion,
		TaskProfile:      "policy-answer",
		ModelProfile:     "approved-balanced-v1",
		Messages:         []ModelMessage{{Role: RoleUser, Content: "Summarize the policy."}},
		ContextRefs:      []ContextReference{{ID: "doc-42", Version: "3", Digest: "sha256:abc"}},
		Tools:            []ToolSchema{{Name: "search_policy", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		Output:           OutputConstraint{Mode: OutputSchema, Schema: json.RawMessage(`{"type":"object","required":["answer"]}`)},
		Deadline:         time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
		Limits:           ModelLimits{MaxInputTokens: 3000, MaxOutputTokens: 500, MaxCostMicros: 8000},
		TraceID:          "trace-1",
		Processing:       ProcessingPolicy{Residency: "us", Retention: "zero", TrainingUse: UseDenied, Logging: UseDenied},
		RequiredFeatures: []ModelFeature{FeatureTools, FeatureStructuredJSON},
	}
}

func TestTodo_AGENT_019(t *testing.T) {
	req := contractRequest()
	if err := ValidateModelRequest(req); err != nil {
		t.Fatalf("ValidateModelRequest() error = %v", err)
	}
	result := ModelResult{
		ContractVersion:   ContractVersion,
		Text:              "Ready for review",
		ToolProposals:     []ToolProposal{{ID: "proposal-1", Name: "search_policy", Arguments: json.RawMessage(`{"query":"leave"}`)}},
		Usage:             ModelUsage{InputTokens: 50, OutputTokens: 12, TotalTokens: 62, CostMicros: 4},
		Finish:            FinishToolCalls,
		Provider:          ModelIdentity{ProviderID: "provider-a", ModelID: "model-b", Version: "2026-09"},
		ProviderRequestID: "request-987",
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal(ModelResult) error = %v", err)
	}
	var decoded ModelResult
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal(ModelResult) error = %v", err)
	}
	if !reflect.DeepEqual(decoded, result) {
		t.Fatalf("contract round trip = %#v, want %#v", decoded, result)
	}
}

func TestTodo_AGENT_019_Golden(t *testing.T) {
	got, err := json.Marshal(contractRequest())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"contract_version":1,"task_profile":"policy-answer","model_profile":"approved-balanced-v1","messages":[{"role":"user","content":"Summarize the policy."}],"context_refs":[{"id":"doc-42","version":"3","digest":"sha256:abc"}],"tools":[{"name":"search_policy","description":"","input_schema":{"type":"object"}}],"output":{"mode":"schema","schema":{"type":"object","required":["answer"]}},"deadline":"2026-09-29T12:00:00Z","limits":{"max_input_tokens":3000,"max_output_tokens":500,"max_cost_micros":8000},"trace_id":"trace-1","processing":{"residency":"us","retention":"zero","training_use":"denied","logging":"denied"},"required_features":["tools","structured_json"]}`
	if string(got) != want {
		t.Fatalf("request JSON = %s\nwant       = %s", got, want)
	}
}

func TestTodo_AGENT_019_Conformance(t *testing.T) {
	req := contractRequest()
	caps := AdapterCapabilities{
		ContractVersions: []int{ContractVersion},
		Features:         []ModelFeature{FeatureTools, FeatureStructuredJSON},
		OutputModes:      []OutputMode{OutputSchema},
		MaxTools:         2,
	}
	if refusal := CheckCapabilities(req, caps); refusal != nil {
		t.Fatalf("CheckCapabilities() refusal = %+v, want nil", refusal)
	}
	if !caps.Supports(FeatureTools) || caps.Supports(FeatureStreaming) {
		t.Fatalf("Supports() does not reflect advertised features")
	}
}

func TestValidateModelRequest_RequiresMatchingToolContinuation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		messages []ModelMessage
	}{
		{name: "valid pair", messages: []ModelMessage{{Role: RoleAssistant, ToolCallID: "call-1", ToolName: "documents.search", ToolArguments: json.RawMessage(`{"query":"leave"}`)}, {Role: RoleTool, ToolCallID: "call-1", Content: `{"hits":[]}`}}},
		{name: "orphan result", messages: []ModelMessage{{Role: RoleTool, ToolCallID: "call-1", Content: `{}`}}},
		{name: "mismatched result", messages: []ModelMessage{{Role: RoleAssistant, ToolCallID: "call-1", ToolName: "documents.search", ToolArguments: json.RawMessage(`{"query":"leave"}`)}, {Role: RoleTool, ToolCallID: "call-2", Content: `{}`}}},
		{name: "missing result", messages: []ModelMessage{{Role: RoleAssistant, ToolCallID: "call-1", ToolName: "documents.search", ToolArguments: json.RawMessage(`{"query":"leave"}`)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := contractRequest()
			req.Messages = tc.messages
			got := ValidateModelRequest(req)
			if (tc.name == "valid pair") != (got == nil) {
				t.Fatalf("ValidateModelRequest() error = %v", got)
			}
		})
	}
}

func TestTodo_AGENT_019_Fault(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ModelRequest)
	}{
		{name: "unknown_contract", mutate: func(r *ModelRequest) { r.ContractVersion++ }},
		{name: "empty_message", mutate: func(r *ModelRequest) { r.Messages[0].Content = " " }},
		{name: "invalid_schema", mutate: func(r *ModelRequest) { r.Output.Schema = json.RawMessage(`{broken`) }},
		{name: "duplicate_tool", mutate: func(r *ModelRequest) { r.Tools = append(r.Tools, r.Tools[0]) }},
		{name: "negative_cost", mutate: func(r *ModelRequest) { r.Limits.MaxCostMicros = -1 }},
		{name: "invalid_policy", mutate: func(r *ModelRequest) { r.Processing.Logging = "maybe" }},
		{name: "unspecified_policy", mutate: func(r *ModelRequest) { r.Processing.TrainingUse = UseUnspecified }},
		{name: "missing_residency", mutate: func(r *ModelRequest) { r.Processing.Residency = " " }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := contractRequest()
			tc.mutate(&req)
			if err := ValidateModelRequest(req); !errors.Is(err, ErrInvalidModelRequest) {
				t.Fatalf("ValidateModelRequest() error = %v, want ErrInvalidModelRequest", err)
			}
		})
	}
}

func TestTodo_AGENT_019_Integration(t *testing.T) {
	req := contractRequest()
	cases := []struct {
		name    string
		caps    AdapterCapabilities
		code    RefusalCode
		feature ModelFeature
	}{
		{name: "version", caps: AdapterCapabilities{ContractVersions: []int{2}, OutputModes: []OutputMode{OutputSchema}, MaxTools: 1}, code: RefusalContractVersion},
		{name: "output mode", caps: AdapterCapabilities{ContractVersions: []int{1}, OutputModes: []OutputMode{OutputText}, MaxTools: 1}, code: RefusalOutputMode},
		{name: "feature", caps: AdapterCapabilities{ContractVersions: []int{1}, Features: []ModelFeature{FeatureTools}, OutputModes: []OutputMode{OutputSchema}, MaxTools: 1}, code: RefusalFeature, feature: FeatureStructuredJSON},
		{name: "tool limit", caps: AdapterCapabilities{ContractVersions: []int{1}, Features: []ModelFeature{FeatureTools}, OutputModes: []OutputMode{OutputSchema}, MaxTools: 0}, code: RefusalToolLimit, feature: FeatureTools},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refusal := CheckCapabilities(req, tc.caps)
			if refusal == nil || refusal.Code != tc.code || refusal.Feature != tc.feature || !errors.Is(*refusal, ErrUnsupportedModelFeature) {
				t.Fatalf("CheckCapabilities() refusal = %+v, want code=%q feature=%q and typed error", refusal, tc.code, tc.feature)
			}
		})
	}
	adapter := contractTestAdapter{caps: cases[2].caps}
	_, err := adapter.Invoke(context.Background(), req)
	var refusal *ModelRefusal
	if !errors.As(err, &refusal) || refusal.Code != RefusalFeature || adapter.calls != 0 {
		t.Fatalf("unsupported invocation error=%v calls=%d, want typed refusal and zero calls", err, adapter.calls)
	}
}

type contractTestAdapter struct {
	caps  AdapterCapabilities
	calls int
}

func (a *contractTestAdapter) Capabilities() AdapterCapabilities { return a.caps }

func (a *contractTestAdapter) Invoke(_ context.Context, req ModelRequest) (ModelResult, error) {
	if err := ValidateModelRequest(req); err != nil {
		return ModelResult{}, err
	}
	if refusal := CheckCapabilities(req, a.Capabilities()); refusal != nil {
		return ModelResult{}, refusal
	}
	a.calls++
	return ModelResult{ContractVersion: ContractVersion, Finish: FinishComplete}, nil
}

func TestTodo_AGENT_019_SecurityIdentity(t *testing.T) {
	result := ModelResult{
		ContractVersion:   ContractVersion,
		Finish:            FinishComplete,
		Provider:          ModelIdentity{ProviderID: "p", ModelID: "m", Version: "v"},
		ProviderRequestID: "provider-session-should-not-own-run",
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"run_id", "conversation_id", "session_id", "transcript"} {
		if _, exists := fields[forbidden]; exists {
			t.Fatalf("result JSON contains provider-authoritative state field %q", forbidden)
		}
	}
	if result.ProviderRequestID == "" {
		t.Fatal("provider request ID is available as trace metadata")
	}
}
