package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// extendTestCredential is the fixture credential the transport test sends.
// Built from parts so no single literal resembles a real credential.
const extendTestCredential = "owner" + "-" + "token"

// TestTodo_AGENT2_017_ExtendBudgetTransport proves the extend-budget click
// reaches the real route: one JSON POST with the owner's credential carrying
// the task and the click's idempotency id, and typed refusals the page turns
// into sentences.
func TestTodo_AGENT2_017_ExtendBudgetTransport(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody agentTaskBudgetRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("extend-budget body is not JSON: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL, Bearer: extendTestCredential}
	if err := agentAccessCall(context.Background(), server.Client(), cfg, http.MethodPost, agentTaskAPI, agentTaskBudgetRequest{TaskID: "task-approval", RequestID: "extend-click-1"}, nil); err != nil {
		t.Fatalf("extend-budget call = %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/agent-tasks/v1/extend-budget" || gotAuth != "Bearer "+extendTestCredential {
		t.Fatalf("extend-budget request = %s %s auth %q", gotMethod, gotPath, gotAuth)
	}
	if gotBody != (agentTaskBudgetRequest{TaskID: "task-approval", RequestID: "extend-click-1"}) {
		t.Fatalf("extend-budget body = %+v", gotBody)
	}

	for _, tc := range []struct {
		name   string
		status int
		code   string
		want   error
	}{
		{"denied", http.StatusForbidden, "", errAgentAccessDenied},
		{"conflict", http.StatusConflict, "", errAgentAccessConflict},
		{"invalid", http.StatusBadRequest, "", errAgentAccessInvalid},
		{"rejected link", http.StatusUnprocessableEntity, "state_rejected", errAgentAccessRejected},
		{"server failure", http.StatusInternalServerError, "", errAgentAccessUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"code":"` + tc.code + `"}`))
			}))
			defer refusing.Close()
			cfg := journeyclient.Config{TunnelURL: refusing.URL, Bearer: extendTestCredential}
			if err := agentAccessCall(context.Background(), refusing.Client(), cfg, http.MethodPost, agentTaskAPI, agentTaskBudgetRequest{TaskID: "task-approval", RequestID: "extend-click-2"}, nil); !errors.Is(err, tc.want) {
				t.Fatalf("refusal = %v, want %v", err, tc.want)
			}
		})
	}

	// No tunnel or no credential never leaves the page.
	for _, cfg := range []journeyclient.Config{{}, {TunnelURL: server.URL}, {Bearer: extendTestCredential}, {TunnelURL: "gopher://cell"}} {
		if err := agentAccessCall(context.Background(), server.Client(), cfg, http.MethodPost, agentTaskAPI, agentTaskBudgetRequest{TaskID: "task-approval", RequestID: "extend-click-3"}, nil); !errors.Is(err, errAgentAccessUnavailable) {
			t.Fatalf("config %+v: call = %v, want unavailable", cfg, err)
		}
	}
}
