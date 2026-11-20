package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestIssue3749_ClientReadsTheDocumentRoute_Integration(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	client := issue3790Client(200, `{"items":[{"conversation_id":"general","name":"general","direct":false,"messages":2,"latest_at":"2026-11-20T09:20:00Z","href":"/workspace/app/chat#channel=general&message=m2&at=9"},{"conversation_id":"dm","direct":true,"messages":1,"href":"/workspace/app/chat#channel=dm&message=m3&at=2"}],"incomplete":true}`, func(r *http.Request, body []byte) {
		gotPath, gotMethod, gotBody = r.URL.EscapedPath(), r.Method, string(body)
		if r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Scheme != "https" {
			t.Errorf("endpoint or authorization: %s", r.URL)
		}
	})
	got, err := documentDiscussionsCall(context.Background(), client, issue3790Config(), "doc 1")
	if err != nil || len(got.Items) != 2 || !got.Incomplete || gotMethod != http.MethodGet || gotPath != "/api/chat/documents/doc%201/discussions" || gotBody != "" {
		t.Fatalf("read: %+v %v %s %s %q", got, err, gotMethod, gotPath, gotBody)
	}
	if first := got.Items[0]; first.Name != "general" || first.Messages != 2 || first.Href != "/workspace/app/chat#channel=general&message=m2&at=9" || first.Direct {
		t.Fatalf("first %+v", first)
	}
	if !got.Items[1].Direct {
		t.Fatalf("direct flag lost: %+v", got.Items[1])
	}
	if _, err := documentDiscussionsCall(context.Background(), client, issue3790Config(), "a/b"); chatworkErrorCode(err) != "invalid_argument" {
		t.Fatalf("a document id with a slash reached the network: %v", err)
	}
}

func TestIssue3749_ClientReducesRefusalsToTheirCodes_Security(t *testing.T) {
	for status, code := range map[int]string{403: "permission_denied", 404: "not_found", 503: "unavailable"} {
		client := issue3790Client(status, `{"code":"`+code+`","detail":"board-only conversation"}`, nil)
		_, err := documentDiscussionsCall(context.Background(), client, issue3790Config(), "doc-1")
		if chatworkErrorCode(err) != code || strings.Contains(err.Error(), "board-only") {
			t.Errorf("status %d: %v", status, err)
		}
	}
	cfg := issue3790Config()
	cfg.Bearer = ""
	if _, err := documentDiscussionsCall(context.Background(), issue3790Client(200, `{}`, nil), cfg, "doc-1"); chatworkErrorCode(err) != "unavailable" {
		t.Fatalf("no bearer: %v", err)
	}
}
