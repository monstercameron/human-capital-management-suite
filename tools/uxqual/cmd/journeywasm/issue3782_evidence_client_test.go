package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestIssue3782_ClientListsTheEvidenceOfTheConversation(t *testing.T) {
	var method, path, sent string
	body := `{"bound":true,"case_ref":"ER-2041","can_attach":true,"excerpts":[{"id":"e1","case_revision":"rev-3","note":"why","attached_by":"hr","attached_at":"2026-11-19T10:00:00Z","items":[` +
		`{"id":"i1","status":"current","author_id":"mgr","authored_at":"2026-11-19T09:00:00Z","body":"asked Monday","fingerprint":"a1b2c3d4e5f6"},` +
		`{"id":"i2","status":"gone"}]}]}`
	state, err := evidenceRead(context.Background(), issue3790Client(200, body, func(r *http.Request, b []byte) {
		method, path, sent = r.Method, r.URL.EscapedPath(), string(b)
	}), issue3790Config(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/chat/case-evidence/v1/list" || sent != `{"conversation_id":"thread"}` {
		t.Fatalf("sent %s %s %q; the conversation id belongs in the body, not the address", method, path, sent)
	}
	if !state.Loaded || state.ConversationID != "thread" || !state.Bound || !state.CanAttach || state.CaseRef != "ER-2041" || len(state.Excerpts) != 1 {
		t.Fatalf("state %+v", state)
	}
	e := state.Excerpts[0]
	if e.CaseRevision != "rev-3" || e.AttachedBy != "hr" || !e.AttachedAt.Equal(time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)) || len(e.Items) != 2 {
		t.Fatalf("excerpt %+v", e)
	}
	if e.Items[0].Body != "asked Monday" || e.Items[0].Status != "current" || e.Items[0].AuthorID != "mgr" || e.Items[0].Fingerprint != "a1b2c3d4e5f6" || e.Items[1].Status != "gone" || e.Items[1].Body != "" {
		t.Fatalf("items %+v", e.Items)
	}
	// An unbound conversation is loaded with nothing to show.
	state, err = evidenceRead(context.Background(), issue3790Client(200, `{"bound":false,"can_attach":true,"excerpts":[]}`, nil), issue3790Config(), "thread")
	if err != nil || !state.Loaded || state.Bound || state.CanAttach || len(state.Excerpts) != 0 {
		t.Fatalf("unbound state %+v err %v", state, err)
	}
}

func TestIssue3782_ClientAttachesIdsNeverText(t *testing.T) {
	var path, sent string
	state, err := evidenceAttach(context.Background(), issue3790Client(200, `{"bound":true,"case_ref":"ER-9","can_attach":true,"excerpts":[]}`, func(r *http.Request, b []byte) {
		path, sent = r.URL.EscapedPath(), string(b)
	}), issue3790Config(), "thread", "ER-9", "rev-3", "why", []string{"m1", "m2"})
	if err != nil || !state.Bound || path != "/api/chat/case-evidence/v1/attach" {
		t.Fatalf("state %+v err %v path %q", state, err, path)
	}
	if sent != `{"conversation_id":"thread","case_ref":"ER-9","case_revision":"rev-3","note":"why","message_ids":["m1","m2"]}` {
		t.Fatalf("sent %q", sent)
	}
}

func TestIssue3782_ClientReducesRefusalsToCodes_Security(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		code   string
	}{
		"denied":       {403, `{"code":"permission_denied"}`, "permission_denied"},
		"case changed": {409, `{"code":"case_changed"}`, "case_changed"},
		"conflict":     {409, ``, "case_changed"},
		"no body":      {404, ``, "not_found"},
		"invalid":      {400, ``, "invalid_request"},
		"down":         {503, `{"code":"unavailable"}`, "unavailable"},
		"garbled":      {200, `<html>`, "unavailable"},
		"html fail":    {500, `<html>boom</html>`, "unavailable"},
	} {
		_, err := evidenceAttach(context.Background(), issue3790Client(tc.status, tc.body, nil), issue3790Config(), "thread", "ER-1", "", "", []string{"m1"})
		var refused *caseError
		if !errors.As(err, &refused) || refused.Code != tc.code {
			t.Errorf("%s: err %v, want code %s", name, err, tc.code)
		}
	}
	if _, err := evidenceRead(context.Background(), nil, issue3790Config(), "thread"); err == nil {
		t.Error("a missing client read evidence")
	}
}
