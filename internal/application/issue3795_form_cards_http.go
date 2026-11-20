package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3795: the routes of a form card, under /api/chat/ (the prefix the
// page's content security policy lets the Chat client call):
//
//	GET  /api/chat/conversations/{id}/forms?post={post}&post={post}   what the reader may see of each card
//	POST /api/chat/conversations/{id}/forms/{post}/answers            answer a card once
//
// Posting a card is not a route: an agent run posts through the conversation
// service, and reads the answer through ChatFormCards.Result.
const (
	chatFormPrefix = "/api/chat/conversations/"
)

// ChatFormPort is the form-card use case as the HTTP surface uses it.
type ChatFormPort interface {
	Views(context.Context, *trust.Principal, string, []string) (map[string]ChatFormView, error)
	Submit(context.Context, *trust.Principal, string, string, map[string]string) (ChatFormView, error)
}

type chatFormAnswers struct {
	Values map[string]string `json:"values"`
}

type chatFormReply struct {
	Views map[string]ChatFormView `json:"views,omitempty"`
	View  *ChatFormView           `json:"view,omitempty"`
}

type chatFormError struct {
	Code   string            `json:"code"`
	Fields map[string]string `json:"fields,omitempty"`
}

// chatFormRoute parses a form-card path into its conversation, the post (empty
// for the list route) and whether it is the answer route.
func chatFormRoute(path string) (conversation, post string, answer, ok bool) {
	rest, found := strings.CutPrefix(path, chatFormPrefix)
	if !found {
		return "", "", false, false
	}
	parts := strings.Split(rest, "/")
	switch {
	case len(parts) == 2 && parts[1] == "forms" && parts[0] != "":
		return parts[0], "", false, true
	case len(parts) == 4 && parts[1] == "forms" && parts[3] == "answers" && parts[0] != "" && parts[2] != "":
		return parts[0], parts[2], true, true
	}
	return "", "", false, false
}

func chatFormHTTPError(w http.ResponseWriter, err error) {
	status, body := http.StatusServiceUnavailable, chatFormError{Code: "unavailable"}
	var invalid *ChatFormInvalidError
	switch {
	case errors.As(err, &invalid):
		status, body = http.StatusBadRequest, chatFormError{Code: "invalid_form", Fields: invalid.Fields}
	case errors.Is(err, ErrChatFormInvalid):
		status, body = http.StatusBadRequest, chatFormError{Code: "invalid_argument"}
	case errors.Is(err, ErrChatFormNotFound):
		status, body = http.StatusNotFound, chatFormError{Code: "not_found"}
	case errors.Is(err, ErrChatFormDenied):
		status, body = http.StatusForbidden, chatFormError{Code: "permission_denied"}
	case errors.Is(err, ErrChatFormConflict):
		status, body = http.StatusConflict, chatFormError{Code: "already_answered"}
	default:
		// The body never carries the cause; a developer cell can opt in to
		// seeing it in the server log, as the other chat surfaces do.
		if err != nil && os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_form_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ChatFormHandler answers the form-card routes for an admitted person.
type ChatFormHandler struct{ Forms ChatFormPort }

func (h ChatFormHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	conversation, post, answer, ok := chatFormRoute(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	want := http.MethodGet
	if answer {
		want = http.MethodPost
	}
	if r.Method != want {
		w.Header().Set("Allow", want)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(chatFormError{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(chatFormError{Code: "unauthenticated"})
		return
	}
	// A form is answered by a person who read it. An agent reads the answer
	// through the run's own governed hook, not this route.
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatFormHTTPError(w, ErrChatFormDenied)
		return
	}
	if h.Forms == nil {
		chatFormHTTPError(w, ErrChatFormUnavailable)
		return
	}
	if answer {
		var body chatFormAnswers
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.Values == nil {
			chatFormHTTPError(w, ErrChatFormInvalid)
			return
		}
		view, err := h.Forms.Submit(r.Context(), verified, conversation, post, body.Values)
		if err != nil {
			chatFormHTTPError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(chatFormReply{View: &view})
		return
	}
	views, err := h.Forms.Views(r.Context(), verified, conversation, r.URL.Query()["post"])
	if err != nil {
		chatFormHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(chatFormReply{Views: views})
}

// OverlayChatForms mounts the form-card routes with the admission the
// neighbouring Chat surfaces use. Paths that are not form routes pass through.
func OverlayChatForms(next http.Handler, forms ChatFormPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatFormHandler{Forms: forms}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, _, ok := chatFormRoute(r.URL.Path); !ok {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatFormError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
