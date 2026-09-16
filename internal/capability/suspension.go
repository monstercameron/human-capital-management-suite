package capability

import "context"

// CodeCapabilitySuspended refuses an invocation of a capability whose
// governing authority is suspended right now (WF-RUN-039). It is not an
// authorization refusal: the caller may hold every scope the definition names
// and still be refused, because the suspension is a property of the
// capability, not of the caller.
const CodeCapabilitySuspended = "CAPABILITY_SUSPENDED"

// SuspensionSource reports whether a capability may be invoked at this
// instant, independently of who is calling. The concrete source lives outside
// this package -- the operator gateway suspends an authority family whose
// bypass obligation is past its due review -- and this package only enforces
// the answer it is handed, exactly as it does for an [Authorization].
//
// A source that cannot answer must report the capability suspended: a
// governance control that fails open is not a control.
type SuspensionSource interface {
	SuspendedCapability(ctx context.Context, id string) (reason string, suspended bool)
}

// WithSuspensions makes the gateway consult src before every invocation. A
// gateway composed without it enforces no suspension at all, which is the
// behaviour every caller had before WF-RUN-039.
func WithSuspensions(src SuspensionSource) GatewayOption {
	return func(g *Gateway) { g.suspensions = src }
}

// suspended reports the refusal reason when the gateway's suspension source
// refuses id, and "" when there is no source or the capability is admitted.
func (g *Gateway) suspended(ctx context.Context, id string) (string, bool) {
	if g.suspensions == nil {
		return "", false
	}
	reason, suspended := g.suspensions.SuspendedCapability(ctx, id)
	if !suspended {
		return "", false
	}
	if reason == "" {
		reason = "capability is suspended"
	}
	return reason, true
}
