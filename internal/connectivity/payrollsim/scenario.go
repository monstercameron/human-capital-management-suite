package payrollsim

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// Mode selects how the simulator treats new pay changes.
type Mode string

const (
	// ModeApply accepts and later applies every change.
	ModeApply Mode = "apply"
	// ModeReject accepts every change and later reports it rejected.
	ModeReject Mode = "reject"
	// ModeRejectAtIntake refuses changes synchronously with 422.
	ModeRejectAtIntake Mode = "reject_at_intake"
	// ModeFlaky fails intake with 503 at FlakyRate, otherwise behaves like
	// ModeApply, to prove the caller retries with the same idempotency key.
	ModeFlaky Mode = "flaky"
)

const defaultRejectReason = "simulated rejection"

// ReversalMode selects how the simulator answers a reversal request.
type ReversalMode string

const (
	// ReversalSucceed (the default; the empty value means the same) accepts
	// reversals of ACCEPTED and APPLIED changes.
	ReversalSucceed ReversalMode = "succeed"
	// ReversalFailTransient answers every new reversal with 503, to prove the
	// caller retries compensation with the same idempotency key.
	ReversalFailTransient ReversalMode = "fail_transient"
	// ReversalRefuse answers every new reversal with 422, the provider
	// declining to undo a change the caller must then escalate.
	ReversalRefuse ReversalMode = "refuse"
)

// Scenario is the control-plane behaviour. It is captured per change at
// intake, so switching scenarios never rewrites the fate of a change already
// accepted.
type Scenario struct {
	Mode               Mode    `json:"mode"`
	ApplyDelayMS       int64   `json:"apply_delay_ms"`
	RejectReason       string  `json:"reject_reason"`
	FlakyRate          float64 `json:"flaky_rate"`
	DuplicateCallbacks bool    `json:"duplicate_callbacks"`
	// ReversalMode governs POST /v1/pay-changes/{change_ref}/reversal; empty
	// means ReversalSucceed.
	ReversalMode ReversalMode `json:"reversal_mode,omitempty"`
	// DropCallbacks processes changes and reversals but never sends their
	// callbacks, so the caller's status-polling fallback is the only way to
	// learn the outcome.
	DropCallbacks bool `json:"drop_callbacks"`
	// CallbackDelayMS is an extra wait before the first attempt of every
	// callback, to exercise early/late arrival ordering on the receiver.
	CallbackDelayMS int64 `json:"callback_delay_ms"`
}

// DefaultScenario applies every change after 1.5s.
func DefaultScenario() Scenario {
	return Scenario{Mode: ModeApply, ApplyDelayMS: defaultApplyDelay.Milliseconds()}
}

func (sc Scenario) applyDelay() time.Duration {
	return time.Duration(sc.ApplyDelayMS) * time.Millisecond
}

func (sc Scenario) callbackDelay() time.Duration {
	return time.Duration(sc.CallbackDelayMS) * time.Millisecond
}

func (sc Scenario) rejectReason() string {
	if sc.RejectReason == "" {
		return defaultRejectReason
	}
	return sc.RejectReason
}

// scenarioPatch is the PUT body. Pointer fields make omitted fields keep
// their current value, so a demo can flip just the mode without silently
// zeroing the delay.
type scenarioPatch struct {
	Mode               *Mode         `json:"mode"`
	ApplyDelayMS       *int64        `json:"apply_delay_ms"`
	RejectReason       *string       `json:"reject_reason"`
	FlakyRate          *float64      `json:"flaky_rate"`
	DuplicateCallbacks *bool         `json:"duplicate_callbacks"`
	ReversalMode       *ReversalMode `json:"reversal_mode"`
	DropCallbacks      *bool         `json:"drop_callbacks"`
	CallbackDelayMS    *int64        `json:"callback_delay_ms"`
}

// apply merges p into sc, reporting the first invalid field.
func (p scenarioPatch) apply(sc Scenario) (Scenario, string) {
	if p.Mode != nil {
		switch *p.Mode {
		case ModeApply, ModeReject, ModeRejectAtIntake, ModeFlaky:
			sc.Mode = *p.Mode
		default:
			return sc, "mode"
		}
	}
	if p.ApplyDelayMS != nil {
		if *p.ApplyDelayMS < 0 {
			return sc, "apply_delay_ms"
		}
		sc.ApplyDelayMS = *p.ApplyDelayMS
	}
	if p.RejectReason != nil {
		sc.RejectReason = *p.RejectReason
	}
	if p.FlakyRate != nil {
		if *p.FlakyRate < 0 || *p.FlakyRate > 1 {
			return sc, "flaky_rate"
		}
		sc.FlakyRate = *p.FlakyRate
	}
	if p.DuplicateCallbacks != nil {
		sc.DuplicateCallbacks = *p.DuplicateCallbacks
	}
	if p.ReversalMode != nil {
		switch *p.ReversalMode {
		case "", ReversalSucceed, ReversalFailTransient, ReversalRefuse:
			sc.ReversalMode = *p.ReversalMode
		default:
			return sc, "reversal_mode"
		}
	}
	if p.DropCallbacks != nil {
		sc.DropCallbacks = *p.DropCallbacks
	}
	if p.CallbackDelayMS != nil {
		if *p.CallbackDelayMS < 0 {
			return sc, "callback_delay_ms"
		}
		sc.CallbackDelayMS = *p.CallbackDelayMS
	}
	return sc, ""
}

// requireAPIKey authenticates the pay-change endpoints. It runs before the
// handler reads the body, so an unauthenticated request can never reserve an
// idempotency key or learn whether a change exists.
func (s *Server) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		keys := s.apiKeys
		s.mu.Unlock()
		if len(keys) > 0 && !matchAny(keys, r.Header.Get(APIKeyHeader)) {
			s.log.Warn("request with invalid api key", "method", r.Method, "path", r.URL.Path)
			w.Header().Set("WWW-Authenticate", `ApiKey realm="payrollsim"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_api_key"})
			return
		}
		next(w, r)
	}
}

// requireControl enforces the control token. Comparison is constant-time
// because the token is a shared secret even in a simulator.
func (s *Server) requireControl(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.controlToken != "" {
			got := r.Header.Get(ControlTokenHeader)
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.controlToken)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) handleGetScenario(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Scenario())
}

func (s *Server) handlePutScenario(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too_large"})
			return
		}
		writeInvalid(w, "body")
		return
	}
	var p scenarioPatch
	if err := json.Unmarshal(raw, &p); err != nil {
		writeInvalid(w, "body")
		return
	}
	s.mu.Lock()
	next, field := p.apply(s.scenario)
	if field == "" {
		s.scenario = next
	}
	s.mu.Unlock()
	if field != "" {
		writeInvalid(w, field)
		return
	}
	s.log.Info("scenario updated", "mode", string(next.Mode), "apply_delay_ms", next.ApplyDelayMS, "flaky_rate", next.FlakyRate, "duplicate_callbacks", next.DuplicateCallbacks)
	writeJSON(w, http.StatusOK, next)
}

// handleReset forgets every change and cancels their pending processing and
// callbacks, so a test starts from a provider that has never heard of it.
func (s *Server) handleReset(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	cleared := len(s.changes)
	for _, ch := range s.changes {
		ch.cancel()
		if ch.rev != nil {
			ch.rev.cancel()
		}
	}
	s.changes = make(map[string]*change)
	s.mu.Unlock()
	s.log.Info("changes reset", "cleared", cleared)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "cleared": cleared})
}

// Scenario returns the current scenario.
func (s *Server) Scenario() Scenario {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scenario
}
