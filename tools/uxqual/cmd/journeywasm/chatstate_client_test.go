package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

type chatstateRoundTrip func(*http.Request) (*http.Response, error)

func (f chatstateRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTodo_CHATSTATE_002_Client(t *testing.T) {
	posts := 0
	client := chatstateClient{BaseURL: "http://fixture.invalid", Bearer: "fixture", Tenant: "tenant", HTTP: &http.Client{Transport: chatstateRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer fixture" || r.URL.Path != "/api/chat/v1/channel-status/room" {
			t.Fatal("session or path missing")
		}
		w := httptest.NewRecorder()
		if r.Method == "POST" {
			posts++
			_ = json.NewEncoder(w).Encode(chat.ChannelStatus{Revision: 2})
			return w.Result(), nil
		}
		_ = json.NewEncoder(w).Encode(chat.ChannelStatusSnapshot{Status: chat.ChannelStatus{TenantID: "tenant", ConversationID: "room", Revision: 2, Status: chatpolicy.StatusLocked}, Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusOpen}}})
		return w.Result(), nil
	})}}
	view := chatui.ChannelStatusView{Status: chat.ChannelStatus{Revision: 1}, CanPost: true}
	loaded, err := client.Load(t.Context(), "room", view)
	if err != nil || loaded.CanPost || !loaded.Updated || loaded.Status.Status != chatpolicy.StatusLocked {
		t.Fatalf("load = %+v %v", loaded, err)
	}
	if _, err = client.Change(t.Context(), chat.ChangeChannelStatusRequest{ConversationID: "room", Status: chatpolicy.StatusLocked, ExpectedRevision: 1, Reason: "incident"}, view); err != nil || posts != 1 {
		t.Fatalf("change = %v posts %d", err, posts)
	}
	ctx, cancel := context.WithCancel(t.Context())
	count := 0
	err = client.Watch(ctx, "room", view, func(v chatui.ChannelStatusView) {
		count++
		if v.Status.Revision != 2 {
			t.Fatal("watch did not update")
		}
		cancel()
	})
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatalf("watch cancellation = %v count %d", err, count)
	}
	client.HTTP.Transport = chatstateRoundTrip(func(*http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"code":"revision_conflict"}`))
		return w.Result(), nil
	})
	failed, err := client.Change(t.Context(), chat.ChangeChannelStatusRequest{ConversationID: "room"}, view)
	if err == nil || failed.Error != "revision_conflict" {
		t.Fatalf("typed refusal = %+v %v", failed, err)
	}
	client.HTTP.Transport = chatstateRoundTrip(func(*http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		_, _ = w.Write([]byte(strings.Repeat("x", 65<<10)))
		return w.Result(), nil
	})
	failed, err = client.Load(t.Context(), "room", view)
	if err == nil || !failed.Unavailable || failed.CanPost {
		t.Fatal("oversize read did not fail closed")
	}
}
