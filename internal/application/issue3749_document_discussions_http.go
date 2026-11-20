package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3749: the document's "Discussed in" route, under /api/chat/ (the
// prefix the page's content security policy lets the client call):
//
//	GET /api/chat/documents/{id}/discussions   the conversations that mention it, for this reader
const (
	documentDiscussionsPrefix = "/api/chat/documents/"
	documentDiscussionsSuffix = "/discussions"
)

// DocumentDiscussionsPort is the service as the HTTP surface uses it.
type DocumentDiscussionsPort interface {
	List(context.Context, *trust.Principal, string) (DocumentDiscussionsResult, error)
}

// documentDiscussionsID returns the document id of a discussions path.
func documentDiscussionsID(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, documentDiscussionsPrefix)
	if !ok {
		return "", false
	}
	escaped, ok := strings.CutSuffix(rest, documentDiscussionsSuffix)
	if !ok || escaped == "" || strings.Contains(escaped, "/") {
		return "", false
	}
	id, err := url.PathUnescape(escaped)
	if err != nil || id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

// DocumentDiscussionsHandler answers the route for an admitted person.
type DocumentDiscussionsHandler struct{ Discussions DocumentDiscussionsPort }

func (h DocumentDiscussionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	documentID, ok := documentDiscussionsID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
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
	if h.Discussions == nil {
		chatMessageWorkHTTPError(w, ErrChatWorkUnavailable)
		return
	}
	result, err := h.Discussions.List(r.Context(), verified, documentID)
	if err != nil {
		chatMessageWorkHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// OverlayDocumentDiscussions mounts the route with the admission the
// neighbouring Chat surfaces use; other paths fall through untouched.
func OverlayDocumentDiscussions(next http.Handler, port DocumentDiscussionsPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := DocumentDiscussionsHandler{Discussions: port}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := documentDiscussionsID(r.URL.Path); !ok {
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
