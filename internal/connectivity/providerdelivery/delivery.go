// Package providerdelivery holds the outbound clients that hand outbox
// changes to third-party providers: a payroll provider authenticated with an
// API key and an IAM provider authenticated with OAuth 2.0 client
// credentials.
//
// Each Deliver, Reverse or Status is exactly one attempt (plus, for IAM, one
// retry after a rejected bearer token). Retrying and backoff belong to the
// queue; the Result tells it what to do: Outcome says whether to retry,
// Class gives a stable failure class for metrics and alerting, and
// RetryAfter carries the provider's Retry-After hint.
//
// Deliver, Reverse and Status return a non-nil error only when the input is
// invalid, in which case no request was sent. Every provider or network
// outcome is a Result. API keys, tokens and payload values never appear in
// errors or Results.
package providerdelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// Outcome is what the queue should do with a change after one attempt.
type Outcome int

// Purpose is one policy purpose used on outbound provider calls.
type Purpose string

const (
	// Delivered: the provider accepted the change (202) or confirmed an
	// earlier acceptance (200 replay).
	Delivered Outcome = iota + 1
	// Retry: the attempt failed transiently; try again later.
	Retry
	// Rejected: retrying the same request cannot succeed.
	Rejected
)

func (o Outcome) String() string {
	switch o {
	case Delivered:
		return "delivered"
	case Retry:
		return "retry"
	case Rejected:
		return "rejected"
	default:
		return "unknown"
	}
}

// Stable Result.Class values.
const (
	ClassDelivered               = "delivered"
	ClassTransientNetwork        = "transient_network"
	ClassTransientTimeout        = "transient_timeout"
	ClassTransientStatus         = "transient_status"
	ClassAuthRefreshFailed       = "auth_refresh_failed"
	ClassRejectedRequest         = "rejected_request"
	ClassRejectedConflict        = "rejected_conflict"
	ClassRejectedBusiness        = "rejected_business"
	ClassRejectedAuth            = "rejected_auth"
	maxReasonLen                 = 200
	defaultTimeout               = 15 * time.Second
	maxRetryAfter                = time.Hour
	maxResponseBytes       int64 = 64 << 10
)

// ClassSettledNotReversible is Reverse's answer to a provider 409
// not_reversible: the change never took effect (the provider rejected it),
// so there is nothing to undo. Its Outcome is Delivered because the queue
// must stop retrying the reversal: the desired end state (the change is not
// in effect) already holds.
const ClassSettledNotReversible = "settled_not_reversible"

// Result is the outcome of one Deliver or Reverse attempt (and, inside
// StatusResult, of one Status call).
type Result struct {
	Outcome Outcome
	// Class is one of the Class* constants.
	Class string
	// Status is the provider HTTP status, or 0 when none was received.
	Status int
	// Reason is a short, sanitised machine reason (a provider reason code,
	// or a local one such as "timeout").
	Reason string
	// ProviderRef is the provider's reference for a delivered change.
	ProviderRef string
	// RetryAfter is the provider's Retry-After hint on 429/503, clamped to
	// [0, 1h]; zero when absent or malformed.
	RetryAfter time.Duration
}

var (
	// ErrInvalidPayload reports an outbox payload that cannot be mapped to
	// a provider request. No request was sent.
	ErrInvalidPayload = errors.New("providerdelivery: invalid payload")
	// ErrInvalidCallbackURL reports a callback URL that is not an absolute
	// http(s) URL. No request was sent.
	ErrInvalidCallbackURL = errors.New("providerdelivery: invalid callback URL")
)

// Doer is the HTTP surface the clients need.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// providerDoer selects the enforcing gateway adapter when configured. The
// injectable Doer remains available for deterministic provider protocol
// fixtures; production composition supplies Gateway and workload identity.
func providerDoer(gateway *egress.Gateway, client Doer, principal, tenant, purpose string, classes []dlp.DataClass, credentialLease *lease.CredentialLease) (Doer, error) {
	if gateway == nil {
		if client == nil {
			return nil, errors.New("providerdelivery: Gateway or Client HTTP port is required")
		}
		return client, nil
	}
	if client != nil {
		return nil, errors.New("providerdelivery: configure either Gateway or Client, not both")
	}
	return egress.NewHTTPDoer(gateway, purpose, principal, tenant, classes, credentialLease)
}

func noFollow(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// sender is the transport shared by both clients.
type sender struct {
	client  Doer
	timeout time.Duration
	now     func() time.Time
	// headers adds observability headers to every request (nil: none).
	headers HeaderSource
}

// newSender applies timing defaults. Redirects are never followed: an
// injected *http.Client is copied with CheckRedirect replaced, so a 3xx
// reaches classify instead of re-sending payloads to another location.
func newSender(client Doer, timeout time.Duration, now func() time.Time) sender {
	switch c := client.(type) {
	case *http.Client:
		cp := *c
		cp.CheckRedirect = noFollow
		client = &cp
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if now == nil {
		now = time.Now
	}
	return sender{client: client, timeout: timeout, now: now}
}

type response struct {
	status int
	header http.Header
	body   []byte
}

// do sends one request (body nil for a GET). On a transport failure it
// returns a Retry Result instead of a response.
func (s sender) do(ctx context.Context, method, endpoint string, header http.Header, body []byte) (response, *Result) {
	if s.client == nil {
		return response{}, &Result{Outcome: Retry, Class: ClassTransientNetwork, Reason: "http_port_not_configured"}
	}
	var rd io.Reader = http.NoBody
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return response{}, &Result{Outcome: Retry, Class: ClassTransientNetwork, Reason: "request_build_failed"}
	}
	req.Header = header
	applyHeaderSource(ctx, req.Header, s.headers)
	resp, err := s.client.Do(req)
	if err != nil {
		return response{}, networkFailure(ctx)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil && ctx.Err() != nil {
		return response{}, networkFailure(ctx)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

// networkFailure classifies a transport error by the attempt context, never
// by the error text (which may echo request details).
func networkFailure(ctx context.Context) *Result {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Result{Outcome: Retry, Class: ClassTransientTimeout, Reason: "timeout"}
	}
	if ctx.Err() != nil {
		return &Result{Outcome: Retry, Class: ClassTransientNetwork, Reason: "canceled"}
	}
	return &Result{Outcome: Retry, Class: ClassTransientNetwork, Reason: "network_error"}
}

type providerBody struct {
	ProviderRef string `json:"provider_ref"`
	Reason      string `json:"reason"`
	Error       string `json:"error"`
	Field       string `json:"field"`
}

// classify maps a provider response to a Result. Authentication statuses
// are handled by each client before this is reached.
func classify(r response, now time.Time) Result {
	var pb providerBody
	_ = json.Unmarshal(r.body, &pb)
	res := Result{Status: r.status}
	switch st := r.status; {
	case st == http.StatusOK || st == http.StatusAccepted:
		res.Outcome, res.Class, res.ProviderRef = Delivered, ClassDelivered, sanitize(pb.ProviderRef)
	case st >= 300 && st < 400:
		res.Outcome, res.Class, res.Reason = Retry, ClassTransientStatus, "redirect_not_followed"
	case st < 300:
		res.Outcome, res.Class, res.Reason = Retry, ClassTransientStatus, "unexpected_status"
	case st == http.StatusRequestTimeout || st == http.StatusTooManyRequests || st >= 500:
		res.Outcome, res.Class = Retry, ClassTransientStatus
		res.Reason = providerReason(pb, "status_"+strconv.Itoa(st))
		if st == http.StatusTooManyRequests || st == http.StatusServiceUnavailable {
			res.RetryAfter = parseRetryAfter(r.header.Get("Retry-After"), now)
		}
	case st == http.StatusConflict:
		res.Outcome, res.Class, res.Reason = Rejected, ClassRejectedConflict, providerReason(pb, "conflict")
	case st == http.StatusUnprocessableEntity:
		res.Outcome, res.Class, res.Reason = Rejected, ClassRejectedBusiness, providerReason(pb, "rejected")
	case st == http.StatusUnauthorized || st == http.StatusForbidden:
		res.Outcome, res.Class, res.Reason = Rejected, ClassRejectedAuth, providerReason(pb, "unauthorized")
	default: // 400, 413 and every other 4xx: the request itself is wrong.
		res.Outcome, res.Class, res.Reason = Rejected, ClassRejectedRequest, providerReason(pb, "status_"+strconv.Itoa(st))
	}
	return res
}

// providerReason prefers the provider's reason, then its error code
// (qualified by the offending field when given), then fallback.
func providerReason(pb providerBody, fallback string) string {
	switch {
	case sanitize(pb.Reason) != "":
		return sanitize(pb.Reason)
	case sanitize(pb.Error) != "" && sanitize(pb.Field) != "":
		return sanitize(pb.Error) + ":" + sanitize(pb.Field)
	case sanitize(pb.Error) != "":
		return sanitize(pb.Error)
	default:
		return fallback
	}
}

// sanitize keeps printable ASCII and bounds the length, so a provider
// cannot inject control characters or unbounded text into logs.
func sanitize(s string) string {
	var b strings.Builder
	for i := 0; i < len(s) && b.Len() < maxReasonLen; i++ {
		if c := s[i]; c >= 0x20 && c <= 0x7e {
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String())
}

// parseRetryAfter reads RFC 9110 Retry-After: delta-seconds or an
// HTTP-date. The result is clamped to [0, 1h]; malformed values yield 0.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if strings.Trim(v, "0123456789") == "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n > int64(maxRetryAfter/time.Second) {
			return maxRetryAfter // all digits: only overflow can fail
		}
		d = time.Duration(n) * time.Second
	} else {
		t, err := http.ParseTime(v)
		if err != nil {
			return 0
		}
		d = t.Sub(now)
	}
	return min(max(d, 0), maxRetryAfter)
}

// validateBaseURL requires an absolute http(s) URL with no userinfo, query
// or fragment, and returns it without a trailing slash.
func validateBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("providerdelivery: BaseURL must be an absolute http(s) URL without userinfo, query or fragment")
	}
	return strings.TrimRight(raw, "/"), nil
}

func validateCallbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ErrInvalidCallbackURL
	}
	return nil
}

// Idempotency-Key prefixes of the undo requests, so an undo never collides
// with the Deliver of the same change ref.
const (
	ReversalIdempotencyPrefix   = "reversal:"
	RevocationIdempotencyPrefix = "revocation:"
)

// StatusResult is the outcome of one Status call.
//
// Result classifies the HTTP call itself, not the change: Outcome Delivered
// means the provider gave an authoritative answer (200 with a recognised
// status, so Known is true; or 404, so Known is false and Reason is the
// provider's not-found reason); Retry means ask again later (transport
// failure, 5xx/429, a 200 whose body is unusable); Rejected means the poll
// itself cannot succeed (auth, a malformed request).
type StatusResult struct {
	// Known is true only when the provider returned the change's state.
	Known bool
	// Status is the provider's state of the change (ACCEPTED, APPLIED,
	// GRANTED, REJECTED, REVERSED, REVOKED or a *_PENDING undo state);
	// empty unless Known.
	Status string
	// ProviderRef and Reason are the provider's reference and its
	// sanitised reason for the state; empty unless Known.
	ProviderRef string
	Reason      string
	Result      Result
}

// Recognised change states per provider. The *_PENDING states are an undo
// accepted but not yet completed.
var (
	payrollStatuses = map[string]bool{"ACCEPTED": true, "APPLIED": true, "REJECTED": true, "REVERSED": true, "REVERSAL_PENDING": true}
	accessStatuses  = map[string]bool{"ACCEPTED": true, "GRANTED": true, "REJECTED": true, "REVOKED": true, "REVOCATION_PENDING": true, "REVERSAL_PENDING": true}
)

type statusBody struct {
	ChangeRef   string `json:"change_ref"`
	ProviderRef string `json:"provider_ref"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
}

// classifyStatus maps a Status response. Authentication statuses are
// handled by each client before this is reached.
func classifyStatus(r response, changeRef string, known map[string]bool, now time.Time) StatusResult {
	switch r.status {
	case http.StatusOK:
		var sb statusBody
		res := Result{Outcome: Retry, Class: ClassTransientStatus, Status: r.status}
		switch err := json.Unmarshal(r.body, &sb); {
		case err != nil:
			res.Reason = "malformed_status_body"
		case sb.ChangeRef != "" && sb.ChangeRef != changeRef:
			res.Reason = "status_change_ref_mismatch"
		case !known[sb.Status]:
			res.Reason = "unrecognised_status"
		default:
			ref := sanitize(sb.ProviderRef)
			res = Result{Outcome: Delivered, Class: ClassDelivered, Status: r.status, ProviderRef: ref}
			return StatusResult{Known: true, Status: sb.Status, ProviderRef: ref, Reason: sanitize(sb.Reason), Result: res}
		}
		return StatusResult{Result: res}
	case http.StatusNotFound:
		var pb providerBody
		_ = json.Unmarshal(r.body, &pb)
		return StatusResult{Result: Result{Outcome: Delivered, Class: ClassDelivered, Status: r.status, Reason: providerReason(pb, "not_found")}}
	}
	if r.status >= 200 && r.status < 300 {
		// A 202/204 is not an answer to a read.
		return StatusResult{Result: Result{Outcome: Retry, Class: ClassTransientStatus, Status: r.status, Reason: "unexpected_status"}}
	}
	return StatusResult{Result: classify(r, now)}
}

// classifyReverse maps a Reverse response: 409 not_reversible means the
// change never took effect, so the undo is settled; everything else is
// classified as for Deliver.
func classifyReverse(r response, now time.Time) Result {
	if r.status == http.StatusConflict {
		var pb providerBody
		if json.Unmarshal(r.body, &pb) == nil && pb.Error == "not_reversible" {
			return Result{Outcome: Delivered, Class: ClassSettledNotReversible, Status: r.status, Reason: "not_reversible"}
		}
	}
	return classify(r, now)
}

// validateChangeRef checks the change ref argument of Reverse and Status. It
// becomes a path segment, so "." and ".." are refused as well as blanks.
func validateChangeRef(changeRef string) error {
	switch strings.TrimSpace(changeRef) {
	case "":
		return errors.Join(ErrInvalidPayload, errors.New("change ref argument is empty"))
	case ".", "..":
		return errors.Join(ErrInvalidPayload, errors.New("change ref argument is not a valid path segment"))
	}
	return nil
}

// changeURL is the escaped resource URL of one change.
func changeURL(collection, changeRef string) string {
	return collection + "/" + url.PathEscape(changeRef)
}

// buildReversalRequest is the undo body shared by payroll reversal and IAM
// revocation.
func buildReversalRequest(changeRef, reason string) ([]byte, error) {
	if err := validateChangeRef(changeRef); err != nil {
		return nil, err
	}
	if strings.TrimSpace(reason) == "" {
		return nil, errors.Join(ErrInvalidPayload, errors.New("reason is required"))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(struct {
		Reason string `json:"reason"`
	}{reason}); err != nil {
		return nil, errors.Join(ErrInvalidPayload, errors.New("reason could not be encoded"))
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// field is one required outbox value checked before sending.
type field struct{ name, value string }

// requireFields checks the shared change_ref contract and every required
// field. Errors name fields only, never values.
func requireFields(changeRef, payloadRef string, fields ...field) error {
	if strings.TrimSpace(changeRef) == "" {
		return errors.Join(ErrInvalidPayload, errors.New("change ref argument is empty"))
	}
	if strings.TrimSpace(payloadRef) == "" {
		return errors.Join(ErrInvalidPayload, errors.New("payload change_ref is missing"))
	}
	if payloadRef != changeRef {
		return errors.Join(ErrInvalidPayload, errors.New("payload change_ref does not match the change ref argument"))
	}
	for _, f := range fields {
		if strings.TrimSpace(f.value) == "" {
			return errors.Join(ErrInvalidPayload, errors.New("payload "+f.name+" is missing"))
		}
	}
	return nil
}
