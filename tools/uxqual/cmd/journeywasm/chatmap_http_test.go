package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatmapHTTPFixture struct {
	calls  int
	path   string
	status int
}

func (f *chatmapHTTPFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls++
	f.path = r.URL.String()
	if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "Bearer fixture" {
		return nil, chat.ErrInvalidArgument
	}
	return &http.Response{StatusCode: f.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
}
func TestTodo_CHATMAP_003_Security(t *testing.T) {
	fixture := &chatmapHTTPFixture{status: 200}
	client := &http.Client{Transport: fixture}
	cfg := journeyclient.Config{TunnelURL: "https://product.example/old?location=secret", Bearer: "fixture"}
	_, err := ChatmapRequestHTTP(context.Background(), client, cfg, "attach", ChatmapRequest{TenantID: "t", ConversationID: "c", PostID: "m", Place: chat.LocationPlace{Position: &chat.LocationPosition{Latitude: 42.123456, Longitude: 10, Accuracy: 20}}})
	if err != nil || fixture.path != "https://product.example/api/chat/locations/v1/attach" {
		t.Fatal(fixture.path, err)
	}
	fixture.status = 302
	if _, err = ChatmapRequestHTTP(context.Background(), client, cfg, "read", ChatmapRequest{}); err != chat.ErrUnavailable || fixture.calls != 2 {
		t.Fatal("redirect followed", err, fixture.calls)
	}
	if _, err = ChatmapRequestHTTP(context.Background(), client, cfg, "unknown", ChatmapRequest{}); err != chat.ErrInvalidArgument || fixture.calls != 2 {
		t.Fatal(err)
	}
	cfg.TunnelURL = "file:///location"
	if _, err = ChatmapRequestHTTP(context.Background(), client, cfg, "read", ChatmapRequest{}); err != chat.ErrUnavailable {
		t.Fatal(err)
	}
}
