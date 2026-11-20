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
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatDecisionsPath serves recorded decisions (issue 3792): /decide records one
// and the Decisions list of a conversation reads them. It sits under /api/chat/,
// the prefix the page's content security policy lets the Chat client call.
const ChatDecisionsPath = "/api/chat/decisions/v1"

// ChatDecisionsPort is the decision service as the HTTP surface uses it.
type ChatDecisionsPort interface {
	Record(context.Context, chat.DecisionRecordRequest) (chat.Decision, error)
	List(ctx context.Context, p chat.Principal, tenant, conversation string) (chat.DecisionList, error)
}

// ChatDecisionCommand is the body of every request; which fields matter depends
// on the action in the path.
type ChatDecisionCommand struct {
	HostTenantID   string `json:"host_tenant_id,omitempty"`
	ConversationID string `json:"conversation_id"`
	Text           string `json:"text,omitempty"`
	Rationale      string `json:"rationale,omitempty"`
	SourcePostID   string `json:"source_post_id,omitempty"`
	SupersedesID   string `json:"supersedes_id,omitempty"`
	ParentID       string `json:"parent_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Label          string `json:"label,omitempty"`
}

// ChatDecisionJSON is one decision as the page reads it.
type ChatDecisionJSON struct {
	ID                string    `json:"id"`
	ConversationID    string    `json:"conversation_id"`
	Text              string    `json:"text"`
	Rationale         string    `json:"rationale,omitempty"`
	OwnerHomeTenantID string    `json:"owner_home_tenant_id"`
	OwnerID           string    `json:"owner_id"`
	DecidedAt         time.Time `json:"decided_at"`
	SourcePostID      string    `json:"source_post_id,omitempty"`
	SourceSequence    uint64    `json:"source_sequence,omitempty"`
	SupersedesID      string    `json:"supersedes_id,omitempty"`
	SupersededByID    string    `json:"superseded_by_id,omitempty"`
	Informal          bool      `json:"informal"`
	CanReplace        bool      `json:"can_replace"`
}

// ChatDecisionsReply answers a record with the decision and a list with the
// decisions the reader may see.
type ChatDecisionsReply struct {
	Decision  *ChatDecisionJSON  `json:"decision,omitempty"`
	Decisions []ChatDecisionJSON `json:"decisions"`
	CanRecord bool               `json:"can_record"`
}

type chatDecisionError struct {
	Code string `json:"code"`
}

func chatDecisionJSON(d chat.Decision, canReplace bool) ChatDecisionJSON {
	return ChatDecisionJSON{ID: d.ID, ConversationID: d.ConversationID, Text: d.Text, Rationale: d.Rationale, OwnerHomeTenantID: d.OwnerHomeTenantID, OwnerID: d.OwnerID,
		DecidedAt: d.DecidedAt.UTC(), SourcePostID: d.SourcePostID, SourceSequence: d.SourceSequence, SupersedesID: d.SupersedesID, SupersededByID: d.SupersededByID, Informal: true, CanReplace: canReplace}
}

func chatDecisionHTTPError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, chat.ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, chat.ErrPermissionDenied):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chat.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, chat.ErrInvalidArgument):
		status, code = http.StatusBadRequest, "invalid_argument"
	case errors.Is(err, chat.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	default:
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_decision_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chatDecisionError{Code: code})
}

// ChatDecisionsHandler answers the two decision actions for an admitted person.
type ChatDecisionsHandler struct{ Decisions ChatDecisionsPort }

func (h ChatDecisionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	action := strings.TrimPrefix(r.URL.Path, ChatDecisionsPath+"/")
	if action != "record" && action != "list" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(chatDecisionError{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		chatDecisionHTTPError(w, chat.ErrUnauthenticated)
		return
	}
	// A decision is recorded by a person; an agent never speaks for the room.
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatDecisionHTTPError(w, chat.ErrPermissionDenied)
		return
	}
	if h.Decisions == nil {
		chatDecisionHTTPError(w, chat.ErrUnavailable)
		return
	}
	var command ChatDecisionCommand
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
		chatDecisionHTTPError(w, chat.ErrInvalidArgument)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	host := command.HostTenantID
	if host == "" {
		host = p.TenantID
	}
	reply := ChatDecisionsReply{Decisions: []ChatDecisionJSON{}}
	switch action {
	case "record":
		recorded, err := h.Decisions.Record(r.Context(), chat.DecisionRecordRequest{Principal: p, TenantID: host, ConversationID: command.ConversationID,
			Text: command.Text, Rationale: command.Rationale, SourcePostID: command.SourcePostID, SupersedesID: command.SupersedesID,
			ParentID: command.ParentID, IdempotencyKey: command.IdempotencyKey, Label: command.Label})
		if err != nil {
			chatDecisionHTTPError(w, err)
			return
		}
		item := chatDecisionJSON(recorded, false)
		reply.Decision = &item
	case "list":
		list, err := h.Decisions.List(r.Context(), p, host, command.ConversationID)
		if err != nil {
			chatDecisionHTTPError(w, err)
			return
		}
		for _, view := range list.Decisions {
			reply.Decisions = append(reply.Decisions, chatDecisionJSON(view.Decision, view.CanReplace))
		}
		reply.CanRecord = list.CanRecord
	}
	_ = json.NewEncoder(w).Encode(reply)
}

// OverlayChatDecisions mounts the decision actions with the admission the
// neighbouring Chat surfaces use. A nil service answers "unavailable".
func OverlayChatDecisions(next http.Handler, decisions ChatDecisionsPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatDecisionsHandler{Decisions: decisions}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatDecisionsPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatDecisionError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// chatDecisionsRoutedStore takes the conversation's write lease before a
// decision is stored, as every other writer that reaches the store directly
// does (the routed message post it sends takes its own).
type chatDecisionsRoutedStore struct {
	chat.DecisionStore
	Directory chatrouting.Directory
	Cache     *chatrouting.RouteCache
	Now       func() time.Time
}

func (s chatDecisionsRoutedStore) InsertDecision(ctx context.Context, d chat.Decision) (chat.Decision, error) {
	lease, err := s.Cache.Resolve(ctx, s.Directory, d.ConversationID, d.TenantID)
	if err != nil {
		if errors.Is(err, chatrouting.ErrNotFound) {
			return chat.Decision{}, chat.ErrNotFound
		}
		return chat.Decision{}, chat.ErrUnavailable
	}
	if err = s.Cache.CheckWrite(ctx, s.Directory, lease, d.TenantID, s.Now()); err != nil {
		s.Cache.Invalidate(d.ConversationID, lease.Route.Epoch)
		return chat.Decision{}, chat.ErrUnavailable
	}
	return s.DecisionStore.InsertDecision(chatrouting.WithWriteLease(ctx, lease), d)
}

// chatDecisionsPort composes the decision service over the served Chat
// runtime, or nil when Chat is not composed.
func chatDecisionsPort(runtime composedChat) ChatDecisionsPort {
	if runtime.core == nil || runtime.store == nil {
		return nil
	}
	var store chat.DecisionStore = runtime.store
	if runtime.routes != nil {
		store = chatDecisionsRoutedStore{DecisionStore: runtime.store, Directory: runtime.routes, Cache: chatrouting.NewRouteCache(5 * time.Second), Now: time.Now}
	}
	service := &chat.DecisionService{Chat: runtime.core, Store: store}
	if runtime.service != nil {
		service.Sender = runtime.service
	}
	return service
}
