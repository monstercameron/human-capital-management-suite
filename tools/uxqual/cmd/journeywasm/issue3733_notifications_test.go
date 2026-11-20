package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestIssue3733_ClientReadsOnlyTheSignedInPersonsFeed(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/notifications" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Query().Get("host") != "tenant" || r.URL.Query().Get("subject") != "" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		body := `{"host_tenant_id":"tenant","unread":1,"items":[
 {"id":"m1","kind":"mention","conversation_id":"room","conversation_name":"ops","conversation_kind":"PUBLIC_CHANNEL","post_id":"m1","sequence":4,"author_id":"dana-id","body":"look?","at":"2026-01-01T03:04:05Z","read":false},
 {"id":"m2","kind":"direct","conversation_id":"dm","conversation_name":"0123456789abcdef-0123","conversation_kind":"DIRECT","post_id":"m2","sequence":1,"author_id":"lee-id","at":"2026-01-01T04:04:05Z","read":true},
 {"id":"","kind":"mention","conversation_id":"room"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	reply, err := chatNotificationsRequest(context.Background(), client, cfg)
	if err != nil || len(reply.Items) != 3 {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	names := map[string]string{"dana-id": "Dana Cole", "lee-id": "0123456789abcdef-4567"}
	items := chatNotificationItems(reply, func(id string) string { return names[id] })
	if len(items) != 2 {
		t.Fatalf("an item with no id must be dropped: %+v", items)
	}
	if a := items[0]; a.ID != "m1" || a.Kind != "mention" || a.AuthorName != "Dana Cole" || a.ConversationName != "ops" || a.Sequence != 4 || a.Read || a.CreatedAt.IsZero() {
		t.Fatalf("first = %+v", a)
	}
	// A name that is an identifier, and a direct conversation's stored name, are
	// never shown.
	if b := items[1]; b.AuthorName != "" || b.ConversationName != "" || !b.Read || b.AuthorID != "lee-id" {
		t.Fatalf("second = %+v", b)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))}, nil
	})
	if _, err := chatNotificationsRequest(context.Background(), client, cfg); err == nil {
		t.Fatal("a refusal must not read as an empty feed")
	}
	if _, err := chatNotificationsRequest(context.Background(), client, journeyclient.Config{}); err == nil {
		t.Fatal("no tunnel or bearer must be unavailable")
	}
}

func TestIssue3733_ReadChoiceIsPostedWithOnlyWhatTheServerNeeds(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
	var got map[string]any
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat/notifications" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		got = nil
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"marked":1}`)), Header: http.Header{}}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := chatNotificationsMarkRead(ctx, client, cfg, []string{"m1"}, false); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["host"] != "tenant" || len(got["ids"].([]any)) != 1 {
		t.Fatalf("body = %v; it may name only the host and the items, never a person", got)
	}
	if err := chatNotificationsMarkRead(ctx, client, cfg, nil, true); err != nil || got["all"] != true {
		t.Fatalf("mark all: %v body=%v", err, got)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"code":"unavailable"}`))}, nil
	})
	if err := chatNotificationsMarkRead(ctx, client, cfg, []string{"m1"}, false); err == nil {
		t.Fatal("a refused save must be an error so the bell can put the item back")
	}
}
