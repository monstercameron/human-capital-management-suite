package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestIssue3916_ClientReadsOnlyTheSignedInPersonsList(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/followed-threads" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Query().Get("host") != "tenant" || r.URL.Query().Get("subject") != "" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		body := `{"host_tenant_id":"tenant","threads":[{"conversation_id":"room","root_post_id":"root","root_sequence":4,"root_author_id":"ana-id","root_body":"plans","reply_count":2,"last_activity":"2026-01-02T03:04:05Z","revision":3}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	reply, err := chatFollowedRequest(context.Background(), client, cfg)
	if err != nil || len(reply.Threads) != 1 {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if revision, ok := chatFollowedRevision(reply, "room", "root"); !ok || revision != 3 {
		t.Fatalf("revision=%d ok=%v", revision, ok)
	}
	if _, ok := chatFollowedRevision(reply, "room", "other"); ok {
		t.Fatal("a thread that is not listed has no revision")
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))}, nil
	})
	if _, err := chatFollowedRequest(context.Background(), client, cfg); err == nil {
		t.Fatal("a refusal must not read as an empty list")
	}
	if _, err := chatFollowedRequest(context.Background(), client, journeyclient.Config{}); err == nil {
		t.Fatal("no tunnel or bearer must be unavailable")
	}
}

func TestIssue3916_RowsUseTheNamesThePersonSees(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "person", Locale: "en-US"}
	model := chatui.Model{Conversations: []chatui.Conversation{{ID: "room", Name: "general", Kind: chatui.PublicChannel}, {ID: "dm", Name: "9f2a7c1d-0000-4000-8000-000000000001", Kind: chatui.DirectMessage}}}
	reply := followedThreadsReply{Threads: []followedThreadJSON{
		{ConversationID: "room", RootPostID: "r1", RootSequence: 4, RootAuthorID: "ana-id", RootBody: "plans", ReplyCount: 2, LastActivity: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		{ConversationID: "dm", RootPostID: "r2", RootAuthorID: "0f2a7c1d-0000-4000-8000-000000000009", RootRemoved: true},
		{ConversationID: "", RootPostID: "bad"},
	}}
	rows := chatFollowedRows(reply, cfg, model, map[string]string{"ana-id": "Ana"}, time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC))
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	if rows[0].Channel != "general" || !rows[0].InChannel || rows[0].Author != "Ana" || rows[0].Replies != 2 || rows[0].Sequence != 4 || rows[0].ActivityLabel == "" {
		t.Fatalf("first row = %+v", rows[0])
	}
	if rows[1].Channel != "" || rows[1].Author != "" || !rows[1].Removed {
		t.Fatalf("an identifier must never stand in for a name: %+v", rows[1])
	}
	if got := chatFollowedWithout(rows, "room", "r1"); len(got) != 1 || got[0].RootPostID != "r2" {
		t.Fatalf("without = %+v", got)
	}
}
