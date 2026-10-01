package agentmodel

import (
	"encoding/json"
	"errors"
	"testing"
)

func validResultFixture() ModelResult {
	return ModelResult{
		ContractVersion:   ContractVersion,
		Text:              "Answer",
		Usage:             ModelUsage{InputTokens: 30, OutputTokens: 10, TotalTokens: 40, CostMicros: 5},
		Finish:            FinishComplete,
		Provider:          ModelIdentity{ProviderID: "provider-a", ModelID: "model-b", Version: "v1"},
		ProviderRequestID: "diagnostic-only-123",
	}
}

func resultCaps() AdapterCapabilities {
	return AdapterCapabilities{
		ContractVersions: []int{ContractVersion},
		Features:         []ModelFeature{FeatureTools, FeatureStructuredJSON, FeatureParallelTools},
		OutputModes:      []OutputMode{OutputText, OutputJSON, OutputSchema},
		MaxTools:         4,
	}
}

func assertValidModelResult(t *testing.T, req ModelRequest, result ModelResult, caps AdapterCapabilities) {
	t.Helper()
	if err := ValidateModelResult(req, result, caps); err != nil {
		t.Fatalf("ValidateModelResult() error = %v, want nil", err)
	}
}

func assertInvalidModelResult(t *testing.T, req ModelRequest, result ModelResult, caps AdapterCapabilities) {
	t.Helper()
	if err := ValidateModelResult(req, result, caps); !errors.Is(err, ErrInvalidModelResult) {
		t.Fatalf("ValidateModelResult() error = %v, want ErrInvalidModelResult", err)
	}
}

func TestTodo_AGENT_019_ResultValidation(t *testing.T) {
	req := contractRequest()
	textReq := req
	textReq.Output = OutputConstraint{Mode: OutputText}
	tests := []struct {
		name    string
		request ModelRequest
		result  ModelResult
	}{
		{name: "text completion", request: textReq, result: validResultFixture()},
		{name: "structured completion", request: req, result: func() ModelResult {
			got := validResultFixture()
			got.Text = `{"answer":"ok"}`
			got.Structured = json.RawMessage(got.Text)
			return got
		}()},
		{name: "tool proposal", request: req, result: func() ModelResult {
			got := validResultFixture()
			got.Text = ""
			got.ToolProposals = []ToolProposal{{ID: "call-1", Name: "search_policy", Arguments: json.RawMessage(`{"query":"leave"}`)}}
			got.Finish = FinishToolCalls
			return got
		}()},
		{name: "parallel proposals with declared capability", request: req, result: func() ModelResult {
			got := validResultFixture()
			got.Text = ""
			got.ToolProposals = []ToolProposal{
				{ID: "call-1", Name: "search_policy", Arguments: json.RawMessage(`{"query":"leave"}`)},
				{ID: "call-2", Name: "search_policy", Arguments: json.RawMessage(`{"query":"sick"}`)},
			}
			got.Finish = FinishToolCalls
			return got
		}()},
		{name: "content refusal", request: req, result: func() ModelResult {
			got := validResultFixture()
			got.Text = ""
			got.Finish = FinishBlocked
			got.Refusal = &ModelRefusal{Code: RefusalContentPolicy}
			return got
		}()},
		{name: "typed failure", request: req, result: func() ModelResult {
			got := validResultFixture()
			got.Text = ""
			got.Finish = ""
			got.Failure = &ModelFailure{Code: FailureUnavailable, Retryable: true}
			return got
		}()},
		{name: "cancellation", request: req, result: func() ModelResult {
			got := validResultFixture()
			got.Text = ""
			got.Finish = FinishCancelled
			return got
		}()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { assertValidModelResult(t, tc.request, tc.result, resultCaps()) })
	}
}

func TestTodo_AGENT_019_ResultValidationRejectsMalformedResults(t *testing.T) {
	baseRequest := contractRequest()
	baseResult := validResultFixture()
	tests := []struct {
		name   string
		mutate func(*ModelRequest, *ModelResult)
	}{
		{name: "unknown result version", mutate: func(_ *ModelRequest, r *ModelResult) { r.ContractVersion++ }},
		{name: "unknown finish", mutate: func(_ *ModelRequest, r *ModelResult) { r.Finish = "paused" }},
		{name: "empty finish", mutate: func(_ *ModelRequest, r *ModelResult) { r.Finish = "" }},
		{name: "negative input usage", mutate: func(_ *ModelRequest, r *ModelResult) { r.Usage.InputTokens = -1 }},
		{name: "negative output usage", mutate: func(_ *ModelRequest, r *ModelResult) { r.Usage.OutputTokens = -1 }},
		{name: "negative total usage", mutate: func(_ *ModelRequest, r *ModelResult) { r.Usage.TotalTokens = -1 }},
		{name: "negative cost usage", mutate: func(_ *ModelRequest, r *ModelResult) { r.Usage.CostMicros = -1 }},
		{name: "inconsistent total usage", mutate: func(_ *ModelRequest, r *ModelResult) { r.Usage.TotalTokens = 35 }},
		{name: "excess input usage", mutate: func(_ *ModelRequest, r *ModelResult) { r.Usage.InputTokens = 3001; r.Usage.TotalTokens = 3011 }},
		{name: "excess output usage", mutate: func(_ *ModelRequest, r *ModelResult) { r.Usage.OutputTokens = 501; r.Usage.TotalTokens = 531 }},
		{name: "excess cost usage", mutate: func(_ *ModelRequest, r *ModelResult) { r.Usage.CostMicros = 8001 }},
		{name: "missing provider identity", mutate: func(_ *ModelRequest, r *ModelResult) { r.Provider.ModelID = " " }},
		{name: "failure with text", mutate: func(_ *ModelRequest, r *ModelResult) { r.Failure = &ModelFailure{Code: FailureUnavailable} }},
		{name: "failure with finish", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Failure = &ModelFailure{Code: FailureUnavailable}
			r.Finish = FinishLength
		}},
		{name: "unknown failure code", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Failure = &ModelFailure{Code: "provider_error"}
			r.Finish = ""
			r.Text = ""
		}},
		{name: "unknown refusal code", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Refusal = &ModelRefusal{Code: "provider_refusal"}
			r.Finish = FinishBlocked
			r.Text = ""
		}},
		{name: "refusal with text", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Refusal = &ModelRefusal{Code: RefusalContentPolicy}
			r.Finish = FinishBlocked
		}},
		{name: "refusal without blocked finish", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Refusal = &ModelRefusal{Code: RefusalContentPolicy}
			r.Finish = FinishComplete
			r.Text = ""
		}},
		{name: "blocked without refusal", mutate: func(_ *ModelRequest, r *ModelResult) { r.Finish = FinishBlocked; r.Text = "" }},
		{name: "cancelled with text", mutate: func(_ *ModelRequest, r *ModelResult) { r.Finish = FinishCancelled }},
		{name: "complete empty", mutate: func(_ *ModelRequest, r *ModelResult) { r.Text = " " }},
		{name: "tool finish without proposal", mutate: func(_ *ModelRequest, r *ModelResult) { r.Finish = FinishToolCalls; r.Text = "" }},
		{name: "proposal with complete finish", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.ToolProposals = []ToolProposal{{ID: "call", Name: "search_policy", Arguments: json.RawMessage(`{}`)}}
		}},
		{name: "tool proposal with unknown name", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Text = ""
			r.Finish = FinishToolCalls
			r.ToolProposals = []ToolProposal{{ID: "call", Name: "unlisted", Arguments: json.RawMessage(`{}`)}}
		}},
		{name: "tool proposal with scalar args", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Text = ""
			r.Finish = FinishToolCalls
			r.ToolProposals = []ToolProposal{{ID: "call", Name: "search_policy", Arguments: json.RawMessage(`null`)}}
		}},
		{name: "tool proposal with malformed args", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Text = ""
			r.Finish = FinishToolCalls
			r.ToolProposals = []ToolProposal{{ID: "call", Name: "search_policy", Arguments: json.RawMessage(`{bad`)}}
		}},
		{name: "duplicate tool proposal IDs", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Text = ""
			r.Finish = FinishToolCalls
			r.ToolProposals = []ToolProposal{{ID: "same", Name: "search_policy", Arguments: json.RawMessage(`{}`)}, {ID: "same", Name: "search_policy", Arguments: json.RawMessage(`{}`)}}
		}},
		{name: "multiple proposals without parallel capability", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Text = ""
			r.Finish = FinishToolCalls
			r.ToolProposals = []ToolProposal{{ID: "one", Name: "search_policy", Arguments: json.RawMessage(`{}`)}, {ID: "two", Name: "search_policy", Arguments: json.RawMessage(`{}`)}}
		}},
		{name: "invalid structured bytes", mutate: func(_ *ModelRequest, r *ModelResult) { r.Text = ""; r.Structured = json.RawMessage(`{bad`) }},
		{name: "structured result for text request", mutate: func(q *ModelRequest, r *ModelResult) {
			q.Output = OutputConstraint{Mode: OutputText}
			r.Text = ""
			r.Structured = json.RawMessage(`{"answer":"ok"}`)
		}},
		{name: "invalid JSON text for structured request", mutate: func(_ *ModelRequest, r *ModelResult) { r.Text = "not json" }},
		{name: "text and structured outputs differ", mutate: func(_ *ModelRequest, r *ModelResult) {
			r.Text = `{"answer":"one"}`
			r.Structured = json.RawMessage(`{"answer":"two"}`)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, result := baseRequest, baseResult
			tc.mutate(&req, &result)
			caps := resultCaps()
			if tc.name == "multiple proposals without parallel capability" {
				caps.Features = []ModelFeature{FeatureTools, FeatureStructuredJSON}
			}
			assertInvalidModelResult(t, req, result, caps)
		})
	}
}

func TestTodo_AGENT_019_ResultValidationRefusesUnsupportedCapabilities(t *testing.T) {
	req := contractRequest()
	result := validResultFixture()
	tests := []struct {
		name string
		caps AdapterCapabilities
	}{
		{name: "unsupported structured output", caps: AdapterCapabilities{ContractVersions: []int{ContractVersion}, Features: []ModelFeature{FeatureTools}, OutputModes: []OutputMode{OutputSchema}, MaxTools: 4}},
		{name: "unsupported output mode", caps: AdapterCapabilities{ContractVersions: []int{ContractVersion}, Features: []ModelFeature{FeatureTools, FeatureStructuredJSON}, OutputModes: []OutputMode{OutputText}, MaxTools: 4}},
		{name: "unsupported contract", caps: AdapterCapabilities{ContractVersions: []int{2}, Features: []ModelFeature{FeatureTools, FeatureStructuredJSON}, OutputModes: []OutputMode{OutputSchema}, MaxTools: 4}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModelResult(req, result, tc.caps)
			if !errors.Is(err, ErrUnsupportedModelFeature) {
				t.Fatalf("ValidateModelResult() error = %v, want typed unsupported-feature refusal", err)
			}
		})
	}
	noTools := contractRequest()
	noTools.Tools = nil
	noTools.RequiredFeatures = []ModelFeature{FeatureStructuredJSON}
	result.Text = ""
	result.Finish = FinishToolCalls
	result.ToolProposals = []ToolProposal{{ID: "call", Name: "search_policy", Arguments: json.RawMessage(`{}`)}}
	caps := AdapterCapabilities{ContractVersions: []int{ContractVersion}, Features: []ModelFeature{FeatureStructuredJSON}, OutputModes: []OutputMode{OutputSchema}, MaxTools: 0}
	if err := ValidateModelResult(noTools, result, caps); !errors.Is(err, ErrInvalidModelResult) {
		t.Fatalf("tool output with no permitted tool set error = %v, want ErrInvalidModelResult", err)
	}
}
