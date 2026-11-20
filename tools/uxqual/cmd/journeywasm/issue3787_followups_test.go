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

func TestIssue3787_ClientReadsOnlyTheSignedInPersonsList(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/follow-ups" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Query().Get("host") != "tenant" || r.URL.Query().Get("subject") != "" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		body := `{"host_tenant_id":"tenant","owed":[{"conversation_id":"room","post_id":"m1","sequence":4,"direction":"owed","author_id":"dana-id","body":"confirm?","asked_at":"2026-01-01T03:04:05Z"}],"waiting":[]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	reply, err := chatFollowUpsRequest(context.Background(), client, cfg)
	if err != nil || len(reply.Owed) != 1 {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))}, nil
	})
	if _, err := chatFollowUpsRequest(context.Background(), client, cfg); err == nil {
		t.Fatal("a refusal must not read as an empty list")
	}
	if _, err := chatFollowUpsRequest(context.Background(), client, journeyclient.Config{}); err == nil {
		t.Fatal("no tunnel or bearer must be unavailable")
	}
}

func TestIssue3787_ChoiceIsPostedWithOnlyWhatTheServerNeeds(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var got map[string]any
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat/follow-ups" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"snoozed"}`)), Header: http.Header{}}, nil
	})}
	if err := chatFollowUpChoose(context.Background(), client, cfg, "room", "m1", "owed", "snooze", now); err != nil {
		t.Fatal(err)
	}
	if got["conversation_id"] != "room" || got["post_id"] != "m1" || got["direction"] != "owed" || got["action"] != "snooze" || got["until"] != "2026-01-03T03:04:05Z" || got["host"] != "tenant" {
		t.Fatalf("body = %v", got)
	}
	got = nil
	if err := chatFollowUpChoose(context.Background(), client, cfg, "room", "m1", "owed", "done", now); err != nil || got["action"] != "resolve" || got["until"] != nil {
		t.Fatalf("done maps to resolve and carries no time: %v err=%v", got, err)
	}
	if err := chatFollowUpChoose(context.Background(), client, cfg, "room", "m1", "owed", "explode", now); err == nil {
		t.Fatal("an unknown action must not be sent")
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))}, nil
	})
	if err := chatFollowUpChoose(context.Background(), client, cfg, "room", "m1", "owed", "dismiss", now); err == nil {
		t.Fatal("a refusal must be reported")
	}
}

func TestIssue3787_RowsUseTheNamesThePersonSees(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "person", Locale: "en-US"}
	model := chatui.Model{Conversations: []chatui.Conversation{{ID: "room", Name: "general", Kind: chatui.PublicChannel}, {ID: "dm", Name: "9f2a7c1d-0000-4000-8000-000000000001", Kind: chatui.DirectMessage}}}
	now := time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)
	reply := followUpsReply{
		Owed: []followUpJSON{
			{ConversationID: "room", PostID: "m1", Sequence: 4, Direction: "owed", AuthorID: "dana-id", Body: "confirm?", AskedAt: time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC), Evidence: "reacted"},
			{ConversationID: "", PostID: "bad"},
		},
		Waiting: []followUpJSON{
			{ConversationID: "dm", PostID: "m2", Direction: "waiting", AuthorID: "person", Body: "swap?", AskedAt: now.Add(-3 * time.Hour), WaitingOn: []string{"priya-id", "0f2a7c1d-0000-4000-8000-000000000009"}, ThreadRootID: "root-1", Kept: true},
		},
	}
	rows := chatFollowUpRows(reply, cfg, model, map[string]string{"dana-id": "Dana", "priya-id": "Priya"}, now)
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	if r := rows[0]; r.Channel != "general" || !r.InChannel || r.Who != "Dana" || r.AgeHours != 30 || r.Evidence != "reacted" || r.Sequence != 4 || r.Direction != "owed" {
		t.Fatalf("owed row = %+v", r)
	}
	// An identifier never stands in for a name, in the channel or among the people asked.
	if r := rows[1]; r.Channel != "" || r.Who != "Priya" || r.AgeHours != 3 || !r.Kept || r.ThreadRootID != "root-1" {
		t.Fatalf("waiting row = %+v", r)
	}
}

func TestIssue3787_RowsAfterAChoiceDropTheRow(t *testing.T) {
	rows := []chatui.FollowUpRow{{ConversationID: "room", PostID: "m1", Direction: "owed"}, {ConversationID: "room", PostID: "m1", Direction: "waiting"}}
	if got := chatFollowUpsWithout(rows, "room", "m1", "owed"); len(got) != 1 || got[0].Direction != "waiting" {
		t.Fatalf("without = %+v", got)
	}
	kept := chatFollowUpsMarkKept(rows, "room", "m1", "waiting")
	if kept[0].Kept || !kept[1].Kept {
		t.Fatalf("kept = %+v", kept)
	}
}
