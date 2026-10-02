package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type chatbug012Body struct {
	Available *bool  `json:"available"`
	Reason    string `json:"reason"`
}

func chatbug012Decode(t *testing.T, w *httptest.ResponseRecorder, what string) {
	t.Helper()
	var body chatbug012Body
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "application/json") || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Available == nil || *body.Available || strings.TrimSpace(body.Reason) == "" {
		t.Fatalf("%s: want 200 and typed available:false body, got %d %q", what, w.Code, w.Body.String())
	}
}

// TestTodo_CHATBUG_012 pins the server half: a Chat read whose service is not
// composed answers 200 with {"available":false,"reason":...}, never 503/404,
// while a write to the same service still fails.
func TestTodo_CHATBUG_012(t *testing.T) {
	t.Run("writing style surface absent", func(t *testing.T) {
		w := httptest.NewRecorder()
		ChattoneHandler{}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ChattonePath+"/suggestion?conversation_id=room", nil))
		chatbug012Decode(t, w, "nil surface")
		var typedNil *ChattoneService
		w = httptest.NewRecorder()
		ChattoneHandler{Surface: typedNil}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ChattonePath+"/suggestion?conversation_id=room", nil))
		chatbug012Decode(t, w, "typed nil surface")
		w = httptest.NewRecorder()
		ChattoneHandler{}.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChattonePath+"/rewrite", strings.NewReader(`{}`)))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("a rewrite on an absent service must still fail, got %d", w.Code)
		}
	})
	t.Run("writing style service half composed", func(t *testing.T) {
		fixture, ctx, _, _, _, _ := chattoneFixture(t)
		w := httptest.NewRecorder()
		ChattoneHandler{Surface: &ChattoneService{Now: fixture.Now}}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ChattonePath+"/suggestion?conversation_id=room", nil).WithContext(ctx))
		chatbug012Decode(t, w, "uncomposed service")
	})
	t.Run("writing style composed answers unchanged", func(t *testing.T) {
		s, ctx, _, _, _, _ := chattoneFixture(t)
		w := httptest.NewRecorder()
		ChattoneHandler{Surface: s}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ChattonePath+"/suggestion?conversation_id=room", nil).WithContext(ctx))
		var body chatbug012Body
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Available != nil {
			t.Fatalf("composed read must keep its normal reply: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("renderings port absent", func(t *testing.T) {
		for _, path := range []string{"settings", "languages?conversation=room", "list?conversation=room&message=m", "selection?conversation=room&message=m"} {
			w := httptest.NewRecorder()
			ChatRenderingHandler{}.ServeHTTP(w, chatrenderRequest(t, http.MethodGet, ChatRenderingPath+"/"+path, ""))
			chatbug012Decode(t, w, path)
		}
		var typedNil *chatrenderHTTPFixture
		w := httptest.NewRecorder()
		ChatRenderingHandler{Port: typedNil}.ServeHTTP(w, chatrenderRequest(t, http.MethodGet, ChatRenderingPath+"/settings", ""))
		chatbug012Decode(t, w, "typed nil port")
		w = httptest.NewRecorder()
		ChatRenderingHandler{}.ServeHTTP(w, chatrenderRequest(t, http.MethodPost, ChatRenderingPath+"/request?conversation=room", `{}`))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("a rendering write on an absent port must still fail, got %d", w.Code)
		}
	})
}
