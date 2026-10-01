package timeclock

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	// ClockEventsPath is the canonical pull event-feed route.
	ClockEventsPath            = "/v1/events"
	maxClockEventsBody         = 1 << 20
	defaultClockEventsPageSize = 100
	maxClockEventsPageSize     = 1000
)

// EventFeed is the application port used by the event-feed transport. The
// transport deliberately depends on Pull rather than the feed implementation.
type EventFeed interface {
	Pull(context.Context, clockservice.ClockEventRequest) (clockservice.ClockEventPage, error)
}

// ClockEventHandler serves the signed, tenant-scoped clock event feed.
type ClockEventHandler struct {
	feed EventFeed
}

// NewClockEventHandler constructs the event-feed HTTP adapter.
func NewClockEventHandler(feed EventFeed) http.Handler {
	return ClockEventHandler{feed: feed}
}

// ClockEventHTTPHandler is a named constructor for cell composition.
func ClockEventHTTPHandler(feed EventFeed) http.Handler {
	return NewClockEventHandler(feed)
}

// ServeHTTP implements the bounded GET /v1/events projection.
func (h ClockEventHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.feed == nil {
		writeClockEventError(w, http.StatusServiceUnavailable, clockservice.ErrClockEventsUnavailable)
		return
	}
	if r.URL.Path != ClockEventsPath {
		writeClockEventError(w, http.StatusNotFound, errors.New("event feed route not found"))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeClockEventError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if !clockEventBodyWithinLimit(w, r) {
		return
	}
	p, ok := trust.FromContext(r.Context())
	if !ok || p == nil {
		writeClockEventError(w, http.StatusUnauthorized, clockservice.ErrInvalidPrincipal)
		return
	}
	limit, err := clockEventPageSize(r.URL.Query().Get("page_size"))
	if err != nil {
		writeClockEventError(w, http.StatusBadRequest, err)
		return
	}
	if r.URL.Query().Get("fields") != "" || r.URL.Query().Get("field_mask") != "" {
		writeClockEventError(w, http.StatusBadRequest, errors.New("event field masks are managed by the subscription"))
		return
	}
	if r.URL.Query().Get("types") != "" || r.URL.Query().Get("start") == "now" {
		writeClockEventError(w, http.StatusBadRequest, errors.New("event filter is not supported by the clock feed"))
		return
	}
	page, err := h.feed.Pull(r.Context(), clockservice.ClockEventRequest{
		Tenant:       string(p.Tenant()),
		Organization: p.OrganizationScopeID(),
		Worker:       strings.TrimSpace(r.URL.Query().Get("worker")),
		Cursor:       r.URL.Query().Get("cursor"),
		Limit:        limit,
	})
	if err != nil {
		writeClockEventError(w, clockEventStatus(err), err)
		return
	}
	writeClockEventJSON(w, page)
}

func clockEventBodyWithinLimit(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxClockEventsBody)
	_, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		writeClockEventError(w, http.StatusRequestEntityTooLarge, errors.New("request body exceeds 1 MiB"))
		return false
	}
	return true
}

func clockEventPageSize(raw string) (int, error) {
	if raw == "" {
		return defaultClockEventsPageSize, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > maxClockEventsPageSize {
		return 0, errors.New("page_size must be between 1 and 1000")
	}
	return n, nil
}

func clockEventStatus(err error) int {
	switch {
	case errors.Is(err, clockservice.ErrClockCursorInvalid):
		return http.StatusBadRequest
	case errors.Is(err, clockservice.ErrClockScopeDenied):
		return http.StatusForbidden
	case errors.Is(err, clockservice.ErrClockEventsUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, clockservice.ErrClockEventInvalid):
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func writeClockEventJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func writeClockEventError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: clockEventCode(code), Message: err.Error()})
}

func clockEventCode(code int) string {
	switch code {
	case http.StatusBadRequest:
		return "invalid_argument"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "permission_denied"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusRequestEntityTooLarge:
		return "resource_exhausted"
	case http.StatusBadGateway:
		return "bad_gateway"
	case http.StatusServiceUnavailable:
		return "unavailable"
	default:
		return "internal"
	}
}
