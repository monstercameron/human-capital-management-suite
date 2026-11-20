package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestIssue3791_ClientReadsTheWorkOfAConversation(t *testing.T) {
	var path string
	body := `{"items":[
		{"kind":"intent","id":"i1","title":"Promotion request","state":"awaiting_approval","source":"approvals","observed_at":"2026-11-19T09:30:00Z","revision":3,"href":"/workspace/app/journeys?journey=i1"},
		{"kind":"task","id":"t1","title":"Swap laptop","state":"open","source":"tasks","detail":"To do","owner_id":"dana","due_date":"2026-12-04","project_id":"ops","task_id":"t1"},
		{"kind":"intent","id":"","title":"No id","state":"done","source":"approvals"},
		{"kind":"mystery","id":"m","title":"Odd kind","state":"done","source":"x"}
	],"incomplete":["tasks","bogus"],"as_of":"2026-11-19T09:30:00Z"}`
	state, err := linkedworkRead(context.Background(), issue3790Client(200, body, func(r *http.Request, _ []byte) { path = r.URL.EscapedPath() }), issue3790Config(), "gen eral/1")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/chat/conversations/gen%20eral%2F1/work" {
		t.Fatalf("path %q", path)
	}
	if !state.Enabled || state.State != "ready" || len(state.Items) != 2 {
		t.Fatalf("state %+v", state)
	}
	first := state.Items[0]
	if first.Kind != "intent" || first.State != "awaiting_approval" || first.Revision != 3 || first.Href == "" || !first.ObservedAt.Equal(time.Date(2026, 11, 19, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("first %+v", first)
	}
	if task := state.Items[1]; task.ProjectID != "ops" || task.OwnerID != "dana" || task.DueDate != "2026-12-04" || task.Detail != "To do" {
		t.Fatalf("task %+v", task)
	}
	// Only sources this page knows are named, and the server's word is kept.
	if len(state.Incomplete) != 1 || state.Incomplete[0] != "tasks" {
		t.Fatalf("incomplete %v", state.Incomplete)
	}
	if state.AsOf.IsZero() {
		t.Fatal("freshness was dropped")
	}
}

// A refusal or an unreadable answer is never read as an empty list.
func TestIssue3791_ClientReducesRefusalsToTheirCodes_Security(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		code   string
		hide   bool
	}{
		"unreadable conversation": {404, `{"code":"not_found"}`, "not_found", true},
		"no permission":           {403, `{"code":"permission_denied"}`, "permission_denied", true},
		"route not served":        {404, ``, "not_found", true},
		"owner down":              {503, `{"code":"unavailable"}`, "unavailable", false},
		"garbled":                 {200, `<html>`, "unavailable", false},
	}
	for name, tc := range cases {
		_, err := linkedworkRead(context.Background(), issue3790Client(tc.status, tc.body, nil), issue3790Config(), "general")
		if err == nil || chatworkErrorCode(err) != tc.code {
			t.Errorf("%s: err=%v code=%s", name, err, chatworkErrorCode(err))
		}
		if hide := linkedworkHides(err); hide != tc.hide {
			t.Errorf("%s: hides=%v, want %v", name, hide, tc.hide)
		}
	}
}
