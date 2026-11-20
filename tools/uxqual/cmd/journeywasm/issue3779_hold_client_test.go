package main

import (
	"context"
	"net/http"
	"testing"
)

func TestIssue3779_ClientReadsTheHoldOfTheConversation(t *testing.T) {
	var method, path, sent string
	state, err := holdRead(context.Background(), issue3790Client(200, `{"held":true,"count":2}`, func(r *http.Request, b []byte) {
		method, path, sent = r.Method, r.URL.EscapedPath(), string(b)
	}), issue3790Config(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/chat/holds/v1/conversation" || sent != `{"conversation_id":"thread"}` {
		t.Fatalf("sent %s %s %q; the conversation id belongs in the body, not the address", method, path, sent)
	}
	if !state.Loaded || state.ConversationID != "thread" || !state.Held || state.Count != 2 {
		t.Fatalf("state %+v", state)
	}
	// Not held is loaded and not held, whatever a stray count says.
	state, err = holdRead(context.Background(), issue3790Client(200, `{"held":false,"count":1}`, nil), issue3790Config(), "thread")
	if err != nil || !state.Loaded || state.Held {
		t.Fatalf("not-held state %+v err %v", state, err)
	}
}

func TestIssue3779_ClientShowsNothingOnRefusalOrFailure_Security(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"not an administrator": {403, `{"code":"permission_denied"}`},
		"cannot open":          {404, `{"code":"not_found"}`},
		"down":                 {503, `{"code":"unavailable"}`},
		"garbled":              {200, `<html>`},
	} {
		state, err := holdRead(context.Background(), issue3790Client(tc.status, tc.body, nil), issue3790Config(), "thread")
		if err == nil || state.Loaded || state.Held {
			t.Errorf("%s: state %+v err %v; a refusal must show no badge", name, state, err)
		}
	}
	if _, err := holdRead(context.Background(), nil, issue3790Config(), "thread"); err == nil {
		t.Error("a missing client read a hold")
	}
	if _, err := holdRead(context.Background(), issue3790Client(200, `{"held":true,"count":1}`, nil), issue3790Config(), ""); err == nil {
		t.Error("an empty conversation id read a hold")
	}
}
