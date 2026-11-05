package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATSEARCH_002_UserinfoTunnel pins that a tunnel address carrying
// userinfo is accepted, exactly as journeyclient's own validTunnelURL and
// every other REST client accept it. The Go transport drops userinfo rather
// than sending it, so the request still authenticates with the bearer alone.
func TestTodo_CHATSEARCH_002_UserinfoTunnel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" || r.URL.Path != "/api/chat/search" {
			t.Error("wrong endpoint or authentication")
		}
		_ = json.NewEncoder(w).Encode(chatsearch.Response{Mode: "keyword"})
	}))
	defer server.Close()
	withUser := strings.Replace(server.URL, "://", "://reader@", 1)
	cfg := journeyclient.Config{TunnelURL: withUser, Bearer: "fixture"}
	var got chatsearch.Response
	if err := chatsearchRequest(context.Background(), server.Client(), cfg, "/api/chat/search", "POST", chatsearch.Request{Query: "budget"}, &got); err != nil || got.Mode != "keyword" {
		t.Fatalf("userinfo tunnel refused: %+v %v", got, err)
	}
}
