package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var issue3788Config = journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}

func TestIssue3788_ClientReadsOnlyTheSignedInPersonsMessages(t *testing.T) {
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/scheduled-posts" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Query().Get("host") != "tenant" || r.URL.Query().Get("subject") != "" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		body := `{"host_tenant_id":"tenant","posts":[{"id":"s1","conversation_id":"room","body":"hello","send_at":"2026-03-04T07:30:00Z","zone":"UTC","state":"pending","revision":2,"quiet_people":1}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	reply, err := chatScheduledList(context.Background(), client, issue3788Config)
	if err != nil || len(reply.Posts) != 1 || reply.Posts[0].Revision != 2 {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))}, nil
	})
	if _, err := chatScheduledList(context.Background(), client, issue3788Config); err == nil {
		t.Fatal("a refusal must not read as an empty tray")
	}
	if _, err := chatScheduledList(context.Background(), client, journeyclient.Config{}); err == nil {
		t.Fatal("no tunnel or bearer must be unavailable")
	}
}

func TestIssue3788_ScheduleEditCancelAreSentToTheOneRoute(t *testing.T) {
	var method string
	var got map[string]any
	status, answer := 201, `{"post":{"id":"s1","conversation_id":"room","body":"hello","send_at":"2026-03-04T07:30:00Z","state":"pending","revision":1,"quiet_people":1}}`
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/chat/scheduled-posts" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		method, got = r.Method, nil
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(answer)), Header: http.Header{}}, nil
	})}
	at := "2026-03-04T07:30:00Z"
	post, outcome := chatScheduledSend(context.Background(), client, issue3788Config, http.MethodPost, scheduledPostBody{ConversationID: "room", Body: "hello", SendAt: at, Zone: "Europe/Berlin"})
	if outcome != "saved" || post.ID != "s1" || post.QuietPeople != 1 || method != http.MethodPost {
		t.Fatalf("post=%+v outcome=%s method=%s", post, outcome, method)
	}
	if got["host"] != "tenant" || got["conversation_id"] != "room" || got["body"] != "hello" || got["send_at"] != at || got["zone"] != "Europe/Berlin" {
		t.Fatalf("body = %v", got)
	}
	for _, field := range []string{"author", "subject", "author_id", "home_tenant_id"} {
		if _, ok := got[field]; ok {
			t.Fatalf("the request names the person (%s): %v", field, got)
		}
	}
	status = 200
	if _, outcome = chatScheduledSend(context.Background(), client, issue3788Config, http.MethodPut, scheduledPostBody{ID: "s1", Body: "x", SendAt: at, Revision: 4}); outcome != "saved" || method != http.MethodPut || got["revision"] != float64(4) || got["id"] != "s1" {
		t.Fatalf("edit: outcome=%s method=%s body=%v", outcome, method, got)
	}
	answer = `{"status":"cancelled"}`
	if _, outcome = chatScheduledSend(context.Background(), client, issue3788Config, http.MethodDelete, scheduledPostBody{ID: "s1"}); outcome != "saved" || method != http.MethodDelete || got["id"] != "s1" {
		t.Fatalf("cancel: outcome=%s method=%s body=%v", outcome, method, got)
	}
}

func TestIssue3788_RefusalsAreSortedIntoTheWordsThePageUses(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{422, `{"code":"limit_reached"}`, "limit"},
		{400, `{"code":"invalid_time"}`, "time"},
		{400, `{"code":"invalid_argument"}`, "failed"},
		{403, `{"code":"permission_denied"}`, "failed"},
		{409, `{"code":"conflict"}`, "failed"},
		{503, `{"code":"unavailable"}`, "failed"},
	} {
		client := &http.Client{Transport: chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
		})}
		if post, outcome := chatScheduledSend(context.Background(), client, issue3788Config, http.MethodPost, scheduledPostBody{ConversationID: "room", Body: "x", SendAt: "2026-03-04T07:30:00Z"}); outcome != tc.want || post.ID != "" {
			t.Fatalf("%d %s: outcome=%s post=%+v", tc.status, tc.body, outcome, post)
		}
	}
	if _, outcome := chatScheduledSend(context.Background(), nil, issue3788Config, http.MethodPost, scheduledPostBody{}); outcome != "failed" {
		t.Fatalf("no client: %s", outcome)
	}
	if _, outcome := chatScheduledSend(context.Background(), http.DefaultClient, journeyclient.Config{}, http.MethodPost, scheduledPostBody{}); outcome != "failed" {
		t.Fatalf("no tunnel: %s", outcome)
	}
}

func TestIssue3788_RowsUseTheNamesThePersonSees(t *testing.T) {
	model := chatui.Model{Conversations: []chatui.Conversation{{ID: "room", Name: "general", Kind: chatui.PublicChannel}, {ID: "dm", Name: "9f2a7c1d-0000-4000-8000-000000000001", Kind: chatui.DirectMessage}}}
	at := time.Date(2026, 3, 4, 7, 30, 0, 0, time.UTC)
	rows := chatScheduledRows(scheduledPostsReply{Posts: []scheduledPostJSON{
		{ID: "s1", ConversationID: "room", Body: "hello", SendAt: at, State: "pending", Revision: 2, QuietPeople: 1},
		{ID: "s2", ConversationID: "dm", Body: "hi", SendAt: at, State: "failed", Refusal: "no_access", Revision: 3},
		{ID: "s3", ConversationID: "removed-room", Body: "hi", SendAt: at, State: "pending", Revision: 1},
		{ConversationID: "room", Body: "no id"},
	}}, model)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Channel != "general" || !rows[0].InChannel || rows[0].QuietPeople != 1 || rows[0].Revision != 2 {
		t.Fatalf("channel row = %+v", rows[0])
	}
	if rows[1].Channel != "" || rows[1].Refusal != "no_access" || rows[2].Channel != "" {
		t.Fatalf("an identifier must never stand in for a name: %+v", rows[1:])
	}
	if left := chatScheduledWithout(rows, "s2"); len(left) != 2 || left[0].ID != "s1" || left[1].ID != "s3" {
		t.Fatalf("without = %+v", left)
	}
}
