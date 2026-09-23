package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Kind names the wire protocol a request arrived on. It is recorded for
// telemetry and is deliberately excluded from [Invocation.TrustedFingerprint]:
// the whole contract is that the trusted context does not depend on it.
type Kind string

// Supported transport kinds.
const (
	// KindGRPC is the canonical native gRPC service surface.
	KindGRPC Kind = "grpc"
	// KindHTTPEdge is the HTTP/JSON edge projection of the same methods.
	KindHTTPEdge Kind = "http-edge"
)

// Metadata is the read-only view of a request's transport metadata that
// admission needs. gRPC metadata and HTTP headers each adapt to it, which is
// how one screening implementation covers both.
//
// Keys are compared case-insensitively; implementations return lowercase names
// from Keys.
type Metadata interface {
	// Keys returns every metadata name present on the request.
	Keys() []string
	// Get returns the values for name, or nil when it is absent.
	Get(name string) []string
}

// MapMetadata adapts a plain map to [Metadata]. It is the adapter both
// transports build, and it is exported so a test can construct a request's
// metadata without a live connection.
type MapMetadata map[string][]string

// Keys implements [Metadata].
func (m MapMetadata) Keys() []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, strings.ToLower(k))
	}
	return keys
}

// Get implements [Metadata].
func (m MapMetadata) Get(name string) []string {
	if v, ok := m[strings.ToLower(name)]; ok {
		return v
	}
	for k, v := range m {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

// Metadata names admission reads. Everything else is caller payload and is
// ignored; the reserved trusted-context names are rejected outright by
// trust.RejectCallerSelectedAuthority.
const (
	// AuthorizationMetadataKey carries the credential.
	AuthorizationMetadataKey = "authorization"
	// RequestIDMetadataKey carries the caller's own request identifier. A
	// client request identifier is explicitly permitted by the trusted-request
	// boundary: it correlates, it does not authorize.
	RequestIDMetadataKey = "x-request-id"
)

// maxRequestIDBytes bounds an accepted caller request identifier.
const maxRequestIDBytes = 128

// Config is the admission configuration one server instance shares across
// every transport it exposes. Passing the same Config to the gRPC server and
// to the HTTP edge is what makes their trusted context identical by
// construction.
type Config struct {
	// Verifier turns a presented credential into a principal. Required.
	Verifier trust.Verifier
	// Audience is the audience this listener answers to. Optional; when empty
	// the verifier's own configured audience applies.
	Audience string
	// Now supplies the current time. Nil means time.Now.
	Now func() time.Time
	// NewRequestID mints a request identifier when the caller supplied none.
	// Nil means a digest-derived identifier, which keeps a fixture
	// deterministic without a clock or a random source.
	NewRequestID func() string
	// MaxDeadline caps every request's deadline. Zero means 30 seconds.
	MaxDeadline time.Duration
	// MaxStreamDeadline caps a server-streaming call instead. A live feed is
	// not a slow request: capping a watch at the unary deadline cut every chat
	// and journey stream at exactly thirty seconds, which is a defect and not a
	// budget. Zero means 15 minutes, matching the stream lifetime ceiling the
	// streaming handlers declare for themselves.
	MaxStreamDeadline time.Duration
	// Validator performs strict structural validation. Nil means
	// [DefaultValidator].
	Validator Validator
	// Logger receives one structured record per completed request. Nil means
	// no logging.
	Logger Logger
}

// defaultMaxDeadline is the server-imposed deadline cap.
const defaultMaxDeadline = 30 * time.Second

// defaultMaxStreamDeadline is the server-imposed cap on a server-streaming
// call. It matches the lifetime ceiling the streaming handlers declare, so the
// transport bounds a live feed exactly once and at the same number.
const defaultMaxStreamDeadline = 15 * time.Minute

// now returns the configured clock reading.
func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// maxDeadline returns the effective server deadline cap.
func (c Config) maxDeadline() time.Duration {
	if c.MaxDeadline > 0 {
		return c.MaxDeadline
	}
	return defaultMaxDeadline
}

// maxStreamDeadline returns the effective cap for a server-streaming call.
func (c Config) maxStreamDeadline() time.Duration {
	if c.MaxStreamDeadline > 0 {
		return c.MaxStreamDeadline
	}
	return defaultMaxStreamDeadline
}

// validator returns the effective structural validator.
func (c Config) validator() Validator {
	if c.Validator != nil {
		return c.Validator
	}
	return DefaultValidator{}
}

// Invocation is the one immutable trusted context that admission constructs
// and every downstream layer consumes. There is no setter: a handler that
// wants different trusted context does not get to have it.
type Invocation struct {
	principal           *trust.Principal
	requestID           string
	method              string
	kind                Kind
	receivedAt          time.Time
	tenantID            string
	organizationScopeID string
	purpose             string
}

// Principal returns the authenticated principal.
func (i *Invocation) Principal() *trust.Principal { return i.principal }

// RequestID returns the correlation identifier for this request.
func (i *Invocation) RequestID() string { return i.requestID }

// Method returns the fully qualified method name.
func (i *Invocation) Method() string { return i.method }

// Kind returns the wire protocol the request arrived on.
func (i *Invocation) Kind() Kind { return i.kind }

// ReceivedAt returns the admission instant.
func (i *Invocation) ReceivedAt() time.Time { return i.receivedAt }

// TenantID returns the server-resolved tenant.
func (i *Invocation) TenantID() string { return i.tenantID }

// OrganizationScopeID returns the server-resolved organization scope.
func (i *Invocation) OrganizationScopeID() string { return i.organizationScopeID }

// Purpose returns the resolved and authorized purpose of processing.
func (i *Invocation) Purpose() string { return i.purpose }

// EvidenceID returns the authentication evidence identifier.
func (i *Invocation) EvidenceID() string {
	if i.principal == nil {
		return ""
	}
	return i.principal.EvidenceID()
}

// TrustedFingerprint is the canonical digest over every trusted value the
// server derived: the principal's own fingerprint plus the resolved method,
// tenant, organization scope and purpose. It excludes the transport kind, the
// request identifier and the arrival instant, because those are the parts that
// are allowed to differ between two equivalent requests.
//
// Equality of this string across native gRPC and the HTTP edge is the
// ENDPOINT-002 assertion.
func (i *Invocation) TrustedFingerprint() string {
	h := sha256.New()
	write := func(label, v string) { fmt.Fprintf(h, "%s=%d:%s;", label, len(v), v) }
	if i.principal != nil {
		write("principal", i.principal.Fingerprint())
	} else {
		write("principal", "")
	}
	write("method", i.method)
	write("tenant", i.tenantID)
	write("orgscope", i.organizationScopeID)
	write("purpose", i.purpose)
	return hex.EncodeToString(h.Sum(nil))
}

// String returns a redacted, log-safe description.
func (i *Invocation) String() string {
	return fmt.Sprintf("invocation(method=%s transport=%s request=%s tenant=%s purpose=%s evidence=%s)",
		i.method, i.kind, i.requestID, i.tenantID, i.purpose, i.EvidenceID())
}

// invocationContextKey is the unexported context key for the invocation.
type invocationContextKey struct{}

// WithInvocation returns a child context carrying inv.
func WithInvocation(ctx context.Context, inv *Invocation) context.Context {
	if inv == nil {
		return ctx
	}
	return context.WithValue(ctx, invocationContextKey{}, inv)
}

// InvocationFromContext returns the invocation carried by ctx.
func InvocationFromContext(ctx context.Context) (*Invocation, bool) {
	inv, ok := ctx.Value(invocationContextKey{}).(*Invocation)
	return inv, ok && inv != nil
}

// AdmissionRequest is one request presented for admission.
type AdmissionRequest struct {
	// Metadata is the request's transport metadata.
	Metadata Metadata
	// Method is the fully qualified gRPC method name, e.g.
	// "/hcmnext.intents.v1.IntentService/CreateIntent". Both transports use
	// the gRPC spelling so that telemetry and policy have one method vocabulary.
	Method string
	// Kind is the wire protocol the request arrived on.
	Kind Kind
	// Message is the decoded request message. Admission validates it and
	// overwrites its trusted fields in place.
	Message proto.Message
}

// Admit runs the whole trusted-context construction for one request and
// returns a context carrying the principal and the immutable [Invocation].
//
// The order of the steps is part of the contract:
//
//  1. Screen for caller-selected trusted context. A request that tries to
//     choose its own principal, tenant, scope, purpose, assurance, authority or
//     placement is rejected before anything else happens, so a probe cannot
//     learn whether the selected value exists.
//  2. Resolve the correlation identifier.
//  3. Authenticate. An unauthenticated caller never reaches validation, so
//     validation feedback is not an oracle for anonymous callers.
//  4. Validate the request structurally, strictly.
//  5. Overwrite the request's trusted fields with server-derived values and
//     resolve the effective tenant, organization scope and purpose.
//
// Every returned error is already correlated, and every error raised after
// step 3 also carries the authentication evidence reference.
func Admit(ctx context.Context, cfg Config, req AdmissionRequest) (context.Context, *Invocation, *envelope.Error) {
	principal, requestID, admitErr := PreAdmit(ctx, cfg, req.Metadata, req.Method)
	if admitErr != nil {
		return ctx, nil, admitErr
	}

	evidence := envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"}

	if req.Message != nil {
		if vErr := cfg.validator().Validate(req.Method, req.Message); vErr != nil {
			return ctx, nil, vErr.WithCorrelation(requestID).WithEvidence(evidence)
		}
	}

	scope, scopeErr := ApplyTrustedContext(req.Message, principal)
	if scopeErr != nil {
		return ctx, nil, scopeErr.WithCorrelation(requestID).WithEvidence(evidence)
	}

	inv := &Invocation{
		principal:           principal,
		requestID:           requestID,
		method:              req.Method,
		kind:                req.Kind,
		receivedAt:          cfg.now().UTC(),
		tenantID:            scope.TenantID,
		organizationScopeID: scope.OrganizationScopeID,
		purpose:             scope.Purpose,
	}
	ctx = trust.WithPrincipal(ctx, principal)
	ctx = WithInvocation(ctx, inv)
	return ctx, inv, nil
}

// PreAdmit performs the first three admission steps: the reserved-metadata
// screen, correlation-identifier resolution and authentication. It returns the
// verified principal and the correlation identifier.
//
// It is exported because the HTTP edge has to run these steps before its
// transport library decodes the body, and running them in a different order
// there would hand an anonymous caller a validity oracle that a gRPC caller
// does not get. One function, one order, both transports.
func PreAdmit(ctx context.Context, cfg Config, md Metadata, method string) (*trust.Principal, string, *envelope.Error) {
	if md == nil {
		md = MapMetadata(nil)
	}
	if carrier, ok := ctx.Value(preAdmissionContextKey{}).(*preAdmission); ok && carrier != nil &&
		carrier.method == method && carrier.fingerprint == preAdmissionFingerprint(md) {
		return carrier.principal, carrier.requestID, nil
	}
	requestID := resolveRequestID(cfg, md, method)

	if selected := trust.RejectCallerSelectedAuthority(md.Keys()); len(selected) > 0 {
		err := envelope.New(envelope.CodeInvalidArgument,
			"trusted_context.caller_selected_authority",
			"the request may not select trusted context").
			WithCorrelation(requestID)
		for _, key := range selected {
			err.WithViolation("metadata."+key,
				"trusted context is derived server-side and may not be supplied by the caller",
				"trusted_request_boundary.reserved_metadata")
		}
		return nil, requestID, err
	}

	principal, authErr := authenticate(ctx, cfg, md)
	if authErr != nil {
		return nil, requestID, authErr.WithCorrelation(requestID)
	}
	return principal, requestID, nil
}

// authenticate extracts the credential and verifies it, projecting every
// verification failure onto one owned condition. The projection is
// deliberately lossy: a caller learns that authentication failed, never which
// of signature, issuer, audience, expiry or assurance was the reason.
func authenticate(ctx context.Context, cfg Config, md Metadata) (*trust.Principal, *envelope.Error) {
	if cfg.Verifier == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"transport.no_verifier_configured",
			"the listener cannot authenticate requests")
	}

	scheme, token := splitAuthorization(md.Get(AuthorizationMetadataKey))
	principal, err := cfg.Verifier.Verify(ctx, trust.Credential{
		Scheme:   scheme,
		Token:    token,
		Audience: cfg.Audience,
	})
	if err != nil {
		return nil, envelope.New(envelope.CodeUnauthenticated,
			authenticationReasonRef(err),
			"the request carries no valid authentication").
			WithDiagnostic(err)
	}
	if principal == nil {
		return nil, envelope.New(envelope.CodeUnauthenticated,
			"authentication.no_principal",
			"the request carries no valid authentication")
	}
	return principal, nil
}

// authenticationReasonRef maps a verification failure to a stable owned reason
// identifier. The reason identifiers are for operators reading telemetry; the
// caller-visible condition is UNAUTHENTICATED either way.
func authenticationReasonRef(err error) string {
	switch {
	case errors.Is(err, trust.ErrNoCredential):
		return "authentication.missing_credential"
	case errors.Is(err, trust.ErrUnsupportedScheme):
		return "authentication.unsupported_scheme"
	case errors.Is(err, trust.ErrExpiredCredential):
		return "authentication.expired_credential"
	case errors.Is(err, trust.ErrWrongIssuer):
		return "authentication.wrong_issuer"
	case errors.Is(err, trust.ErrWrongAudience):
		return "authentication.wrong_audience"
	case errors.Is(err, trust.ErrMissingAssurance):
		return "authentication.missing_assurance"
	default:
		return "authentication.invalid_credential"
	}
}

// splitAuthorization splits an Authorization value into scheme and token.
func splitAuthorization(values []string) (scheme, token string) {
	if len(values) == 0 {
		return "", ""
	}
	raw := strings.TrimSpace(values[0])
	if raw == "" {
		return "", ""
	}
	if s, t, ok := strings.Cut(raw, " "); ok {
		return s, strings.TrimSpace(t)
	}
	return "", raw
}

// resolveRequestID accepts a caller-supplied request identifier when it is
// bounded printable ASCII, and mints one otherwise.
func resolveRequestID(cfg Config, md Metadata, method string) string {
	if v := md.Get(RequestIDMetadataKey); len(v) > 0 {
		candidate := strings.TrimSpace(v[0])
		if candidate != "" && len(candidate) <= maxRequestIDBytes && printableASCII(candidate) {
			return candidate
		}
	}
	if cfg.NewRequestID != nil {
		return cfg.NewRequestID()
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%d", method, cfg.now().UnixNano()))
	return "req:" + hex.EncodeToString(sum[:8])
}

// printableASCII reports whether s contains only printable ASCII.
func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// CapDeadline returns a context whose deadline is at most the configured
// server cap. A caller that asks for longer gets the cap; a caller that asks
// for less keeps its own, shorter deadline; a caller that asks for nothing
// gets the cap rather than an unbounded request.
// The cap uses wall time, not [Config.Now]: a deadline is transport mechanics,
// and a fixture that pins business time must not thereby cancel its own
// requests.
func CapDeadline(ctx context.Context, cfg Config) (context.Context, context.CancelFunc) {
	return capTo(ctx, cfg.maxDeadline())
}

// CapStreamDeadline is [CapDeadline] for a server-streaming call, which is
// bounded by the stream lifetime ceiling rather than the unary request budget. A
// watch is meant to stay open; cutting it at the unary cap ended every chat and
// journey stream at exactly thirty seconds with DEADLINE_EXCEEDED.
func CapStreamDeadline(ctx context.Context, cfg Config) (context.Context, context.CancelFunc) {
	return capTo(ctx, cfg.maxStreamDeadline())
}

func capTo(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && !deadline.After(time.Now().Add(limit)) {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, limit)
}
