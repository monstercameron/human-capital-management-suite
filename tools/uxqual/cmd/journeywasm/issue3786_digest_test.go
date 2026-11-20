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

func TestIssue3786_ClientReadsOnlyTheSignedInPersonsDigest(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/digest" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Query().Get("host") != "tenant" || r.URL.Query().Get("subject") != "" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		body := `{"host_tenant_id":"tenant","pref":{"cadence":"daily","minute":480,"timezone":"UTC"},"due":true,"through":"2026-03-10T08:05:00Z","items":[
 {"why":"quiet_hours","conversation_id":"room","conversation_name":"ops","conversation_kind":"PUBLIC_CHANNEL","post_id":"m1","sequence":4,"author_id":"dana-id","at":"2026-03-10T02:00:00Z","count":1},
 {"why":"followed","conversation_id":"dm","conversation_name":"0123456789abcdef-0123","conversation_kind":"DIRECT","post_id":"m2","author_id":"lee-id","at":"2026-03-10T03:00:00Z","count":2},
 {"why":"muted","conversation_id":"","post_id":"x"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	reply, err := chatDigestRequest(context.Background(), client, cfg)
	if err != nil || !reply.Due || len(reply.Items) != 3 {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	names := map[string]string{"dana-id": "Dana Cole", "lee-id": "0123456789abcdef-4567"}
	d := chatDigestView(reply, func(id string) string { return names[id] })
	if d.Cadence != "daily" || d.Minute != 480 || !d.Due || len(d.Items) != 2 || d.Through.IsZero() {
		t.Fatalf("digest = %+v; an item with no room must be dropped", d)
	}
	if a := d.Items[0]; a.AuthorName != "Dana Cole" || a.ConversationName != "ops" || a.Why != "quiet_hours" || a.Sequence != 4 {
		t.Fatalf("first = %+v", a)
	}
	// Identifier-looking names and a direct conversation's stored name are never shown.
	if b := d.Items[1]; b.AuthorName != "" || b.ConversationName != "" || b.Count != 2 {
		t.Fatalf("second = %+v", b)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"code":"unavailable"}`))}, nil
	})
	if _, err := chatDigestRequest(context.Background(), client, cfg); err == nil {
		t.Fatal("a refusal must not read as an empty digest")
	}
	if _, err := chatDigestRequest(context.Background(), client, journeyclient.Config{}); err == nil {
		t.Fatal("no tunnel or bearer must be unavailable")
	}
}

func TestIssue3786_ChoiceAndSeenAreSentWithOnlyWhatTheServerNeeds(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
	var got map[string]any
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat/digest" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		got = nil
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := chatDigestPost(ctx, client, cfg, chatDigestCommand{Action: "set", Cadence: "daily", Minute: 480, Timezone: "Europe/Berlin"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got["host"] != "tenant" || got["action"] != "set" || got["timezone"] != "Europe/Berlin" || got["through"] != nil {
		t.Fatalf("body = %v; it may name only the host and the choice, never a person", got)
	}
	through := time.Date(2026, 3, 10, 8, 5, 0, 0, time.UTC)
	if err := chatDigestPost(ctx, client, cfg, chatDigestCommand{Action: "seen", Through: &through}); err != nil || got["through"] != "2026-03-10T08:05:00Z" || got["cadence"] != nil {
		t.Fatalf("seen: %v body=%v", err, got)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"code":"unavailable"}`))}, nil
	})
	if err := chatDigestPost(ctx, client, cfg, chatDigestCommand{Action: "seen", Through: &through}); err == nil {
		t.Fatal("a refused save must be an error so the bell can say so")
	}
}
