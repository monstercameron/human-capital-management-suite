package iamsim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// ResultEvent is the callback body; its marshalled bytes are exactly what is
// signed, and they are identical across every retry and duplicate of one
// event so the receiver's dedupe sees one event.
type ResultEvent struct {
	EventID        string `json:"event_id"`
	ChangeRef      string `json:"change_ref"`
	CorrelationKey string `json:"correlation_key"`
	ProviderRef    string `json:"provider_ref"`
	Outcome        string `json:"outcome"`
	Reason         string `json:"reason"`
	JobCode        string `json:"job_code"`
	Grade          string `json:"grade"`
	EffectiveDate  string `json:"effective_date"`
	OccurredAt     string `json:"occurred_at"`
}

// process runs one accepted change to completion: wait the scenario's grant
// delay, settle the outcome, then deliver the signed result callback.
func (s *Server) process(ctx context.Context, ch *change, sc Scenario) {
	defer s.jobs.Done()
	defer ch.cancel()
	if err := s.sleep(ctx, sc.grantDelay()); err != nil {
		s.log.Info("processing cancelled", "change_ref", ch.req.ChangeRef)
		return
	}
	occurred := s.now().UTC()
	outcome, reason, eventType := StatusGranted, "", EventGranted
	if sc.Mode == ModeReject {
		outcome, reason, eventType = StatusRejected, sc.rejectReason(), EventRejected
	}
	s.mu.Lock()
	if ch.status != StatusAccepted {
		// A revocation cancelled this change while the grant timer was
		// running; the late grant loses and must not call back.
		status := ch.status
		s.mu.Unlock()
		s.log.Info("grant superseded", "change_ref", ch.req.ChangeRef, "status", status)
		return
	}
	ch.status = outcome
	ch.reason = reason
	if outcome == StatusGranted {
		ch.grantedAt = occurred
	}
	eventID := "evt_" + s.newID()
	s.mu.Unlock()

	body, err := json.Marshal(ResultEvent{
		EventID:        eventID,
		ChangeRef:      ch.req.ChangeRef,
		CorrelationKey: ch.req.CorrelationKey,
		ProviderRef:    ch.providerRef,
		Outcome:        outcome,
		Reason:         reason,
		JobCode:        ch.req.JobCode,
		Grade:          ch.req.Grade,
		EffectiveDate:  ch.req.EffectiveDate,
		OccurredAt:     occurred.Format(time.RFC3339),
	})
	if err != nil {
		s.log.Error("callback encode failed", "change_ref", ch.req.ChangeRef, "err", err)
		return
	}
	s.log.Info("change processed", "change_ref", ch.req.ChangeRef, "outcome", outcome, "event_id", eventID)
	s.send(ctx, ch, &ch.delivery, sc, eventID, eventType, body)
}

// send delivers one event under the scenario captured for it: nothing at all
// when callbacks are dropped, otherwise after the extra callback delay, once
// or (with duplicates on) twice.
func (s *Server) send(ctx context.Context, ch *change, d *delivery, sc Scenario, eventID, eventType string, body []byte) {
	if sc.DropCallbacks {
		s.log.Info("callback dropped by scenario", "change_ref", ch.req.ChangeRef, "event_id", eventID)
		return
	}
	if delay := sc.callbackDelay(); delay > 0 {
		if s.sleep(ctx, delay) != nil {
			s.log.Info("callback cancelled", "change_ref", ch.req.ChangeRef, "event_id", eventID)
			return
		}
	}
	deliveries := 1
	if sc.DuplicateCallbacks {
		deliveries = 2
	}
	for range deliveries {
		if !s.deliver(ctx, ch, d, eventID, eventType, body) {
			return
		}
	}
}

// deliver sends one event with retries and reports whether it was
// acknowledged. Network errors, 5xx, 408 and 429 are transient; any other
// 4xx is permanent, because resending the same bytes to a receiver that
// judged them invalid cannot succeed.
func (s *Server) deliver(ctx context.Context, ch *change, d *delivery, eventID, eventType string, body []byte) bool {
	ref := ch.req.ChangeRef
	for attempt := 1; attempt <= s.maxAttempts; attempt++ {
		status, err := s.attempt(ctx, ch, d, eventID, eventType, body)
		ok := err == nil && status >= 200 && status < 300
		s.mu.Lock()
		d.attempts++
		d.lastStatus = status
		if ok {
			d.delivered = true
		}
		s.mu.Unlock()
		if ok {
			s.log.Info("callback delivered", "change_ref", ref, "event_id", eventID, "attempt", attempt, "status", status)
			return true
		}
		if ctx.Err() != nil {
			s.log.Info("callback cancelled", "change_ref", ref, "event_id", eventID, "attempt", attempt)
			return false
		}
		if err == nil && !retryable(status) {
			s.log.Warn("callback rejected permanently", "change_ref", ref, "event_id", eventID, "attempt", attempt, "status", status)
			return false
		}
		if attempt == s.maxAttempts {
			break
		}
		wait := s.backoff(attempt)
		s.log.Warn("callback failed, retrying", "change_ref", ref, "event_id", eventID, "attempt", attempt, "status", status, "err", errText(err), "backoff", wait)
		if s.sleep(ctx, wait) != nil {
			s.log.Info("callback cancelled", "change_ref", ref, "event_id", eventID, "attempt", attempt)
			return false
		}
	}
	s.log.Error("callback gave up", "change_ref", ref, "event_id", eventID, "attempts", s.maxAttempts)
	return false
}

// attempt performs one signed POST. The timestamp and signature are fresh per
// attempt so a long retry tail stays inside the receiver's replay window. It
// echoes the originating request's trace context as a child traceparent (a
// new span id per attempt, same trace) and its correlation id unchanged.
func (s *Server) attempt(ctx context.Context, ch *change, d *delivery, eventID, eventType string, body []byte) (int, error) {
	ts := s.now().UnixNano()
	s.mu.Lock()
	secret := s.webhookSecret
	s.mu.Unlock()
	sig := webhook.Sign(secret, webhook.Request{
		EventID:   eventID,
		EventType: eventType,
		Schema:    Schema,
		Timestamp: time.Unix(0, ts),
		Payload:   body,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.req.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Webhook-Id", eventID)
	req.Header.Set("Webhook-Event", eventType)
	req.Header.Set("Webhook-Schema", Schema)
	req.Header.Set("Webhook-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("Webhook-Tenant", ch.req.Tenant)
	req.Header.Set("Webhook-Signature", sig)
	d.echo.ApplyEcho(req.Header)
	var resp *http.Response
	if s.gateway != nil {
		result, gatewayErr := s.gateway.Do(ctx, egress.Request{
			Method: http.MethodPost, Target: ch.req.CallbackURL,
			Purpose: "iamsim_callback_delivery", Principal: "iamsim_callback",
			Tenant: ch.req.Tenant, Payload: body, DataClasses: []dlp.DataClass{dlp.ClassPII},
			Headers: req.Header, NoRedirect: true,
		})
		if gatewayErr != nil {
			return 0, gatewayErr
		}
		resp = result.Response
	} else if s.client != nil {
		resp, err = s.client.Do(req)
	} else {
		return 0, errors.New("iamsim: callback HTTP port is not configured")
	}
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxRequestBytes))
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// backoff is the wait after failed attempt n (1-based): base doubled n-1
// times, capped at the configured maximum.
func (s *Server) backoff(attempt int) time.Duration {
	d := s.backoffBase
	for i := 1; i < attempt; i++ {
		if d >= s.backoffMax/2 {
			return s.backoffMax
		}
		d *= 2
	}
	return min(d, s.backoffMax)
}

// retryable reports whether a non-2xx status is worth another attempt. Only
// 4xx other than 408 and 429 is final; 3xx is retried because the client does
// not follow redirects and a moved receiver may come back.
func retryable(status int) bool {
	if status >= 400 && status < 500 {
		return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests
	}
	return true
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
