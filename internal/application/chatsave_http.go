package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const SavedMessagesPath = "/api/chat/saved"

type SavedMessagesPort interface {
	SaveForLater(context.Context, chat.SavedRequest) (chat.SavedItem, error)
	Unsave(context.Context, chat.SavedRequest) error
	UpdateSaved(context.Context, chat.SavedRequest, chat.SavedChange) (chat.SavedItem, error)
	ListSaved(context.Context, chat.SavedListRequest) (chat.SavedPage, error)
	SearchSaved(context.Context, chat.Principal, string) ([]chat.SavedItem, error)
}

type SavedMessageCommand struct {
	Action         string     `json:"action"`
	ConversationID string     `json:"conversation_id"`
	PostID         string     `json:"post_id"`
	Note           *string    `json:"note,omitempty"`
	SetDue         bool       `json:"set_due,omitempty"`
	DueAt          *time.Time `json:"due_at,omitempty"`
}

type SavedMessagesHandler struct{ Service SavedMessagesPort }

type SavedMessagesError struct {
	Code string `json:"code"`
}

func chatsaveHTTPError(w http.ResponseWriter, err error) {
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
	case errors.Is(err, chat.ErrSavedLimit):
		status, code = http.StatusConflict, "saved_limit"
	case errors.Is(err, chat.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	default:
		// The body never carries the cause. A developer cell can opt in to
		// seeing it in the server log, as the other chat and agent surfaces do.
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.chat_saved_unavailable", "cause", err.Error())
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(SavedMessagesError{Code: code})
}

func (h SavedMessagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != SavedMessagesPath {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		chatsaveHTTPError(w, chat.ErrUnauthenticated)
		return
	}
	p := chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	host := r.URL.Query().Get("host")
	if host == "" {
		host = p.TenantID
	}
	if err := chat.ValidateSavedOwner(r.Context(), p, host); err != nil {
		chatsaveHTTPError(w, err)
		return
	}
	if h.Service == nil {
		chatsaveHTTPError(w, chat.ErrUnavailable)
		return
	}
	var reply any
	var err error
	switch r.Method {
	case http.MethodGet:
		if query, exists := r.URL.Query()["query"]; exists {
			if len(query) != 1 {
				chatsaveHTTPError(w, chat.ErrInvalidArgument)
				return
			}
			reply, err = h.Service.SearchSaved(r.Context(), p, query[0])
		} else {
			limit := uint64(50)
			if raw := r.URL.Query().Get("limit"); raw != "" {
				limit, err = strconv.ParseUint(raw, 10, 32)
				if err != nil || limit == 0 || limit > 200 {
					chatsaveHTTPError(w, chat.ErrInvalidArgument)
					return
				}
			}
			reply, err = h.Service.ListSaved(r.Context(), chat.SavedListRequest{Principal: p, TenantID: host, Tab: chat.SavedState(r.URL.Query().Get("tab")), Page: chat.Page{PageSize: uint32(limit), Cursor: r.URL.Query().Get("cursor")}})
		}
	case http.MethodPost:
		var command SavedMessageCommand
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			chatsaveHTTPError(w, chat.ErrInvalidArgument)
			return
		}
		req := chat.SavedRequest{Principal: p, TenantID: host, ConversationID: command.ConversationID, PostID: command.PostID}
		switch command.Action {
		case "save":
			reply, err = h.Service.SaveForLater(r.Context(), req)
		case "remove":
			err = h.Service.Unsave(r.Context(), req)
			reply = struct {
				Removed bool `json:"removed"`
			}{err == nil}
		case "done", "reopen":
			state := chat.SavedDone
			if command.Action == "reopen" {
				state = chat.SavedTodo
			}
			reply, err = h.Service.UpdateSaved(r.Context(), req, chat.SavedChange{State: &state})
		case "note":
			if command.Note == nil {
				err = chat.ErrInvalidArgument
			} else {
				reply, err = h.Service.UpdateSaved(r.Context(), req, chat.SavedChange{Note: command.Note})
			}
		case "due":
			if !command.SetDue {
				err = chat.ErrInvalidArgument
			} else {
				reply, err = h.Service.UpdateSaved(r.Context(), req, chat.SavedChange{SetDue: true, DueAt: command.DueAt})
			}
		default:
			err = chat.ErrInvalidArgument
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(SavedMessagesError{Code: "method_not_allowed"})
		return
	}
	if err != nil {
		chatsaveHTTPError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(reply)
}

func OverlaySavedMessages(next http.Handler, service SavedMessagesPort, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := SavedMessagesHandler{Service: service}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != SavedMessagesPath && !strings.HasPrefix(r.URL.Path, SavedMessagesPath+"/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			// A request the edge refuses never reaches the handler's own log line.
			if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
				slog.Warn("hcmnext.chat_saved_denied", "method", r.Method, "status", denied.HTTPStatus(), "code", denied.Code(), "reason", denied.ReasonRef())
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(denied.HTTPStatus())
			_ = json.NewEncoder(w).Encode(SavedMessagesError{Code: "request_denied"})
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
