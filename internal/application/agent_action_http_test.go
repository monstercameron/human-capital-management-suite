package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type agentActionHTTPStub struct {
	calls   int
	request AgentActionSubmitRequest
	err     error
}

func (s *agentActionHTTPStub) Compile(context.Context, AgentActionCompileRequest) (AgentActionState, error) {
	s.calls++
	return AgentActionState{IntentID: "durable", State: "draft"}, s.err
}
func (s *agentActionHTTPStub) Submit(_ context.Context, req AgentActionSubmitRequest) (AgentActionState, error) {
	s.calls++
	s.request = req
	return AgentActionState{IntentID: req.IntentID, State: "awaiting approval"}, s.err
}
func (s *agentActionHTTPStub) Observe(context.Context, string) (AgentActionState, error) {
	s.calls++
	return AgentActionState{IntentID: "durable", State: "executing"}, s.err
}

func TestAgentActionHandlerForwardsTypedCommands(t *testing.T) {
	port := &agentActionHTTPStub{}
	handler := NewAgentActionHandler(port)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, AgentActionsPath+"submit", strings.NewReader(`{"intent_id":"durable","proposal_revision_id":"revision","proposal_digest":"exact","expected_instance_version":3}`)))
	if response.Code != http.StatusOK || port.calls != 1 || port.request.ProposalDigest != "exact" || port.request.ExpectedInstanceVersion != 3 {
		t.Fatalf("forwarding: status=%d request=%+v", response.Code, port.request)
	}
	var state AgentActionState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || state.State != "awaiting approval" {
		t.Fatalf("response: %+v %v", state, err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, AgentActionsPath+"durable", nil))
	if response.Code != http.StatusOK || port.calls != 2 || !strings.Contains(response.Body.String(), "executing") {
		t.Fatalf("observation: %d %s", response.Code, response.Body.String())
	}
}

func TestAgentActionHandlerRejectsForgedApprovalAndExtraBodies(t *testing.T) {
	port := &agentActionHTTPStub{}
	for _, body := range []string{`{"approved":true}`, `{} {}`, `null null`, `{"reaction":"thumbs-up"}`, strings.Repeat("x", (256<<10)+1)} {
		response := httptest.NewRecorder()
		NewAgentActionHandler(port).ServeHTTP(response, httptest.NewRequest(http.MethodPost, AgentActionsPath+"submit", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest || port.calls != 0 {
			t.Fatalf("malformed body forwarded: status=%d calls=%d", response.Code, port.calls)
		}
	}
	for _, target := range []string{AgentActionsPath + "approve", AgentActionsPath + "durable/approve"} {
		response := httptest.NewRecorder()
		NewAgentActionHandler(port).ServeHTTP(response, httptest.NewRequest(http.MethodPost, target, strings.NewReader(`{}`)))
		if response.Code != http.StatusNotFound || port.calls != 0 {
			t.Fatalf("approval surface exists: %d", response.Code)
		}
	}
}

func TestAgentActionHandlerRefusalPreservesPartialDurableDraft(t *testing.T) {
	port := &agentActionHTTPStub{err: ErrAgentActionInput}
	response := httptest.NewRecorder()
	NewAgentActionHandler(port).ServeHTTP(response, httptest.NewRequest(http.MethodPost, AgentActionsPath+"compile", strings.NewReader(`{}`)))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "durable") {
		t.Fatalf("draft error result lost: %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	NewAgentActionHandler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodPost, AgentActionsPath+"submit", strings.NewReader(`{}`)))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("uncomposed handler: %d", response.Code)
	}
}
