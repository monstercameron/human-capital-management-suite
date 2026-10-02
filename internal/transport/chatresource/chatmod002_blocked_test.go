package chatresource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_CHATMOD_002_ResourceBlocked: an integration or agent posting through
// the resource API is told its text was refused (422, a stable code), not that
// chat is down (503), and nothing of the rule is revealed.
func TestTodo_CHATMOD_002_ResourceBlocked(t *testing.T) {
	s := &apiService{sendErr: fmt.Errorf("send: %w", &chatfilter.BlockedError{RuleName: "Profanity", Span: chatfilter.Span{Start: 0, End: 4}})}
	w := httptest.NewRecorder()
	NewHandler(apiConfig(t, trust.SubjectKindIntegration), s).ServeHTTP(w, apiRequest(http.MethodPost, "/v1/conversations/channel-a/posts", `{"body":"damn it"}`))
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "chat.content_blocked") || strings.Contains(w.Body.String(), "Profanity") {
		t.Fatalf("a refused text answered %d %s", w.Code, w.Body.String())
	}
	s.sendErr = chatfilter.ErrUnavailable
	w = httptest.NewRecorder()
	NewHandler(apiConfig(t, trust.SubjectKindIntegration), s).ServeHTTP(w, apiRequest(http.MethodPost, "/v1/conversations/channel-a/posts", `{"body":"hello"}`))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unavailable filter answered %d", w.Code)
	}
}
