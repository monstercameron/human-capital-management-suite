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

// Issue 3793: the routes of a typed request, under /api/chat/ (the prefix the
// page's content security policy lets the Chat client call):
//
//	GET  /api/chat/conversations/{id}/requests                       which request commands this person may use here
//	POST /api/chat/conversations/{id}/requests/{command}/preview     check a request and read back what would be sent
//	POST /api/chat/conversations/{id}/requests/{command}             send one reviewed request
//
// The client never names the intent, the schema or the installation: the
// server builds the proposal from the command's own definition.
const chatRequestPrefix = "/api/chat/conversations/"

// ChatRequestPort is the request use case as the HTTP surface uses it.
type ChatRequestPort interface {
	Availability(context.Context, *trust.Principal, string) ([]ChatIntentRequestAvailability, error)
	Preview(context.Context, *trust.Principal, string, string, map[string]string) (ChatIntentRequestPreview, error)
	Submit(context.Context, *trust.Principal, string, string, map[string]string, string) (ChatIntentRequestReceipt, error)
}

type chatRequestBody struct {
	Values     map[string]string `json:"values"`
	Submission string            `json:"submission,omitempty"`
}

type chatRequestReply struct {
	Commands []ChatIntentRequestAvailability `json:"commands,omitempty"`
	Preview  *ChatIntentRequestPreview       `json:"preview,omitempty"`
	Receipt  *ChatIntentRequestReceipt       `json:"receipt,omitempty"`
}

type chatRequestError struct {
	Code   string            `json:"code"`
	Reason string            `json:"reason,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

// chatRequestRoute parses a request path: the conversation, the command (empty
// for the list route) and whether it is the preview route.
func chatRequestRoute(path string) (conversation, command string, preview, ok bool) {
	rest, found := strings.CutPrefix(path, chatRequestPrefix)
	if !found {
		return "", "", false, false
	}
	parts := strings.Split(rest, "/")
	switch {
	case len(parts) == 2 && parts[1] == "requests" && parts[0] != "":
		return parts[0], "", false, true
	case len(parts) == 3 && parts[1] == "requests" && parts[0] != "" && parts[2] != "":
		return parts[0], parts[2], false, true
	case len(parts) == 4 && parts[1] == "requests" && parts[3] == "preview" && parts[0] != "" && parts[2] != "":
		return parts[0], parts[2], true, true
	}
	return "", "", false, false
}

func chatRequestHTTPError(w http.ResponseWriter, err error) {
	status, body := http.StatusServiceUnavailable, chatRequestError{Code: "unavailable"}
	var invalid *ChatIntentRequestInvalidError
	var missing *ChatIntentRequestNotAvailableError
	switch {
	case errors.As(err, &invalid):
		status, body = http.StatusBadRequest, chatRequestError{Code: "invalid_request", Fields: invalid.Fields}
	case errors.As(err, &missing):
		status, body = http.StatusConflict, chatRequestError{Code: "not_available", Reason: missing.Reason}
	case errors.Is(err, ErrChatIntentRequestInvalid):
		status, body = http.StatusBadRequest, chatRequestError{Code: "invalid_argument"}
	case errors.Is(err, ErrChatIntentRequestNotFound):
		status, body = http.StatusNotFound, chatRequestError{Code: "not_found"}
	case errors.Is(err, ErrChatIntentRequestDenied):
		status, body = http.StatusForbidden, chatRequestError{Code: "permission_denied"}
	default:
		// The body never carries the cause; a developer cell can opt in to
		// seeing it in the server log, as the other chat surfaces do.
		if err != nil && os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_request_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ChatRequestHandler answers the request routes for an admitted person.
type ChatRequestHandler struct{ Requests ChatRequestPort }

func (h ChatRequestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	conversation, command, preview, ok := chatRequestRoute(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	want := http.MethodPost
	if command == "" {
		want = http.MethodGet
	}
	if r.Method != want {
		w.Header().Set("Allow", want)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(chatRequestError{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(chatRequestError{Code: "unauthenticated"})
		return
	}
	// A request is asked for by a person who read it. An agent proposes through
	// its own governed path, not this route.
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatRequestHTTPError(w, ErrChatIntentRequestDenied)
		return
	}
	if h.Requests == nil {
		chatRequestHTTPError(w, ErrChatIntentRequestUnavailable)
		return
	}
	if command == "" {
		commands, err := h.Requests.Availability(r.Context(), verified, conversation)
		if err != nil {
			chatRequestHTTPError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(chatRequestReply{Commands: commands})
		return
	}
	var body chatRequestBody
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.Values == nil {
		chatRequestHTTPError(w, ErrChatIntentRequestInvalid)
		return
	}
	if preview {
		shown, err := h.Requests.Preview(r.Context(), verified, conversation, command, body.Values)
		if err != nil {
			chatRequestHTTPError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(chatRequestReply{Preview: &shown})
		return
	}
	receipt, err := h.Requests.Submit(r.Context(), verified, conversation, command, body.Values, body.Submission)
	if err != nil {
		chatRequestHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(chatRequestReply{Receipt: &receipt})
}

// OverlayChatRequests mounts the request routes with the admission the
// neighbouring Chat surfaces use. Paths that are not request routes pass through.
func OverlayChatRequests(next http.Handler, requests ChatRequestPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatRequestHandler{Requests: requests}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, _, ok := chatRequestRoute(r.URL.Path); !ok {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatRequestError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
