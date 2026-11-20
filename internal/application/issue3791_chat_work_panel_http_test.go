package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3791HTTPPort struct {
	asked []string
	err   error
}

func (f *issue3791HTTPPort) List(_ context.Context, _ *trust.Principal, conversation string) (ChatLinkedWorkResult, error) {
	f.asked = append(f.asked, conversation)
	return ChatLinkedWorkResult{Items: []ChatLinkedWorkItem{{Kind: "intent", ID: "i1", State: "awaiting_approval", Source: "approvals"}}, Incomplete: []string{"tasks"}, AsOf: "2026-11-19T09:30:00Z"}, f.err
}

func issue3791Serve(t *testing.T, kind trust.SubjectKind, port ChatLinkedWorkPort, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil).WithContext(chatsaveHTTPContext(t, kind))
	recorder := httptest.NewRecorder()
	ChatLinkedWorkHandler{Work: port}.ServeHTTP(recorder, request)
	return recorder
}

func TestIssue3791_WorkRouteTakesTheConversationFromThePath_Integration(t *testing.T) {
	port := &issue3791HTTPPort{}
	recorder := issue3791Serve(t, trust.SubjectKindHuman, port, http.MethodGet, "/api/chat/conversations/general/work")
	if recorder.Code != http.StatusOK || len(port.asked) != 1 || port.asked[0] != "general" {
		t.Fatalf("status=%d asked=%v body=%s", recorder.Code, port.asked, recorder.Body.String())
	}
	var reply ChatLinkedWorkResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil || len(reply.Items) != 1 || reply.Items[0].State != "awaiting_approval" || len(reply.Incomplete) != 1 || reply.AsOf == "" {
		t.Fatalf("reply=%s err=%v", recorder.Body.String(), err)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("work must not be cached: %v", recorder.Header())
	}
}

func TestIssue3791_WorkRouteRefusalShapes_Security(t *testing.T) {
	cases := []struct {
		name   string
		kind   trust.SubjectKind
		method string
		target string
		err    error
		status int
		code   string
	}{
		{"an agent has no panel", trust.SubjectKindAgent, http.MethodGet, "/api/chat/conversations/general/work", nil, http.StatusForbidden, "permission_denied"},
		{"post is not allowed", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/general/work", nil, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"unreadable conversation reads as absent", trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/private/work", ErrChatWorkNotFound, http.StatusNotFound, "not_found"},
		{"blank conversation", trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/general/work", ErrChatWorkInvalid, http.StatusBadRequest, "invalid_argument"},
		{"owner down", trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/general/work", ErrChatWorkUnavailable, http.StatusServiceUnavailable, "unavailable"},
	}
	for _, tc := range cases {
		port := &issue3791HTTPPort{err: tc.err}
		recorder := issue3791Serve(t, tc.kind, port, tc.method, tc.target)
		if recorder.Code != tc.status || !strings.Contains(recorder.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("%s: status=%d body=%s", tc.name, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "approvals") {
			t.Errorf("%s: a refusal carried work: %s", tc.name, recorder.Body.String())
		}
		if tc.kind == trust.SubjectKindAgent && len(port.asked) != 0 {
			t.Errorf("%s: work was read for an agent", tc.name)
		}
	}
}

func TestIssue3791_WorkRouteLeavesOtherPathsAlone(t *testing.T) {
	reached := ""
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = r.URL.Path })
	handler := OverlayChatLinkedWork(next, &issue3791HTTPPort{}, transport.Config{})
	for _, path := range []string{"/api/chat/conversations/general", "/api/chat/conversations//work", "/api/chat/conversations/a/b/work", "/api/chat/messages/linked-work"} {
		reached = ""
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if reached != path {
			t.Errorf("%s was swallowed", path)
		}
	}
}
