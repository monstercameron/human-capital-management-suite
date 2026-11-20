package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3733: GET /api/chat/notifications lists the signed-in person's chat
// notifications (mentions, replies in threads they follow, direct messages);
// POST marks some, or all, of them read. The list is computed from messages the
// person can read right now; the only thing stored is their own read state. The
// person always comes from the session, never from the request.
const ChatNotificationsPath = "/api/chat/notifications"

type ChatNotificationsPort interface {
	Notifications(ctx context.Context, p chat.Principal, host string, now time.Time) ([]chatrecipient.Notification, error)
	MarkNotificationsRead(ctx context.Context, p chat.Principal, host string, postIDs []string, all bool, now time.Time) (int64, error)
}

type ChatNotificationJSON struct {
	ID                 string    `json:"id"`
	Kind               string    `json:"kind"`
	ConversationID     string    `json:"conversation_id"`
	ConversationName   string    `json:"conversation_name,omitempty"`
	ConversationKind   string    `json:"conversation_kind"`
	PostID             string    `json:"post_id"`
	Sequence           uint64    `json:"sequence"`
	ThreadRootID       string    `json:"thread_root_id,omitempty"`
	ThreadRootSequence uint64    `json:"thread_root_sequence,omitempty"`
	AuthorID           string    `json:"author_id"`
	CoveringID         string    `json:"covering_id,omitempty"` // cover notices only: who is away (issue 3789)
	Body               string    `json:"body,omitempty"`
	At                 time.Time `json:"at"`
	Read               bool      `json:"read"`
}

type ChatNotificationsReply struct {
	HostTenantID string                 `json:"host_tenant_id"`
	Unread       int                    `json:"unread"`
	Items        []ChatNotificationJSON `json:"items"`
}

type ChatNotificationsCommand struct {
	Host string   `json:"host,omitempty"`
	IDs  []string `json:"ids,omitempty"`
	All  bool     `json:"all,omitempty"`
}

type ChatNotificationsError struct {
	Code string `json:"code"`
}

// chatNotificationsReaderPort applies the reader's own rendering selection to
// the message text, as the saved and follow-up lists do, so an item never shows
// text the conversation itself would hold back.
type chatNotificationsReaderPort struct {
	ChatNotificationsPort
	Rendering ChatRenderingPort
}

func (s chatNotificationsReaderPort) Notifications(ctx context.Context, p chat.Principal, host string, now time.Time) ([]chatrecipient.Notification, error) {
	rows, err := s.ChatNotificationsPort.Notifications(ctx, p, host, now)
	if err != nil {
		return nil, err
	}
	out := append([]chatrecipient.Notification(nil), rows...)
	for i := range out {
		// A cover notice says where and who, never what (issue 3789).
		if out[i].Kind == chatrecipient.NotificationCover {
			out[i].Body = ""
			continue
		}
		selected, mark, err := s.Rendering.ReadRenderingSelection(ctx, chatstore.RenderingScope{Principal: p, Tenant: host, Conversation: out[i].ConversationID}, out[i].PostID)
		if err != nil || mark.State == "pending" || mark.State == "unavailable" {
			out[i].Body = ""
			continue
		}
		out[i].Body = chatrecipient.FollowedPreview(selected.Text)
	}
	return out, nil
}

func chatNotificationsPort(runtime composedChat) ChatNotificationsPort {
	if runtime.extensions == nil || runtime.extensions.Recipients == nil {
		return nil
	}
	core := ChatNotificationsPort(runtime.extensions.Recipients)
	if runtime.renderings == nil {
		return core
	}
	return chatNotificationsReaderPort{ChatNotificationsPort: core, Rendering: runtime.renderings}
}

func chatNotificationsError(w http.ResponseWriter, err error) {
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
	default:
		// The body never carries the cause.
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_notifications_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ChatNotificationsError{Code: code})
}

type ChatNotificationsHandler struct {
	Service ChatNotificationsPort
	Now     func() time.Time
}

func (h ChatNotificationsHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func chatNotificationRows(rows []chatrecipient.Notification) ([]ChatNotificationJSON, int) {
	out := make([]ChatNotificationJSON, 0, len(rows))
	unread := 0
	for _, row := range rows {
		if !row.Read {
			unread++
		}
		out = append(out, ChatNotificationJSON{ID: row.ID, Kind: row.Kind, ConversationID: row.ConversationID, ConversationName: row.ConversationName,
			ConversationKind: row.ConversationKind, PostID: row.PostID, Sequence: row.Sequence, ThreadRootID: row.ThreadRootID,
			ThreadRootSequence: row.ThreadRootSequence, AuthorID: row.AuthorID, CoveringID: row.CoveringID, Body: row.Body, At: row.At.UTC(), Read: row.Read})
	}
	return out, unread
}

func (h ChatNotificationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != ChatNotificationsPath {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		chatNotificationsError(w, chat.ErrUnauthenticated)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	host := r.URL.Query().Get("host")
	var command ChatNotificationsCommand
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		// Decide who is asking before the body is read.
		if err := chat.ValidateSavedOwner(r.Context(), p, p.TenantID); err != nil {
			chatNotificationsError(w, err)
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			chatNotificationsError(w, chat.ErrInvalidArgument)
			return
		}
		host = command.Host
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(ChatNotificationsError{Code: "method_not_allowed"})
		return
	}
	if host == "" {
		host = p.TenantID
	}
	// Only the person's own, signed-in human identity may use this feed.
	if err := chat.ValidateSavedOwner(r.Context(), p, host); err != nil {
		chatNotificationsError(w, err)
		return
	}
	if h.Service == nil {
		chatNotificationsError(w, chat.ErrUnavailable)
		return
	}
	now := h.now()
	if r.Method == http.MethodPost {
		marked, err := h.Service.MarkNotificationsRead(r.Context(), p, host, command.IDs, command.All, now)
		if err != nil {
			chatNotificationsError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			Marked int64 `json:"marked"`
		}{marked})
		return
	}
	rows, err := h.Service.Notifications(r.Context(), p, host, now)
	if err != nil {
		chatNotificationsError(w, err)
		return
	}
	items, unread := chatNotificationRows(rows)
	_ = json.NewEncoder(w).Encode(ChatNotificationsReply{HostTenantID: host, Unread: unread, Items: items})
}

func OverlayChatNotifications(next http.Handler, service ChatNotificationsPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatNotificationsHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatNotificationsPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(ChatNotificationsError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
