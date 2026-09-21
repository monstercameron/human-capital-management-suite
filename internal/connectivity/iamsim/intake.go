package iamsim

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry/providerwire"
)

// AccessChangeRequest is the POST /v1/access-changes body. It is a
// comparable value so replay detection can compare meaning with ==.
type AccessChangeRequest struct {
	ChangeRef      string `json:"change_ref"`
	Tenant         string `json:"tenant"`
	WorkerRef      string `json:"worker_ref"`
	JobCode        string `json:"job_code"`
	Grade          string `json:"grade"`
	EffectiveDate  string `json:"effective_date"`
	CorrelationKey string `json:"correlation_key"`
	CallbackURL    string `json:"callback_url"`
}

// acceptResponse is both the 202 body and, byte for byte, the 200 replay body.
type acceptResponse struct {
	ChangeRef   string `json:"change_ref"`
	ProviderRef string `json:"provider_ref"`
	Status      string `json:"status"`
}

// codePattern bounds job codes and grades to the vendor's identifier
// alphabet, so an HCM-side mapping bug (a display name instead of a code)
// fails at intake rather than being granted.
var codePattern = regexp.MustCompile(`^[A-Z0-9-]{1,32}$`)

// validate checks req and returns the offending JSON field name, or "".
func (req AccessChangeRequest) validate() string {
	required := []struct{ field, value string }{
		{"change_ref", req.ChangeRef},
		{"tenant", req.Tenant},
		{"worker_ref", req.WorkerRef},
	}
	for _, f := range required {
		if strings.TrimSpace(f.value) == "" {
			return f.field
		}
	}
	if !codePattern.MatchString(req.JobCode) {
		return "job_code"
	}
	if !codePattern.MatchString(req.Grade) {
		return "grade"
	}
	if _, err := time.Parse(time.DateOnly, req.EffectiveDate); err != nil || len(req.EffectiveDate) != len(time.DateOnly) {
		return "effective_date"
	}
	if strings.TrimSpace(req.CorrelationKey) == "" {
		return "correlation_key"
	}
	u, err := url.Parse(req.CallbackURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "callback_url"
	}
	return ""
}

// handleIntake implements POST /v1/access-changes; requireBearer has already
// authenticated the caller. Order matters: syntax is judged before
// idempotency (a malformed replay is still malformed), and idempotency before
// the scenario (a replay of an accepted change must never start failing
// because the scenario was switched afterwards).
func (s *Server) handleIntake(w http.ResponseWriter, r *http.Request) {
	wc := providerwire.FromHeader(r.Header)
	log := s.log.With(wireAttrs(wc)...)
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > MaxIdempotencyKeyLen {
		writeInvalid(w, "Idempotency-Key")
		return
	}
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
	var req AccessChangeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeInvalid(w, "body")
		return
	}
	if field := req.validate(); field != "" {
		writeInvalid(w, field)
		return
	}
	if req.ChangeRef != key {
		writeInvalid(w, "change_ref")
		return
	}

	s.mu.Lock()
	if existing, ok := s.changes[key]; ok {
		same := existing.req == req
		body := existing.acceptBody
		s.mu.Unlock()
		if !same {
			log.Warn("intake idempotency key reused", "change_ref", key)
			writeJSON(w, http.StatusConflict, map[string]string{"error": "idempotency_key_reused"})
			return
		}
		log.Info("intake replay", "change_ref", key)
		writeRaw(w, http.StatusOK, body)
		return
	}
	if s.closing {
		s.mu.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	}
	sc := s.scenario
	switch sc.Mode {
	case ModeRejectAtIntake:
		s.mu.Unlock()
		reason := sc.rejectReason()
		log.Info("intake rejected by scenario", "change_ref", key, "reason", reason)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "rejected", "reason": reason})
		return
	case ModeFlaky:
		if s.random() < sc.FlakyRate {
			s.mu.Unlock()
			log.Info("intake transient failure injected", "change_ref", key)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
			return
		}
	}
	providerRef := "IAMSIM-" + shortID(s.newID())
	body, err := json.Marshal(acceptResponse{ChangeRef: key, ProviderRef: providerRef, Status: StatusAccepted})
	if err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	ctx, cancel := context.WithCancel(s.baseCtx)
	ch := &change{req: req, providerRef: providerRef, acceptBody: body, cancel: cancel, status: StatusAccepted}
	ch.echo = wc
	s.changes[key] = ch
	s.jobs.Add(1)
	s.mu.Unlock()

	log.Info("intake accepted", "change_ref", key, "provider_ref", providerRef, "tenant", req.Tenant, "mode", string(sc.Mode))
	go s.process(ctx, ch, sc)
	writeRaw(w, http.StatusAccepted, body)
}

func writeInvalid(w http.ResponseWriter, field string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": field})
}

// shortID compresses a minted id to 12 uppercase characters, enough to stay
// unique across a test run while reading like a vendor reference.
func shortID(id string) string {
	id = strings.ToUpper(strings.ReplaceAll(id, "-", ""))
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}
