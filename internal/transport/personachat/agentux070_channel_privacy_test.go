package personachat

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

type privacyFixture struct {
	surfaceFixture
	room    string
	private bool
	calls   int
	err     error
}

func (s *privacyFixture) SetChannelPrivacy(_ context.Context, room string, private bool) (ChannelPrivacy, error) {
	s.calls++
	s.room, s.private = room, private
	return ChannelPrivacy{Private: private, CanChange: true}, s.err
}

// TestTodo_AGENTUX_070_ChannelPrivacyRoute is the browser's way for a channel's
// manager to require private agent answers: one POST, answered with the
// requirement as it now stands.
func TestTodo_AGENTUX_070_ChannelPrivacyRoute(t *testing.T) {
	surface := &privacyFixture{}
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		Handler{Surface: surface}.ServeHTTP(w, httptest.NewRequest("POST", Path+"/channel-privacy", strings.NewReader(body)))
		return w
	}
	w := post(`{"conversation_id":"general","private":true}`)
	if w.Code != 200 || surface.room != "general" || !surface.private || !strings.Contains(w.Body.String(), `"private":true`) || !strings.Contains(w.Body.String(), `"can_change":true`) {
		t.Fatalf("require = %d %s (%q %v)", w.Code, w.Body.String(), surface.room, surface.private)
	}
	// Turning it off is a value, not an absent field.
	if w = post(`{"conversation_id":"general","private":false}`); w.Code != 200 || surface.private || surface.calls != 2 {
		t.Fatalf("stop requiring = %d %s", w.Code, w.Body.String())
	}
	calls := surface.calls
	for name, body := range map[string]string{
		"no choice":     `{"conversation_id":"general"}`,
		"no room":       `{"private":true}`,
		"blank room":    `{"conversation_id":"  ","private":true}`,
		"unknown field": `{"conversation_id":"general","private":true,"tier":"T3"}`,
		"no body":       ``,
	} {
		if w = post(body); w.Code != 400 {
			t.Errorf("%s = %d, want 400", name, w.Code)
		}
	}
	if surface.calls != calls {
		t.Fatal("a malformed request reached the surface")
	}
	surface.err = ErrDenied
	if w = post(`{"conversation_id":"general","private":true}`); w.Code != 403 {
		t.Fatalf("a member who does not manage the channel = %d", w.Code)
	}
	surface.err = ErrConflict
	if w = post(`{"conversation_id":"general","private":true}`); w.Code != 409 {
		t.Fatalf("a change over a newer policy = %d", w.Code)
	}
	// A surface that cannot change it is unavailable, not a pretend success.
	w = httptest.NewRecorder()
	Handler{Surface: &surfaceFixture{}}.ServeHTTP(w, httptest.NewRequest("POST", Path+"/channel-privacy", strings.NewReader(`{"conversation_id":"general","private":true}`)))
	if w.Code != 503 {
		t.Fatalf("a surface that cannot change it = %d", w.Code)
	}
	w = httptest.NewRecorder()
	Handler{Surface: surface}.ServeHTTP(w, httptest.NewRequest("GET", Path+"/channel-privacy", nil))
	if w.Code != 404 {
		t.Fatalf("GET = %d", w.Code)
	}
}
