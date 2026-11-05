package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATTONE_006_Browser: when both model calls stall, the server's
// typed "stalled" (HTTP 503) reaches the status line as "stalled", not as the
// general "unavailable", so the line can name the retry. Anything the server
// does not type stays "unavailable". (The "stalled" status words and the
// five-second "taking longer" escalation live in the toolbar copy and the wasm
// controller; this test pins the client contract up to them.)
func TestTodo_CHATTONE_006_Browser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != chattoneHTTPPath+"/rewrite" || r.Method != http.MethodPost {
			t.Error("wrong route", r.URL.Path, r.Method)
		}
		var draft chattoneDraftRequest
		if json.NewDecoder(r.Body).Decode(&draft) != nil || draft.StyleID == "" {
			t.Error("draft changed in transit")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":"stalled"}`)
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL, Bearer: "fixture"}
	_, err := chattoneRequest(context.Background(), server.Client(), cfg, "rewrite", chattoneDraftRequest{"room", "Please keep this draft", "professional"})
	if err == nil || chattoneErrorCode(err) != "stalled" {
		t.Fatalf("both calls stalled, the status code is %q, want stalled", chattoneErrorCode(err))
	}

	untyped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":"something the client does not know"}`)
	}))
	defer untyped.Close()
	_, err = chattoneRequest(context.Background(), untyped.Client(), journeyclient.Config{TunnelURL: untyped.URL, Bearer: "fixture"}, "rewrite", chattoneDraftRequest{"room", "Please keep this draft", "professional"})
	if err == nil || chattoneErrorCode(err) != "unavailable" {
		t.Fatalf("an untyped failure surfaces as %q, want unavailable", chattoneErrorCode(err))
	}
}
