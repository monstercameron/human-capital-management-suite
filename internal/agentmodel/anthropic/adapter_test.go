package anthropic

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

func requestFixture() agentmodel.ModelRequest {
	return agentmodel.ModelRequest{
		ContractVersion: agentmodel.ContractVersion,
		TaskProfile:     "policy-answer", ModelProfile: "approved-claude-profile",
		Messages: []agentmodel.ModelMessage{
			{Role: agentmodel.RoleSystem, Content: "Use the approved policy only."},
			{Role: agentmodel.RoleDeveloper, Content: "Do not disclose personal data."},
			{Role: agentmodel.RoleUser, Content: "What is the leave policy?"},
		},
		ContextRefs: []agentmodel.ContextReference{{ID: "doc-7", Version: "2", Digest: "sha256:abc"}},
		Tools:       []agentmodel.ToolSchema{{Name: "lookup_policy", Description: "Look up a policy", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`)}},
		Output:      agentmodel.OutputConstraint{Mode: agentmodel.OutputSchema, Schema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`)},
		Deadline:    time.Now().Add(time.Minute), Limits: agentmodel.ModelLimits{MaxOutputTokens: 300}, TraceID: "trace-stable-42",
		Processing:       agentmodel.ProcessingPolicy{Residency: "us", Retention: "zero", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied},
		RequiredFeatures: []agentmodel.ModelFeature{agentmodel.FeatureTools, agentmodel.FeatureStructuredJSON},
	}
}

func newTestAdapter(t *testing.T, server *httptest.Server) *Adapter {
	t.Helper()
	adapter, err := New(Config{
		APIKey: "test-secret-never-log", ModelProfile: "approved-claude-profile",
		Identity: agentmodel.ModelIdentity{ProviderID: "anthropic", ModelID: "claude-test", Version: "claude-test-2026-09"},
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return adapter
}

func responseJSON(id, stop string, content any) string {
	return fmt.Sprintf(`{"id":%q,"type":"message","role":"assistant","model":"claude-test","stop_reason":%q,"content":%s,"usage":{"input_tokens":23,"output_tokens":9}}`, id, stop, mustJSON(content))
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func TestTodo_AGENT_022(t *testing.T) {
	var received messagesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/messages" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-secret-never-log" || r.Header.Get("anthropic-version") != apiVersion {
			t.Errorf("Anthropic authentication/version headers missing")
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
		_, _ = io.WriteString(w, responseJSON("msg-1", "end_turn", []any{map[string]any{"type": "text", "text": `{"answer":"Paid leave is available."}`}}))
	}))
	defer server.Close()
	adapter := newTestAdapter(t, server)
	result, err := adapter.Invoke(context.Background(), requestFixture())
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if received.Model != "claude-test" || received.MaxTokens != 300 || received.System != "Use the approved policy only.\n\nDo not disclose personal data." {
		t.Fatalf("mapped model/limits/system = %+v", received)
	}
	if len(received.Messages) != 1 || received.Messages[0].Role != "user" || len(received.Tools) != 1 || received.Tools[0].Name != "lookup_policy" {
		t.Fatalf("mapped messages/tools = %+v / %+v", received.Messages, received.Tools)
	}
	if received.ToolChoice == nil || received.ToolChoice.Type != "auto" || !received.ToolChoice.DisableParallelToolUse {
		t.Fatalf("tool choice = %+v", received.ToolChoice)
	}
	if received.OutputConfig == nil || received.OutputConfig.Format.Type != "json_schema" || string(received.OutputConfig.Format.Schema) != string(requestFixture().Output.Schema) {
		t.Fatalf("output config = %+v", received.OutputConfig)
	}
	if strings.Contains(mustJSON(received), "doc-7") || strings.Contains(mustJSON(received), "trace-stable-42") {
		t.Fatal("HCM context reference or trace identity leaked into provider state")
	}
	if result.Text != `{"answer":"Paid leave is available."}` || string(result.Structured) != result.Text || result.ProviderRequestID != "msg-1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.InputTokens != 23 || result.Usage.OutputTokens != 9 || result.Usage.TotalTokens != 32 || result.Finish != agentmodel.FinishComplete {
		t.Fatalf("usage/finish = %+v / %s", result.Usage, result.Finish)
	}
	if adapter.Identity() != (agentmodel.ModelIdentity{ProviderID: "anthropic", ModelID: "claude-test", Version: "claude-test-2026-09"}) {
		t.Fatalf("Identity() = %+v", adapter.Identity())
	}
}

func TestAdapter_MapsInvocationToolContinuation(t *testing.T) {
	req := requestFixture()
	req.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
	req.Messages = append(req.Messages,
		agentmodel.ModelMessage{Role: agentmodel.RoleAssistant, ToolCallID: "call-a", ToolName: "documents_search", ToolArguments: json.RawMessage(`{"query":"leave"}`)},
		agentmodel.ModelMessage{Role: agentmodel.RoleTool, ToolCallID: "call-a", Content: `{"hits":[{"document_id":"doc-1"}]}`},
	)
	if err := agentmodel.ValidateModelRequest(req); err != nil {
		t.Fatalf("ValidateModelRequest() error = %v", err)
	}
	mapped, err := makeRequest("claude-test", req)
	if err != nil {
		t.Fatalf("makeRequest() error = %v", err)
	}
	if len(mapped.Messages) != 3 || mapped.Messages[1].Role != "assistant" || mapped.Messages[2].Role != "user" {
		t.Fatalf("tool continuation roles = %+v", mapped.Messages)
	}
	var use, result []contentBlock
	if err := json.Unmarshal(mapped.Messages[1].Content, &use); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mapped.Messages[2].Content, &result); err != nil {
		t.Fatal(err)
	}
	if len(use) != 1 || use[0].Type != "tool_use" || use[0].ID != "call-a" || use[0].Name != "documents_search" || string(use[0].Input) != `{"query":"leave"}` || len(result) != 1 || result[0].Type != "tool_result" || result[0].ToolUseID != "call-a" || result[0].Content != `{"hits":[{"document_id":"doc-1"}]}` {
		t.Fatalf("tool continuation blocks = %+v / %+v", use, result)
	}
}

func TestTodo_AGENT_022_Conformance(t *testing.T) {
	tests := []struct {
		name         string
		stop         string
		blocks       any
		wantFinish   agentmodel.FinishReason
		wantText     string
		wantProposal bool
	}{
		{name: "tool_proposal", stop: "tool_use", blocks: []any{map[string]any{"type": "tool_use", "id": "toolu_1", "name": "lookup_policy", "input": map[string]any{"query": "leave"}}}, wantFinish: agentmodel.FinishToolCalls, wantProposal: true},
		{name: "length", stop: "max_tokens", blocks: []any{map[string]any{"type": "text", "text": "partial"}}, wantFinish: agentmodel.FinishLength, wantText: "partial"},
		{name: "complete", stop: "end_turn", blocks: []any{map[string]any{"type": "text", "text": "hello"}}, wantFinish: agentmodel.FinishComplete, wantText: "hello"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, responseJSON("msg-x", tc.stop, tc.blocks))
			}))
			defer server.Close()
			req := requestFixture()
			req.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
			result, err := newTestAdapter(t, server).Invoke(context.Background(), req)
			if err != nil {
				t.Fatalf("Invoke() error = %v", err)
			}
			if result.Finish != tc.wantFinish || result.Text != tc.wantText || (len(result.ToolProposals) == 1) != tc.wantProposal {
				t.Fatalf("result = %+v", result)
			}
			if tc.wantProposal {
				proposal := result.ToolProposals[0]
				if proposal.ID != "toolu_1" || proposal.Name != "lookup_policy" || string(proposal.Arguments) != `{"query":"leave"}` {
					t.Fatalf("proposal = %+v", proposal)
				}
			}
		})
	}
}

func TestTodo_AGENT_022_Fault(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantCode agentmodel.FailureCode
	}{
		{name: "invalid_json", status: http.StatusOK, body: "{broken", wantCode: agentmodel.FailureInvalid},
		{name: "unknown_stop", status: http.StatusOK, body: responseJSON("msg-x", "unknown", []any{map[string]any{"type": "text", "text": "hi"}}), wantCode: agentmodel.FailureInvalid},
		{name: "unknown_tool", status: http.StatusOK, body: responseJSON("msg-x", "tool_use", []any{map[string]any{"type": "tool_use", "id": "x", "name": "delete_everything", "input": map[string]any{}}}), wantCode: agentmodel.FailureInvalid},
		{name: "tool_stop_mismatch", status: http.StatusOK, body: responseJSON("msg-x", "end_turn", []any{map[string]any{"type": "tool_use", "id": "x", "name": "lookup_policy", "input": map[string]any{}}}), wantCode: agentmodel.FailureInvalid},
		{name: "bad_arguments", status: http.StatusOK, body: `{"id":"msg-x","type":"message","role":"assistant","model":"claude-test","stop_reason":"tool_use","content":[{"type":"tool_use","id":"x","name":"lookup_policy","input":[] }],"usage":{"input_tokens":2,"output_tokens":3}}`, wantCode: agentmodel.FailureInvalid},
		{name: "overloaded", status: 529, body: `{"error":"private provider error"}`, wantCode: agentmodel.FailureUnavailable},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"error":"credential echo"}`, wantCode: agentmodel.FailureInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			result, err := newTestAdapter(t, server).Invoke(context.Background(), requestFixture())
			var providerErr providerError
			if !errors.As(err, &providerErr) || providerErr.code != tc.wantCode || result.Failure == nil || result.Failure.Code != tc.wantCode {
				t.Fatalf("result=%+v error=%v, want normalized %s", result, err, tc.wantCode)
			}
			if strings.Contains(err.Error(), "credential echo") || strings.Contains(err.Error(), "private provider error") {
				t.Fatalf("provider body leaked: %v", err)
			}
		})
	}
	t.Run("refusal", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, responseJSON("msg-r", "refusal", []any{map[string]any{"type": "text", "text": "sensitive refusal detail"}}))
		}))
		defer server.Close()
		result, err := newTestAdapter(t, server).Invoke(context.Background(), requestFixture())
		if err != nil || result.Refusal == nil || result.Refusal.Code != agentmodel.RefusalContentPolicy || result.Finish != agentmodel.FinishBlocked || result.Text != "" {
			t.Fatalf("refusal result=%+v error=%v", result, err)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
		}))
		defer server.Close()
		defer close(release)
		adapter := newTestAdapter(t, server)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		finished := make(chan struct {
			result agentmodel.ModelResult
			err    error
		}, 1)
		go func() {
			result, err := adapter.Invoke(ctx, requestFixture())
			finished <- struct {
				result agentmodel.ModelResult
				err    error
			}{result: result, err: err}
		}()
		<-started
		cancel()
		var got struct {
			result agentmodel.ModelResult
			err    error
		}
		select {
		case got = <-finished:
		case <-time.After(2 * time.Second):
			t.Fatal("Invoke did not return after request context cancellation")
		}
		if !errors.Is(got.err, context.Canceled) || got.result.Finish != agentmodel.FinishCancelled {
			t.Fatalf("cancellation result=%+v error=%v", got.result, got.err)
		}
	})
}

func TestTodo_AGENT_022_Security(t *testing.T) {
	var redirected atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1); w.WriteHeader(http.StatusOK) }))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-secret-never-log" {
			t.Error("API key header missing on intended endpoint")
		}
		w.Header().Set("Location", second.URL+"/steal")
		w.WriteHeader(http.StatusFound)
	}))
	defer first.Close()
	_, err := newTestAdapter(t, first).Invoke(context.Background(), requestFixture())
	if err == nil || redirected.Load() != 0 || strings.Contains(err.Error(), "test-secret-never-log") {
		t.Fatalf("redirect error=%v second endpoint calls=%d", err, redirected.Load())
	}
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{name: "empty_key", cfg: Config{ModelProfile: "p", Identity: agentmodel.ModelIdentity{ProviderID: "anthropic", ModelID: "m", Version: "v"}}},
		{name: "provider_mismatch", cfg: Config{APIKey: "k", ModelProfile: "p", Identity: agentmodel.ModelIdentity{ProviderID: "other", ModelID: "m", Version: "v"}}},
		{name: "remote_http", cfg: Config{APIKey: "k", ModelProfile: "p", Identity: agentmodel.ModelIdentity{ProviderID: "anthropic", ModelID: "m", Version: "v"}, BaseURL: "http://example.com"}},
		{name: "url_userinfo", cfg: Config{APIKey: "k", ModelProfile: "p", Identity: agentmodel.ModelIdentity{ProviderID: "anthropic", ModelID: "m", Version: "v"}, BaseURL: "https://user:pass@example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("New() error=%v", err)
			}
		})
	}
	if (&Adapter{}).Identity() != (agentmodel.ModelIdentity{}) {
		t.Fatal("empty adapter identity is not zero")
	}
	if got := newTestAdapter(t, first).Capabilities(); got.Supports(agentmodel.FeatureParallelTools) || got.Supports(agentmodel.FeatureStreaming) {
		t.Fatalf("unsupported capabilities advertised: %+v", got)
	}
}

func TestTodo_AGENT_022_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.MaxTokens != 1024 {
			t.Errorf("default max tokens=%d", body.MaxTokens)
		}
		_, _ = io.WriteString(w, responseJSON("msg-i", "end_turn", []any{map[string]any{"type": "text", "text": "hello"}}))
	}))
	defer server.Close()
	req := requestFixture()
	req.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputText}
	req.Tools = nil
	req.Limits.MaxOutputTokens = 0
	result, err := newTestAdapter(t, server).Invoke(context.Background(), req)
	if err != nil || result.Text != "hello" || result.Finish != agentmodel.FinishComplete {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
