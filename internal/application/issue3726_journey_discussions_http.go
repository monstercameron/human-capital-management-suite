package application

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3726: the journey's "Discussed in" routes, under /api/chat/ (the
// prefix the page's content security policy lets the client call):
//
//	GET  /api/chat/journeys/{id}/discussions   the conversations it was discussed in, for this reader
//	POST /api/chat/journeys/{id}/discussions   remember that it was shared into a conversation
const (
	journeyDiscussionsPrefix = "/api/chat/journeys/"
	journeyDiscussionsSuffix = "/discussions"
)

// JourneyDiscussionsPort is the service as the HTTP surface uses it.
type JourneyDiscussionsPort interface {
	List(context.Context, *trust.Principal, string) (JourneyDiscussionsResult, error)
	Record(ctx context.Context, p *trust.Principal, journeyID, conversationID string) error
}

// journeyDiscussionsID returns the journey id of a discussions path.
func journeyDiscussionsID(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, journeyDiscussionsPrefix)
	if !ok {
		return "", false
	}
	escaped, ok := strings.CutSuffix(rest, journeyDiscussionsSuffix)
	if !ok || escaped == "" || strings.Contains(escaped, "/") {
		return "", false
	}
	id, err := url.PathUnescape(escaped)
	if err != nil || id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

type journeyDiscussionCommand struct {
	ConversationID string `json:"conversation_id"`
}

// JourneyDiscussionsHandler answers the routes for an admitted person.
type JourneyDiscussionsHandler struct{ Discussions JourneyDiscussionsPort }

func (h JourneyDiscussionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	journeyID, ok := journeyDiscussionsID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
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
	if r.Method == http.MethodPost {
		var command journeyDiscussionCommand
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			chatMessageWorkHTTPError(w, ErrChatWorkInvalid)
			return
		}
		if err := h.Discussions.Record(r.Context(), verified, journeyID, command.ConversationID); err != nil {
			chatMessageWorkHTTPError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	result, err := h.Discussions.List(r.Context(), verified, journeyID)
	if err != nil {
		chatMessageWorkHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// OverlayJourneyDiscussions mounts the routes with the admission the
// neighbouring Chat surfaces use; other paths fall through untouched.
func OverlayJourneyDiscussions(next http.Handler, port JourneyDiscussionsPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := JourneyDiscussionsHandler{Discussions: port}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := journeyDiscussionsID(r.URL.Path); !ok {
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
