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

// ChatMessageWorkPath is the prefix of the message-to-work routes:
//
//	POST /api/chat/messages/{id}/convert         make a task from a message
//	GET  /api/chat/messages/linked-work          what a conversation's messages became
//	GET  /api/chat/messages/convert-options      the projects a task can be made in
//
// They sit under /api/chat/, the prefix the page's content security policy lets
// the Chat client call.
const ChatMessageWorkPath = "/api/chat/messages/"

// ChatMessageWorkPort is the conversion as the HTTP surface uses it.
type ChatMessageWorkPort interface {
	Projects(context.Context, *trust.Principal) ([]ChatWorkProject, error)
	Linked(context.Context, *trust.Principal, string) ([]ChatWorkLink, error)
	Convert(context.Context, *trust.Principal, ChatWorkConvertRequest) (ChatWorkLink, error)
}

// ChatMessageWorkCommand is the body of a conversion.
type ChatMessageWorkCommand struct {
	ConversationID string `json:"conversation_id"`
	Target         string `json:"target"`
	ProjectID      string `json:"project_id"`
	Title          string `json:"title"`
	OwnerID        string `json:"owner_id,omitempty"`
	DueDate        string `json:"due_date,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type chatMessageWorkReply struct {
	Projects []ChatWorkProject `json:"projects,omitempty"`
	Links    []ChatWorkLink    `json:"links,omitempty"`
	Link     *ChatWorkLink     `json:"link,omitempty"`
}

type chatMessageWorkError struct {
	Code string `json:"code"`
}

func chatMessageWorkHTTPError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, ErrChatWorkInvalid):
		status, code = http.StatusBadRequest, "invalid_argument"
	case errors.Is(err, ErrChatWorkNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, ErrChatWorkCaseRestricted):
		status, code = http.StatusForbidden, "case_restricted"
	case errors.Is(err, ErrChatWorkDenied):
		status, code = http.StatusForbidden, "permission_denied"
	default:
		// The body never carries the cause; a developer cell can opt in to
		// seeing it in the server log, as the other chat surfaces do.
		if err != nil && os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_message_work_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chatMessageWorkError{Code: code})
}

// ChatMessageWorkHandler answers the message-to-work routes for an admitted person.
type ChatMessageWorkHandler struct{ Work ChatMessageWorkPort }

func (h ChatMessageWorkHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	rest := strings.TrimPrefix(r.URL.Path, ChatMessageWorkPath)
	messageID, convert := strings.CutSuffix(rest, "/convert")
	switch {
	case convert && messageID != "" && !strings.Contains(messageID, "/"):
	case rest == "linked-work" || rest == "convert-options":
	default:
		http.NotFound(w, r)
		return
	}
	wantMethod := http.MethodGet
	if convert {
		wantMethod = http.MethodPost
	}
	if r.Method != wantMethod {
		w.Header().Set("Allow", wantMethod)
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
	// Work is made by a person who has read the message. An agent that should
	// file work gets its own governed path, not this one.
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatMessageWorkHTTPError(w, ErrChatWorkDenied)
		return
	}
	if h.Work == nil {
		chatMessageWorkHTTPError(w, ErrChatWorkUnavailable)
		return
	}
	var reply chatMessageWorkReply
	var err error
	switch {
	case convert:
		var command ChatMessageWorkCommand
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			chatMessageWorkHTTPError(w, ErrChatWorkInvalid)
			return
		}
		var link ChatWorkLink
		link, err = h.Work.Convert(r.Context(), verified, ChatWorkConvertRequest{
			ConversationID: command.ConversationID, MessageID: messageID, Target: command.Target, ProjectID: command.ProjectID,
			Title: command.Title, OwnerID: command.OwnerID, DueDate: command.DueDate, IdempotencyKey: command.IdempotencyKey,
		})
		reply.Link = &link
	case rest == "linked-work":
		reply.Links, err = h.Work.Linked(r.Context(), verified, r.URL.Query().Get("conversation_id"))
		if reply.Links == nil {
			reply.Links = []ChatWorkLink{}
		}
	default:
		reply.Projects, err = h.Work.Projects(r.Context(), verified)
		if reply.Projects == nil {
			reply.Projects = []ChatWorkProject{}
		}
	}
	if err != nil {
		chatMessageWorkHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(reply)
}

// OverlayChatMessageWork mounts the message-to-work routes with the admission
// the neighbouring Chat surfaces use.
func OverlayChatMessageWork(next http.Handler, work ChatMessageWorkPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatMessageWorkHandler{Work: work}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatMessageWorkPath) {
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
