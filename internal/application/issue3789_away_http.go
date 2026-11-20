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

// Issue 3789: the away setting.
//
// GET /api/chat/away returns the signed-in person's own setting; POST saves it
// against the revision the client read (a zero until ends the absence).
// POST /api/chat/away/lookup names up to 50 colleagues and returns the running,
// unhidden absences among them: until when and who covers, nothing else. The
// person always comes from the session. The setting is a chat preference: it
// grants the cover no conversation and no approval, and it carries no leave
// details, because none are stored.
const (
	ChatAwayPath       = "/api/chat/away"
	ChatAwayLookupPath = "/api/chat/away/lookup"
)

type ChatAwayPort interface {
	Away(ctx context.Context, p chat.Principal, now time.Time) (chatrecipient.Away, error)
	PutAway(ctx context.Context, p chat.Principal, a chatrecipient.Away, expected uint64, now time.Time) (chatrecipient.Away, error)
	AwayOf(ctx context.Context, p chat.Principal, ids []string, now time.Time) ([]chatrecipient.AwayPublic, error)
}

type ChatAwayReply struct {
	Revision    uint64     `json:"revision"`
	Active      bool       `json:"active"`
	Until       *time.Time `json:"until,omitempty"`
	CoverID     string     `json:"cover_id,omitempty"`
	Hidden      bool       `json:"hidden"`
	NotifyCover bool       `json:"notify_cover"`
	Now         time.Time  `json:"now"`
}

type ChatAwayCommand struct {
	ExpectedRevision uint64     `json:"expected_revision"`
	Until            *time.Time `json:"until,omitempty"`
	CoverID          string     `json:"cover_id,omitempty"`
	Hidden           bool       `json:"hidden"`
	NotifyCover      bool       `json:"notify_cover"`
}

type ChatAwayPersonJSON struct {
	MemberID string    `json:"member_id"`
	Until    time.Time `json:"until"`
	CoverID  string    `json:"cover_id,omitempty"`
}

type ChatAwayLookupCommand struct {
	Members []string `json:"members"`
}

type ChatAwayLookupReply struct {
	People []ChatAwayPersonJSON `json:"people"`
}

type ChatAwayError struct {
	Code string `json:"code"`
}

func chatAwayError(w http.ResponseWriter, err error) {
	out := ChatAwayError{Code: "unavailable"}
	status := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, chat.ErrUnauthenticated):
		status, out.Code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, chat.ErrPermissionDenied):
		status, out.Code = http.StatusForbidden, "permission_denied"
	case errors.Is(err, chat.ErrNotFound):
		status, out.Code = http.StatusNotFound, "not_found"
	case errors.Is(err, chat.ErrConflict):
		status, out.Code = http.StatusConflict, "conflict"
	case errors.Is(err, chat.ErrInvalidArgument):
		status, out.Code = http.StatusBadRequest, "invalid_argument"
	default:
		// The body never carries the cause.
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_away_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

func chatAwayPort(runtime composedChat) ChatAwayPort {
	if runtime.extensions == nil || runtime.extensions.Recipients == nil {
		return nil
	}
	return runtime.extensions.Recipients
}

type ChatAwayHandler struct {
	Service ChatAwayPort
	Now     func() time.Time
}

func (h ChatAwayHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func chatAwayReply(a chatrecipient.Away, now time.Time) ChatAwayReply {
	out := ChatAwayReply{Revision: a.Revision, Active: a.Active(now), Hidden: a.Hidden, Now: now}
	if out.Active {
		until := a.Until.UTC()
		out.Until, out.CoverID, out.NotifyCover = &until, a.CoverID, a.NotifyCover
	}
	return out
}

func (h ChatAwayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	lookup := r.URL.Path == ChatAwayLookupPath
	if r.URL.Path != ChatAwayPath && !lookup {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		chatAwayError(w, chat.ErrUnauthenticated)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	allowed := r.Method == http.MethodGet || r.Method == http.MethodPost
	if lookup {
		allowed = r.Method == http.MethodPost
	}
	if !allowed {
		if lookup {
			w.Header().Set("Allow", "POST")
		} else {
			w.Header().Set("Allow", "GET, POST")
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(ChatAwayError{Code: "method_not_allowed"})
		return
	}
	// Only a signed-in human may use the setting. Decide that before the body is read.
	if err := chat.ValidateSavedOwner(r.Context(), p, p.TenantID); err != nil {
		chatAwayError(w, err)
		return
	}
	decode := func(into any) bool {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(into) != nil || decoder.Decode(new(any)) != io.EOF {
			chatAwayError(w, chat.ErrInvalidArgument)
			return false
		}
		return true
	}
	var command ChatAwayCommand
	var lookupCommand ChatAwayLookupCommand
	if lookup && !decode(&lookupCommand) {
		return
	}
	if !lookup && r.Method == http.MethodPost && !decode(&command) {
		return
	}
	if h.Service == nil {
		chatAwayError(w, chat.ErrUnavailable)
		return
	}
	now := h.now()
	if lookup {
		people, err := h.Service.AwayOf(r.Context(), p, lookupCommand.Members, now)
		if err != nil {
			chatAwayError(w, err)
			return
		}
		out := ChatAwayLookupReply{People: make([]ChatAwayPersonJSON, 0, len(people))}
		for _, person := range people {
			out.People = append(out.People, ChatAwayPersonJSON{MemberID: person.MemberID, Until: person.Until.UTC(), CoverID: person.CoverID})
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	if r.Method == http.MethodPost {
		next := chatrecipient.Away{CoverID: command.CoverID, Hidden: command.Hidden, NotifyCover: command.NotifyCover}
		if command.Until != nil {
			next.Until = command.Until.UTC()
		}
		if _, err := h.Service.PutAway(r.Context(), p, next, command.ExpectedRevision, now); err != nil {
			chatAwayError(w, err)
			return
		}
	}
	current, err := h.Service.Away(r.Context(), p, now)
	if err != nil {
		chatAwayError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(chatAwayReply(current, now))
}

func OverlayChatAway(next http.Handler, service ChatAwayPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatAwayHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatAwayPath && r.URL.Path != ChatAwayLookupPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(ChatAwayError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
