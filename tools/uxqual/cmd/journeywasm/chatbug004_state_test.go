package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// A search that the service refuses still puts the searched words on the model,
// so the results area exists to say so. Without that the client had no results
// area and printed the notice above the channel header instead.
func TestTodo_CHATBUG_004(t *testing.T) {
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"unavailable"}`))
	}))
	defer refused.Close()
	var response chatsearch.Response
	err := chatsearchRequest(t.Context(), refused.Client(), journeyclient.Config{TunnelURL: refused.URL, Bearer: "token"}, "/api/chat/search", http.MethodPost, chatsearch.Request{Query: "holiday"}, &response)
	if err == nil || chatsearchErrorCode(err) == "" {
		t.Fatalf("a 503 was not reported as a search error: %v", err)
	}
	if errors.Is(err, chatsearch.ErrInvalid) {
		t.Fatalf("a 503 was reported as an invalid request: %v", err)
	}
	m := chatui.Model{Search: "", SearchLoading: true, SearchError: "stale"}
	chatsearchSettle(&m, "holiday")
	if m.Search != "holiday" || m.SearchLoading || m.SearchError != "" {
		t.Fatalf("settled model %+v", m)
	}
	chatsearchSettle(&m, "")
	if m.Search != "" {
		t.Fatalf("clearing left Search = %q", m.Search)
	}
}
