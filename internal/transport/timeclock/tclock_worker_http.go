package timeclock

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"google.golang.org/grpc/status"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// WorkerSelfService is the optional human-principal clock boundary. It is
// separate from ClockDeviceService because browser workers have no device
// identity or punch token.
type WorkerSelfService interface {
	GetSelfClock(context.Context, *trust.Principal) (clockservice.SelfClockStatus, error)
	ExecuteSelfClockAction(context.Context, *trust.Principal, clockservice.SelfClockActionRequest) (clockservice.SelfClockActionResult, error)
}

// WorkerSelfHTTPHandler serves the authenticated worker's own clock status
// and published-workflow IN/OUT actions.
func WorkerSelfHTTPHandler(app WorkerSelfService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		validRoute := (r.Method == http.MethodGet && r.URL.Path == "/v1/time/self") || (r.Method == http.MethodPost && (r.URL.Path == "/v1/time/self/in" || r.URL.Path == "/v1/time/self/out"))
		if !validRoute {
			writeAPIError(w, http.StatusNotFound, serviceError(clockservice.ErrInvalidRequest))
			return
		}
		p, err := principal(r.Context())
		if err != nil {
			writeAPIError(w, httpStatus(serviceError(err)), serviceError(err))
			return
		}
		if app == nil {
			writeAPIError(w, http.StatusServiceUnavailable, serviceError(clockservice.ErrUnavailable))
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/time/self":
			status, callErr := app.GetSelfClock(r.Context(), p)
			if callErr != nil {
				writeSelfAPIError(w, callErr)
				return
			}
			writeJSON(w, http.StatusOK, status)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/time/self/"):
			action := strings.TrimPrefix(r.URL.Path, "/v1/time/self/")
			if action != "in" && action != "out" {
				writeAPIError(w, http.StatusNotFound, serviceError(clockservice.ErrInvalidRequest))
				return
			}
			var req struct {
				ExpectedRevision uint64 `json:"expected_revision"`
				IdempotencyKey   string `json:"idempotency_key"`
			}
			body, readErr := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
			decoder := json.NewDecoder(strings.NewReader(string(body)))
			decoder.DisallowUnknownFields()
			decodeErr := decoder.Decode(&req)
			var trailing any
			trailingErr := decoder.Decode(&trailing)
			if readErr != nil || len(body) > 1<<20 || decodeErr != nil || !errors.Is(trailingErr, io.EOF) {
				writeAPIError(w, http.StatusBadRequest, serviceError(clockservice.ErrInvalidRequest))
				return
			}
			result, callErr := app.ExecuteSelfClockAction(r.Context(), p, clockservice.SelfClockActionRequest{Action: strings.ToUpper(action), ExpectedRevision: req.ExpectedRevision, IdempotencyKey: req.IdempotencyKey})
			if callErr != nil {
				writeSelfAPIError(w, callErr)
				return
			}
			writeJSON(w, http.StatusOK, result)
		default:
			writeAPIError(w, http.StatusNotFound, serviceError(clockservice.ErrInvalidRequest))
		}
	})
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

// writeSelfAPIError writes a self-clock failure. An ineligibility decision
// carries its closed reason so the browser can tell the worker why, and never
// the internal detail; every other failure keeps the shared error shape, so an
// outage is not mistaken for a decision about the worker.
func writeSelfAPIError(w http.ResponseWriter, err error) {
	svc := serviceError(err)
	reason, ok := clockservice.ReasonOf(err)
	if !ok {
		writeAPIError(w, httpStatus(svc), svc)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus(svc))
	_ = json.NewEncoder(w).Encode(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}{Code: grpcCodeName(status.Code(svc)), Message: "clock not available: " + string(reason), Reason: string(reason)})
}
