package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func issue3793Serve(t *testing.T, requests ChatRequestPort, kind trust.SubjectKind, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, kind))
	recorder := httptest.NewRecorder()
	ChatRequestHandler{Requests: requests}.ServeHTTP(recorder, request)
	return recorder
}

func TestIssue3793_RequestRoutesAnswerWithShortCodes_Security(t *testing.T) {
	s, port, _ := issue3793Fixture(t)
	list := issue3793Serve(t, s, trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/room/requests", "")
	var reply chatRequestReply
	if err := json.Unmarshal(list.Body.Bytes(), &reply); list.Code != 200 || err != nil || len(reply.Commands) != 1 || !reply.Commands[0].Available {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}
	good := `{"values":{"employee":"person","starts":"2026-12-01","ends":"2026-12-05","reason":"Family trip"}}`
	shown := issue3793Serve(t, s, trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/requests/leave/preview", good)
	if err := json.Unmarshal(shown.Body.Bytes(), &reply); shown.Code != 200 || err != nil || reply.Preview == nil || reply.Preview.Via != "HR Assistant" || len(port.proposals) != 0 {
		t.Fatalf("preview = %d %s", shown.Code, shown.Body.String())
	}
	invalid := issue3793Serve(t, s, trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/requests/leave", `{"submission":"submit-0001-aaaa","values":{"employee":"person","starts":"2026-12-05","ends":"2026-12-01","reason":"x"}}`)
	var refused chatRequestError
	if err := json.Unmarshal(invalid.Body.Bytes(), &refused); invalid.Code != 400 || err != nil || refused.Code != "invalid_request" || refused.Fields["ends"] != chat.IntentRequestErrBadRange || len(port.proposals) != 0 {
		t.Fatalf("invalid = %d %s", invalid.Code, invalid.Body.String())
	}
	sent := issue3793Serve(t, s, trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/requests/leave", strings.Replace(good, `{"values"`, `{"submission":"submit-0001-aaaa","values"`, 1))
	reply = chatRequestReply{}
	if err := json.Unmarshal(sent.Body.Bytes(), &reply); sent.Code != 200 || err != nil || reply.Receipt == nil || reply.Receipt.IntentID != "intent-1" || len(port.proposals) != 1 {
		t.Fatalf("sent = %d %s", sent.Code, sent.Body.String())
	}
	cases := []struct {
		name         string
		kind         trust.SubjectKind
		method, path string
		body         string
		want         int
	}{
		{"agent", trust.SubjectKindAgent, http.MethodPost, "/api/chat/conversations/room/requests/leave", `{"values":{}}`, 403},
		{"agent list", trust.SubjectKindAgent, http.MethodGet, "/api/chat/conversations/room/requests", "", 403},
		{"wrong method", trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/room/requests/leave", "", 405},
		{"list by post", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/requests", `{}`, 405},
		{"unknown field", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/requests/leave", `{"values":{},"tenant":"other"}`, 400},
		{"no values", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/requests/leave", `{}`, 400},
		{"no submission key", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/requests/leave", good, 400},
		{"unknown command", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/requests/fire/preview", good, 404},
		{"other route", trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/room/requests/leave/preview/x", "", 404},
	}
	before := len(port.proposals)
	for _, c := range cases {
		if got := issue3793Serve(t, s, c.kind, c.method, c.path, c.body); got.Code != c.want {
			t.Fatalf("%s: status %d, want %d (%s)", c.name, got.Code, c.want, got.Body.String())
		}
	}
	if len(port.proposals) != before {
		t.Fatal("a refused call reached the intent kernel")
	}
	port.installs = nil
	gone := issue3793Serve(t, s, trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/requests/leave/preview", good)
	refused = chatRequestError{}
	if err := json.Unmarshal(gone.Body.Bytes(), &refused); gone.Code != 409 || err != nil || refused.Code != "not_available" || refused.Reason != ChatIntentRequestNotInstalled {
		t.Fatalf("not installed = %d %s", gone.Code, gone.Body.String())
	}
	off := issue3793Serve(t, nil, trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/room/requests", "")
	if off.Code != 503 || strings.Contains(off.Body.String(), "chat request") {
		t.Fatalf("an unconfigured surface answers a short unavailable: %d %s", off.Code, off.Body.String())
	}
}
