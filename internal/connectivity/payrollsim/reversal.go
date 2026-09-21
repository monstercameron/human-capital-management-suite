package payrollsim

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

// ReversalKeyPrefix prefixes the change_ref to form the one Idempotency-Key a
// reversal request may carry, so a reversal can never be confused with (or
// replayed as) the original intake.
const ReversalKeyPrefix = "reversal:"

// MaxReversalReasonLen bounds the reversal reason, in characters.
const MaxReversalReasonLen = 500

// reversal is the bookkeeping of one accepted reversal. Every mutable field is
// guarded by Server.mu; reason and requestedAt are immutable.
type reversal struct {
	reason      string
	requestedAt time.Time
	// fromStatus is the change status the reversal found: ACCEPTED means the
	// pending apply was cancelled, APPLIED means the change is being undone.
	fromStatus  string
	cancel      context.CancelFunc
	completedAt time.Time
	eventID     string
	delivery
}

// ReversalStatus is the "reversal" object of GET /v1/pay-changes/{change_ref}.
type ReversalStatus struct {
	RequestedAt string         `json:"requested_at"`
	CompletedAt string         `json:"completed_at"`
	Reason      string         `json:"reason"`
	Callback    CallbackStatus `json:"callback"`
}

// status renders the reversal for the GET endpoint; the caller holds mu.
func (rv *reversal) status() *ReversalStatus {
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

// ReversalEvent is the reversal callback body: the result body plus the
// reason the caller gave for undoing the change.
type ReversalEvent struct {
	ResultEvent
	ReversalReason string `json:"reversal_reason"`
}

// reversalRequest is the reversal POST body.
type reversalRequest struct {
	Reason string `json:"reason"`
}

// reversalResponse is the 202 body and the 200 replay body.
type reversalResponse struct {
	ChangeRef string `json:"change_ref"`
	Status    string `json:"status"`
}

// handleReversal implements POST /v1/pay-changes/{change_ref}/reversal, the
// compensation step of the hand-off. Order mirrors intake: syntax, then
// idempotency (a replay must never start failing because the scenario was
// switched afterwards), then the change's own state, then the scenario.
func (s *Server) handleReversal(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("change_ref")
	wc := providerwire.FromHeader(r.Header)
	log := s.log.With(wireAttrs(wc)...)
	if r.Header.Get("Idempotency-Key") != ReversalKeyPrefix+ref {
		writeInvalid(w, "Idempotency-Key")
		return
	}
	reason, ok := readReversalReason(w, r)
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
			log.Warn("reversal idempotency key reused", "change_ref", ref)
			writeJSON(w, http.StatusConflict, map[string]string{"error": "idempotency_key_reused"})
			return
		}
		log.Info("reversal replay", "change_ref", ref, "status", status)
		writeJSON(w, http.StatusOK, reversalResponse{ChangeRef: ref, Status: status})
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
		log.Info("reversal transient failure injected", "change_ref", ref)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	case ReversalRefuse:
		s.mu.Unlock()
		refusal := sc.rejectReason()
		log.Info("reversal refused by scenario", "change_ref", ref, "reason", refusal)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "rejected", "reason": refusal})
		return
	}
	from := ch.status
	if from == StatusAccepted {
		// Cancel the pending apply. Even if its timer has already fired, the
		// apply re-checks the status under mu and finds REVERSAL_PENDING.
		ch.cancel()
	}
	ctx, cancel := context.WithCancel(s.baseCtx)
	rv := &reversal{reason: reason, requestedAt: s.now(), fromStatus: from, cancel: cancel}
	rv.echo = mergeEcho(wc, ch.echo)
	ch.rev = rv
	ch.status = StatusReversalPending
	s.jobs.Add(1)
	s.mu.Unlock()

	log.Info("reversal accepted", "change_ref", ref, "from_status", from)
	go s.reverse(ctx, ch, rv, sc)
	writeJSON(w, http.StatusAccepted, reversalResponse{ChangeRef: ref, Status: StatusReversalPending})
}

// readReversalReason reads and validates the bounded reversal body, writing
// the error response itself when it returns false.
func readReversalReason(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too_large"})
			return "", false
		}
		writeInvalid(w, "body")
		return "", false
	}
	var req reversalRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeInvalid(w, "body")
		return "", false
	}
	if strings.TrimSpace(req.Reason) == "" || utf8.RuneCountInString(req.Reason) > MaxReversalReasonLen {
		writeInvalid(w, "reason")
		return "", false
	}
	return req.Reason, true
}

// reverse completes one accepted reversal. A change that was already APPLIED
// takes the scenario's apply delay to undo; one whose apply was cancelled is
// settled at once. Either way the result is REVERSED plus a reversal callback
// with its own event id.
func (s *Server) reverse(ctx context.Context, ch *change, rv *reversal, sc Scenario) {
	defer s.jobs.Done()
	defer rv.cancel()
	if rv.fromStatus == StatusApplied {
		if err := s.sleep(ctx, sc.applyDelay()); err != nil {
			s.log.Info("reversal cancelled", "change_ref", ch.req.ChangeRef)
			return
		}
	}
	occurred := s.now().UTC()
	s.mu.Lock()
	ch.status = StatusReversed
	rv.completedAt = occurred
	rv.eventID = "evt_" + s.newID()
	eventID, reason := rv.eventID, ch.reason
	s.mu.Unlock()

	body, err := json.Marshal(ReversalEvent{
		ResultEvent: ResultEvent{
			EventID:        eventID,
			ChangeRef:      ch.req.ChangeRef,
			CorrelationKey: ch.req.CorrelationKey,
			ProviderRef:    ch.providerRef,
			Outcome:        StatusReversed,
			Reason:         reason,
			EffectiveDate:  ch.req.EffectiveDate,
			BasePay:        ch.req.BasePay,
			OccurredAt:     occurred.Format(time.RFC3339),
		},
		ReversalReason: rv.reason,
	})
	if err != nil {
		s.log.Error("reversal callback encode failed", "change_ref", ch.req.ChangeRef, "err", err)
		return
	}
	s.log.Info("change reversed", "change_ref", ch.req.ChangeRef, "event_id", eventID)
	s.send(ctx, ch, &rv.delivery, sc, eventID, EventReversed, body)
}
