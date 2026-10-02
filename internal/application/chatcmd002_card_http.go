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

// Chatcmd002CardPath serves polls and to-do lists posted as messages: posting
// one from its preview, reading what a reader may see of it, and voting,
// ticking, closing and reopening. The tidy step of the preview keeps its own
// route (Chatcmd003TidyPath) because it is the only one that calls a model.
// Both sit under /api/chat/, the prefix the page's content security policy
// lets the Chat client call.
const Chatcmd002CardPath = "/api/chat/message-card/v1"

// chatcmd002ReadLimit is how many cards one read may ask for: a page of
// messages holds fewer.
const chatcmd002ReadLimit = 100

// Chatcmd002CardPort is the card service as the HTTP surface uses it.
type Chatcmd002CardPort interface {
	Post(context.Context, chat.Chatcmd002PostRequest) (chat.Post, error)
	Read(context.Context, chat.Chatcmd002Request) (chat.Chatcmd002View, error)
	Mutate(context.Context, chat.Chatcmd002Request) (chat.Post, error)
}

// Chatcmd002CardCommand is the body of every request. Which fields matter
// depends on the action in the path.
type Chatcmd002CardCommand struct {
	HostTenantID     string                   `json:"host_tenant_id,omitempty"`
	ConversationID   string                   `json:"conversation_id"`
	ParentID         string                   `json:"parent_id,omitempty"`
	IdempotencyKey   string                   `json:"idempotency_key,omitempty"`
	Card             *chat.Chatcmd002Card     `json:"card,omitempty"`
	PostID           string                   `json:"post_id,omitempty"`
	PostIDs          []string                 `json:"post_ids,omitempty"`
	ExpectedRevision uint64                   `json:"expected_revision,omitempty"`
	Mutation         *chat.Chatcmd002Mutation `json:"mutation,omitempty"`
}

// Chatcmd002CardReply answers a post or a mutation with the message it wrote
// and what the caller may now see of the card, and a read with one view per
// card the caller may read.
type Chatcmd002CardReply struct {
	PostID   string                         `json:"post_id,omitempty"`
	Revision uint64                         `json:"revision,omitempty"`
	View     *chat.Chatcmd002View           `json:"view,omitempty"`
	Views    map[string]chat.Chatcmd002View `json:"views,omitempty"`
}

type chatcmd002CardError struct {
	Code string `json:"code"`
}

func chatcmd002HTTPError(w http.ResponseWriter, err error) {
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
		// The body never carries the cause. A developer cell can opt in to
		// seeing it in the server log, as the other chat surfaces do.
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_card_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chatcmd002CardError{Code: code})
}

// Chatcmd002CardHandler answers the three card actions for an admitted person.
type Chatcmd002CardHandler struct{ Cards Chatcmd002CardPort }

func (h Chatcmd002CardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	action := strings.TrimPrefix(r.URL.Path, Chatcmd002CardPath+"/")
	if action != "post" && action != "read" && action != "mutate" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(chatcmd002CardError{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		chatcmd002HTTPError(w, chat.ErrUnauthenticated)
		return
	}
	// A card is posted, voted on and ticked by a person. An agent that should
	// post one gets its own governed path, not this one.
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatcmd002HTTPError(w, chat.ErrPermissionDenied)
		return
	}
	if h.Cards == nil {
		chatcmd002HTTPError(w, chat.ErrUnavailable)
		return
	}
	var command Chatcmd002CardCommand
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
		chatcmd002HTTPError(w, chat.ErrInvalidArgument)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	host := command.HostTenantID
	if host == "" {
		host = p.TenantID
	}
	request := chat.Chatcmd002Request{Principal: p, TenantID: host, ConversationID: command.ConversationID, PostID: command.PostID, ExpectedRevision: command.ExpectedRevision}
	ctx := r.Context()
	var reply Chatcmd002CardReply
	switch action {
	case "post":
		if command.Card == nil {
			chatcmd002HTTPError(w, chat.ErrInvalidArgument)
			return
		}
		// Reaching this route is the person pressing Post on the preview.
		posted, err := h.Cards.Post(ctx, chat.Chatcmd002PostRequest{Accepted: true, Card: *command.Card,
			SendPostRequest: chat.SendPostRequest{Principal: p, TenantID: host, ConversationID: command.ConversationID, ParentID: command.ParentID, IdempotencyKey: command.IdempotencyKey}})
		if err != nil {
			chatcmd002HTTPError(w, err)
			return
		}
		reply.PostID, reply.Revision = posted.ID, posted.Revision
	case "read":
		ids := command.PostIDs
		if command.PostID != "" {
			ids = append([]string{command.PostID}, ids...)
		}
		if len(ids) == 0 || len(ids) > chatcmd002ReadLimit {
			chatcmd002HTTPError(w, chat.ErrInvalidArgument)
			return
		}
		reply.Views = map[string]chat.Chatcmd002View{}
		for _, id := range ids {
			if _, seen := reply.Views[id]; seen || id == "" {
				continue
			}
			request.PostID = id
			view, err := h.Cards.Read(ctx, request)
			switch {
			case err == nil:
				reply.Views[id] = view
			case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrInvalidArgument):
				// A message that is gone, or is not a card, has no view; the
				// others are still answered.
			default:
				chatcmd002HTTPError(w, err)
				return
			}
		}
	case "mutate":
		if command.Mutation == nil || command.PostID == "" {
			chatcmd002HTTPError(w, chat.ErrInvalidArgument)
			return
		}
		request.Mutation = *command.Mutation
		changed, err := h.Cards.Mutate(ctx, request)
		if err != nil {
			chatcmd002HTTPError(w, err)
			return
		}
		reply.PostID, reply.Revision = changed.ID, changed.Revision
		// The answer carries what this person may now see, so their card is
		// right before the conversation's own event arrives.
		if view, err := h.Cards.Read(ctx, request); err == nil {
			reply.View = &view
		}
	}
	_ = json.NewEncoder(w).Encode(reply)
}

// OverlayChatcmd002Cards mounts the card actions with the admission the
// neighbouring Chat surfaces use.
func OverlayChatcmd002Cards(next http.Handler, cards Chatcmd002CardPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := Chatcmd002CardHandler{Cards: cards}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, Chatcmd002CardPath+"/") || r.URL.Path == Chatcmd003TidyPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatcmd002CardError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// chatcmd002RoutedRepository takes the conversation's write lease before a card
// is changed, as every other writer that reaches the store directly does. A
// read needs none.
type chatcmd002RoutedRepository struct {
	chat.Chatcmd002Repository
	Directory chatrouting.Directory
	Cache     *chatrouting.RouteCache
	Now       func() time.Time
}

func (s chatcmd002RoutedRepository) Chatcmd002Mutate(ctx context.Context, r chat.Chatcmd002Request, authorize func(context.Context) error) (chat.Post, error) {
	lease, err := s.Cache.Resolve(ctx, s.Directory, r.ConversationID, r.TenantID)
	if err != nil {
		if errors.Is(err, chatrouting.ErrNotFound) {
			return chat.Post{}, chat.ErrNotFound
		}
		return chat.Post{}, chat.ErrUnavailable
	}
	if err = s.Cache.CheckWrite(ctx, s.Directory, lease, r.TenantID, s.Now()); err != nil {
		s.Cache.Invalidate(r.ConversationID, lease.Route.Epoch)
		return chat.Post{}, chat.ErrUnavailable
	}
	return s.Chatcmd002Repository.Chatcmd002Mutate(chatrouting.WithWriteLease(ctx, lease), r, authorize)
}

// chatcmd002CardPort composes the card service over the served Chat runtime,
// or nil when Chat is not composed.
func chatcmd002CardPort(runtime composedChat) Chatcmd002CardPort {
	if runtime.core == nil || runtime.store == nil {
		return nil
	}
	var repository chat.Chatcmd002Repository = runtime.store
	if runtime.routes != nil {
		repository = chatcmd002RoutedRepository{Chatcmd002Repository: runtime.store, Directory: runtime.routes, Cache: chatrouting.NewRouteCache(5 * time.Second), Now: time.Now}
	}
	cards := &chat.Chatcmd002Service{Chat: runtime.core, Repository: repository}
	if runtime.service != nil {
		cards.Sender = runtime.service
	}
	return cards
}
