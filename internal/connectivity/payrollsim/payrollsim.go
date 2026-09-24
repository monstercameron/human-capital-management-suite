// Package payrollsim is a standalone simulator of a third-party payroll
// provider (think ADP) used to integration-test the promotion workflow's
// payroll hand-off end to end.
//
// Semantic owner: connectivity.
//
// It exists because the real hand-off is asynchronous and adversarial in ways
// an in-process fake cannot show: the provider accepts a pay change, applies
// or rejects it later, and reports the result through a signed callback that
// it retries until the receiver acknowledges it. The simulator reproduces that
// shape over real HTTP so the HCM side's idempotent intake, signature
// verification and receipt dedupe are exercised against the same wire
// contract a production provider adapter will see.
//
// Everything a test needs to control is injectable (clock, randomness, id
// minting, the callback HTTP client and the backoff sleep), and all mutable
// state lives on the Server value, never at package level, so several
// simulators can run side by side in one test binary.
package payrollsim

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry/providerwire"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// Contract constants shared by the intake and callback halves. They are the
// wire contract the HCM receiver is built against, so they are exported for
// tests on both sides to reference rather than retype.
const (
	// EventApplied, EventRejected and EventReversed are the Webhook-Event
	// values.
	EventApplied  = "payroll.change.applied"
	EventRejected = "payroll.change.rejected"
	EventReversed = "payroll.change.reversed"
	// Schema is the Webhook-Schema value the receiver allowlists.
	Schema = "payrollsim.change_result/v1"

	StatusAccepted = "ACCEPTED"
	StatusApplied  = "APPLIED"
	StatusRejected = "REJECTED"
	// StatusReversalPending is reported between an accepted reversal request
	// and its completion; StatusReversed once the change has been undone.
	StatusReversalPending = "REVERSAL_PENDING"
	StatusReversed        = "REVERSED"

	// MaxRequestBytes bounds every request body the simulator reads.
	MaxRequestBytes = 64 << 10
	// MaxIdempotencyKeyLen bounds the Idempotency-Key header.
	MaxIdempotencyKeyLen = 200

	// ControlTokenHeader carries the control-plane token.
	ControlTokenHeader = "X-Sim-Control-Token"
	// APIKeyHeader carries the static API key that authenticates the
	// pay-change endpoints, the way a vendor issues one key per client.
	APIKeyHeader = "X-Api-Key"

	defaultMaxAttempts = 10
	defaultBackoffBase = time.Second
	defaultBackoffMax  = 60 * time.Second
	defaultApplyDelay  = 1500 * time.Millisecond
)

// Config wires a Server. Only Secret is required; every other field has a
// production-sane default so tools/integrationsim/cmd/payrollsim stays a thin flag parser, while
// tests replace the nondeterministic pieces.
type Config struct {
	// Secret is the HMAC key callbacks are signed with; the receiver must
	// register the same bytes.
	Secret []byte
	// APIKey authenticates POST /v1/pay-changes and GET
	// /v1/pay-changes/{change_ref}. Empty disables the check, which is only
	// meant for embedding the simulator in a test that is not about auth;
	// tools/integrationsim/cmd/payrollsim always configures one.
	APIKey string
	// APIKeys are further valid keys, so a client can rotate from one key to
	// the next without an outage; the effective set is APIKey plus APIKeys
	// (empty entries ignored). PUT /v1/_control/api-keys replaces the set at
	// runtime.
	APIKeys []string
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
	// Now is the clock used for timestamps (default time.Now).
	Now func() time.Time
	// Random returns a value in [0,1) for flaky-mode decisions. It is only
	// ever called with the server lock held, so it need not be goroutine-safe.
	// Default: a PCG source seeded from the wall clock.
	Random func() float64
	// NewID mints opaque unique ids for provider refs and event ids
	// (default uuid.NewString).
	NewID func() string
	// Sleep waits d or until ctx is done, returning ctx.Err() in the latter
	// case. It is used for the apply delay and retry backoff so tests can
	// observe the schedule without waiting it out.
	Sleep func(ctx context.Context, d time.Duration) error
	// HTTPClient is the injected callback port used by simulator fixtures.
	// Production composition should set Gateway; no direct client is created
	// when neither port is supplied.
	HTTPClient *http.Client
	// Gateway routes callback delivery through centralized DNS, TLS, proxy,
	// trust and DLP enforcement. When present it takes precedence over
	// HTTPClient.
	Gateway *egress.Gateway
	// Overload coordinates callback attempts. Nil installs a bounded local
	// retry budget and circuit breaker for this simulator instance.
	Overload *edge.Coordinator
	// Logger receives structured intake, callback and retry logs
	// (default: discarded).
	Logger *slog.Logger
}

// Server is one simulated payroll provider. It is an http.Handler.
type Server struct {
	controlToken string
	maxAttempts  int
	backoffBase  time.Duration
	backoffMax   time.Duration
	now          func() time.Time
	random       func() float64
	newID        func() string
	sleep        func(ctx context.Context, d time.Duration) error
	client       Doer
	gateway      *egress.Gateway
	overload     *edge.Coordinator
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
	// secret signs callbacks; it is guarded by mu because
	// PUT /v1/_control/webhook-secret rotates it at runtime.
	secret []byte
	// apiKeys holds the SHA-256 of every valid API key (empty: auth off).
	apiKeys []keyHash
}

// New builds a Server from cfg, applying defaults.
func New(cfg Config) *Server {
	s := &Server{
		secret:       append([]byte(nil), cfg.Secret...),
		apiKeys:      hashKeys(append([]string{cfg.APIKey}, cfg.APIKeys...)),
		controlToken: cfg.ControlToken,
		maxAttempts:  cfg.MaxAttempts,
		backoffBase:  cfg.BackoffBase,
		backoffMax:   cfg.BackoffMax,
		now:          cfg.Now,
		random:       cfg.Random,
		newID:        cfg.NewID,
		sleep:        cfg.Sleep,
		gateway:      cfg.Gateway,
		overload:     cfg.Overload,
		log:          cfg.Logger,
		changes:      make(map[string]*change),
	}
	if cfg.Scenario != nil {
		s.scenario = *cfg.Scenario
	} else {
		s.scenario = DefaultScenario()
	}
	if cfg.HTTPClient != nil {
		s.client = cfg.HTTPClient
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
	if s.random == nil {
		seed := uint64(time.Now().UnixNano())
		s.random = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)).Float64
	}
	if s.newID == nil {
		s.newID = uuid.NewString
	}
	if s.sleep == nil {
		s.sleep = sleepContext
	}
	if s.gateway != nil {
		// Never downgrade to the injected client when the enforcing gateway is
		// configured alongside it.
		s.client = nil
	}
	if s.overload == nil {
		s.overload = &edge.Coordinator{
			Breaker: edge.NewCircuitBreaker(edge.CircuitConfig{FailureThreshold: 5, Cooldown: 30 * time.Second}),
			Retry:   edge.RetryLedger{Provisioner: admission.NewProvisioner(), Allowed: min(3, max(0, s.maxAttempts-1)), Version: "payrollsim-callback-v1"},
		}
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	s.baseCtx, s.cancelBase = context.WithCancel(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/pay-changes", s.requireAPIKey(s.handleIntake))
	mux.HandleFunc("GET /v1/pay-changes/{change_ref}", s.requireAPIKey(s.handleStatus))
	mux.HandleFunc("POST /v1/pay-changes/{change_ref}/reversal", s.requireAPIKey(s.handleReversal))
	mux.HandleFunc("GET /v1/_control/scenario", s.requireControl(s.handleGetScenario))
	mux.HandleFunc("PUT /v1/_control/scenario", s.requireControl(s.handlePutScenario))
	mux.HandleFunc("POST /v1/_control/reset", s.requireControl(s.handleReset))
	mux.HandleFunc("PUT /v1/_control/api-keys", s.requireControl(s.handlePutAPIKeys))
	mux.HandleFunc("PUT /v1/_control/webhook-secret", s.requireControl(s.handlePutWebhookSecret))
	mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux = mux
	return s
}

// Doer is the narrow outbound callback port.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// ServeHTTP routes a request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// Shutdown stops accepting pay changes and waits for in-flight processing and
// callback delivery to finish. If ctx ends first, the remaining work is
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

// change is one accepted pay change and its delivery bookkeeping. Every
// mutable field is guarded by Server.mu; req and acceptBody are immutable.
//
// status is the state machine: ACCEPTED -> APPLIED | REJECTED by the apply
// job, and ACCEPTED | APPLIED -> REVERSAL_PENDING -> REVERSED by a reversal.
// Both jobs only move the status after re-checking it under mu, so whichever
// reaches the lock second observes the other's move: an apply that finds the
// change no longer ACCEPTED gives up without applying or calling back.
type change struct {
	req         PayChangeRequest
	providerRef string
	acceptBody  []byte
	cancel      context.CancelFunc

	status    string
	reason    string
	appliedAt time.Time
	// delivery counts the apply/reject callback.
	delivery
	// rev is nil until a reversal is accepted.
	rev *reversal
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

// mergeEcho is the context an undo callback echoes: the undo request's own
// values, falling back field by field to the original intake's.
func mergeEcho(undo, original providerwire.Context) providerwire.Context {
	if undo.TraceParent == "" {
		undo.TraceParent = original.TraceParent
	}
	if undo.CorrelationID == "" {
		undo.CorrelationID = original.CorrelationID
	}
	return undo
}

// ChangeStatus is the GET /v1/pay-changes/{change_ref} response.
type ChangeStatus struct {
	ChangeRef   string         `json:"change_ref"`
	ProviderRef string         `json:"provider_ref"`
	Status      string         `json:"status"`
	Reason      string         `json:"reason"`
	AppliedAt   string         `json:"applied_at"`
	Callback    CallbackStatus `json:"callback"`
	// Reversal is present once a reversal has been accepted.
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
		if !ch.appliedAt.IsZero() {
			out.AppliedAt = ch.appliedAt.UTC().Format(time.RFC3339)
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
