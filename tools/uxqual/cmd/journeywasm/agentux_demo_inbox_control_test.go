package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type agentuxDemoInboxRoundTrip func(*http.Request) (*http.Response, error)

func (f agentuxDemoInboxRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAgentUXDemo_Inbox_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(agentuxDemoInboxControl(productui.ResolveProductLocale(locale)))
		if err != nil || strings.ContainsAny(markup, "⟦⟧") || strings.Contains(markup, "AGENTUX") || strings.Contains(markup, ` value=`) || strings.Contains(markup, "font-family") {
			t.Fatalf("unsafe %s rendering: %s %v", locale, markup, err)
		}
		for _, want := range []string{agentuxDemoInboxText(locale, "title"), `data-support-demo-form="true"`, `for="support-demo-from"`, `aria-describedby="support-demo-status"`, `aria-live="polite"`, `grid-template-columns:minmax(0,1fr)`} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s missing %q", locale, want)
			}
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("missing RTL direction")
		}
	}
}

func TestAgentUXDemo_InboxClient(t *testing.T) {
	client := &http.Client{Transport: agentuxDemoInboxRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != agentdemo.Path || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer session" || r.Method != http.MethodPost {
			t.Fatalf("request binding: %s %s", r.Method, r.URL)
		}
		var email agentdemo.Email
		if err := json.NewDecoder(r.Body).Decode(&email); err != nil || email.Subject != "Late delivery" || email.IdempotencyKey != "email-key-123" {
			t.Fatalf("email payload: %+v %v", email, err)
		}
		return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader(`{"message_id":"email-1","state":"QUEUED"}`)), Header: http.Header{}}, nil
	})}
	receipt, err := agentuxDemoInboxRequest(context.Background(), client, journeyclient.Config{TunnelURL: "wss://workspace.example/old?tenant=other", Bearer: "session"}, agentdemo.Email{Subject: "Late delivery", IdempotencyKey: "email-key-123"})
	if err != nil || receipt.MessageID != "email-1" {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
	for _, tc := range []struct {
		status int
		err    error
	}{{400, agentdemo.ErrInvalid}, {403, agentdemo.ErrDenied}, {409, agentdemo.ErrConflict}, {500, agentdemo.ErrUnavailable}} {
		client.Transport = agentuxDemoInboxRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("provider details")), Header: http.Header{}}, nil
		})
		if _, err := agentuxDemoInboxRequest(context.Background(), client, journeyclient.Config{TunnelURL: "https://workspace.example", Bearer: "session"}, agentdemo.Email{}); !errors.Is(err, tc.err) || agentuxDemoInboxErrorKey(err) == "" {
			t.Fatalf("status %d: %v", tc.status, err)
		}
	}
}
