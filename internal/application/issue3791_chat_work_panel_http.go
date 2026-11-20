package application

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3791: GET /api/chat/conversations/{id}/work answers "Work from this
// conversation": what the conversation proposed and the tasks its messages
// became, each with a state, its source and when it was read. It sits under
// /api/chat/, the prefix the page's content security policy lets the Chat
// client call.
const (
	chatLinkedWorkPrefix = "/api/chat/conversations/"
	chatLinkedWorkSuffix = "/work"
)

// ChatLinkedWorkPort is the panel as the HTTP surface uses it.
type ChatLinkedWorkPort interface {
	List(context.Context, *trust.Principal, string) (ChatLinkedWorkResult, error)
}

// chatLinkedWorkConversation returns the conversation id of a panel path.
func chatLinkedWorkConversation(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, chatLinkedWorkPrefix)
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(rest, chatLinkedWorkSuffix)
	if !ok || id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

// ChatLinkedWorkHandler answers the panel route for an admitted person.
type ChatLinkedWorkHandler struct{ Work ChatLinkedWorkPort }

func (h ChatLinkedWorkHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	conversation, ok := chatLinkedWorkConversation(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(chatMessageWorkError{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(chatMessageWorkError{Code: "unauthenticated"})
		return
	}
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatMessageWorkHTTPError(w, ErrChatWorkDenied)
		return
	}
	if h.Work == nil {
		chatMessageWorkHTTPError(w, ErrChatWorkUnavailable)
		return
	}
	result, err := h.Work.List(r.Context(), verified, conversation)
	if err != nil {
		chatMessageWorkHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// OverlayChatLinkedWork mounts the panel route with the admission the
// neighbouring Chat surfaces use. Paths that are not the panel route fall
// through untouched.
func OverlayChatLinkedWork(next http.Handler, work ChatLinkedWorkPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatLinkedWorkHandler{Work: work}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := chatLinkedWorkConversation(r.URL.Path); !ok {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatMessageWorkError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
