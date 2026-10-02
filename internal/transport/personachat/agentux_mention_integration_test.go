package personachat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type agentuxMentionSurface struct {
	conversation string
	candidates   []struct {
		profile   Profile
		invocable bool
	}
}

func (s *agentuxMentionSurface) Directory(_ context.Context, conversation string) (Directory, error) {
	s.conversation = conversation
	directory := Directory{}
	for _, candidate := range s.candidates {
		if candidate.invocable {
			directory.Personas = append(directory.Personas, candidate.profile)
		}
	}
	return directory, nil
}

func (*agentuxMentionSurface) Progress(context.Context, string) (Progress, error) {
	return Progress{}, nil
}

func (*agentuxMentionSurface) Retry(context.Context, string, string) (RetryResult, error) {
	return RetryResult{}, nil
}

func TestTodo_AGENTUX_004_Integration(t *testing.T) {
	visible := Profile{Reference: Reference{Kind: "AGENT_MENTION", TenantID: "tenant-a", ID: "policy-helper", Display: "Policy Helper", ConversationID: "room-a"}, Purpose: "Answer policy questions"}
	hidden := Profile{Reference: Reference{Kind: "AGENT_MENTION", TenantID: "tenant-a", ID: "payroll", Display: "Payroll", ConversationID: "room-a"}, Purpose: "Review restricted payroll"}
	surface := &agentuxMentionSurface{candidates: []struct {
		profile   Profile
		invocable bool
	}{{visible, true}, {hidden, false}}}

	request := httptest.NewRequest(http.MethodGet, Path+"?conversation_id=room-a", nil)
	response := httptest.NewRecorder()
	Handler{Surface: surface}.ServeHTTP(response, request)
	if response.Code != http.StatusOK || surface.conversation != "room-a" {
		t.Fatalf("served lookup = %d, conversation %q", response.Code, surface.conversation)
	}
	var directory Directory
	if err := json.NewDecoder(response.Body).Decode(&directory); err != nil {
		t.Fatal(err)
	}
	if len(directory.Personas) != 1 || directory.Personas[0].Reference.ID != "policy-helper" {
		t.Fatalf("served directory exposed non-invocable personas: %+v", directory.Personas)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("persona directory cache policy = %q", response.Header().Get("Cache-Control"))
	}
}
