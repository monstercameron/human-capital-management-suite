package iamsim

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// Mode selects how the simulator treats new access changes.
type Mode string

const (
	// ModeGrant accepts and later grants every change.
	ModeGrant Mode = "grant"
	// ModeReject accepts every change and later reports it rejected.
	ModeReject Mode = "reject"
	// ModeRejectAtIntake refuses changes synchronously with 422.
	ModeRejectAtIntake Mode = "reject_at_intake"
	// ModeFlaky fails intake with 503 at FlakyRate, otherwise behaves like
	// ModeGrant, to prove the caller retries with the same idempotency key.
	ModeFlaky Mode = "flaky"
)

const defaultRejectReason = "simulated rejection"

// ReversalMode selects how the simulator answers a revocation request.
type ReversalMode string

const (
	// ReversalSucceed (the default; the empty value means the same) accepts
	// revocations of ACCEPTED and GRANTED changes.
	ReversalSucceed ReversalMode = "succeed"
	// ReversalFailTransient answers every new revocation with 503, to prove
	// the caller retries compensation with the same idempotency key.
	ReversalFailTransient ReversalMode = "fail_transient"
	// ReversalRefuse answers every new revocation with 422, the provider
	// declining to withdraw access the caller must then escalate.
	ReversalRefuse ReversalMode = "refuse"
)

// Scenario is the control-plane behaviour. The access-change part is
// captured per change at intake, so switching scenarios never rewrites the
// fate of a change already accepted; the token part applies to tokens issued
// after the switch.
type Scenario struct {
	Mode               Mode    `json:"mode"`
	GrantDelayMS       int64   `json:"grant_delay_ms"`
	RejectReason       string  `json:"reject_reason"`
	FlakyRate          float64 `json:"flaky_rate"`
	DuplicateCallbacks bool    `json:"duplicate_callbacks"`
	// TokenTTLSeconds is the lifetime of newly issued access tokens. A short
	// value lets a test watch the client refresh on expiry.
	TokenTTLSeconds int64 `json:"token_ttl_seconds"`
	// TokenEndpointFlakyRate fails POST /oauth2/token with 503 at this rate,
	// to prove the client retries stage one without spinning.
	TokenEndpointFlakyRate float64 `json:"token_endpoint_flaky_rate"`
	// ReversalMode governs POST /v1/access-changes/{change_ref}/revocation;
	// empty means ReversalSucceed.
	ReversalMode ReversalMode `json:"reversal_mode,omitempty"`
	// DropCallbacks processes changes and revocations but never sends their
	// callbacks, so the caller's status-polling fallback is the only way to
	// learn the outcome.
	DropCallbacks bool `json:"drop_callbacks"`
	// CallbackDelayMS is an extra wait before the first attempt of every
	// callback, to exercise early/late arrival ordering on the receiver.
	CallbackDelayMS int64 `json:"callback_delay_ms"`
}

// DefaultScenario grants every change after 1.5s and issues 5-minute tokens.
func DefaultScenario() Scenario {
	return Scenario{
		Mode:            ModeGrant,
		GrantDelayMS:    defaultGrantDelay.Milliseconds(),
		TokenTTLSeconds: int64(defaultTokenTTL / time.Second),
	}
}

func (sc Scenario) grantDelay() time.Duration {
	return time.Duration(sc.GrantDelayMS) * time.Millisecond
}

// tokenTTL falls back to the default for a non-positive value, because a
// zero-lifetime token would make every stage-two call fail and look like a
// client bug rather than a scenario choice.
func (sc Scenario) tokenTTL() time.Duration {
	if sc.TokenTTLSeconds <= 0 {
		return defaultTokenTTL
	}
	return time.Duration(sc.TokenTTLSeconds) * time.Second
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
// zeroing the delay or the token lifetime.
type scenarioPatch struct {
	Mode                   *Mode         `json:"mode"`
	GrantDelayMS           *int64        `json:"grant_delay_ms"`
	RejectReason           *string       `json:"reject_reason"`
	FlakyRate              *float64      `json:"flaky_rate"`
	DuplicateCallbacks     *bool         `json:"duplicate_callbacks"`
	TokenTTLSeconds        *int64        `json:"token_ttl_seconds"`
	TokenEndpointFlakyRate *float64      `json:"token_endpoint_flaky_rate"`
	ReversalMode           *ReversalMode `json:"reversal_mode"`
	DropCallbacks          *bool         `json:"drop_callbacks"`
	CallbackDelayMS        *int64        `json:"callback_delay_ms"`
}

// apply merges p into sc, reporting the first invalid field.
func (p scenarioPatch) apply(sc Scenario) (Scenario, string) {
	if p.Mode != nil {
		switch *p.Mode {
		case ModeGrant, ModeReject, ModeRejectAtIntake, ModeFlaky:
			sc.Mode = *p.Mode
		default:
			return sc, "mode"
		}
	}
	if p.GrantDelayMS != nil {
		if *p.GrantDelayMS < 0 {
			return sc, "grant_delay_ms"
		}
		sc.GrantDelayMS = *p.GrantDelayMS
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
	if p.TokenTTLSeconds != nil {
		if *p.TokenTTLSeconds <= 0 {
			return sc, "token_ttl_seconds"
		}
		sc.TokenTTLSeconds = *p.TokenTTLSeconds
	}
	if p.TokenEndpointFlakyRate != nil {
		if *p.TokenEndpointFlakyRate < 0 || *p.TokenEndpointFlakyRate > 1 {
			return sc, "token_endpoint_flaky_rate"
		}
		sc.TokenEndpointFlakyRate = *p.TokenEndpointFlakyRate
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

// requireControl enforces the control token. Control endpoints are the test
// harness's plane, not the provider API, so they take this token rather than
// an OAuth bearer. Comparison is constant-time because the token is a shared
// secret even in a simulator.
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
	s.log.Info("scenario updated", "mode", string(next.Mode), "grant_delay_ms", next.GrantDelayMS, "flaky_rate", next.FlakyRate,
		"duplicate_callbacks", next.DuplicateCallbacks, "token_ttl_seconds", next.TokenTTLSeconds, "token_endpoint_flaky_rate", next.TokenEndpointFlakyRate)
	writeJSON(w, http.StatusOK, next)
}

// handleRevokeTokens invalidates every outstanding token, the lever a test
// pulls to prove the client refreshes on 401 invalid_token.
func (s *Server) handleRevokeTokens(w http.ResponseWriter, _ *http.Request) {
	n := s.RevokeAllTokens()
	s.log.Info("tokens revoked", "revoked", n)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "revoked": n})
}

// handleReset forgets every change and cancels their pending processing and
// callbacks, so a test starts from a provider that has never heard of it.
// Tokens and the scenario are left alone: tokens have their own revoke lever,
// and a harness that set a scenario expects it to survive a data reset.
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
