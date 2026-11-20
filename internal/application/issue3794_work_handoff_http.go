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

// ChatWorkHandoffPath is the prefix of the work hand-off routes:
//
//	GET  /api/chat/work-handoffs?conversation_id=   the viewer's offers in a conversation
//	POST /api/chat/work-handoffs                    offer one linked work item
//	POST /api/chat/work-handoffs/{id}/accept        the recipient accepts
//	POST /api/chat/work-handoffs/{id}/decline       the recipient declines
//	POST /api/chat/work-handoffs/{id}/withdraw      the offerer takes it back
//
// They sit under /api/chat/, the prefix the page's content security policy lets
// the Chat client call.
const ChatWorkHandoffPath = "/api/chat/work-handoffs"

// ChatWorkHandoffPort is hand-off as the HTTP surface uses it.
type ChatWorkHandoffPort interface {
	List(context.Context, *trust.Principal, string) ([]ChatWorkHandoff, error)
	Offer(context.Context, *trust.Principal, ChatWorkHandoffOfferRequest) (ChatWorkHandoff, error)
	Accept(context.Context, *trust.Principal, string) (ChatWorkHandoff, error)
	Decline(context.Context, *trust.Principal, string) (ChatWorkHandoff, error)
	Withdraw(context.Context, *trust.Principal, string) (ChatWorkHandoff, error)
}

// ChatWorkHandoffCommand is the body of an offer.
type ChatWorkHandoffCommand struct {
	ConversationID string `json:"conversation_id"`
	ProjectID      string `json:"project_id"`
	TaskID         string `json:"task_id"`
	Mode           string `json:"mode"`
	RecipientID    string `json:"recipient_id"`
	NextAction     string `json:"next_action"`
	Note           string `json:"note,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type chatWorkHandoffReply struct {
	Handoffs []ChatWorkHandoff `json:"handoffs,omitempty"`
	Handoff  *ChatWorkHandoff  `json:"handoff,omitempty"`
}

type chatWorkHandoffError struct {
	Code string `json:"code"`
}

func chatWorkHandoffHTTPError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, ErrChatWorkInvalid):
		status, code = http.StatusBadRequest, "invalid_argument"
	case errors.Is(err, ErrChatWorkNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, ErrChatWorkDenied):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, ErrChatWorkConflict):
		status, code = http.StatusConflict, "conflict"
	default:
		if err != nil && os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_work_handoff_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chatWorkHandoffError{Code: code})
}

// ChatWorkHandoffHandler answers the hand-off routes for an admitted person.
type ChatWorkHandoffHandler struct{ Handoffs ChatWorkHandoffPort }

func (h ChatWorkHandoffHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, ChatWorkHandoffPath), "/")
	id, verb := "", ""
	switch {
	case rest == "":
	default:
		parts := strings.Split(rest, "/")
		if len(parts) != 2 || parts[0] == "" || (parts[1] != "accept" && parts[1] != "decline" && parts[1] != "withdraw") {
			http.NotFound(w, r)
			return
		}
		id, verb = parts[0], parts[1]
	}
	wantMethod := http.MethodPost
	if rest == "" && r.Method == http.MethodGet {
		wantMethod = http.MethodGet
	}
	if r.Method != wantMethod {
		w.Header().Set("Allow", "GET, POST")
		if verb != "" {
			w.Header().Set("Allow", http.MethodPost)
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(chatWorkHandoffError{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(chatWorkHandoffError{Code: "unauthenticated"})
		return
	}
	// A person hands work to a person. An agent that should file or move work
	// gets its own governed path, not this one.
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatWorkHandoffHTTPError(w, ErrChatWorkDenied)
		return
	}
	if h.Handoffs == nil {
		chatWorkHandoffHTTPError(w, ErrChatWorkUnavailable)
		return
	}
	var reply chatWorkHandoffReply
	var err error
	switch {
	case verb == "accept":
		var one ChatWorkHandoff
		one, err = h.Handoffs.Accept(r.Context(), verified, id)
		reply.Handoff = &one
	case verb == "decline":
		var one ChatWorkHandoff
		one, err = h.Handoffs.Decline(r.Context(), verified, id)
		reply.Handoff = &one
	case verb == "withdraw":
		var one ChatWorkHandoff
		one, err = h.Handoffs.Withdraw(r.Context(), verified, id)
		reply.Handoff = &one
	case r.Method == http.MethodGet:
		reply.Handoffs, err = h.Handoffs.List(r.Context(), verified, r.URL.Query().Get("conversation_id"))
		if reply.Handoffs == nil {
			reply.Handoffs = []ChatWorkHandoff{}
		}
	default:
		var command ChatWorkHandoffCommand
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			chatWorkHandoffHTTPError(w, ErrChatWorkInvalid)
			return
		}
		var one ChatWorkHandoff
		one, err = h.Handoffs.Offer(r.Context(), verified, ChatWorkHandoffOfferRequest{
			ConversationID: command.ConversationID, ProjectID: command.ProjectID, TaskID: command.TaskID, Mode: command.Mode,
			RecipientID: command.RecipientID, NextAction: command.NextAction, Note: command.Note, IdempotencyKey: command.IdempotencyKey,
		})
		reply.Handoff = &one
	}
	if err != nil {
		chatWorkHandoffHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(reply)
}

// OverlayChatWorkHandoffs mounts the hand-off routes with the admission the
// neighbouring Chat surfaces use.
func OverlayChatWorkHandoffs(next http.Handler, handoffs ChatWorkHandoffPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatWorkHandoffHandler{Handoffs: handoffs}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatWorkHandoffPath && !strings.HasPrefix(r.URL.Path, ChatWorkHandoffPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatWorkHandoffError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
