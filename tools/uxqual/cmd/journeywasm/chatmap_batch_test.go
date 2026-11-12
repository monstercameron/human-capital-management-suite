package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_ISSUE3676_BatchBody checks the message lookup sends every post id in
// one body, under the field name the server reads.
func TestTodo_ISSUE3676_BatchBody(t *testing.T) {
	var sent map[string]any
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/api/chat/locations/v1/message" || json.Unmarshal(raw, &sent) != nil {
			t.Fatal(r.URL.Path, string(raw))
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"m1":[],"m2":[]}`)), Request: r}, nil
	})
	cfg := journeyclient.Config{TunnelURL: "https://product.example", Bearer: "fixture"}
	raw, err := ChatmapRequestHTTP(context.Background(), &http.Client{Transport: transport}, cfg, "message", ChatmapRequest{TenantID: "t", ConversationID: "c", PostIDs: []string{"m1", "m2"}})
	if err != nil || string(raw) != `{"m1":[],"m2":[]}` {
		t.Fatal(string(raw), err)
	}
	ids, _ := sent["PostIDs"].([]any)
	if len(ids) != 2 || ids[0] != "m1" || ids[1] != "m2" || sent["PostID"] != "" {
		t.Fatal(sent)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
