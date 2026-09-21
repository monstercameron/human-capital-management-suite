package iamsim

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry/providerwire"
)

// RevocationKeyPrefix prefixes the change_ref to form the one
// Idempotency-Key a revocation request may carry, so a revocation can never
// be confused with (or replayed as) the original intake.
const RevocationKeyPrefix = "revocation:"

// MaxReversalReasonLen bounds the revocation reason, in characters.
const MaxReversalReasonLen = 500

// revocation is the bookkeeping of one accepted revocation. Every mutable
// field is guarded by Server.mu; reason and requestedAt are immutable.
type revocation struct {
	reason      string
	requestedAt time.Time
	// fromStatus is the change status the revocation found: ACCEPTED means
	// the pending grant was cancelled, GRANTED means access is being pulled.
	fromStatus  string
	cancel      context.CancelFunc
	completedAt time.Time
	eventID     string
	delivery
}

// ReversalStatus is the "reversal" object of GET
// /v1/access-changes/{change_ref}.
type ReversalStatus struct {
	RequestedAt string         `json:"requested_at"`
	CompletedAt string         `json:"completed_at"`
	Reason      string         `json:"reason"`
	Callback    CallbackStatus `json:"callback"`
}

// status renders the revocation for the GET endpoint; the caller holds mu.
func (rv *revocation) status() *ReversalStatus {
	out := &ReversalStatus{
		RequestedAt: rv.requestedAt.UTC().Format(time.RFC3339),
		Reason:      rv.reason,
		Callback:    CallbackStatus{Attempts: rv.attempts, Delivered: rv.delivered, LastStatus: rv.lastStatus},
	}
	if !rv.completedAt.IsZero() {
		out.CompletedAt = rv.completedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// RevocationEvent is the revocation callback body: the result body plus the
// reason the caller gave for withdrawing access.
type RevocationEvent struct {
	ResultEvent
	ReversalReason string `json:"reversal_reason"`
}

// revocationResponse is the 202 body and the 200 replay body.
type revocationResponse struct {
	ChangeRef string `json:"change_ref"`
	Status    string `json:"status"`
}

// handleRevocation implements POST
// /v1/access-changes/{change_ref}/revocation, the compensation step of the
// hand-off; requireBearer has already checked access.write. Order mirrors
// intake: syntax, then idempotency (a replay must never start failing because
// the scenario was switched afterwards), then the change's state, then the
// scenario.
func (s *Server) handleRevocation(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("change_ref")
	wc := providerwire.FromHeader(r.Header)
	log := s.log.With(wireAttrs(wc)...)
	if r.Header.Get("Idempotency-Key") != RevocationKeyPrefix+ref {
		writeInvalid(w, "Idempotency-Key")
		return
	}
	reason, ok := readRevocationReason(w, r)
	if !ok {
		return
	}

	s.mu.Lock()
	ch, found := s.changes[ref]
	if !found {
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	if ch.rev != nil {
		same := ch.rev.reason == reason
		status := ch.status
		s.mu.Unlock()
		if !same {
			log.Warn("revocation idempotency key reused", "change_ref", ref)
			writeJSON(w, http.StatusConflict, map[string]string{"error": "idempotency_key_reused"})
			return
		}
		log.Info("revocation replay", "change_ref", ref, "status", status)
		writeJSON(w, http.StatusOK, revocationResponse{ChangeRef: ref, Status: status})
		return
	}
	if ch.status == StatusRejected {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_reversible", "status": StatusRejected})
		return
	}
	if s.closing {
		s.mu.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	}
	sc := s.scenario
	switch sc.ReversalMode {
	case ReversalFailTransient:
		s.mu.Unlock()
		log.Info("revocation transient failure injected", "change_ref", ref)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	case ReversalRefuse:
		s.mu.Unlock()
		refusal := sc.rejectReason()
		log.Info("revocation refused by scenario", "change_ref", ref, "reason", refusal)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "rejected", "reason": refusal})
		return
	}
	from := ch.status
	if from == StatusAccepted {
		// Cancel the pending grant. Even if its timer has already fired, the
		// grant re-checks the status under mu and finds REVERSAL_PENDING.
		ch.cancel()
	}
	ctx, cancel := context.WithCancel(s.baseCtx)
	rv := &revocation{reason: reason, requestedAt: s.now(), fromStatus: from, cancel: cancel}
	rv.echo = mergeEcho(wc, ch.echo)
	ch.rev = rv
	ch.status = StatusReversalPending
	s.jobs.Add(1)
	s.mu.Unlock()

	log.Info("revocation accepted", "change_ref", ref, "from_status", from)
	go s.revoke(ctx, ch, rv, sc)
	writeJSON(w, http.StatusAccepted, revocationResponse{ChangeRef: ref, Status: StatusReversalPending})
}

// readRevocationReason reads and validates the bounded revocation body,
// writing the error response itself when it returns false.
func readRevocationReason(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Reason string `json:"reason"`
	}
	if !readControlJSON(w, r, &body) {
		return "", false
	}
	if strings.TrimSpace(body.Reason) == "" || utf8.RuneCountInString(body.Reason) > MaxReversalReasonLen {
		writeInvalid(w, "reason")
		return "", false
	}
	return body.Reason, true
}

// revoke completes one accepted revocation. Access already GRANTED takes the
// scenario's grant delay to withdraw; a change whose grant was cancelled is
// settled at once. Either way the result is REVOKED plus a revocation
// callback with its own event id.
func (s *Server) revoke(ctx context.Context, ch *change, rv *revocation, sc Scenario) {
	defer s.jobs.Done()
	defer rv.cancel()
	if rv.fromStatus == StatusGranted {
		if err := s.sleep(ctx, sc.grantDelay()); err != nil {
			s.log.Info("revocation cancelled", "change_ref", ch.req.ChangeRef)
			return
		}
	}
	occurred := s.now().UTC()
	s.mu.Lock()
	ch.status = StatusRevoked
	rv.completedAt = occurred
	rv.eventID = "evt_" + s.newID()
	eventID, reason := rv.eventID, ch.reason
	s.mu.Unlock()

	body, err := json.Marshal(RevocationEvent{
		ResultEvent: ResultEvent{
			EventID:        eventID,
			ChangeRef:      ch.req.ChangeRef,
			CorrelationKey: ch.req.CorrelationKey,
			ProviderRef:    ch.providerRef,
			Outcome:        StatusRevoked,
			Reason:         reason,
			JobCode:        ch.req.JobCode,
			Grade:          ch.req.Grade,
			EffectiveDate:  ch.req.EffectiveDate,
			OccurredAt:     occurred.Format(time.RFC3339),
		},
		ReversalReason: rv.reason,
	})
	if err != nil {
		s.log.Error("revocation callback encode failed", "change_ref", ch.req.ChangeRef, "err", err)
		return
	}
	s.log.Info("access revoked", "change_ref", ch.req.ChangeRef, "event_id", eventID)
	s.send(ctx, ch, &rv.delivery, sc, eventID, EventRevoked, body)
}

// readControlJSON decodes a bounded JSON body into v, writing the error
// response itself when it returns false.
func readControlJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too_large"})
			return false
		}
		writeInvalid(w, "body")
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		writeInvalid(w, "body")
		return false
	}
	return true
}
