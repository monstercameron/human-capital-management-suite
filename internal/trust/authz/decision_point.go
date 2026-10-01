package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/breakglass"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

// ErrDecisionPointDenied identifies a decision refused by one of the
// additional authority bounds composed by the policy decision point.
var ErrDecisionPointDenied = errors.New("authz: policy decision point denied")

// ErrSensitiveAuditRequired is returned when a sensitive decision has no
// durable evidence sink. Sensitive authorization must fail closed when its
// evidence cannot be recorded.
var ErrSensitiveAuditRequired = errors.New("authz: sensitive decision audit is required")

// SensitiveDecisionAudit records the redaction-safe evidence for a sensitive
// authorization decision. Implementations must make the record durable.
type SensitiveDecisionAudit interface {
	RecordSensitiveDecision(context.Context, SensitiveDecisionEvidence) error
}

// SensitiveDecisionEvidence is the minimum evidence needed to review a
// sensitive decision. It deliberately excludes payloads and protected values.
type SensitiveDecisionEvidence struct {
	DecisionID string
	Principal  string
	Actor      string
	Capability string
	Outcome    string
	Reason     string
	OccurredAt time.Time
}

// DecisionPointRequest supplies the ordinary policy request and any
// server-evaluated, narrowly bounded authority that may accompany it. A
// caller cannot use these fields to widen a decision: each is intersected
// with the ordinary policy result and checked against the exact call.
type DecisionPointRequest struct {
	Policy     Request
	Capability string
	Actor      string
	// Delegation, JIT and BreakGlass are authoritative only when populated by
	// the server composition root after durable lookup and evaluation. These
	// exported references cannot prove provenance themselves; transports must
	// never decode them from caller input or token claims.
	Delegation *trust.EffectiveAuthority
	JIT        *jit.Grant
	BreakGlass *breakglass.Grant
	Sensitive  bool
	Action     string
}

// DecisionPoint composes the one pure policy evaluator with delegation, JIT,
// break-glass lifecycle checks and sensitive-decision evidence.
type DecisionPoint struct {
	audit SensitiveDecisionAudit
	now   func() time.Time
}

// DecisionPointOption configures a DecisionPoint.
type DecisionPointOption func(*DecisionPoint)

// WithDecisionPointClock pins the decision point's clock for deterministic
// expiry and evidence tests.
func WithDecisionPointClock(now func() time.Time) DecisionPointOption {
	return func(p *DecisionPoint) {
		if now != nil {
			p.now = now
		}
	}
}

// NewDecisionPoint constructs the unified policy decision point.
func NewDecisionPoint(audit SensitiveDecisionAudit, opts ...DecisionPointOption) *DecisionPoint {
	p := &DecisionPoint{audit: audit, now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Decide evaluates one call through the shared policy evaluator and then
// applies every supplied bounded authority. Base policy denial cannot be
// bypassed by delegation, JIT or break glass. Successful JIT and break-glass
// calls record their lifecycle use only after the complete decision allows.
func (p *DecisionPoint) Decide(ctx context.Context, req DecisionPointRequest) (Decision, error) {
	if p == nil {
		return Decision{}, fmt.Errorf("%w: nil decision point", ErrDecisionPointDenied)
	}
	if strings.TrimSpace(req.Capability) == "" || strings.TrimSpace(req.Actor) == "" {
		return Decision{}, fmt.Errorf("%w: capability and actor are required", ErrDecisionPointDenied)
	}
	decision, err := Enforce(req.Policy)
	if err != nil {
		return Decision{}, err
	}
	if err := decision.Validate(); err != nil {
		return Decision{}, err
	}
	if !decision.SubjectDisclosable {
		return p.finish(ctx, req, decision, "DENIED", decision.SubjectDenialReason)
	}
	at := req.Policy.EffectiveAt.Time()
	if req.Delegation != nil {
		if reason := delegationDenial(req, decision, at); reason != "" {
			return p.finish(ctx, req, decision, "DENIED", reason)
		}
	}
	if req.JIT != nil {
		if reason := jitDenial(req, decision, at); reason != "" {
			return p.finish(ctx, req, decision, "DENIED", reason)
		}
	}
	if req.BreakGlass != nil {
		if reason := breakGlassDenial(req, decision, at); reason != "" {
			return p.finish(ctx, req, decision, "DENIED", reason)
		}
	}
	// Sensitive evidence is committed before grant lifecycle mutation. This
	// ordering means an unavailable evidence sink cannot consume a JIT or
	// break-glass grant. The grant checks above are repeated by Use, which
	// closes the small race where a grant expires between validation and use.
	if req.Sensitive {
		if err := p.auditSensitive(ctx, req, decision, "ALLOWED", ""); err != nil {
			return Decision{}, err
		}
	}
	if req.JIT != nil {
		if err := req.JIT.Use(req.Action, at); err != nil {
			return p.finish(ctx, req, decision, "DENIED", err.Error())
		}
	}
	if req.BreakGlass != nil {
		if err := req.BreakGlass.Use(req.Capability, req.Action, at); err != nil {
			return p.finish(ctx, req, decision, "DENIED", err.Error())
		}
	}
	if req.Sensitive {
		return decision, nil
	}
	return p.finish(ctx, req, decision, "ALLOWED", "")
}

func (p *DecisionPoint) finish(ctx context.Context, req DecisionPointRequest, decision Decision, outcome, reason string) (Decision, error) {
	if !req.Sensitive {
		if outcome == "DENIED" {
			return Decision{}, fmt.Errorf("%w: %s", ErrDecisionPointDenied, reason)
		}
		return decision, nil
	}
	if err := p.auditSensitive(ctx, req, decision, outcome, reason); err != nil {
		return Decision{}, err
	}
	if outcome == "DENIED" {
		return Decision{}, fmt.Errorf("%w: %s", ErrDecisionPointDenied, reason)
	}
	return decision, nil
}

func (p *DecisionPoint) auditSensitive(ctx context.Context, req DecisionPointRequest, decision Decision, outcome, reason string) error {
	if p.audit == nil {
		return ErrSensitiveAuditRequired
	}
	if err := p.audit.RecordSensitiveDecision(ctx, SensitiveDecisionEvidence{
		DecisionID: decision.EvidenceID, Principal: req.Policy.Principal.Subject(), Actor: req.Actor,
		Capability: req.Capability, Outcome: outcome, Reason: reason, OccurredAt: p.now().UTC(),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrSensitiveAuditRequired, err)
	}
	return nil
}

func delegationDenial(req DecisionPointRequest, decision Decision, at time.Time) string {
	d := req.Delegation
	if d.Delegate != req.Actor || d.Tenant != req.Policy.Principal.Tenant() || !at.Before(d.ExpiresAt) || at.Before(d.NotBefore) {
		return "delegation is not current for this actor and tenant"
	}
	if !contains(d.Capabilities, req.Capability) || !contains(d.Purposes, decision.Purpose) || !contains(d.Resources, req.Policy.Subject.String()) {
		return "delegation does not cover this capability, purpose or subject"
	}
	for field := range decision.Fields {
		if len(d.Fields) > 0 && !contains(d.Fields, string(field)) {
			return "delegation does not cover every requested field"
		}
	}
	return ""
}

func jitDenial(req DecisionPointRequest, decision Decision, at time.Time) string {
	g := req.JIT
	if g.Principal != req.Actor || g.Tenant != req.Policy.Principal.Tenant() || !g.IsActive(at) || !contains(g.Capabilities, req.Capability) || g.Purpose != decision.Purpose {
		return "JIT grant is not current or does not cover this call"
	}
	for field := range decision.Fields {
		if len(g.Fields) > 0 && !contains(g.Fields, string(field)) {
			return "JIT grant does not cover every requested field"
		}
	}
	return ""
}

func breakGlassDenial(req DecisionPointRequest, decision Decision, at time.Time) string {
	g := req.BreakGlass
	if g.User != req.Actor || !g.IsActive(at) || !contains(g.Capabilities, req.Capability) {
		return "break-glass grant is not current or does not cover this call"
	}
	return ""
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
