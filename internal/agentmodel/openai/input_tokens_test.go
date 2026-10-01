package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestTodo_AGENT_024_ExactInputBudget(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		allowed    bool
	}{
		{"within_limit", `{"object":"response.input_tokens","input_tokens":23}`, 200, true},
		{"above_limit", `{"object":"response.input_tokens","input_tokens":101}`, 200, false},
		{"missing_count", `{"object":"response.input_tokens"}`, 200, false},
		{"invalid_count", `{"object":"response.input_tokens","input_tokens":-1}`, 200, false},
		{"unavailable", `{"error":"no input count"}`, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counts, calls := 0, 0
			req := openAIRequest()
			req.Limits.MaxInputTokens = 100
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/responses/input_tokens" {
					counts++
					var count inputTokenCountRequest
					if json.NewDecoder(r.Body).Decode(&count) != nil || r.Header.Get("Authorization") != "Bearer test-secret-never-log" || r.Header.Get("X-Client-Request-Id") == "" {
						t.Error("input count request identity absent")
					}
					original := makeRequest("gpt-test", req)
					if count.Model != original.Model || !reflect.DeepEqual(count.Input, original.Input) || !reflect.DeepEqual(count.Tools, original.Tools) || !reflect.DeepEqual(count.Text, original.Text) {
						t.Error("input count omitted inference input or schema")
					}
					w.WriteHeader(tc.status)
					io.WriteString(w, tc.body)
					return
				}
				calls++
				io.WriteString(w, responseJSON("actual", "completed", []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": `{"answer":"approved"}`}}}}))
			}))
			defer server.Close()
			adapter := newTestAdapter(t, server)
			adapter.preflightInputTokens = true
			result, err := adapter.Invoke(context.Background(), req)
			if counts != 1 {
				t.Fatalf("counts=%d", counts)
			}
			if tc.allowed {
				if err != nil || calls != 1 || len(result.Structured) == 0 {
					t.Fatalf("allowed=%+v err=%v calls=%d", result, err, calls)
				}
			} else if calls != 0 || (err == nil && result.Refusal == nil) {
				t.Fatalf("oversized/unknown input reached inference result=%+v err=%v calls=%d", result, err, calls)
			}
		})
	}
}
