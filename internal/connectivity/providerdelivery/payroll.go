package providerdelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// APIKeyHeader carries the payroll provider API key.
const APIKeyHeader = "X-Api-Key"

// PayrollConfig wires a PayrollClient.
type PayrollConfig struct {
	// BaseURL is the provider root; changes go to {BaseURL}/v1/pay-changes.
	BaseURL string
	// APIKey authenticates every request. It is required.
	APIKey string
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
	// Timeout bounds one Deliver, Reverse or Status attempt (default 15s).
	Timeout time.Duration
	// Now is the clock used for HTTP-date Retry-After (default time.Now).
	Now func() time.Time
	// Headers adds observability headers (traceparent, X-Correlation-Id)
	// to every request; nil adds none. It never overrides X-Api-Key,
	// Idempotency-Key or any header the client sets itself.
	Headers HeaderSource
}

// String redacts the API key.
func (c PayrollConfig) String() string {
	return fmt.Sprintf("providerdelivery.PayrollConfig{BaseURL:%q, APIKey:%s}", c.BaseURL, redactIfSet(c.APIKey))
}

// GoString redacts the API key under %#v.
func (c PayrollConfig) GoString() string { return c.String() }

func redactIfSet(s string) string {
	if s == "" {
		return `""`
	}
	return "[REDACTED]"
}

// PayrollClient delivers pay changes to the payroll provider.
type PayrollClient struct {
	endpoint string
	apiKey   string
	send     sender
}

// NewPayrollClient validates cfg and returns a client.
func NewPayrollClient(cfg PayrollConfig) (*PayrollClient, error) {
	base, err := validateBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if cfg.APIKey == "" {
		return nil, errors.New("providerdelivery: payroll APIKey is required")
	}
	client, err := providerDoer(cfg.Gateway, cfg.Client, cfg.Principal, cfg.Tenant, string(PurposePayrollDelivery), []dlp.DataClass{dlp.ClassPII, dlp.ClassCompensation}, cfg.Lease)
	if err != nil {
		return nil, err
	}
	send := newSender(client, cfg.Timeout, cfg.Now)
	send.headers = cfg.Headers
	return &PayrollClient{
		endpoint: base + "/v1/pay-changes",
		apiKey:   cfg.APIKey,
		send:     send,
	}, nil
}

// PurposePayrollDelivery is the established workforce authorization purpose
// used to deliver payroll records.
const PurposePayrollDelivery Purpose = authz.PurposePayrollProcessing

// String redacts the API key.
func (c *PayrollClient) String() string {
	if c == nil {
		return "providerdelivery.PayrollClient(nil)"
	}
	return fmt.Sprintf("providerdelivery.PayrollClient{Endpoint:%q, APIKey:[REDACTED]}", c.endpoint)
}

// GoString redacts under %#v.
func (c *PayrollClient) GoString() string { return c.String() }

// money is the outbox and wire shape of base pay. Amount is carried as the
// literal decimal text so no float ever touches pay.
type money struct {
	Amount   decimalText `json:"amount"`
	Currency string      `json:"currency"`
}

// decimalText accepts a JSON string or number and keeps its exact text.
type decimalText string

func (d *decimalText) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*d = decimalText(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return errors.New("amount must be a string or number")
	}
	*d = decimalText(n.String())
	return nil
}

// payrollOutbox is the pay-change outbox payload.
type payrollOutbox struct {
	Kind           string `json:"kind"`
	ChangeRef      string `json:"change_ref"`
	Tenant         string `json:"tenant"`
	WorkerRef      string `json:"worker_ref"`
	BasePay        money  `json:"base_pay"`
	PayFrequency   string `json:"pay_frequency"`
	EffectiveDate  string `json:"effective_date"`
	CorrelationKey string `json:"correlation_key"`
}

// payrollRequest is the POST /v1/pay-changes body. Field order is the wire
// order.
type payrollRequest struct {
	ChangeRef      string `json:"change_ref"`
	Tenant         string `json:"tenant"`
	WorkerRef      string `json:"worker_ref"`
	BasePay        money  `json:"base_pay"`
	EffectiveDate  string `json:"effective_date"`
	CorrelationKey string `json:"correlation_key"`
	CallbackURL    string `json:"callback_url"`
}

// buildPayrollRequest maps the outbox payload to the provider body: kind
// and pay_frequency are dropped, callback_url is added.
func buildPayrollRequest(changeRef string, payload []byte, callbackURL string) ([]byte, error) {
	var in payrollOutbox
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, errors.Join(ErrInvalidPayload, errors.New("payload is not a pay-change JSON object"))
	}
	if err := requireFields(changeRef, in.ChangeRef,
		field{"tenant", in.Tenant}, field{"worker_ref", in.WorkerRef},
		field{"base_pay.amount", string(in.BasePay.Amount)}, field{"base_pay.currency", in.BasePay.Currency},
		field{"effective_date", in.EffectiveDate}, field{"correlation_key", in.CorrelationKey}); err != nil {
		return nil, err
	}
	if err := validateCallbackURL(callbackURL); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payrollRequest{
		ChangeRef: in.ChangeRef, Tenant: in.Tenant, WorkerRef: in.WorkerRef, BasePay: in.BasePay,
		EffectiveDate: in.EffectiveDate, CorrelationKey: in.CorrelationKey, CallbackURL: callbackURL,
	}); err != nil {
		return nil, errors.Join(ErrInvalidPayload, errors.New("payload could not be encoded"))
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Deliver makes one attempt to hand the change to the payroll provider.
// changeRef is the outbox change reference and must equal the payload's
// change_ref; it is sent as the Idempotency-Key. A non-nil error means the
// input was invalid and nothing was sent.
func (c *PayrollClient) Deliver(ctx context.Context, changeRef string, payload []byte, callbackURL string) (Result, error) {
	body, err := buildPayrollRequest(changeRef, payload, callbackURL)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.send.timeout)
	defer cancel()
	h := c.header()
	h.Set("Content-Type", "application/json")
	h.Set("Idempotency-Key", changeRef)
	resp, failed := c.send.do(ctx, http.MethodPost, c.endpoint, h, body)
	if failed != nil {
		return *failed, nil
	}
	if r, ok := invalidAPIKey(resp); ok {
		return r, nil
	}
	return classify(resp, c.send.now()), nil
}

// invalidAPIKey maps a 401: a bad API key is configuration, not a transient
// fault.
func invalidAPIKey(resp response) (Result, bool) {
	if resp.status != http.StatusUnauthorized {
		return Result{}, false
	}
	return Result{Outcome: Rejected, Class: ClassRejectedAuth, Status: resp.status, Reason: "invalid_api_key"}, true
}

// Reverse makes one attempt to ask the payroll provider to undo the change
// changeRef (POST {base}/v1/pay-changes/{changeRef}/reversal with
// Idempotency-Key "reversal:"+changeRef). A 202 (reversal pending) or 200
// (replay, or already reversed) is Delivered; a 409 not_reversible is
// Delivered with [ClassSettledNotReversible]; every other status is
// classified as for Deliver. A non-nil error means changeRef or reason was
// invalid and nothing was sent.
func (c *PayrollClient) Reverse(ctx context.Context, changeRef, reason string) (Result, error) {
	body, err := buildReversalRequest(changeRef, reason)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.send.timeout)
	defer cancel()
	h := c.header()
	h.Set("Content-Type", "application/json")
	h.Set("Idempotency-Key", ReversalIdempotencyPrefix+changeRef)
	resp, failed := c.send.do(ctx, http.MethodPost, changeURL(c.endpoint, changeRef)+"/reversal", h, body)
	if failed != nil {
		return *failed, nil
	}
	if r, ok := invalidAPIKey(resp); ok {
		return r, nil
	}
	return classifyReverse(resp, c.send.now()), nil
}

// Status makes one attempt to read the provider's current state of
// changeRef (GET {base}/v1/pay-changes/{changeRef}). See [StatusResult] for
// how the answer is reported. A non-nil error means changeRef was invalid and
// nothing was sent.
func (c *PayrollClient) Status(ctx context.Context, changeRef string) (StatusResult, error) {
	if err := validateChangeRef(changeRef); err != nil {
		return StatusResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.send.timeout)
	defer cancel()
	resp, failed := c.send.do(ctx, http.MethodGet, changeURL(c.endpoint, changeRef), c.header(), nil)
	if failed != nil {
		return StatusResult{Result: *failed}, nil
	}
	if r, ok := invalidAPIKey(resp); ok {
		return StatusResult{Result: r}, nil
	}
	return classifyStatus(resp, changeRef, payrollStatuses, c.send.now()), nil
}

func (c *PayrollClient) header() http.Header {
	h := http.Header{}
	h.Set("Accept", "application/json")
	h.Set(APIKeyHeader, c.apiKey)
	return h
}
