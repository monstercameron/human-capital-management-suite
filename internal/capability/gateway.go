package capability

import (
	"context"
	"time"
)

// AuthorizationDecision is a decision already made by an authorization
// component this package does not implement (TRUST-011/GOVERN-001..003 are
// not prerequisites here). The gateway only enforces the decision it is
// handed.
type AuthorizationDecision string

const (
	Allow AuthorizationDecision = "ALLOW"
	Deny  AuthorizationDecision = "DENY"
)

// Authorization is the caller-supplied decision input CAP-002 requires. It is
// produced elsewhere (a policy engine, a session, a test fixture); this
// package neither authenticates nor computes it.
type Authorization struct {
	Decision   AuthorizationDecision
	Scopes     []string
	Reason     string
	SubjectRef string
	// Tenant is the tenant key the verified principal (or the pinned
	// workflow delegation) acts in. The gateway copies it onto every
	// evidence record so a durable sink scopes the row to that tenant; it
	// is never inferred here (WF-RUN-035).
	Tenant string
}

func (a Authorization) hasScope(scope string) bool {
	for _, s := range a.Scopes {
		if s == scope || s == "*" {
			return true
		}
	}
	return false
}

// InvocationEvidence is one gateway decision - an invocation or a refusal -
// recorded for evidence. Reference: capability-registry-and-lifecycle.md
// "Security, Failure, and Evidence".
type InvocationEvidence struct {
	CapabilityID      string
	CapabilityVersion uint32
	SubjectRef        string
	// Tenant is the tenant key the decision was made in, as the caller's
	// verified principal names it. A durable sink refuses a record without
	// one rather than guess (WF-RUN-035).
	Tenant     string
	Decision   string // "INVOKED" or the refusal Code
	ReasonCode string
	OccurredAt time.Time
	// Purpose, IdempotencyKey, Deadline and EffectClass are copied from a
	// governed [Invocation] envelope; they stay empty for a P1A interactive
	// call that presents none. EffectClass is the resolved capability's
	// published class.
	Purpose        string
	IdempotencyKey string
	Deadline       time.Time
	EffectClass    EffectClass
}

// EvidenceSink is the port an invocation evidence record is written through.
// The concrete sink (a ledger append, a test recorder) lives outside this
// package.
type EvidenceSink interface {
	RecordInvocation(ctx context.Context, evt InvocationEvidence) (evidenceID string, err error)
}

// InvokeRequest is one call into the governed gateway.
type InvokeRequest struct {
	Capability    Key
	Payload       any
	Authorization Authorization
	// Invocation is the governed execution envelope a workflow step presents
	// (WF-RUN-034). Nil keeps the P1A interactive contract.
	Invocation *Invocation
}

// InvokeResult is a successful invocation's typed result.
type InvokeResult struct {
	Response   any
	EvidenceID string
}

// Gateway is CAP-002's governed capability gateway: the one path every
// transport shares (capability-registry-and-lifecycle.md, platform-plane-
// model.md). It resolves the exact capability version, requires an
// already-made Authorization decision, enforces the P1A zero-effect rule and
// records evidence for every invocation and refusal.
type Gateway struct {
	registry *Registry
	evidence EvidenceSink
	now      func() time.Time
	// suspensions reports capabilities whose governing authority is suspended
	// (WF-RUN-039); nil enforces no suspension. See suspension.go.
	suspensions SuspensionSource
}

// GatewayOption configures a Gateway.
type GatewayOption func(*Gateway)

// WithClock replaces the gateway's source of evidence timestamps.
func WithClock(now func() time.Time) GatewayOption {
	return func(g *Gateway) { g.now = now }
}

// NewGateway builds a Gateway over a registry and an evidence sink.
func NewGateway(registry *Registry, evidence EvidenceSink, opts ...GatewayOption) *Gateway {
	g := &Gateway{registry: registry, evidence: evidence, now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Invoke resolves req.Capability, enforces authorization, effect class and
// status, and - only once every check passes - calls the bound handler.
// Every path, success or refusal, records one InvocationEvidence.
func (g *Gateway) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	rec, found := g.registry.Lookup(req.Capability)
	if !found {
		return g.refuse(ctx, req, CodeUnknownCapability, "capability is not registered")
	}

	handler, status, _ := g.registry.handlerFor(req.Capability)
	if status == StatusRetired {
		return g.refuse(ctx, req, CodeCapabilityDisabled, "capability version is retired")
	}

	// A suspended capability is refused before authorization is even read: the
	// suspension is a property of the capability, so no scope, envelope or
	// effect class can talk past it.
	if reason, suspended := g.suspended(ctx, req.Capability.ID); suspended {
		return g.refuse(ctx, req, CodeCapabilitySuspended, reason)
	}

	if req.Authorization.Decision != Allow {
		reason := req.Authorization.Reason
		if reason == "" {
			reason = "authorization decision is not ALLOW"
		}
		return g.refuse(ctx, req, CodeUnauthorized, reason)
	}
	if !req.Authorization.hasScope(rec.Definition.AuthZScopeRef) {
		return g.refuse(ctx, req, CodeUnauthorized, "authorization does not grant "+rec.Definition.AuthZScopeRef)
	}

	if req.Invocation != nil {
		now := g.now()
		if code, reason := req.Invocation.check(ctx, rec.Definition, now); code != "" {
			return g.refuse(ctx, req, code, reason)
		}
		// The deadline is measured on the gateway's clock, which a composition
		// may pin; the handler receives the remaining budget as a timeout.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Invocation.Deadline.Sub(now))
		defer cancel()
	}

	if rec.Definition.EffectClass.IsWrite() {
		return g.refuse(ctx, req, CodeWriteEffectRefusedP1A, "capability declares a write effect class; P1A invokes zero-effect capabilities only")
	}

	response, err := handler(ctx, req.Payload)
	if err != nil {
		return g.refuse(ctx, req, CodeHandlerFailed, err.Error())
	}

	evidenceID, evErr := g.evidence.RecordInvocation(ctx, req.Invocation.evidenceOf(InvocationEvidence{
		CapabilityID:      req.Capability.ID,
		CapabilityVersion: req.Capability.Version,
		SubjectRef:        req.Authorization.SubjectRef,
		Tenant:            req.Authorization.Tenant,
		Decision:          "INVOKED",
		OccurredAt:        g.now(),
	}, rec.Definition, true))
	if evErr != nil {
		return InvokeResult{}, evErr
	}

	return InvokeResult{Response: response, EvidenceID: evidenceID}, nil
}

// refuse records refusal evidence and returns the typed GatewayError. It
// never calls the handler.
func (g *Gateway) refuse(ctx context.Context, req InvokeRequest, code, reason string) (InvokeResult, error) {
	rec, found := g.registry.Lookup(req.Capability)
	evidenceID, evErr := g.evidence.RecordInvocation(ctx, req.Invocation.evidenceOf(InvocationEvidence{
		CapabilityID:      req.Capability.ID,
		CapabilityVersion: req.Capability.Version,
		SubjectRef:        req.Authorization.SubjectRef,
		Tenant:            req.Authorization.Tenant,
		Decision:          "REFUSED",
		ReasonCode:        code,
		OccurredAt:        g.now(),
	}, rec.Definition, found))
	if evErr != nil {
		evidenceID = ""
	}
	return InvokeResult{}, &GatewayError{
		Code:       code,
		Capability: req.Capability.ID,
		Version:    req.Capability.Version,
		Reason:     reason,
		EvidenceID: evidenceID,
	}
}
