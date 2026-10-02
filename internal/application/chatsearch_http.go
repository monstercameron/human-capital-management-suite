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

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const ChatSearchPath = "/api/chat/search"

type ChatSearchPort interface {
	Search(context.Context, chatsearch.Request) (chatsearch.Response, error)
}
type ChatSearchHistory interface {
	Remember(chatsearch.Actor, string) error
	List(chatsearch.Actor) []string
	Clear(chatsearch.Actor)
}
type ChatSearchHTTP struct {
	Port    ChatSearchPort
	History ChatSearchHistory
}
type ChatSearchError struct {
	Code string `json:"code"`
}

func chatsearchHTTPError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ChatSearchError{code})
}

// chatsearchLogCause records why a search degraded, for a developer cell that
// opts in with HCMNEXT_AGENT_DEBUG_CAUSES=1. The response body never carries it.
func chatsearchLogCause(message string, cause error, attrs ...any) {
	if cause == nil || os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") != "1" {
		return
	}
	slog.Warn(message, append([]any{"cause", cause.Error()}, attrs...)...)
}

func OverlayChatSearch(next http.Handler, search ChatSearchHTTP, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatSearchPath && r.URL.Path != ChatSearchPath+"/recent" {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			chatsearchHTTPError(w, denied.HTTPStatus(), "denied")
			return
		}
		search.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (h ChatSearchHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	p, ok := trust.FromContext(r.Context())
	_, admitted := transport.InvocationFromContext(r.Context())
	if !ok || !admitted || p.SubjectKind() != trust.SubjectKindHuman || !time.Now().Before(p.ExpiresAt()) {
		chatsearchHTTPError(w, http.StatusUnauthorized, "denied")
		return
	}
	actor := chatsearch.Actor{TenantID: p.Tenant().String(), HomeTenantID: p.Tenant().String(), PersonID: p.Subject()}
	if r.URL.Path == ChatSearchPath+"/recent" {
		if h.History == nil {
			chatsearchHTTPError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(h.History.List(actor))
		case http.MethodDelete:
			h.History.Clear(actor)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "GET, DELETE")
			chatsearchHTTPError(w, http.StatusMethodNotAllowed, "method")
		}
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		chatsearchHTTPError(w, http.StatusMethodNotAllowed, "method")
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		chatsearchHTTPError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	var request chatsearch.Request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		chatsearchHTTPError(w, http.StatusBadRequest, "invalid")
		return
	}
	if h.Port == nil {
		chatsearchHTTPError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	request.Actor, request.At = actor, time.Now().UTC()
	request.BeforeAt, request.BeforeKey = time.Time{}, ""
	response, err := h.Port.Search(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, chatsearch.ErrInvalid):
			chatsearchHTTPError(w, http.StatusBadRequest, "invalid")
		case errors.Is(err, chatsearch.ErrUnavailable):
			chatsearchLogCause("hcmnext.chat_search_unavailable", err)
			chatsearchHTTPError(w, http.StatusServiceUnavailable, "meaning_unavailable")
		default:
			chatsearchLogCause("hcmnext.chat_search_unavailable", err)
			chatsearchHTTPError(w, http.StatusServiceUnavailable, "unavailable")
		}
		return
	}
	for _, failure := range response.Failures {
		chatsearchLogCause("hcmnext.chat_search_source_unavailable", failure.Err, "source", string(failure.Kind))
	}
	if h.History != nil && !request.Transient {
		display := request.DisplayQuery
		if display == "" {
			display = request.Query
		}
		// The recent-searches list is a convenience; failing to record a query
		// must not take its answer away.
		if err := h.History.Remember(actor, display); err != nil {
			chatsearchLogCause("hcmnext.chat_search_history_unavailable", err)
		}
	}
	_ = json.NewEncoder(w).Encode(response)
}
