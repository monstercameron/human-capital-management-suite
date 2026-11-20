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
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3788: Send later. GET /api/chat/scheduled-posts lists the signed-in
// person's own waiting and refused messages; POST schedules one; PUT changes
// its text or time; DELETE cancels it. The person is the session's, never a
// field of the request, and nothing here sends a message: the delivery job
// does, as the author, through the ordinary Send service.
const ScheduledPostsPath = "/api/chat/scheduled-posts"

// ScheduledPostsPort is the use case the route calls.
type ScheduledPostsPort interface {
	ScheduleSend(ctx context.Context, p chat.Principal, host, conversation, body string, sendAt time.Time, zone, orgScope string, now time.Time) (chatrecipient.ScheduledPost, error)
	ScheduledPosts(ctx context.Context, p chat.Principal, host string) ([]chatrecipient.ScheduledPost, error)
	EditScheduled(ctx context.Context, p chat.Principal, host, id, body string, sendAt time.Time, zone string, expected uint64, now time.Time) (chatrecipient.ScheduledPost, error)
	CancelScheduled(ctx context.Context, p chat.Principal, host, id string) error
}

type ScheduledPostJSON struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Body           string    `json:"body"`
	SendAt         time.Time `json:"send_at"`
	Zone           string    `json:"zone"`
	State          string    `json:"state"`
	Refusal        string    `json:"refusal,omitempty"`
	Revision       uint64    `json:"revision"`
	QuietPeople    int       `json:"quiet_people,omitempty"`
}

type ScheduledPostsReply struct {
	HostTenantID string              `json:"host_tenant_id"`
	Posts        []ScheduledPostJSON `json:"posts"`
}

type ScheduledPostReply struct {
	Post ScheduledPostJSON `json:"post"`
}

type ScheduledPostCommand struct {
	Host           string    `json:"host,omitempty"`
	ID             string    `json:"id,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Body           string    `json:"body,omitempty"`
	SendAt         time.Time `json:"send_at,omitempty"`
	Zone           string    `json:"zone,omitempty"`
	Revision       uint64    `json:"revision,omitempty"`
}

type ScheduledPostsError struct {
	Code string `json:"code"`
}

func scheduledPostsPort(runtime composedChat) ScheduledPostsPort {
	if runtime.extensions == nil || runtime.extensions.Recipients == nil {
		return nil
	}
	return runtime.extensions.Recipients
}

func scheduledPostsError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, chat.ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, chat.ErrPermissionDenied):
		status, code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chat.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, chat.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, chatrecipient.ErrScheduledLimit):
		status, code = http.StatusUnprocessableEntity, "limit_reached"
	case errors.Is(err, chatrecipient.ErrScheduledTime):
		status, code = http.StatusBadRequest, "invalid_time"
	case errors.Is(err, chat.ErrInvalidArgument):
		status, code = http.StatusBadRequest, "invalid_argument"
	default:
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_scheduled_posts_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ScheduledPostsError{Code: code})
}

func scheduledPostJSON(post chatrecipient.ScheduledPost) ScheduledPostJSON {
	return ScheduledPostJSON{ID: post.ID, ConversationID: post.ConversationID, Body: post.Body, SendAt: post.SendAt.UTC(), Zone: post.Zone, State: post.State, Refusal: post.Refusal, Revision: post.Revision, QuietPeople: post.QuietPeople}
}

type ScheduledPostsHandler struct {
	Service ScheduledPostsPort
	Now     func() time.Time
}

func (h ScheduledPostsHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h ScheduledPostsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != ScheduledPostsPath {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		scheduledPostsError(w, chat.ErrUnauthenticated)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	switch r.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
	default:
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(ScheduledPostsError{Code: "method_not_allowed"})
		return
	}
	// Decide who is asking before the body is read: only the person's own,
	// signed-in human identity may keep messages to send later.
	if err := chat.ValidateSavedOwner(r.Context(), p, p.TenantID); err != nil {
		scheduledPostsError(w, err)
		return
	}
	host := r.URL.Query().Get("host")
	var command ScheduledPostCommand
	if r.Method != http.MethodGet {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			scheduledPostsError(w, chat.ErrInvalidArgument)
			return
		}
		host = command.Host
	}
	if host == "" {
		host = p.TenantID
	}
	if err := chat.ValidateSavedOwner(r.Context(), p, host); err != nil {
		scheduledPostsError(w, err)
		return
	}
	if h.Service == nil {
		scheduledPostsError(w, chat.ErrUnavailable)
		return
	}
	now := h.now()
	switch r.Method {
	case http.MethodGet:
		posts, err := h.Service.ScheduledPosts(r.Context(), p, host)
		if err != nil {
			scheduledPostsError(w, err)
			return
		}
		reply := ScheduledPostsReply{HostTenantID: host, Posts: make([]ScheduledPostJSON, 0, len(posts))}
		for _, post := range posts {
			reply.Posts = append(reply.Posts, scheduledPostJSON(post))
		}
		_ = json.NewEncoder(w).Encode(reply)
	case http.MethodPost:
		post, err := h.Service.ScheduleSend(r.Context(), p, host, strings.TrimSpace(command.ConversationID), command.Body, command.SendAt, command.Zone, verified.OrganizationScopeID(), now)
		if err != nil {
			scheduledPostsError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(ScheduledPostReply{Post: scheduledPostJSON(post)})
	case http.MethodPut:
		post, err := h.Service.EditScheduled(r.Context(), p, host, strings.TrimSpace(command.ID), command.Body, command.SendAt, command.Zone, command.Revision, now)
		if err != nil {
			scheduledPostsError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(ScheduledPostReply{Post: scheduledPostJSON(post)})
	case http.MethodDelete:
		if err := h.Service.CancelScheduled(r.Context(), p, host, strings.TrimSpace(command.ID)); err != nil {
			scheduledPostsError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			Status string `json:"status"`
		}{"cancelled"})
	}
}

// OverlayScheduledPosts serves the route behind the same admission every other
// Chat route passes.
func OverlayScheduledPosts(next http.Handler, service ScheduledPostsPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ScheduledPostsHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ScheduledPostsPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(ScheduledPostsError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
