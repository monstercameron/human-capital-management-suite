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
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3785: POST /api/chat/catch-up/v1 returns the signed-in person's private
// "Catch me up" brief for one conversation or thread. The person comes from
// the session, never from the request. It is a read: it writes no post and
// does not move the person's read position. The request is a POST only so the
// conversation and post ids travel in a body rather than in a logged URL.
const ChatCatchUpPath = "/api/chat/catch-up/v1"

type ChatCatchUpPort interface {
	CatchUp(ctx context.Context, p chat.Principal, host, conversation, threadRootID, anchorPostID string, now time.Time) (chatrecipient.CatchUpBrief, error)
}

type ChatCatchUpCommand struct {
	Host           string `json:"host,omitempty"`
	ConversationID string `json:"conversation_id"`
	ThreadRootID   string `json:"thread_root_id,omitempty"`
	SincePostID    string `json:"since_post_id,omitempty"`
}

type ChatCatchUpLineJSON struct {
	PostID       string    `json:"post_id"`
	Sequence     uint64    `json:"sequence"`
	ThreadRootID string    `json:"thread_root_id,omitempty"`
	AuthorID     string    `json:"author_id"`
	Text         string    `json:"text"`
	At           time.Time `json:"at"`
}

type ChatCatchUpSectionJSON struct {
	Kind  string                `json:"kind"`
	More  int                   `json:"more"`
	Lines []ChatCatchUpLineJSON `json:"lines"`
}

type ChatCatchUpReply struct {
	HostTenantID     string                   `json:"host_tenant_id"`
	ConversationID   string                   `json:"conversation_id"`
	ConversationName string                   `json:"conversation_name,omitempty"`
	ThreadRootID     string                   `json:"thread_root_id,omitempty"`
	Cursor           uint64                   `json:"cursor"`
	After            uint64                   `json:"after"`
	Through          uint64                   `json:"through"`
	CoveredFrom      uint64                   `json:"covered_from"`
	Total            int                      `json:"total"`
	Covered          int                      `json:"covered"`
	Omitted          int                      `json:"omitted"`
	Truncated        bool                     `json:"truncated"`
	Generated        time.Time                `json:"generated_at"`
	Method           string                   `json:"method"`
	Sections         []ChatCatchUpSectionJSON `json:"sections"`
}

type ChatCatchUpError struct {
	Code string `json:"code"`
}

func chatCatchUpPort(runtime composedChat) ChatCatchUpPort {
	if runtime.extensions == nil || runtime.extensions.Recipients == nil {
		return nil
	}
	return runtime.extensions.Recipients
}

func chatCatchUpError(w http.ResponseWriter, err error) {
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
			slog.Warn("hcmnext.chat_catch_up_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ChatCatchUpError{Code: code})
}

type ChatCatchUpHandler struct {
	Service ChatCatchUpPort
	Now     func() time.Time
}

func (h ChatCatchUpHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func chatCatchUpReply(b chatrecipient.CatchUpBrief) ChatCatchUpReply {
	out := ChatCatchUpReply{HostTenantID: b.HostTenantID, ConversationID: b.ConversationID, ConversationName: b.ConversationName, ThreadRootID: b.ThreadRootID,
		Cursor: b.Cursor, After: b.After, Through: b.Through, CoveredFrom: b.CoveredFrom, Total: b.Total, Covered: b.Covered, Omitted: b.Omitted,
		Truncated: b.Truncated, Generated: b.Generated.UTC(), Method: b.Method, Sections: []ChatCatchUpSectionJSON{}}
	for _, section := range b.Sections {
		next := ChatCatchUpSectionJSON{Kind: section.Kind, More: section.More, Lines: []ChatCatchUpLineJSON{}}
		for _, line := range section.Lines {
			next.Lines = append(next.Lines, ChatCatchUpLineJSON{PostID: line.PostID, Sequence: line.Sequence, ThreadRootID: line.ThreadRootID, AuthorID: line.AuthorID, Text: line.Text, At: line.At.UTC()})
		}
		out.Sections = append(out.Sections, next)
	}
	return out
}

func (h ChatCatchUpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != ChatCatchUpPath {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		chatCatchUpError(w, chat.ErrUnauthenticated)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(ChatCatchUpError{Code: "method_not_allowed"})
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	// Decide who is asking before the body is read: only the person's own,
	// signed-in human identity may ask for a brief.
	if err := chat.ValidateSavedOwner(r.Context(), p, p.TenantID); err != nil {
		chatCatchUpError(w, err)
		return
	}
	var command ChatCatchUpCommand
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF || command.ConversationID == "" {
		chatCatchUpError(w, chat.ErrInvalidArgument)
		return
	}
	host := command.Host
	if host == "" {
		host = p.TenantID
	}
	if err := chat.ValidateSavedOwner(r.Context(), p, host); err != nil {
		chatCatchUpError(w, err)
		return
	}
	if h.Service == nil {
		chatCatchUpError(w, chat.ErrUnavailable)
		return
	}
	brief, err := h.Service.CatchUp(r.Context(), p, host, command.ConversationID, command.ThreadRootID, command.SincePostID, h.now())
	if err != nil {
		chatCatchUpError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(chatCatchUpReply(brief))
}

func OverlayChatCatchUp(next http.Handler, service ChatCatchUpPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatCatchUpHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatCatchUpPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(ChatCatchUpError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
