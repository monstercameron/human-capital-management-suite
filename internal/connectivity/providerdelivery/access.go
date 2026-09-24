package providerdelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/oauthcc"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// AccessConfig wires an AccessClient.
type AccessConfig struct {
	// BaseURL is the provider root; changes go to {BaseURL}/v1/access-changes.
	BaseURL string
	// Tokens supplies bearer tokens. It is required and may be shared by
	// every client of the same provider.
	Tokens *oauthcc.TokenSource
	// Client is the injected HTTP port used by protocol fixtures and adapters.
	// Configure Client or Gateway; a nil port never opens a direct client.
	Client Doer
	// Gateway routes the provider call through centralized DNS, TLS, proxy,
	// outbound trust, DLP and receipt enforcement. When set, Client must be nil
	// and Principal and Tenant are required.
	Gateway   *egress.Gateway
	Principal string
	Tenant    string
	Lease     *lease.CredentialLease
	// Timeout bounds one Deliver, Reverse or Status attempt, token fetch
	// and the single invalid_token retry included (default 15s).
	Timeout time.Duration
	// Now is the clock used for HTTP-date Retry-After (default time.Now).
	Now func() time.Time
	// Headers adds observability headers (traceparent, X-Correlation-Id)
	// to every request; nil adds none. It never overrides Authorization,
	// Idempotency-Key or any header the client sets itself. To trace the
	// token fetch too, build Tokens with oauthcc.Config.Client set to
	// NewHeaderDoer(client, Headers).
	Headers HeaderSource
}

// AccessClient delivers access changes to the IAM provider.
type AccessClient struct {
	endpoint string
	tokens   *oauthcc.TokenSource
	send     sender
}

// PurposeAccessChangeDelivery is the narrow policy purpose for IAM changes.
// It is distinct from payroll processing so outbound allowlists can scope it.
const PurposeAccessChangeDelivery Purpose = "access_change_delivery"

// NewAccessClient validates cfg and returns a client.
func NewAccessClient(cfg AccessConfig) (*AccessClient, error) {
	base, err := validateBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if cfg.Tokens == nil {
		return nil, errors.New("providerdelivery: access Tokens is required")
	}
	client, err := providerDoer(cfg.Gateway, cfg.Client, cfg.Principal, cfg.Tenant, string(PurposeAccessChangeDelivery), []dlp.DataClass{dlp.ClassPII}, cfg.Lease)
	if err != nil {
		return nil, err
	}
	send := newSender(client, cfg.Timeout, cfg.Now)
	send.headers = cfg.Headers
	return &AccessClient{
		endpoint: base + "/v1/access-changes",
		tokens:   cfg.Tokens,
		send:     send,
	}, nil
}

// String never renders a token.
func (c *AccessClient) String() string {
	if c == nil {
		return "providerdelivery.AccessClient(nil)"
	}
	return fmt.Sprintf("providerdelivery.AccessClient{Endpoint:%q}", c.endpoint)
}

// GoString never renders a token under %#v.
func (c *AccessClient) GoString() string { return c.String() }

// accessOutbox is the IAM access-change outbox payload.
type accessOutbox struct {
	Kind           string `json:"kind"`
	ChangeRef      string `json:"change_ref"`
	Tenant         string `json:"tenant"`
	WorkerRef      string `json:"worker_ref"`
	JobCode        string `json:"job_code"`
	Grade          string `json:"grade"`
	EffectiveDate  string `json:"effective_date"`
	CorrelationKey string `json:"correlation_key"`
}

// accessRequest is the POST /v1/access-changes body in wire order.
type accessRequest struct {
	ChangeRef      string `json:"change_ref"`
	Tenant         string `json:"tenant"`
	WorkerRef      string `json:"worker_ref"`
	JobCode        string `json:"job_code"`
	Grade          string `json:"grade"`
	EffectiveDate  string `json:"effective_date"`
	CorrelationKey string `json:"correlation_key"`
	CallbackURL    string `json:"callback_url"`
}

func buildAccessRequest(changeRef string, payload []byte, callbackURL string) ([]byte, error) {
	var in accessOutbox
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, errors.Join(ErrInvalidPayload, errors.New("payload is not an access-change JSON object"))
	}
	if err := requireFields(changeRef, in.ChangeRef,
		field{"tenant", in.Tenant}, field{"worker_ref", in.WorkerRef},
		field{"job_code", in.JobCode}, field{"grade", in.Grade},
		field{"effective_date", in.EffectiveDate}, field{"correlation_key", in.CorrelationKey}); err != nil {
		return nil, err
	}
	if err := validateCallbackURL(callbackURL); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(accessRequest{
		ChangeRef: in.ChangeRef, Tenant: in.Tenant, WorkerRef: in.WorkerRef, JobCode: in.JobCode, Grade: in.Grade,
		EffectiveDate: in.EffectiveDate, CorrelationKey: in.CorrelationKey, CallbackURL: callbackURL,
	}); err != nil {
		return nil, errors.Join(ErrInvalidPayload, errors.New("payload could not be encoded"))
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Deliver makes one attempt to hand the change to the IAM provider. If the
// provider rejects the bearer token as invalid_token, the token is
// invalidated and the request is retried exactly once with a fresh one. A
// non-nil error means the input was invalid and nothing was sent.
func (c *AccessClient) Deliver(ctx context.Context, changeRef string, payload []byte, callbackURL string) (Result, error) {
	body, err := buildAccessRequest(changeRef, payload, callbackURL)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.send.timeout)
	defer cancel()
	resp, failed := c.call(ctx, http.MethodPost, c.endpoint, changeRef, body)
	if failed != nil {
		return *failed, nil
	}
	return classify(resp, c.send.now()), nil
}

// Reverse makes one attempt to ask the IAM provider to revoke the access
// change changeRef (POST {base}/v1/access-changes/{changeRef}/revocation with
// Idempotency-Key "revocation:"+changeRef), with the same single
// invalid_token refresh as Deliver. A 202 (revocation pending) or 200 (replay, or already revoked) is
// Delivered; a 409 not_reversible is Delivered with
// [ClassSettledNotReversible]; every other status is classified as for
// Deliver. A non-nil error means changeRef or reason was invalid and nothing
// was sent.
func (c *AccessClient) Reverse(ctx context.Context, changeRef, reason string) (Result, error) {
	body, err := buildReversalRequest(changeRef, reason)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.send.timeout)
	defer cancel()
	resp, failed := c.call(ctx, http.MethodPost, changeURL(c.endpoint, changeRef)+"/revocation", RevocationIdempotencyPrefix+changeRef, body)
	if failed != nil {
		return *failed, nil
	}
	return classifyReverse(resp, c.send.now()), nil
}

// Status makes one attempt to read the IAM provider's current state of
// changeRef (GET {base}/v1/access-changes/{changeRef}), with the same single
// invalid_token refresh as Deliver. See [StatusResult]. A non-nil error means
// changeRef was invalid and nothing was sent.
func (c *AccessClient) Status(ctx context.Context, changeRef string) (StatusResult, error) {
	if err := validateChangeRef(changeRef); err != nil {
		return StatusResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.send.timeout)
	defer cancel()
	resp, failed := c.call(ctx, http.MethodGet, changeURL(c.endpoint, changeRef), "", nil)
	if failed != nil {
		return StatusResult{Result: *failed}, nil
	}
	return classifyStatus(resp, changeRef, accessStatuses, c.send.now()), nil
}

// call sends one authenticated request. If the provider rejects the bearer
// token as invalid_token, the token is invalidated and the request is sent
// exactly once more with a fresh one. A non-nil Result is a final answer
// (transport, token or refresh failure); otherwise the response is for the
// caller to classify.
func (c *AccessClient) call(ctx context.Context, method, endpoint, idempotencyKey string, body []byte) (response, *Result) {
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		r := tokenFailure(ctx, err)
		return response{}, &r
	}
	resp, failed := c.send.do(ctx, method, endpoint, c.header(idempotencyKey, tok, body != nil), body)
	if failed != nil {
		return response{}, failed
	}
	if resp.status == http.StatusUnauthorized && invalidToken(resp.header) {
		c.tokens.Invalidate(tok)
		if tok, err = c.tokens.Token(ctx); err != nil {
			r := tokenFailure(ctx, err)
			return response{}, &r
		}
		if resp, failed = c.send.do(ctx, method, endpoint, c.header(idempotencyKey, tok, body != nil), body); failed != nil {
			return response{}, failed
		}
		if resp.status == http.StatusUnauthorized {
			return response{}, &Result{Outcome: Retry, Class: ClassAuthRefreshFailed, Status: resp.status, Reason: "invalid_token_after_refresh"}
		}
	}
	return resp, nil
}

func (c *AccessClient) header(idempotencyKey, tok string, hasBody bool) http.Header {
	h := http.Header{}
	if hasBody {
		h.Set("Content-Type", "application/json")
	}
	h.Set("Accept", "application/json")
	if idempotencyKey != "" {
		h.Set("Idempotency-Key", idempotencyKey)
	}
	h.Set("Authorization", "Bearer "+tok)
	return h
}

// invalidToken reports an RFC 6750 invalid_token challenge.
func invalidToken(h http.Header) bool {
	for _, v := range h.Values("WWW-Authenticate") {
		if strings.Contains(v, "invalid_token") {
			return true
		}
	}
	return false
}

// tokenFailure classifies a token source error: a 400/401 from the token
// endpoint is a credential or configuration fault (Rejected); anything
// else is transient.
func tokenFailure(ctx context.Context, err error) Result {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return Result{Outcome: Retry, Class: ClassTransientTimeout, Reason: "token_timeout"}
	}
	var te *oauthcc.TokenError
	if errors.As(err, &te) {
		if te.Status == http.StatusBadRequest || te.Status == http.StatusUnauthorized {
			reason := "token_rejected"
			if code := sanitize(te.Code); code != "" {
				reason = "token_" + code
			}
			return Result{Outcome: Rejected, Class: ClassRejectedAuth, Status: te.Status, Reason: reason}
		}
		return Result{Outcome: Retry, Class: ClassAuthRefreshFailed, Status: te.Status, Reason: "token_unavailable"}
	}
	if ctx.Err() != nil {
		return Result{Outcome: Retry, Class: ClassTransientNetwork, Reason: "canceled"}
	}
	return Result{Outcome: Retry, Class: ClassAuthRefreshFailed, Reason: "token_unavailable"}
}
