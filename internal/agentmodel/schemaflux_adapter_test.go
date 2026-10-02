package agentmodel

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type typedAdapterFixture struct {
	body    string
	calls   int
	request ModelRequest
	fail    bool
	refuse  bool
}

func (*typedAdapterFixture) Identity() ModelIdentity {
	return ModelIdentity{ProviderID: "openai", ModelID: "gpt-5", Version: "2025-08-07"}
}
func (*typedAdapterFixture) Capabilities() AdapterCapabilities {
	return AdapterCapabilities{ContractVersions: []int{ContractVersion}, Features: []ModelFeature{FeatureTools, FeatureStructuredJSON}, OutputModes: []OutputMode{OutputText, OutputSchema}, MaxTools: 128}
}
func (p *typedAdapterFixture) Invoke(_ context.Context, req ModelRequest) (ModelResult, error) {
	p.calls++
	p.request = req
	if p.refuse {
		return ModelResult{ContractVersion: ContractVersion, Provider: p.Identity(), Finish: FinishBlocked, Refusal: &ModelRefusal{Code: RefusalContentPolicy}, Usage: ModelUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30}}, nil
	}
	if p.fail {
		return ModelResult{ContractVersion: ContractVersion, Provider: p.Identity(), Failure: &ModelFailure{Code: FailureUnavailable, Retryable: true, Message: "provider secret must not escape"}}, errors.New("provider secret must not escape")
	}
	return ModelResult{ContractVersion: ContractVersion, Provider: p.Identity(), Finish: FinishComplete, Text: p.body, Structured: json.RawMessage(p.body), Usage: ModelUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30}}, nil
}
func typedAdapterTestFixture(t *testing.T, body string) (*SchemaFluxAdapter, *typedAdapterFixture, ModelRequest) {
	t.Helper()
	provider := &typedAdapterFixture{body: body}
	selection := ModelSelection{ProfileID: "profile", ProfileDigest: strings.Repeat("a", 64), Identity: provider.Identity()}
	pricing, err := NewPricingSchedule(PricingSchedule{Version: "rates-v1", Authority: "test-admin", Signature: "test-verified-signature", Entries: []PricingEntry{{Identity: provider.Identity(), InputMicrosPerToken: 2, OutputMicrosPerToken: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewSchemaFluxAdapter(provider, selection, pricing)
	if err != nil {
		t.Fatal(err)
	}
	req := contractRequest()
	req.ModelProfile = "profile"
	req.Output = OutputConstraint{Mode: OutputText}
	req.RequiredFeatures = nil
	req.Deadline = time.Now().Add(time.Minute)
	return adapter, provider, req
}
func TestTodo_AGENT2_026_OpenAI_TypedReply(t *testing.T) {
	adapter, provider, request := typedAdapterTestFixture(t, `{"text":"A useful answer","tool_proposals":[]}`)
	result, err := adapter.Invoke(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "A useful answer" || result.Usage.CostMicros != 120 || result.Finish != FinishComplete || provider.calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, provider.calls)
	}
	if provider.request.Output.Mode != OutputSchema || !strings.Contains(string(provider.request.Output.Schema), "tool_proposals") || len(provider.request.Tools) != 0 {
		t.Fatalf("request did not use typed output: %+v", provider.request.Output)
	}
	if !reflect.DeepEqual(provider.request.Messages[0], request.Messages[0]) {
		t.Fatal("classified input changed")
	}
}

func TestTodo_AGENTUX_PATH_T1_PersonaInstructionsMatchTurnTools(t *testing.T) {
	for _, tc := range []struct {
		name       string
		withTools  bool
		want       string
		mustNotSay string
	}{
		{name: "answer turn", want: "No tools are available in this turn", mustNotSay: "propose exactly one available document search"},
		{name: "search turn", withTools: true, want: "propose exactly one available document search", mustNotSay: "No tools are available in this turn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter, provider, request := typedAdapterTestFixture(t, `{"text":"The Paid time off policy says carryover is limited.","tool_proposals":[],"requested_actions":["read_policy"]}`)
			request.ActionPolicy = &RequestedActionPolicy{ProfileDigest: "sha256:" + strings.Repeat("a", 64), Allowed: []RequestedAction{ActionReadPolicy}}
			request.Tools = nil
			if tc.withTools {
				request.Tools = []ToolSchema{{Name: "search_policy", Description: "Search policy documents", InputSchema: json.RawMessage(`{"type":"object"}`)}}
			}
			if _, err := adapter.Invoke(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			instruction := provider.request.Messages[len(provider.request.Messages)-1].Content
			if !strings.Contains(instruction, tc.want) || strings.Contains(instruction, tc.mustNotSay) {
				t.Fatalf("instruction does not describe the turn tools: %q", instruction)
			}
			for _, required := range []string{"plain text", "no links", "[[1]]", "actually used", "numbered list", "how many documents", "semantic version", "no line beginning with Source"} {
				if !strings.Contains(instruction, required) {
					t.Errorf("instruction missing %q: %q", required, instruction)
				}
			}
		})
	}
}
func TestTodo_AGENT2_026_OpenAI_ToolProposals(t *testing.T) {
	adapter, provider, request := typedAdapterTestFixture(t, `{"text":"","tool_proposals":[{"id":"call-1","name":"search_policy","arguments_json":"{\"query\":\"leave\"}"}]}`)
	result, err := adapter.Invoke(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Finish != FinishToolCalls || len(result.ToolProposals) != 1 || string(result.ToolProposals[0].Arguments) != `{"query":"leave"}` || provider.calls != 1 {
		t.Fatalf("proposals=%+v", result)
	}
	if !strings.Contains(provider.request.Messages[len(provider.request.Messages)-1].Content, "search_policy") {
		t.Fatal("owned tool definitions were absent")
	}
}
func TestTodo_AGENT2_026_OpenAI_Security(t *testing.T) {
	for _, body := range []string{`{"text":"","tool_proposals":[{"id":"call-1","name":"delete_payroll","arguments_json":"{}"}]}`, `{"text":123,"tool_proposals":[]}`, `{"text":"hello"}`} {
		adapter, provider, request := typedAdapterTestFixture(t, body)
		result, err := adapter.Invoke(context.Background(), request)
		if err == nil || result.Text != "" || len(result.ToolProposals) != 0 || provider.calls != 1 {
			t.Fatalf("unvalidated output escaped: %+v err=%v calls=%d", result, err, provider.calls)
		}
	}
}
func TestTodo_AGENT2_026_OpenAI_Fault(t *testing.T) {
	adapter, provider, request := typedAdapterTestFixture(t, "")
	provider.fail = true
	result, err := adapter.Invoke(context.Background(), request)
	if err == nil || strings.Contains(err.Error(), "secret") || provider.calls != 1 || result.Failure == nil || result.Failure.Code != FailureUnavailable {
		t.Fatalf("failure=%+v err=%v calls=%d", result, err, provider.calls)
	}
	if _, err := NewSchemaFluxAdapter(provider, ModelSelection{}, nil); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("empty config=%v", err)
	}
}

func TestTodo_AGENT2_026_OpenAI_Cancellation(t *testing.T) {
	adapter, provider, request := typedAdapterTestFixture(t, `{"text":"hello","tool_proposals":[]}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := adapter.Invoke(ctx, request)
	if !errors.Is(err, context.Canceled) || result.Finish != FinishCancelled || result.Provider != adapter.Identity() || provider.calls != 0 {
		t.Fatalf("cancelled result=%+v err=%v calls=%d", result, err, provider.calls)
	}
}

func TestTodo_AGENT_024_TypedProviderRefusalIsPriced(t *testing.T) {
	adapter, provider, request := typedAdapterTestFixture(t, "")
	provider.refuse = true
	result, err := adapter.Invoke(context.Background(), request)
	if err != nil || result.Refusal == nil || result.Finish != FinishBlocked || result.Usage.CostMicros != 120 || provider.calls != 1 {
		t.Fatalf("refusal=%+v err=%v calls=%d", result, err, provider.calls)
	}
}
