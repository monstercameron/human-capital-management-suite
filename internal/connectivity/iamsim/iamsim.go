// Package iamsim is a standalone simulator of a third-party identity and
// access provider (think Okta or Entra) used to integration-test the
// promotion workflow's access hand-off end to end.
//
// Semantic owner: connectivity.
//
// Its defining trait is the two-stage authentication real IAM vendors use:
// the client first exchanges its credentials for a short-lived opaque access
// token (OAuth 2.0 client credentials, RFC 6749 section 4.4) and only then
// calls the API with that bearer token (RFC 6750). Tokens expire and can be
// revoked on demand, so the HCM side's token caching and refresh-on-401 logic
// is exercised against real wire behaviour rather than a stub that never says
// no. Accepted access changes are granted or rejected later and reported
// through a signed callback that is retried until acknowledged, using the
// same signing scheme as the payroll simulator so one receiver verifies both.
//
// Everything a test needs to control is injectable (clock, token randomness,
// scenario randomness, id minting, the callback HTTP client and the backoff
// sleep), and all mutable state lives on the Server value, never at package
// level, so several simulators can run side by side in one test binary.
package iamsim

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	mrand "math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry/providerwire"
)

// Contract constants. They are the wire contract the HCM side is built
// against, so they are exported for tests on both sides to reference rather
// than retype.
const (
	// EventGranted, EventRejected and EventRevoked are the Webhook-Event
	// values.
	EventGranted  = "access.change.granted"
	EventRejected = "access.change.rejected"
	EventRevoked  = "access.change.revoked"
	// Schema is the Webhook-Schema value the receiver allowlists.
	Schema = "iamsim.access_result/v1"

	StatusAccepted = "ACCEPTED"
	StatusGranted  = "GRANTED"
	StatusRejected = "REJECTED"
	// StatusReversalPending is reported between an accepted revocation
	// request and its completion; StatusRevoked once access is withdrawn.
	StatusReversalPending = "REVERSAL_PENDING"
	StatusRevoked         = "REVOKED"

	// ScopeWrite allows submitting access changes; ScopeRead allows reading
	// their status.
	ScopeWrite = "access.write"
	ScopeRead  = "access.read"

	// Realm is the WWW-Authenticate realm on every challenge.
	Realm = "iamsim"

	// MaxRequestBytes bounds every request body the simulator reads.
	MaxRequestBytes = 64 << 10
	// MaxIdempotencyKeyLen bounds the Idempotency-Key header.
	MaxIdempotencyKeyLen = 200

	// ControlTokenHeader carries the control-plane token.
	ControlTokenHeader = "X-Sim-Control-Token"

	// DefaultClientID is the local development client identifier.
	DefaultClientID = "hcm-next-local"

	defaultMaxAttempts = 10
	defaultBackoffBase = time.Second
	defaultBackoffMax  = 60 * time.Second
	defaultGrantDelay  = 1500 * time.Millisecond
	defaultTokenTTL    = 300 * time.Second
)

// Config wires a Server. ClientID, ClientSecret and WebhookSecret are
// required for a useful server; every other field has a production-sane
// default so tools/integrationsim/cmd/iamsim stays a thin flag parser, while tests replace the
// nondeterministic pieces.
type Config struct {
	// ClientID and ClientSecret are the single registered OAuth client.
	ClientID     string
	ClientSecret string
	// ClientSecrets are further valid secrets for the same client, so it can
	// rotate without an outage; the effective set is ClientSecret plus
	// ClientSecrets (empty entries ignored). PUT /v1/_control/client-secrets
	// replaces the set at runtime; tokens already issued stay valid.
	ClientSecrets []string
	// ClientScopes are the scopes the client may be granted (default: all).
	ClientScopes []string
	// WebhookSecret is the HMAC key callbacks are signed with; the receiver
	// must register the same bytes.
	WebhookSecret []byte
	// ControlToken guards the /v1/_control endpoints. Empty leaves them open,
	// which is acceptable only on a developer machine.
	ControlToken string
	// Scenario is the initial scenario; nil means DefaultScenario().
	Scenario *Scenario
	// MaxAttempts caps callback attempts per delivery (default 10).
	MaxAttempts int
	// BackoffBase is the first retry wait, doubled per attempt (default 1s).
	BackoffBase time.Duration
	// BackoffMax caps a single retry wait (default 60s).
	BackoffMax time.Duration
	// Now is the clock used for token expiry and timestamps (default
	// time.Now).
	Now func() time.Time
	// TokenRand supplies access-token bytes (default crypto/rand.Reader).
	// Tests may make it deterministic; production must keep it
	// cryptographically random because the token is the credential.
	TokenRand io.Reader
	// Random returns a value in [0,1) for flaky-mode decisions. It is only
	// ever called with the server lock held, so it need not be goroutine-safe.
	// Default: a PCG source seeded from the wall clock.
	Random func() float64
	// NewID mints opaque unique ids for provider refs and event ids
	// (default uuid.NewString).
	NewID func() string
	// Sleep waits d or until ctx is done, returning ctx.Err() in the latter
	// case. It is used for the grant delay and retry backoff so tests can
	// observe the schedule without waiting it out.
	Sleep func(ctx context.Context, d time.Duration) error
	// HTTPClient delivers callbacks. The default does not follow redirects,
	// because a provider treating a 3xx as delivery would lose the event.
	HTTPClient *http.Client
	// Logger receives structured logs (default: discarded). Secrets and raw
	// tokens are never logged; tokens appear only as a short hash prefix.
	Logger *slog.Logger
}

// Server is one simulated IAM provider. It is an http.Handler.
type Server struct {
	clientID     string
	clientScopes []string
	controlToken string
	maxAttempts  int
	backoffBase  time.Duration
	backoffMax   time.Duration
	now          func() time.Time
	tokenRand    io.Reader
	random       func() float64
	newID        func() string
	sleep        func(ctx context.Context, d time.Duration) error
	client       *http.Client
	log          *slog.Logger
	mux          *http.ServeMux

	// baseCtx parents every background job; cancelBase aborts them when a
	// shutdown's drain deadline passes.
	baseCtx    context.Context
	cancelBase context.CancelFunc
	// jobs counts background processing goroutines. Add happens only under
	// mu while closing is false, so Shutdown's Wait never races an Add.
	jobs sync.WaitGroup

	mu       sync.Mutex
	closing  bool
	scenario Scenario
	changes  map[string]*change
	// tokens is keyed by the SHA-256 of the access token. The raw token is
	// never retained, so a dump of server state cannot be replayed as a
	// credential.
	tokens map[tokenHash]tokenRecord
	// clientSecrets holds the SHA-256 of every valid client secret and
	// webhookSecret signs callbacks; both rotate at runtime, hence under mu.
	clientSecrets []tokenHash
	webhookSecret []byte
}

// New builds a Server from cfg, applying defaults.
func New(cfg Config) *Server {
	s := &Server{
		clientID:      cfg.ClientID,
		clientSecrets: hashSecrets(append([]string{cfg.ClientSecret}, cfg.ClientSecrets...)),
		clientScopes:  normalizeScopes(cfg.ClientScopes),
		webhookSecret: append([]byte(nil), cfg.WebhookSecret...),
		controlToken:  cfg.ControlToken,
		maxAttempts:   cfg.MaxAttempts,
		backoffBase:   cfg.BackoffBase,
		backoffMax:    cfg.BackoffMax,
		now:           cfg.Now,
		tokenRand:     cfg.TokenRand,
		random:        cfg.Random,
		newID:         cfg.NewID,
		sleep:         cfg.Sleep,
		client:        cfg.HTTPClient,
		log:           cfg.Logger,
		changes:       make(map[string]*change),
		tokens:        make(map[tokenHash]tokenRecord),
	}
	if len(s.clientScopes) == 0 {
		s.clientScopes = []string{ScopeRead, ScopeWrite}
	}
	if cfg.Scenario != nil {
		s.scenario = *cfg.Scenario
	} else {
		s.scenario = DefaultScenario()
	}
	if s.maxAttempts <= 0 {
		s.maxAttempts = defaultMaxAttempts
	}
	if s.backoffBase <= 0 {
		s.backoffBase = defaultBackoffBase
	}
	if s.backoffMax <= 0 {
		s.backoffMax = defaultBackoffMax
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.tokenRand == nil {
		s.tokenRand = rand.Reader
	}
	if s.random == nil {
		seed := uint64(time.Now().UnixNano())
		s.random = mrand.New(mrand.NewPCG(seed, seed^0x9e3779b97f4a7c15)).Float64
	}
	if s.newID == nil {
		s.newID = uuid.NewString
	}
	if s.sleep == nil {
		s.sleep = sleepContext
	}
	if s.client == nil {
		s.client = &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	s.baseCtx, s.cancelBase = context.WithCancel(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", s.handleToken)
	mux.HandleFunc("POST /v1/access-changes", s.requireBearer(s.handleIntake, ScopeWrite))
	mux.HandleFunc("GET /v1/access-changes/{change_ref}", s.requireBearer(s.handleStatus, ScopeRead, ScopeWrite))
	mux.HandleFunc("POST /v1/access-changes/{change_ref}/revocation", s.requireBearer(s.handleRevocation, ScopeWrite))
	mux.HandleFunc("GET /v1/_control/scenario", s.requireControl(s.handleGetScenario))
	mux.HandleFunc("PUT /v1/_control/scenario", s.requireControl(s.handlePutScenario))
	mux.HandleFunc("POST /v1/_control/tokens/revoke", s.requireControl(s.handleRevokeTokens))
	mux.HandleFunc("POST /v1/_control/reset", s.requireControl(s.handleReset))
	mux.HandleFunc("PUT /v1/_control/client-secrets", s.requireControl(s.handlePutClientSecrets))
	mux.HandleFunc("PUT /v1/_control/webhook-secret", s.requireControl(s.handlePutWebhookSecret))
	mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux = mux
	return s
}

// ServeHTTP routes a request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// Shutdown stops accepting access changes and waits for in-flight processing
// and callback delivery to finish. If ctx ends first, the remaining work is
// cancelled (its callbacks are abandoned mid-retry, exactly as a provider
// crash would leave them) and ctx.Err() is returned once it has stopped.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.jobs.Wait()
		close(done)
	}()
	select {
	case <-done:
		s.cancelBase()
		return nil
	case <-ctx.Done():
		s.cancelBase()
		<-done
		return ctx.Err()
	}
}

// change is one accepted access change and its delivery bookkeeping. Every
// mutable field is guarded by Server.mu; req and acceptBody are immutable.
//
// status is the state machine: ACCEPTED -> GRANTED | REJECTED by the grant
// job, and ACCEPTED | GRANTED -> REVERSAL_PENDING -> REVOKED by a revocation.
// Both jobs only move the status after re-checking it under mu, so whichever
// reaches the lock second observes the other's move: a grant that finds the
// change no longer ACCEPTED gives up without granting or calling back.
type change struct {
	req         AccessChangeRequest
	providerRef string
	acceptBody  []byte
	cancel      context.CancelFunc

	status    string
	reason    string
	grantedAt time.Time
	// delivery counts the grant/reject callback.
	delivery
	// rev is nil until a revocation is accepted.
	rev *revocation
}

// delivery is the bookkeeping for one event's callback attempts. echo is
// the validated trace and correlation context of the request that started
// the event; it is set before the delivery is published and never changes,
// so it is read without mu.
type delivery struct {
	attempts   int
	delivered  bool
	lastStatus int
	echo       providerwire.Context
}

// wireAttrs renders a request's validated trace and correlation context as
// log attributes; absent or malformed values are omitted.
func wireAttrs(c providerwire.Context) []any {
	var out []any
	if c.TraceParent != "" {
		out = append(out, "traceparent", c.TraceParent)
	}
	if c.CorrelationID != "" {
		out = append(out, "correlation_id", c.CorrelationID)
	}
	return out
}

// mergeEcho is the context a revocation callback echoes: the revocation
// request's own values, falling back field by field to the original
// intake's.
func mergeEcho(undo, original providerwire.Context) providerwire.Context {
	if undo.TraceParent == "" {
		undo.TraceParent = original.TraceParent
	}
	if undo.CorrelationID == "" {
		undo.CorrelationID = original.CorrelationID
	}
	return undo
}

// ChangeStatus is the GET /v1/access-changes/{change_ref} response.
type ChangeStatus struct {
	ChangeRef   string         `json:"change_ref"`
	ProviderRef string         `json:"provider_ref"`
	Status      string         `json:"status"`
	Reason      string         `json:"reason"`
	GrantedAt   string         `json:"granted_at"`
	Callback    CallbackStatus `json:"callback"`
	// Reversal is present once a revocation has been accepted.
	Reversal *ReversalStatus `json:"reversal,omitempty"`
}

// CallbackStatus summarises delivery of a change's result callback.
type CallbackStatus struct {
	Attempts   int  `json:"attempts"`
	Delivered  bool `json:"delivered"`
	LastStatus int  `json:"last_status"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("change_ref")
	wc := providerwire.FromHeader(r.Header)
	s.mu.Lock()
	ch, ok := s.changes[ref]
	var out ChangeStatus
	if ok {
		out = ChangeStatus{
			ChangeRef:   ch.req.ChangeRef,
			ProviderRef: ch.providerRef,
			Status:      ch.status,
			Reason:      ch.reason,
			Callback:    CallbackStatus{Attempts: ch.attempts, Delivered: ch.delivered, LastStatus: ch.lastStatus},
		}
		if !ch.grantedAt.IsZero() {
			out.GrantedAt = ch.grantedAt.UTC().Format(time.RFC3339)
		}
		if ch.rev != nil {
			out.Reversal = ch.rev.status()
		}
	}
	s.mu.Unlock()
	s.log.Info("status read", append([]any{"change_ref", ref, "found", ok}, wireAttrs(wc)...)...)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	writeRaw(w, status, body)
}

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// sleepContext is the production Sleep: a timer that yields to cancellation.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
