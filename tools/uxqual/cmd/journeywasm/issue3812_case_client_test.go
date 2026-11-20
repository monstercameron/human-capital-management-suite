package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestIssue3812_ClientReadsTheCaseOfTheConversation(t *testing.T) {
	var method, path, sent string
	state, err := caseRead(context.Background(), issue3790Client(200, `{"bound":true,"case_ref":"ER-2041","owner_id":"hr","owner_is_me":false,"restricted":true,"can_manage":false}`, func(r *http.Request, b []byte) {
		method, path, sent = r.Method, r.URL.EscapedPath(), string(b)
	}), issue3790Config(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/chat/case/v1/get" || sent != `{"conversation_id":"thread"}` {
		t.Fatalf("sent %s %s %q; the conversation id belongs in the body, not the address", method, path, sent)
	}
	if !state.Loaded || state.ConversationID != "thread" || !state.Bound || !state.Restricted || state.CaseRef != "ER-2041" || state.OwnerID != "hr" || state.CanManage {
		t.Fatalf("state %+v", state)
	}
	// An unbound conversation is loaded and unrestricted, whatever the flag says.
	state, err = caseRead(context.Background(), issue3790Client(200, `{"bound":false,"restricted":true,"can_manage":true}`, nil), issue3790Config(), "thread")
	if err != nil || !state.Loaded || state.Bound || state.Restricted || !state.CanManage {
		t.Fatalf("unbound state %+v err %v", state, err)
	}
}

func TestIssue3812_ClientBindsAndLifts(t *testing.T) {
	var path, sent string
	state, err := caseBind(context.Background(), issue3790Client(200, `{"bound":true,"case_ref":"ER-9","owner_id":"mgr","restricted":true,"can_manage":true}`, func(r *http.Request, b []byte) {
		path, sent = r.URL.EscapedPath(), string(b)
	}), issue3790Config(), "thread", "ER-9", "mgr", true)
	if err != nil || !state.Bound || path != "/api/chat/case/v1/bind" || sent != `{"conversation_id":"thread","case_ref":"ER-9","owner_id":"mgr","restricted":true}` {
		t.Fatalf("bind state %+v err %v path %q sent %q", state, err, path, sent)
	}
	state, err = caseLift(context.Background(), issue3790Client(200, `{"bound":false,"can_manage":true}`, func(r *http.Request, b []byte) { path, sent = r.URL.EscapedPath(), string(b) }), issue3790Config(), "thread")
	if err != nil || state.Bound || path != "/api/chat/case/v1/unbind" || sent != `{"conversation_id":"thread"}` {
		t.Fatalf("lift state %+v err %v path %q sent %q", state, err, path, sent)
	}
}

func TestIssue3812_ClientReducesRefusalsToCodes_Security(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		code   string
	}{
		"denied":    {403, `{"code":"permission_denied"}`, "permission_denied"},
		"no body":   {404, ``, "not_found"},
		"invalid":   {400, ``, "invalid_request"},
		"down":      {503, `{"code":"unavailable"}`, "unavailable"},
		"garbled":   {200, `<html>`, "unavailable"},
		"html fail": {500, `<html>boom</html>`, "unavailable"},
	} {
		_, err := caseBind(context.Background(), issue3790Client(tc.status, tc.body, nil), issue3790Config(), "thread", "ER-1", "hr", true)
		var refused *caseError
		if !errors.As(err, &refused) || refused.Code != tc.code {
			t.Errorf("%s: err %v, want code %s", name, err, tc.code)
		}
	}
	// With no way to reach the server nothing is sent and nothing is shown.
	if _, err := caseRead(context.Background(), nil, issue3790Config(), "thread"); err == nil {
		t.Error("a missing client read a case")
	}
}
