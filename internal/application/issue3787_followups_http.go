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
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3787: GET /api/chat/follow-ups lists what is waiting on the signed-in
// person and what they are waiting on; POST records their own choice for one
// row (snooze, dismiss, mark resolved, keep open). The list is computed from
// posts the person can read; nothing here sends a message or closes a question
// on its own.
const FollowUpsPath = "/api/chat/follow-ups"

type FollowUpsPort interface {
	FollowUps(ctx context.Context, p chat.Principal, host string, now time.Time) ([]chatrecipient.FollowUp, error)
	SetFollowUp(ctx context.Context, p chat.Principal, host, conversation, post, direction, status string, until, now time.Time) error
}

type FollowUpJSON struct {
	ConversationID string     `json:"conversation_id"`
	PostID         string     `json:"post_id"`
	Sequence       uint64     `json:"sequence"`
	Direction      string     `json:"direction"`
	AuthorID       string     `json:"author_id"`
	ThreadRootID   string     `json:"thread_root_id,omitempty"`
	Body           string     `json:"body"`
	AskedAt        time.Time  `json:"asked_at"`
	WaitingOn      []string   `json:"waiting_on"`
	Evidence       string     `json:"evidence,omitempty"`
	EvidenceAt     *time.Time `json:"evidence_at,omitempty"`
	Kept           bool       `json:"kept,omitempty"`
}

type FollowUpsReply struct {
	HostTenantID string         `json:"host_tenant_id"`
	Owed         []FollowUpJSON `json:"owed"`
	Waiting      []FollowUpJSON `json:"waiting"`
}

type FollowUpCommand struct {
	Host           string    `json:"host,omitempty"`
	ConversationID string    `json:"conversation_id"`
	PostID         string    `json:"post_id"`
	Direction      string    `json:"direction"`
	Action         string    `json:"action"`
	Until          time.Time `json:"until,omitempty"`
}

type FollowUpsError struct {
	Code string `json:"code"`
}

// followUpsReaderPort applies the reader's own rendering selection to the
// message text, as the saved and followed lists do, so a row never shows text
// the conversation itself would hold back.
type followUpsReaderPort struct {
	FollowUpsPort
	Rendering ChatRenderingPort
}

func (s followUpsReaderPort) FollowUps(ctx context.Context, p chat.Principal, host string, now time.Time) ([]chatrecipient.FollowUp, error) {
	rows, err := s.FollowUpsPort.FollowUps(ctx, p, host, now)
	if err != nil {
		return nil, err
	}
	out := append([]chatrecipient.FollowUp(nil), rows...)
	for i := range out {
		selected, mark, err := s.Rendering.ReadRenderingSelection(ctx, chatstore.RenderingScope{Principal: p, Tenant: host, Conversation: out[i].ConversationID}, out[i].PostID)
		if err != nil || mark.State == "pending" || mark.State == "unavailable" {
			out[i].Body = ""
			continue
		}
		out[i].Body = chatrecipient.FollowedPreview(selected.Text)
	}
	return out, nil
}

func followUpsPort(runtime composedChat) FollowUpsPort {
	if runtime.extensions == nil || runtime.extensions.Recipients == nil {
		return nil
	}
	core := FollowUpsPort(runtime.extensions.Recipients)
	if runtime.renderings == nil {
		return core
	}
	return followUpsReaderPort{FollowUpsPort: core, Rendering: runtime.renderings}
}

func followUpsError(w http.ResponseWriter, err error) {
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
			slog.Warn("hcmnext.chat_follow_ups_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(FollowUpsError{Code: code})
}

type FollowUpsHandler struct {
	Service FollowUpsPort
	Now     func() time.Time
}

func (h FollowUpsHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func followUpRows(rows []chatrecipient.FollowUp, direction string) []FollowUpJSON {
	out := make([]FollowUpJSON, 0, len(rows))
	for _, row := range rows {
		if row.Direction != direction {
			continue
		}
		item := FollowUpJSON{ConversationID: row.ConversationID, PostID: row.PostID, Sequence: row.Sequence, Direction: row.Direction, AuthorID: row.AuthorID, ThreadRootID: row.ThreadRootID, Body: row.Body, AskedAt: row.AskedAt.UTC(), WaitingOn: append([]string{}, row.WaitingOn...), Evidence: row.Evidence, Kept: row.Kept}
		if !row.EvidenceAt.IsZero() {
			at := row.EvidenceAt.UTC()
			item.EvidenceAt = &at
		}
		out = append(out, item)
	}
	return out
}

func (h FollowUpsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != FollowUpsPath {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		followUpsError(w, chat.ErrUnauthenticated)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	host := r.URL.Query().Get("host")
	var command FollowUpCommand
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		// Decide who is asking before the body is read.
		if err := chat.ValidateSavedOwner(r.Context(), p, p.TenantID); err != nil {
			followUpsError(w, err)
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			followUpsError(w, chat.ErrInvalidArgument)
			return
		}
		host = command.Host
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(FollowUpsError{Code: "method_not_allowed"})
		return
	}
	if host == "" {
		host = p.TenantID
	}
	// Only the person's own, signed-in human identity may use this list.
	if err := chat.ValidateSavedOwner(r.Context(), p, host); err != nil {
		followUpsError(w, err)
		return
	}
	if h.Service == nil {
		followUpsError(w, chat.ErrUnavailable)
		return
	}
	now := h.now()
	if r.Method == http.MethodPost {
		status := ""
		switch command.Action {
		case "dismiss":
			status = chatrecipient.FollowUpDismissed
		case "resolve":
			status = chatrecipient.FollowUpResolved
		case "keep":
			status = chatrecipient.FollowUpKept
		case "snooze":
			status = chatrecipient.FollowUpSnoozed
		default:
			followUpsError(w, chat.ErrInvalidArgument)
			return
		}
		if err := h.Service.SetFollowUp(r.Context(), p, host, strings.TrimSpace(command.ConversationID), strings.TrimSpace(command.PostID), command.Direction, status, command.Until.UTC(), now); err != nil {
			followUpsError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			Status string `json:"status"`
		}{status})
		return
	}
	rows, err := h.Service.FollowUps(r.Context(), p, host, now)
	if err != nil {
		followUpsError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(FollowUpsReply{HostTenantID: host, Owed: followUpRows(rows, chatrecipient.FollowUpOwed), Waiting: followUpRows(rows, chatrecipient.FollowUpWaiting)})
}

func OverlayFollowUps(next http.Handler, service FollowUpsPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := FollowUpsHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != FollowUpsPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(FollowUpsError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
