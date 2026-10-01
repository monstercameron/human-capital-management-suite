package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

func openAIRequest() agentmodel.ModelRequest {
	return agentmodel.ModelRequest{
		ContractVersion: agentmodel.ContractVersion,
		TaskProfile:     "policy-answer", ModelProfile: "approved-gpt-profile",
		Messages:    []agentmodel.ModelMessage{{Role: agentmodel.RoleDeveloper, Content: "Answer from approved context."}, {Role: agentmodel.RoleUser, Content: "What is the leave policy?"}},
		ContextRefs: []agentmodel.ContextReference{{ID: "doc-7", Version: "2", Digest: "sha256:abc"}},
		Tools:       []agentmodel.ToolSchema{{Name: "lookup_policy", Description: "Look up a policy", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)}},
		Output:      agentmodel.OutputConstraint{Mode: agentmodel.OutputSchema, Schema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`)},
		Deadline:    time.Now().Add(time.Minute), Limits: agentmodel.ModelLimits{MaxOutputTokens: 300}, TraceID: "trace-stable-42",
		Processing:       agentmodel.ProcessingPolicy{Residency: "us", Retention: "zero", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied},
		RequiredFeatures: []agentmodel.ModelFeature{agentmodel.FeatureTools, agentmodel.FeatureStructuredJSON},
	}
}

func newTestAdapter(t *testing.T, server *httptest.Server) *Adapter {
	t.Helper()
	adapter, err := New(Config{
		APIKey: "test-secret-never-log", ModelProfile: "approved-gpt-profile",
		Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-test", Version: "gpt-test-2026-09"},
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return adapter
}

func TestTodo_AGENT_021_PinnedSnapshotOnWire(t *testing.T) {
	const snapshot = "gpt-5-mini-2025-08-07"
	calls := 0
	responseModel := snapshot
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != snapshot {
			t.Error("snapshot pin absent from provider request")
		}
		if r.URL.Path == "/responses/input_tokens" {
			io.WriteString(w, `{"object":"response.input_tokens","input_tokens":20}`)
			return
		}
		calls++
		io.WriteString(w, fmt.Sprintf(`{"id":"snapshot-response","model":%q,"status":"completed","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30},"output":[{"type":"message","content":[{"type":"output_text","text":"{\"answer\":\"approved\"}"}]}]}`, responseModel))
	}))
	defer server.Close()
	adapter, err := New(Config{APIKey: "test-only-key", ModelProfile: "approved-gpt-profile", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-5-mini", Version: snapshot}, BaseURL: server.URL, PreflightInputTokens: true})
	if err != nil {
		t.Fatal(err)
	}
	req := openAIRequest()
	req.Limits.MaxInputTokens = 100
	if result, err := adapter.Invoke(context.Background(), req); err != nil || result.Provider.Version != snapshot || calls != 1 {
		t.Fatalf("snapshot call=%+v,%v calls=%d", result, err, calls)
	}
	responseModel = "gpt-5-mini"
	if _, err := adapter.Invoke(context.Background(), req); !errors.Is(err, ErrInvalidResponse) || calls != 2 {
		t.Fatalf("unpinned response accepted: %v", err)
	}
}

func responseJSON(id, status string, output any) string {
	return fmt.Sprintf(`{"id":%q,"model":"gpt-test","status":%q,"usage":{"input_tokens":23,"output_tokens":9,"total_tokens":32},"output":%s}`, id, status, mustJSON(output))
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func TestTodo_AGENT_021(t *testing.T) {
	var received responseRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret-never-log" {
			t.Errorf("missing bearer authorization")
		}
		if r.Header.Get("X-Client-Request-Id") == "" {
			t.Errorf("stable request identity header is empty")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, responseJSON("resp-1", "completed", []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": `{"answer":"The leave policy allows paid leave."}`}}}}))
	}))
	defer server.Close()
	adapter := newTestAdapter(t, server)
	if got := adapter.Identity(); got.ProviderID != "openai" || got.ModelID != "gpt-test" || got.Version != "gpt-test-2026-09" {
		t.Fatalf("Identity() = %+v", got)
	}
	result, err := adapter.Invoke(context.Background(), openAIRequest())
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if received.Model != "gpt-test" || received.ServiceTier != "default" || received.Store || received.MaxOutputTokens != 300 || received.ToolChoice != "auto" || received.ParallelToolCalls {
		t.Fatalf("request controls = %+v", received)
	}
	if received.Text == nil || received.Text.Format.Type != "json_schema" || !received.Text.Format.Strict || received.Text.Format.Name != "hcm_output" {
		t.Fatalf("structured output format = %+v", received.Text)
	}
	if len(received.Input) != 2 || received.Input[0].Role != "developer" || len(received.Tools) != 1 || received.Tools[0].Type != "function" {
		t.Fatalf("mapped input/tools = %+v / %+v", received.Input, received.Tools)
	}
	if strings.Contains(string(mustJSON(received)), "doc-7") {
		t.Fatal("HCM context reference was sent as provider state")
	}
	if result.Text != `{"answer":"The leave policy allows paid leave."}` || string(result.Structured) != result.Text || result.ProviderRequestID != "resp-1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.InputTokens != 23 || result.Usage.OutputTokens != 9 || result.Usage.TotalTokens != 32 || result.Finish != agentmodel.FinishComplete {
		t.Fatalf("usage/finish = %+v / %s", result.Usage, result.Finish)
	}
}

func TestAdapter_MapsInvocationToolContinuation(t *testing.T) {
	req := openAIRequest()
	req.Messages = append(req.Messages,
		agentmodel.ModelMessage{Role: agentmodel.RoleAssistant, ToolCallID: "call-a", ToolName: "documents.search", ToolArguments: json.RawMessage(`{"query":"leave"}`)},
		agentmodel.ModelMessage{Role: agentmodel.RoleTool, ToolCallID: "call-a", Content: `{"hits":[{"document_id":"doc-1"}]}`},
	)
	if err := agentmodel.ValidateModelRequest(req); err != nil {
		t.Fatalf("ValidateModelRequest() error = %v", err)
	}
	mapped := makeRequest("gpt-test", req)
	if len(mapped.Input) != 4 || mapped.Input[2].Type != "function_call" || mapped.Input[2].CallID != "call-a" || mapped.Input[2].Name != "documents.search" || mapped.Input[2].Arguments != `{"query":"leave"}` || mapped.Input[3].Type != "function_call_output" || mapped.Input[3].CallID != "call-a" || mapped.Input[3].Output != `{"hits":[{"document_id":"doc-1"}]}` {
		t.Fatalf("tool continuation was not mapped with matching provider call IDs: %+v", mapped.Input)
	}
}

func TestResponseMapping(t *testing.T) {
	identity := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-test", Version: "gpt-test-2026-09"}
	request := openAIRequest()
	tests := []struct {
		name        string
		status      string
		reason      string
		output      []responseItem
		wantFinish  agentmodel.FinishReason
		wantError   error
		wantRefusal agentmodel.RefusalCode
	}{
		{name: "provider_refusal", status: "completed", output: []responseItem{{Type: "message", Content: []responseContent{{Type: "refusal", Text: "policy text stays private"}}}}, wantFinish: agentmodel.FinishBlocked, wantRefusal: agentmodel.RefusalContentPolicy},
		{name: "max_output_incomplete", status: "incomplete", reason: "max_output_tokens", output: []responseItem{{Type: "message", Content: []responseContent{{Type: "output_text", Text: `{"partial":true}`}}}, {Type: "function_call", CallID: "partial-call", Name: "lookup_policy", Arguments: `{"query":"partial"}`}}, wantFinish: agentmodel.FinishLength},
		{name: "provider_cancelled", status: "cancelled", wantFinish: agentmodel.FinishCancelled, wantError: context.Canceled},
		{name: "provider_failed", status: "failed", wantError: ErrInvalidResponse},
		{name: "unknown_status", status: "queued", wantError: ErrInvalidResponse},
		{name: "unexpected_model", status: "completed", output: []responseItem{{Type: "message", Content: []responseContent{{Type: "output_text", Text: "hello"}}}}, wantError: ErrInvalidResponse},
		{name: "reasoning_is_withheld", status: "completed", output: []responseItem{{Type: "reasoning", Content: []responseContent{{Type: "reasoning_text", Text: "private reasoning"}}}, {Type: "message", Content: []responseContent{{Type: "output_text", Text: "hello"}}}}, wantFinish: agentmodel.FinishComplete},
		{name: "unsupported_content", status: "completed", output: []responseItem{{Type: "message", Content: []responseContent{{Type: "output_image"}}}}, wantError: ErrInvalidResponse},
		{name: "empty_completed", status: "completed", wantError: ErrInvalidResponse},
		{name: "incomplete_unknown_reason", status: "incomplete", reason: "content_filter", wantError: ErrInvalidResponse},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "reasoning_is_withheld" {
				request.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
			}
			response := responseEnvelope{ID: "resp-matrix", Model: identity.ModelID, Status: tc.status, Usage: &responseUsage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}, Output: tc.output}
			if tc.reason != "" {
				response.IncompleteDetails = &struct {
					Reason string `json:"reason"`
				}{Reason: tc.reason}
			}
			if tc.name == "unexpected_model" {
				response.Model = "gpt-other"
			}
			got, err := mapResponse(identity, response, request)
			if tc.wantError != nil {
				if err == nil {
					t.Fatalf("mapResponse() error = %v, want %v", err, tc.wantError)
				}
				if errors.Is(tc.wantError, context.Canceled) && !errors.Is(err, context.Canceled) {
					t.Fatalf("mapResponse() error = %v, want cancellation", err)
				}
				return
			}
			if err != nil || got.Finish != tc.wantFinish {
				t.Fatalf("mapResponse() result=%+v error=%v", got, err)
			}
			if tc.wantRefusal != "" && (got.Refusal == nil || got.Refusal.Code != tc.wantRefusal || got.Text != "") {
				t.Fatalf("refusal result = %+v", got)
			}
			if tc.name == "max_output_incomplete" && (got.Text != "" || len(got.ToolProposals) != 0) {
				t.Fatalf("incomplete provider output escaped: %+v", got)
			}
		})
	}
}

func TestDecodeResponse_RejectsMissingOrTrailingData(t *testing.T) {
	for _, body := range []string{
		`{"id":"resp-1","model":"gpt-test","status":"completed","output":[]}`,
		`{"id":"resp-1","model":"gpt-test","status":"completed","usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1},"output":[]} trailing`,
	} {
		if _, err := decodeResponse([]byte(body)); err == nil {
			t.Fatalf("decodeResponse(%q) accepted malformed envelope", body)
		}
	}
}

func TestInvoke_RejectsBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	adapter := newTestAdapter(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Invoke(ctx, openAIRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Invoke() error = %v", err)
	}
	wrongProfile := openAIRequest()
	wrongProfile.ModelProfile = "other-profile"
	if _, err := adapter.Invoke(context.Background(), wrongProfile); err == nil {
		t.Fatal("profile mismatch was accepted")
	}
	invalid := openAIRequest()
	invalid.Messages[0].Content = " "
	if _, err := adapter.Invoke(context.Background(), invalid); !errors.Is(err, agentmodel.ErrInvalidModelRequest) {
		t.Fatalf("invalid Invoke() error = %v", err)
	}
	var nilAdapter *Adapter
	if _, err := nilAdapter.Invoke(context.Background(), openAIRequest()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil adapter error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("network calls = %d, want zero", calls.Load())
	}
}

func TestRequestMappingModes(t *testing.T) {
	request := openAIRequest()
	request.Limits.MaxOutputTokens = 0
	request.Tools = nil
	request.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
	textRequest := makeRequest("gpt-test", request)
	if textRequest.Text != nil || textRequest.MaxOutputTokens != 0 || len(textRequest.Tools) != 0 || textRequest.ToolChoice != "" {
		t.Fatalf("text request mapping = %+v", textRequest)
	}
	request.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputJSON}
	jsonRequest := makeRequest("gpt-test", request)
	if jsonRequest.Text == nil || jsonRequest.Text.Format.Type != "json_object" {
		t.Fatalf("JSON request format = %+v", jsonRequest.Text)
	}
	var nilAdapter *Adapter
	if got := nilAdapter.Identity(); got != (agentmodel.ModelIdentity{}) {
		t.Fatalf("nil adapter identity = %+v", got)
	}
	if got := nilAdapter.Capabilities(); !got.Supports(agentmodel.FeatureTools) || !got.Supports(agentmodel.FeatureStructuredJSON) || got.Supports(agentmodel.FeatureStreaming) {
		t.Fatalf("capabilities = %+v", got)
	}
}

func TestTodo_AGENT_021_Conformance(t *testing.T) {
	var applicationEffects atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, responseJSON("resp-tool", "completed", []any{map[string]any{
			"type": "function_call", "id": "fc-item", "call_id": "call-approved-5", "name": "lookup_policy", "arguments": `{"query":"leave"}`,
		}}))
	}))
	defer server.Close()
	result, err := newTestAdapter(t, server).Invoke(context.Background(), openAIRequest())
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Finish != agentmodel.FinishToolCalls || len(result.ToolProposals) != 1 {
		t.Fatalf("tool result = %+v", result)
	}
	proposal := result.ToolProposals[0]
	if proposal.ID != "call-approved-5" || proposal.Name != "lookup_policy" || string(proposal.Arguments) != `{"query":"leave"}` {
		t.Fatalf("proposal = %+v", proposal)
	}
	if applicationEffects.Load() != 0 {
		t.Fatalf("adapter executed %d application effects", applicationEffects.Load())
	}
}

func TestTodo_AGENT_021_Fault(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantCode agentmodel.FailureCode
	}{
		{name: "bad_json", status: 200, body: "{broken", wantCode: agentmodel.FailureInvalid},
		{name: "oversized_body", status: 200, body: strings.Repeat("x", maxResponseBytes+1), wantCode: agentmodel.FailureInvalid},
		{name: "rate_limited", status: 429, body: `{"error":"quota text must not escape"}`, wantCode: agentmodel.FailureUnavailable},
		{name: "server_error", status: 503, body: `{"error":"internal"}`, wantCode: agentmodel.FailureUnavailable},
		{name: "unauthorized", status: 401, body: `{"error":"secret echo"}`, wantCode: agentmodel.FailureInvalid},
		{name: "unexpected_function", status: 200, body: responseJSON("resp-x", "completed", []any{map[string]any{"type": "function_call", "call_id": "call-x", "name": "send_money", "arguments": "{}"}}), wantCode: agentmodel.FailureInvalid},
		{name: "malformed_arguments", status: 200, body: responseJSON("resp-x", "completed", []any{map[string]any{"type": "function_call", "call_id": "call-x", "name": "lookup_policy", "arguments": "{bad"}}), wantCode: agentmodel.FailureInvalid},
		{name: "unknown_output", status: 200, body: responseJSON("resp-x", "completed", []any{map[string]any{"type": "computer_call"}}), wantCode: agentmodel.FailureInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
			defer server.Close()
			result, err := newTestAdapter(t, server).Invoke(context.Background(), openAIRequest())
			var providerErr providerError
			if !errors.As(err, &providerErr) || providerErr.code != tc.wantCode {
				t.Fatalf("Invoke() error = %v, want normalized %s", err, tc.wantCode)
			}
			if result.Failure == nil || result.Failure.Code != tc.wantCode {
				t.Fatalf("failure result = %+v", result.Failure)
			}
			if strings.Contains(err.Error(), "secret echo") || strings.Contains(err.Error(), "quota text") {
				t.Fatalf("provider body leaked in error: %v", err)
			}
		})
	}
	t.Run("cancellation", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { close(started); <-release }))
		defer server.Close()
		ctx, cancel := context.WithCancel(context.Background())
		adapter := newTestAdapter(t, server)
		finished := make(chan error, 1)
		go func() { _, err := adapter.Invoke(ctx, openAIRequest()); finished <- err }()
		<-started
		cancel()
		if err := <-finished; !errors.Is(err, context.Canceled) {
			t.Fatalf("Invoke() error = %v, want context.Canceled", err)
		}
		close(release)
	})
	t.Run("request_deadline", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer server.Close()
		req := openAIRequest()
		req.Deadline = time.Now().Add(-time.Second)
		_, err := newTestAdapter(t, server).Invoke(context.Background(), req)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Invoke() error = %v, want deadline", err)
		}
	})
}

func TestTodo_AGENT_021_Security(t *testing.T) {
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { secondCalls.Add(1); w.WriteHeader(200) }))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret-never-log" {
			t.Error("authorization header missing on intended endpoint")
		}
		w.Header().Set("Location", second.URL+"/steal")
		w.WriteHeader(http.StatusFound)
	}))
	defer first.Close()
	adapter := newTestAdapter(t, first)
	_, err := adapter.Invoke(context.Background(), openAIRequest())
	if err == nil || secondCalls.Load() != 0 {
		t.Fatalf("redirect error=%v second endpoint calls=%d", err, secondCalls.Load())
	}
	if strings.Contains(err.Error(), "test-secret-never-log") {
		t.Fatalf("credential leaked in error: %v", err)
	}
	if got := adapter.Capabilities(); got.Supports(agentmodel.FeatureStreaming) {
		t.Fatal("buffered-only adapter advertised streaming")
	}
	defaultAdapter, err := New(Config{APIKey: "key", ModelProfile: "p", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt", Version: "v"}})
	if err != nil || defaultAdapter.endpoint != "https://api.openai.com/v1/responses" {
		t.Fatalf("default endpoint adapter = %+v, error = %v", defaultAdapter, err)
	}
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{name: "empty_key", cfg: Config{ModelProfile: "p", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt", Version: "v"}}},
		{name: "non_loopback_http", cfg: Config{APIKey: "k", ModelProfile: "p", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt", Version: "v"}, BaseURL: "http://example.com/v1"}},
		{name: "url_credentials", cfg: Config{APIKey: "k", ModelProfile: "p", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt", Version: "v"}, BaseURL: "https://user:pass@example.com/v1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("New() error=%v", err)
			}
		})
	}
}

func TestTodo_AGENT_021_Integration(t *testing.T) {
	var identities []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identities = append(identities, r.Header.Get("X-Client-Request-Id"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, responseJSON("resp-integration", "completed", []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "hello"}}}}))
	}))
	defer server.Close()
	adapter := newTestAdapter(t, server)
	req := openAIRequest()
	req.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
	for i := 0; i < 2; i++ {
		result, err := adapter.Invoke(context.Background(), req)
		if err != nil || result.Text != "hello" {
			t.Fatalf("Invoke() result=%+v error=%v", result, err)
		}
	}
	if len(identities) != 2 || identities[0] == "" || identities[0] != identities[1] {
		t.Fatalf("retry request identities = %v", identities)
	}
	if identities[0] != requestIdentity([]byte(mustJSON(makeRequest("gpt-test", req))), req.TraceID) {
		t.Fatal("request identity is not derived from the stable request payload")
	}
	otherRequest := req
	otherRequest.TraceID = "trace-distinct-43"
	if _, err := adapter.Invoke(context.Background(), otherRequest); err != nil {
		t.Fatalf("Invoke() with a distinct trace ID: %v", err)
	}
	if len(identities) != 3 || identities[2] == identities[0] {
		t.Fatalf("distinct trace did not get a distinct request identity: %v", identities)
	}
}

func TestTodo_AGENT_021_BoundedMiniReasoning(t *testing.T) {
	req := openAIRequest()
	for _, model := range []string{"gpt-5-mini-2025-08-07", "gpt-test"} {
		wire, err := json.Marshal(makeRequest(model, req))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]json.RawMessage
		json.Unmarshal(wire, &body)
		if model == "gpt-5-mini-2025-08-07" && string(body["reasoning"]) != `{"effort":"minimal"}` {
			t.Fatalf("bounded mini reasoning missing: %s", body["reasoning"])
		}
		if model == "gpt-test" && body["reasoning"] != nil {
			t.Fatal("unsupported model received reasoning options")
		}
	}
}
