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

// Issue 3786: GET /api/chat/digest returns the signed-in person's digest
// choice and, when the time they chose has passed, the digest itself: the
// mentions that quiet hours or a muted conversation held back, and the
// followed threads that moved. POST saves the choice ("set") or records that
// the digest was seen ("seen"). The person always comes from the session. The
// digest names messages and people, never message text.
const ChatDigestPath = "/api/chat/digest"

type ChatDigestPort interface {
	DigestPrefs(ctx context.Context, p chat.Principal) (chatrecipient.DigestPref, error)
	PutDigestPrefs(ctx context.Context, p chat.Principal, want chatrecipient.DigestPref, now time.Time) (chatrecipient.DigestPref, error)
	Digest(ctx context.Context, p chat.Principal, host string, now time.Time) (chatrecipient.Digest, error)
	DigestSeen(ctx context.Context, p chat.Principal, through, now time.Time) error
}

type ChatDigestPrefJSON struct {
	Cadence  string `json:"cadence"`
	Minute   int    `json:"minute"`
	Timezone string `json:"timezone"`
}

type ChatDigestItemJSON struct {
	Why                string    `json:"why"`
	ConversationID     string    `json:"conversation_id"`
	ConversationName   string    `json:"conversation_name,omitempty"`
	ConversationKind   string    `json:"conversation_kind"`
	PostID             string    `json:"post_id"`
	Sequence           uint64    `json:"sequence"`
	ThreadRootID       string    `json:"thread_root_id,omitempty"`
	ThreadRootSequence uint64    `json:"thread_root_sequence,omitempty"`
	AuthorID           string    `json:"author_id"`
	At                 time.Time `json:"at"`
	Count              int       `json:"count"`
}

type ChatDigestReply struct {
	HostTenantID string               `json:"host_tenant_id"`
	Pref         ChatDigestPrefJSON   `json:"pref"`
	Due          bool                 `json:"due"`
	Through      time.Time            `json:"through"`
	Items        []ChatDigestItemJSON `json:"items"`
}

type ChatDigestCommand struct {
	Host     string    `json:"host,omitempty"`
	Action   string    `json:"action"`
	Cadence  string    `json:"cadence,omitempty"`
	Minute   int       `json:"minute,omitempty"`
	Timezone string    `json:"timezone,omitempty"`
	Through  time.Time `json:"through,omitempty"`
}

type ChatDigestError struct {
	Code string `json:"code"`
}

func chatDigestPort(runtime composedChat) ChatDigestPort {
	if runtime.extensions == nil || runtime.extensions.Recipients == nil {
		return nil
	}
	return runtime.extensions.Recipients
}

func chatDigestError(w http.ResponseWriter, err error) {
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
			slog.Warn("hcmnext.chat_digest_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ChatDigestError{Code: code})
}

type ChatDigestHandler struct {
	Service ChatDigestPort
	Now     func() time.Time
}

func (h ChatDigestHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func chatDigestReply(host string, d chatrecipient.Digest) ChatDigestReply {
	out := ChatDigestReply{HostTenantID: host, Due: d.Due, Through: d.Through.UTC(), Items: []ChatDigestItemJSON{},
		Pref: ChatDigestPrefJSON{Cadence: d.Pref.Cadence, Minute: d.Pref.Minute, Timezone: d.Pref.Timezone}}
	for _, row := range d.Items {
		out.Items = append(out.Items, ChatDigestItemJSON{Why: row.Why, ConversationID: row.ConversationID, ConversationName: row.ConversationName,
			ConversationKind: row.ConversationKind, PostID: row.PostID, Sequence: row.Sequence, ThreadRootID: row.ThreadRootID,
			ThreadRootSequence: row.ThreadRootSequence, AuthorID: row.AuthorID, At: row.At.UTC(), Count: row.Count})
	}
	return out
}

func (h ChatDigestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != ChatDigestPath {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		chatDigestError(w, chat.ErrUnauthenticated)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	host := r.URL.Query().Get("host")
	var command ChatDigestCommand
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		// Decide who is asking before the body is read.
		if err := chat.ValidateSavedOwner(r.Context(), p, p.TenantID); err != nil {
			chatDigestError(w, err)
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			chatDigestError(w, chat.ErrInvalidArgument)
			return
		}
		host = command.Host
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(ChatDigestError{Code: "method_not_allowed"})
		return
	}
	if host == "" {
		host = p.TenantID
	}
	// Only the person's own, signed-in human identity may use the digest.
	if err := chat.ValidateSavedOwner(r.Context(), p, host); err != nil {
		chatDigestError(w, err)
		return
	}
	if h.Service == nil {
		chatDigestError(w, chat.ErrUnavailable)
		return
	}
	now := h.now()
	if r.Method == http.MethodPost {
		switch command.Action {
		case "set":
			pref, err := h.Service.PutDigestPrefs(r.Context(), p, chatrecipient.DigestPref{Cadence: command.Cadence, Minute: command.Minute, Timezone: command.Timezone}, now)
			if err != nil {
				chatDigestError(w, err)
				return
			}
			_ = json.NewEncoder(w).Encode(ChatDigestPrefJSON{Cadence: pref.Cadence, Minute: pref.Minute, Timezone: pref.Timezone})
		case "seen":
			if err := h.Service.DigestSeen(r.Context(), p, command.Through, now); err != nil {
				chatDigestError(w, err)
				return
			}
			_ = json.NewEncoder(w).Encode(struct {
				Seen bool `json:"seen"`
			}{true})
		default:
			chatDigestError(w, chat.ErrInvalidArgument)
		}
		return
	}
	digest, err := h.Service.Digest(r.Context(), p, host, now)
	if err != nil {
		chatDigestError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(chatDigestReply(host, digest))
}

func OverlayChatDigest(next http.Handler, service ChatDigestPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatDigestHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatDigestPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(ChatDigestError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
