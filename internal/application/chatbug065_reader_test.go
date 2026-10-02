package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// chatbug065Port refuses one message as invalid, the way the pipeline does for
// a text whose language it cannot place, and reads every other.
type chatbug065Port struct {
	*chatrenderHTTPFixture
	invalid string
}

func (p *chatbug065Port) ReadRenderingSelection(ctx context.Context, scope chatstore.RenderingScope, id string) (chatrender.Rendering, chatrender.Mark, error) {
	if id == p.invalid {
		return chatrender.Rendering{}, chatrender.Mark{}, chatrender.ErrInvalid
	}
	return p.chatrenderHTTPFixture.ReadRenderingSelection(ctx, scope, id)
}

// TestTodo_CHATBUG_065_Reader: the reading view of a conversation is asked for
// with the same address form in every kind of conversation and answers 200. A
// message the pipeline cannot judge is left out of the answer (the page keeps it
// as written) and does not turn the batch into a 400; a request that is not
// well formed is still refused.
func TestTodo_CHATBUG_065_Reader(t *testing.T) {
	port := &chatbug065Port{chatrenderHTTPFixture: &chatrenderHTTPFixture{rendering: chatrender.Rendering{Text: "safe view"}, settings: chatrender.DefaultPreference("en")}, invalid: "odd"}
	handler := ChatRenderingHandler{Port: port}
	get := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, chatrenderRequest(t, http.MethodGet, ChatRenderingPath+"/reader?"+query, ""))
		return w
	}
	w := get("conversation=dm&tenant=tenant-a&message=one&message=odd&message=two")
	var out map[string]json.RawMessage
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("a batch with one message the pipeline cannot judge answered %d: %s", w.Code, w.Body.String())
	}
	if _, ok := out["one"]; !ok {
		t.Errorf("a readable message is missing from the answer: %v", out)
	}
	if _, ok := out["two"]; !ok {
		t.Errorf("a message after the odd one is missing from the answer: %v", out)
	}
	if _, ok := out["odd"]; ok {
		t.Errorf("the message the pipeline could not judge was answered: %v", out)
	}
	// The malformed requests are still refused.
	for name, query := range map[string]string{"no conversation": "message=one", "no message": "conversation=dm", "too many": "conversation=dm" + repeatMessages(101)} {
		if got := get(query); got.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", name, got.Code)
		}
	}
	// A denial is still a denial.
	port.deny = true
	if got := get("conversation=dm&message=one"); got.Code != http.StatusForbidden {
		t.Errorf("a denied reader answered %d", got.Code)
	}
}

func repeatMessages(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += "&message=m"
	}
	return out
}
