package application

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/clienttelemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatClientTelemetryPath is where the browser client's sendBeacon posts its
// versioned, PII-free health record (#3798).
const ChatClientTelemetryPath = "/api/chat/telemetry"

const (
	// A real session sends one beacon per page hide and at most one on its
	// first error, so a handful per ten minutes is generous.
	chatClientTelemetryPerWindow = 6
	chatClientTelemetryWindow    = 10 * time.Minute
	chatClientTelemetryMaxKeys   = 20000
	// A person can open many tabs, but a client that mints a fresh session
	// nonce per request must not be able to flood the log or the table, so
	// every person is also capped across all of their sessions.
	chatClientTelemetryPerPersonWindow = 60
)

// chatClientTelemetryLogger is the structured logger the request log uses.
type chatClientTelemetryLogger interface {
	Info(msg string, args ...any)
}

type slogTelemetryLogger struct{}

func (slogTelemetryLogger) Info(msg string, args ...any) { slog.Info(msg, args...) }

// ChatClientTelemetryHandler ingests one beacon: authenticate, bound, validate
// against the allow-list, rate-limit by tenant-scoped session hash, then write
// one structured line beside the request log. It stores nothing.
type ChatClientTelemetryHandler struct {
	Limiter   *clienttelemetry.Limiter
	PerPerson *clienttelemetry.Limiter
	Logger    chatClientTelemetryLogger
}

type chatClientTelemetryError struct {
	Code string `json:"code"`
}

func chatClientTelemetryRefuse(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chatClientTelemetryError{Code: code})
}

func (h ChatClientTelemetryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != ChatClientTelemetryPath {
		http.NotFound(w, r)
		return
	}
	verified, ok := trust.FromContext(r.Context())
	if !ok {
		chatClientTelemetryRefuse(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		chatClientTelemetryRefuse(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	record, err := clienttelemetry.Decode(r.Body)
	switch {
	case errors.Is(err, clienttelemetry.ErrTooLarge):
		chatClientTelemetryRefuse(w, http.StatusRequestEntityTooLarge, "too_large")
		return
	case err != nil:
		chatClientTelemetryRefuse(w, http.StatusBadRequest, "invalid_argument")
		return
	}
	tenant := verified.Tenant().String()
	if h.PerPerson != nil && !h.PerPerson.Allow(clienttelemetry.SessionHash(tenant, "person:"+verified.Subject())) {
		chatClientTelemetryRefuse(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	session := clienttelemetry.SessionHash(tenant, record.Session)
	if h.Limiter != nil && !h.Limiter.Allow(session) {
		chatClientTelemetryRefuse(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var logger chatClientTelemetryLogger = slogTelemetryLogger{}
	if h.Logger != nil {
		logger = h.Logger
	}
	logger.Info("hcmnext.chat_client_telemetry", append([]any{"tenant", tenant}, record.Attrs(session)...)...)
	w.WriteHeader(http.StatusNoContent)
}

// OverlayChatClientTelemetry mounts the beacon endpoint. Admission runs before
// the handler, as for every other chat edge. logger may be nil, in which case
// the process logger is used.
func OverlayChatClientTelemetry(next http.Handler, admission transport.Config, logger chatClientTelemetryLogger) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	handler := ChatClientTelemetryHandler{
		Limiter:   clienttelemetry.NewLimiter(chatClientTelemetryPerWindow, chatClientTelemetryWindow, chatClientTelemetryMaxKeys, nil),
		PerPerson: clienttelemetry.NewLimiter(chatClientTelemetryPerPersonWindow, chatClientTelemetryWindow, chatClientTelemetryMaxKeys, nil),
		Logger:    logger,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatClientTelemetryPath {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			w.Header().Set("Cache-Control", "no-store")
			chatClientTelemetryRefuse(w, denied.HTTPStatus(), "request_denied")
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
