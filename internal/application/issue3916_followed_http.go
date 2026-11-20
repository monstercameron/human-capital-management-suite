package application

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3916: GET /api/chat/followed-threads lists the signed-in person's own
// followed threads. It is read-only: following and unfollowing stay on the
// thread-follow RPC the thread header uses, so the list and the header agree
// because they read and write the same rows.
const FollowedThreadsPath = "/api/chat/followed-threads"

type FollowedThreadsPort interface {
	FollowedThreads(ctx context.Context, p chat.Principal, host string) ([]chatrecipient.FollowedThread, error)
}

type FollowedThreadJSON struct {
	ConversationID string    `json:"conversation_id"`
	RootPostID     string    `json:"root_post_id"`
	RootSequence   uint64    `json:"root_sequence"`
	RootAuthorID   string    `json:"root_author_id"`
	RootBody       string    `json:"root_body"`
	RootRemoved    bool      `json:"root_removed,omitempty"`
	ReplyCount     uint64    `json:"reply_count"`
	LastActivity   time.Time `json:"last_activity"`
	Revision       uint64    `json:"revision"`
}

type FollowedThreadsReply struct {
	HostTenantID string               `json:"host_tenant_id"`
	Threads      []FollowedThreadJSON `json:"threads"`
}

type FollowedThreadsError struct {
	Code string `json:"code"`
}

// followedThreadsReaderPort applies the reader's own rendering selection to the
// root previews, as the saved list does, so a preview never shows text the
// conversation itself would hold back.
type followedThreadsReaderPort struct {
	FollowedThreadsPort
	Rendering ChatRenderingPort
}

func (s followedThreadsReaderPort) FollowedThreads(ctx context.Context, p chat.Principal, host string) ([]chatrecipient.FollowedThread, error) {
	rows, err := s.FollowedThreadsPort.FollowedThreads(ctx, p, host)
	if err != nil {
		return nil, err
	}
	out := append([]chatrecipient.FollowedThread(nil), rows...)
	for i := range out {
		if out[i].RootRemoved {
			out[i].RootBody = ""
			continue
		}
		selected, mark, err := s.Rendering.ReadRenderingSelection(ctx, chatstore.RenderingScope{Principal: p, Tenant: host, Conversation: out[i].ConversationID}, out[i].RootPostID)
		if err != nil || mark.State == "pending" || mark.State == "unavailable" {
			out[i].RootBody = ""
			continue
		}
		out[i].RootBody = chatrecipient.FollowedPreview(selected.Text)
	}
	return out, nil
}

func followedThreadsPort(runtime composedChat) FollowedThreadsPort {
	if runtime.extensions == nil || runtime.extensions.Recipients == nil {
		return nil
	}
	core := FollowedThreadsPort(runtime.extensions.Recipients)
	if runtime.renderings == nil {
		return core
	}
	return followedThreadsReaderPort{FollowedThreadsPort: core, Rendering: runtime.renderings}
}

func followedThreadsError(w http.ResponseWriter, err error) {
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
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_followed_threads_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(FollowedThreadsError{Code: code})
}

type FollowedThreadsHandler struct{ Service FollowedThreadsPort }

func (h FollowedThreadsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != FollowedThreadsPath {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		followedThreadsError(w, chat.ErrUnauthenticated)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	host := r.URL.Query().Get("host")
	if host == "" {
		host = p.TenantID
	}
	// Only the person's own, signed-in human identity may read this list.
	if err := chat.ValidateSavedOwner(r.Context(), p, host); err != nil {
		followedThreadsError(w, err)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(FollowedThreadsError{Code: "method_not_allowed"})
		return
	}
	if h.Service == nil {
		followedThreadsError(w, chat.ErrUnavailable)
		return
	}
	rows, err := h.Service.FollowedThreads(r.Context(), p, host)
	if err != nil {
		followedThreadsError(w, err)
		return
	}
	reply := FollowedThreadsReply{HostTenantID: host, Threads: make([]FollowedThreadJSON, 0, len(rows))}
	for _, row := range rows {
		reply.Threads = append(reply.Threads, FollowedThreadJSON{ConversationID: row.ConversationID, RootPostID: row.RootPostID, RootSequence: row.RootSequence, RootAuthorID: row.RootAuthorID, RootBody: row.RootBody, RootRemoved: row.RootRemoved, ReplyCount: row.ReplyCount, LastActivity: row.LastActivity.UTC(), Revision: row.Revision})
	}
	_ = json.NewEncoder(w).Encode(reply)
}

func OverlayFollowedThreads(next http.Handler, service FollowedThreadsPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := FollowedThreadsHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != FollowedThreadsPath && !strings.HasPrefix(r.URL.Path, FollowedThreadsPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(FollowedThreadsError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
