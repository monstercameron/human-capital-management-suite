package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestIssue3793_ClientReadsWhichRequestsThisPersonMayUse(t *testing.T) {
	var method, path string
	got, err := requestRead(context.Background(), issue3790Client(200, `{"commands":[{"command":"leave","available":false,"reason":"paused"},{"command":"","available":true}]}`, func(r *http.Request, _ []byte) {
		method, path = r.Method, r.URL.EscapedPath()
	}), issue3790Config(), "gen eral")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet || path != "/api/chat/conversations/gen%20eral/requests" {
		t.Fatalf("%s %s", method, path)
	}
	leave := got["leave"]
	if !leave.Loaded || leave.Available || leave.Reason != "paused" || len(got) != 1 {
		t.Fatalf("answer %+v", got)
	}
}

func TestIssue3793_ClientPreviewAndSendCarryOnlyTheTypedValuesAndTheSubmissionKey(t *testing.T) {
	var sent, path string
	via, err := requestPreview(context.Background(), issue3790Client(200, `{"preview":{"command":"leave","values":{},"via":"HR Assistant"}}`, func(r *http.Request, b []byte) {
		sent, path = string(b), r.URL.EscapedPath()
	}), issue3790Config(), "general", "leave", map[string]string{"reason": "Trip"})
	if err != nil || via != "HR Assistant" {
		t.Fatalf("via %q err %v", via, err)
	}
	if path != "/api/chat/conversations/general/requests/leave/preview" || sent != `{"values":{"reason":"Trip"}}` {
		t.Fatalf("preview sent %s %s", path, sent)
	}
	href, err := requestSend(context.Background(), issue3790Client(200, `{"receipt":{"command":"leave","intent_id":"i1","href":"/workspace/app/journeys?journey=i1"}}`, func(r *http.Request, b []byte) {
		sent, path = string(b), r.URL.EscapedPath()
	}), issue3790Config(), "general", "leave", "submit-0001-aaaa", map[string]string{"reason": "Trip"})
	if err != nil || href != "/workspace/app/journeys?journey=i1" {
		t.Fatalf("href %q err %v", href, err)
	}
	if path != "/api/chat/conversations/general/requests/leave" || sent != `{"submission":"submit-0001-aaaa","values":{"reason":"Trip"}}` {
		t.Fatalf("send sent %s %s", path, sent)
	}
}

func TestIssue3793_ClientReducesRefusalsToCodes_Security(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		code   string
	}{
		"invalid":   {400, `{"code":"invalid_request","fields":{"ends":"bad_range"}}`, "invalid_request"},
		"gone":      {409, `{"code":"not_available","reason":"paused"}`, "not_available"},
		"denied":    {403, `{"code":"permission_denied"}`, "permission_denied"},
		"no body":   {404, ``, "not_found"},
		"conflict":  {409, ``, "not_available"},
		"down":      {503, `{"code":"unavailable"}`, "unavailable"},
		"garbled":   {200, `<html>`, "unavailable"},
		"no result": {200, `{}`, "unavailable"},
		"no intent": {200, `{"receipt":{"command":"leave"}}`, "unavailable"},
		"html fail": {500, `<html>boom</html>`, "unavailable"},
	}
	for name, tc := range cases {
		_, err := requestSend(context.Background(), issue3790Client(tc.status, tc.body, nil), issue3790Config(), "general", "leave", "submit-0001-aaaa", map[string]string{})
		var refused *requestError
		if !errors.As(err, &refused) || refused.Code != tc.code {
			t.Errorf("%s: err %v, want code %s", name, err, tc.code)
		}
	}
	_, err := requestPreview(context.Background(), issue3790Client(400, cases["invalid"].body, nil), issue3790Config(), "general", "leave", map[string]string{})
	var refused *requestError
	if !errors.As(err, &refused) || refused.Fields["ends"] != "bad_range" {
		t.Errorf("fields lost: %v", err)
	}
	_, err = requestPreview(context.Background(), issue3790Client(409, cases["gone"].body, nil), issue3790Config(), "general", "leave", map[string]string{})
	if !errors.As(err, &refused) || refused.Reason != "paused" {
		t.Errorf("reason lost: %v", err)
	}
	if _, err := requestRead(context.Background(), issue3790Client(200, `{}`, nil), issue3790Config(), "general"); err != nil {
		t.Errorf("an empty answer offers nothing, it is not an error: %v", err)
	}
}
