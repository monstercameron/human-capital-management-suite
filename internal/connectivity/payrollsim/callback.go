package payrollsim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
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
	EffectiveDate  string `json:"effective_date"`
	BasePay        Money  `json:"base_pay"`
	OccurredAt     string `json:"occurred_at"`
}

// process runs one accepted change to completion: wait the scenario's apply
// delay, settle the outcome, then deliver the signed result callback.
func (s *Server) process(ctx context.Context, ch *change, sc Scenario) {
	defer s.jobs.Done()
	defer ch.cancel()
	if err := s.sleep(ctx, sc.applyDelay()); err != nil {
		s.log.Info("processing cancelled", "change_ref", ch.req.ChangeRef)
		return
	}
	occurred := s.now().UTC()
	outcome, reason, eventType := StatusApplied, "", EventApplied
	if sc.Mode == ModeReject {
		outcome, reason, eventType = StatusRejected, sc.rejectReason(), EventRejected
	}
	s.mu.Lock()
	if ch.status != StatusAccepted {
		// A reversal cancelled this change while the apply timer was
		// running; the late apply loses and must not call back.
		status := ch.status
		s.mu.Unlock()
		s.log.Info("apply superseded", "change_ref", ch.req.ChangeRef, "status", status)
		return
	}
	ch.status = outcome
	ch.reason = reason
	if outcome == StatusApplied {
		ch.appliedAt = occurred
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
		EffectiveDate:  ch.req.EffectiveDate,
		BasePay:        ch.req.BasePay,
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
// non-2xx is permanent, because resending the same bytes to a receiver that
// judged them invalid cannot succeed.
func (s *Server) deliver(ctx context.Context, ch *change, d *delivery, eventID, eventType string, body []byte) bool {
	ref := ch.req.ChangeRef
	dependency := callbackDependency(ch.req.CallbackURL)
	var previousFailure edge.FailureSignal
	for attempt := 1; attempt <= s.maxAttempts; attempt++ {
		request := edge.OverloadRequest{
			TenantID: ch.req.Tenant, Dependency: dependency,
			LogicalOperationID: eventID, OperationKind: eventType,
			Attempt: attempt - 1, Failure: previousFailure,
			Signal: admission.BackpressureSignal{Source: "payrollsim-callback", Dependency: dependency, State: admission.BackpressureHealthy},
		}
		decision, decisionErr := s.overload.Decide(s.now(), request)
		if decisionErr != nil || !decision.PerformEffect {
			s.log.Warn("callback attempt refused by overload coordinator", "change_ref", ref, "event_id", eventID, "attempt", attempt, "reason", decision.Reason, "err", errText(decisionErr))
			return false
		}
		status, err := s.attempt(ctx, ch, d, eventID, eventType, body)
		ok := err == nil && status >= 200 && status < 300
		failure := callbackFailure(status, err)
		s.overload.Breaker.Report(ch.req.Tenant, dependency, s.now(), failure == "")
		s.mu.Lock()
		d.attempts++
		d.lastStatus = status
		if ok {
			d.delivered = true
		}
		previousFailure = failure
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

// callbackDependency gives breaker state a stable, bounded key without
// retaining callback paths or query parameters.
func callbackDependency(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "callback"
	}
	return u.Hostname()
}

// callbackFailure translates only retryable transport outcomes into EDGE-007
// signals. Permanent HTTP responses demonstrate reachability and do not trip
// the dependency breaker.
func callbackFailure(status int, err error) edge.FailureSignal {
	if err != nil {
		return edge.FailureTimeout
	}
	switch {
	case status == http.StatusTooManyRequests:
		return edge.FailureRateLimited
	case status == http.StatusRequestTimeout:
		return edge.FailureTimeout
	case status >= http.StatusInternalServerError:
		return edge.FailureUnavailable
	default:
		return ""
	}
}

// attempt performs one signed POST. The timestamp and signature are fresh per
// attempt so a long retry tail stays inside the receiver's replay window. It
// echoes the originating request's trace context as a child traceparent (a
// new span id per attempt, same trace) and its correlation id unchanged.
func (s *Server) attempt(ctx context.Context, ch *change, d *delivery, eventID, eventType string, body []byte) (int, error) {
	ts := s.now().UnixNano()
	s.mu.Lock()
	secret := s.secret
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
			Purpose: "payrollsim_callback_delivery", Principal: "payrollsim_callback",
			Tenant: ch.req.Tenant, Payload: body,
			DataClasses: []dlp.DataClass{dlp.ClassPII, dlp.ClassCompensation},
			Headers:     req.Header, NoRedirect: true,
		})
		if gatewayErr != nil {
			return 0, gatewayErr
		}
		resp = result.Response
	} else if s.client != nil {
		resp, err = s.client.Do(req)
	} else {
		return 0, errors.New("payrollsim: callback HTTP port is not configured")
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

func retryable(status int) bool {
	return status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || (status >= 300 && status < 400)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
