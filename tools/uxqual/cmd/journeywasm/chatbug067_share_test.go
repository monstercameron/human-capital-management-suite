package main

import (
	"net/http"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Every answer the server can give to "Share to channel" is read as shared, as
// a refusal with its reason, or as a failure worth trying again; a refusal is
// never read as "try again".
func TestTodo_CHATBUG_067(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		code                  int
		state, detail         string
		status                chatui.AgentShareStatus
		wantReason, wantTitle string
	}{
		{"shared", http.StatusOK, "", "", chatui.AgentShareShared, "", ""},
		{"the agent answers privately", http.StatusConflict, "agent", "", chatui.AgentShareRefused, "agent", ""},
		{"a named source is closed", http.StatusConflict, "audience", "2026 holiday guide", chatui.AgentShareRefused, "audience", "2026 holiday guide"},
		{"sources are closed, none named", http.StatusConflict, "audience", "", chatui.AgentShareRefused, "audience", ""},
		{"too old", http.StatusConflict, "expired", "", chatui.AgentShareRefused, "expired", ""},
		{"not the asker", http.StatusForbidden, "", "", chatui.AgentShareRefused, "denied", ""},
		{"an unknown conflict", http.StatusConflict, "something-else", "x", chatui.AgentShareFailed, "", ""},
		{"the service is down", http.StatusServiceUnavailable, "", "", chatui.AgentShareFailed, "", ""},
		{"a server error", http.StatusInternalServerError, "", "", chatui.AgentShareFailed, "", ""},
	} {
		status, reason, title := agentShareOutcome(tc.code, tc.state, tc.detail)
		if status != tc.status || reason != tc.wantReason || title != tc.wantTitle {
			t.Fatalf("%s: read as %q %q %q, want %q %q %q", tc.name, status, reason, title, tc.status, tc.wantReason, tc.wantTitle)
		}
	}
}
