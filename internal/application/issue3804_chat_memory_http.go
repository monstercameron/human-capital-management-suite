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

// ChatMemoryPath serves what the agents of a channel remember (issue 3804). It
// sits under /api/chat/, the prefix the page's content security policy lets
// the Chat client call.
const ChatMemoryPath = "/api/chat/memory/v1"

// ChatMemoryPort is the memory service as the HTTP surface uses it.
type ChatMemoryPort interface {
	Propose(context.Context, chat.MemoryProposeRequest) (chat.ChannelMemory, error)
	List(ctx context.Context, p chat.Principal, tenant, conversation string) (chat.MemoryList, error)
	Approve(context.Context, chat.MemoryActRequest) (chat.ChannelMemory, error)
	Forget(context.Context, chat.MemoryActRequest) error
}

// ChatMemoryCommand is the body of every request; which fields matter depends
// on the action in the path. The person is the session's, never a field.
type ChatMemoryCommand struct {
	HostTenantID   string `json:"host_tenant_id,omitempty"`
	ConversationID string `json:"conversation_id"`
	SourcePostID   string `json:"source_post_id,omitempty"`
	ID             string `json:"id,omitempty"`
	TTLDays        int    `json:"ttl_days,omitempty"`
}

// ChatMemoryJSON is one remembered item as the page reads it. The words of an
// item whose source message is gone are empty.
type ChatMemoryJSON struct {
	ID                   string     `json:"id"`
	ConversationID       string     `json:"conversation_id"`
	Text                 string     `json:"text"`
	SourcePostID         string     `json:"source_post_id"`
	SourceSequence       uint64     `json:"source_sequence,omitempty"`
	Source               string     `json:"source"`
	ProposerHomeTenantID string     `json:"proposer_home_tenant_id"`
	ProposerID           string     `json:"proposer_id"`
	Status               string     `json:"status"`
	TTLDays              int        `json:"ttl_days"`
	CreatedAt            time.Time  `json:"created_at"`
	ApprovedAt           *time.Time `json:"approved_at,omitempty"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	CanApprove           bool       `json:"can_approve"`
	CanForget            bool       `json:"can_forget"`
}

// ChatMemoryReply answers a propose or approve with the item, a list with the
// items the reader may see, and a forget with nothing.
type ChatMemoryReply struct {
	Memory      *ChatMemoryJSON  `json:"memory,omitempty"`
	Items       []ChatMemoryJSON `json:"items"`
	CanRemember bool             `json:"can_remember"`
	IsManager   bool             `json:"is_manager"`
}

type chatMemoryError struct {
	Code string `json:"code"`
}

func chatMemoryJSON(m chat.ChannelMemory, canApprove, canForget bool) ChatMemoryJSON {
	out := ChatMemoryJSON{ID: m.ID, ConversationID: m.ConversationID, Text: m.Text, SourcePostID: m.SourcePostID, SourceSequence: m.SourceSequence,
		Source: strings.ToLower(string(m.Source)), ProposerHomeTenantID: m.ProposerHomeTenantID, ProposerID: m.ProposerID, Status: strings.ToLower(string(m.Status)),
		TTLDays: m.TTLDays, CreatedAt: m.CreatedAt.UTC(), CanApprove: canApprove, CanForget: canForget}
	if out.Source == "" {
		out.Source = "live"
	}
	if !m.ApprovedAt.IsZero() {
		at := m.ApprovedAt.UTC()
		out.ApprovedAt = &at
	}
	if !m.ExpiresAt.IsZero() {
		at := m.ExpiresAt.UTC()
		out.ExpiresAt = &at
	}
	return out
}

func chatMemoryHTTPError(w http.ResponseWriter, err error) {
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
			slog.Warn("hcmnext.chat_memory_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chatMemoryError{Code: code})
}

// ChatMemoryHandler answers the four memory actions for an admitted person.
type ChatMemoryHandler struct{ Memory ChatMemoryPort }

func (h ChatMemoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	action := strings.TrimPrefix(r.URL.Path, ChatMemoryPath+"/")
	if action != "propose" && action != "list" && action != "approve" && action != "forget" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(chatMemoryError{Code: "method_not_allowed"})
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok || verified == nil {
		chatMemoryHTTPError(w, chat.ErrUnauthenticated)
		return
	}
	// A memory is asked for by a person; an agent never decides what the room keeps.
	if verified.SubjectKind() != trust.SubjectKindHuman {
		chatMemoryHTTPError(w, chat.ErrPermissionDenied)
		return
	}
	if h.Memory == nil {
		chatMemoryHTTPError(w, chat.ErrUnavailable)
		return
	}
	var command ChatMemoryCommand
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
		chatMemoryHTTPError(w, chat.ErrInvalidArgument)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	host := command.HostTenantID
	if host == "" {
		host = p.TenantID
	}
	reply := ChatMemoryReply{Items: []ChatMemoryJSON{}}
	switch action {
	case "propose":
		item, err := h.Memory.Propose(r.Context(), chat.MemoryProposeRequest{Principal: p, TenantID: host, ConversationID: command.ConversationID, SourcePostID: command.SourcePostID, TTLDays: command.TTLDays})
		if err != nil {
			chatMemoryHTTPError(w, err)
			return
		}
		view := chatMemoryJSON(item, false, true)
		reply.Memory = &view
	case "approve":
		item, err := h.Memory.Approve(r.Context(), chat.MemoryActRequest{Principal: p, TenantID: host, ConversationID: command.ConversationID, ID: command.ID})
		if err != nil {
			chatMemoryHTTPError(w, err)
			return
		}
		view := chatMemoryJSON(item, false, true)
		reply.Memory = &view
	case "forget":
		if err := h.Memory.Forget(r.Context(), chat.MemoryActRequest{Principal: p, TenantID: host, ConversationID: command.ConversationID, ID: command.ID}); err != nil {
			chatMemoryHTTPError(w, err)
			return
		}
	case "list":
		list, err := h.Memory.List(r.Context(), p, host, command.ConversationID)
		if err != nil {
			chatMemoryHTTPError(w, err)
			return
		}
		for _, view := range list.Items {
			reply.Items = append(reply.Items, chatMemoryJSON(view.ChannelMemory, view.CanApprove, view.CanForget))
		}
		reply.CanRemember, reply.IsManager = list.CanRemember, list.IsManager
	}
	_ = json.NewEncoder(w).Encode(reply)
}

// OverlayChatMemory mounts the memory actions with the admission the
// neighbouring Chat surfaces use. A nil service answers "unavailable".
func OverlayChatMemory(next http.Handler, memory ChatMemoryPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatMemoryHandler{Memory: memory}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, ChatMemoryPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(chatMemoryError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// chatMemoryRoutedStore takes the conversation's write lease before an item
// is stored, approved or forgotten, as every other writer that reaches the
// store directly does.
type chatMemoryRoutedStore struct {
	chat.MemoryStore
	Directory chatrouting.Directory
	Cache     *chatrouting.RouteCache
	Now       func() time.Time
}

func (s chatMemoryRoutedStore) lease(ctx context.Context, tenant, conversation string) (context.Context, error) {
	lease, err := s.Cache.Resolve(ctx, s.Directory, conversation, tenant)
	if err != nil {
		if errors.Is(err, chatrouting.ErrNotFound) {
			return nil, chat.ErrNotFound
		}
		return nil, chat.ErrUnavailable
	}
	if err = s.Cache.CheckWrite(ctx, s.Directory, lease, tenant, s.Now()); err != nil {
		s.Cache.Invalidate(conversation, lease.Route.Epoch)
		return nil, chat.ErrUnavailable
	}
	return chatrouting.WithWriteLease(ctx, lease), nil
}

func (s chatMemoryRoutedStore) InsertMemory(ctx context.Context, m chat.ChannelMemory) (chat.ChannelMemory, error) {
	leased, err := s.lease(ctx, m.TenantID, m.ConversationID)
	if err != nil {
		return chat.ChannelMemory{}, err
	}
	return s.MemoryStore.InsertMemory(leased, m)
}

func (s chatMemoryRoutedStore) ApproveMemory(ctx context.Context, tenant, conversation, id, home, approver string, at time.Time) (chat.ChannelMemory, error) {
	leased, err := s.lease(ctx, tenant, conversation)
	if err != nil {
		return chat.ChannelMemory{}, err
	}
	return s.MemoryStore.ApproveMemory(leased, tenant, conversation, id, home, approver, at)
}

func (s chatMemoryRoutedStore) ForgetMemory(ctx context.Context, tenant, conversation, id, home, actor string, at time.Time) error {
	leased, err := s.lease(ctx, tenant, conversation)
	if err != nil {
		return err
	}
	return s.MemoryStore.ForgetMemory(leased, tenant, conversation, id, home, actor, at)
}

// chatMemoryService composes the memory service over the served Chat runtime,
// or nil when Chat is not composed.
func chatMemoryService(runtime composedChat) *chat.MemoryService {
	if runtime.core == nil || runtime.store == nil {
		return nil
	}
	var store chat.MemoryStore = runtime.store
	if runtime.routes != nil {
		store = chatMemoryRoutedStore{MemoryStore: runtime.store, Directory: runtime.routes, Cache: chatrouting.NewRouteCache(5 * time.Second), Now: time.Now}
	}
	return &chat.MemoryService{Chat: runtime.core, Store: store}
}

// chatMemoryPort is the service as the HTTP surface takes it; a nil service
// is a nil port, so the route answers "unavailable".
func chatMemoryPort(runtime composedChat) ChatMemoryPort {
	if service := chatMemoryService(runtime); service != nil {
		return service
	}
	return nil
}
