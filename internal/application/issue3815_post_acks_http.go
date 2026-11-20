package application

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3815: the routes of an acknowledgement, under /api/chat/ (the prefix
// the page's content security policy lets the Chat client call):
//
//	GET  /api/chat/conversations/{id}/acks?post={post}&post={post}   what the reader may see of each post
//	POST /api/chat/conversations/{id}/acks/{post}/require            ask the channel to acknowledge a post
//	POST /api/chat/conversations/{id}/acks/{post}/acknowledge        acknowledge it, once
const chatAckPrefix = "/api/chat/conversations/"

// ChatAckPort is the acknowledgement use case as the HTTP surface uses it.
type ChatAckPort interface {
	Views(context.Context, *trust.Principal, string, []string) (ChatAckReply, error)
	Require(context.Context, *trust.Principal, string, string) (ChatAckView, error)
	Acknowledge(context.Context, *trust.Principal, string, string) (ChatAckView, error)
}

// Views makes the use case satisfy ChatAckPort.
func (c *ChatPostAcks) Views(ctx context.Context, human *trust.Principal, conversation string, posts []string) (ChatAckReply, error) {
	return c.Reply(ctx, human, conversation, posts)
}

type chatAckBody struct {
	Code  string       `json:"code,omitempty"`
	View  *ChatAckView `json:"view,omitempty"`
	Views any          `json:"views,omitempty"`
}

// chatAckRoute parses an acknowledgement path into its conversation, the post
// (empty for the list route) and the action ("require" or "acknowledge").
func chatAckRoute(path string) (conversation, post, action string, ok bool) {
	rest, found := strings.CutPrefix(path, chatAckPrefix)
	if !found {
		return "", "", "", false
	}
	parts := strings.Split(rest, "/")
	switch {
	case len(parts) == 2 && parts[1] == "acks" && parts[0] != "":
		return parts[0], "", "", true
	case len(parts) == 4 && parts[1] == "acks" && parts[0] != "" && parts[2] != "" && (parts[3] == "require" || parts[3] == "acknowledge"):
		return parts[0], parts[2], parts[3], true
	}
	return "", "", "", false
}

func chatAckHTTPError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, ErrChatAckInvalid):
		status, code = http.StatusBadRequest, "invalid_argument"
	case errors.Is(err, ErrChatAckNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, ErrChatAckDenied):
		status, code = http.StatusForbidden, "permission_denied"
	default:
		// The body never carries the cause; a developer cell can opt in to
		// seeing it in the server log, as the other chat surfaces do.
		if err != nil && os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_ack_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chatAckBody{Code: code})
}

// ChatAckHandler answers the acknowledgement routes for an admitted person.
type ChatAckHandler struct{ Acks ChatAckPort }

func (h ChatAckHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	conversation, post, action, ok := chatAckRoute(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	want := http.MethodPost
	if action == "" {
		want = http.MethodGet
	}
	if r.Method != want {
		w.Header().Set("Allow", want)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(chatAckBody{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(chatAckBody{Code: "unauthenticated"})
		return
	}
	// An acknowledgement is a person's own act; an agent never makes one.
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatAckHTTPError(w, ErrChatAckDenied)
		return
	}
	if h.Acks == nil {
		chatAckHTTPError(w, ErrChatAckUnavailable)
		return
	}
	var (
		view *ChatAckView
		err  error
	)
	switch action {
	case "":
		reply, err := h.Acks.Views(r.Context(), verified, conversation, r.URL.Query()["post"])
		if err != nil {
			chatAckHTTPError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(reply)
		return
	case "require":
		var got ChatAckView
		if got, err = h.Acks.Require(r.Context(), verified, conversation, post); err == nil {
			view = &got
		}
	default:
		var got ChatAckView
		if got, err = h.Acks.Acknowledge(r.Context(), verified, conversation, post); err == nil {
			view = &got
		}
	}
	if err != nil {
		chatAckHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(chatAckBody{View: view})
}

// OverlayChatAcks mounts the acknowledgement routes with the admission the
// neighbouring Chat surfaces use. Paths that are not acknowledgement routes
// pass through.
func OverlayChatAcks(next http.Handler, acks ChatAckPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatAckHandler{Acks: acks}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, _, ok := chatAckRoute(r.URL.Path); !ok {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatAckBody{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
