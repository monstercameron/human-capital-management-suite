package capability

import (
	"context"
	"slices"
	"strings"
	"time"
)

// Stable refusal codes for the governed invocation envelope (WF-RUN-034).
const (
	// CodeInvocationInvalid refuses an envelope that names no purpose,
	// deadline, idempotency key or declared effect set.
	CodeInvocationInvalid = "CAPABILITY_INVOCATION_INVALID"
	// CodeDeadlineExceeded refuses an invocation whose deadline has passed
	// before the handler would run.
	CodeDeadlineExceeded = "CAPABILITY_DEADLINE_EXCEEDED"
	// CodeEffectUndeclared refuses a capability whose published effect class
	// is not in the caller's declared effect set.
	CodeEffectUndeclared = "CAPABILITY_EFFECT_UNDECLARED"
)

// Invocation is the governed execution envelope a workflow step presents
// with a capability call (specs/workflow-runtime.md "capability invocation
// contract"): the purpose of processing the authorization was decided for,
// the deadline the call must finish by, the idempotency key that names this
// exact attempt, and the effect classes the calling node is permitted to
// cause. A nil envelope keeps the P1A interactive contract, where the
// request's own transport deadline and zero-effect rule govern the call.
type Invocation struct {
	Purpose         string
	Deadline        time.Time
	IdempotencyKey  string
	DeclaredEffects []EffectClass
}

// check validates the envelope against the resolved definition at instant
// now. It returns the refusal code and reason, or "" when the envelope admits
// the call.
func (inv *Invocation) check(ctx context.Context, def Definition, now time.Time) (string, string) {
	switch {
	case strings.TrimSpace(inv.Purpose) == "":
		return CodeInvocationInvalid, "invocation names no purpose"
	case inv.Deadline.IsZero():
		return CodeInvocationInvalid, "invocation names no deadline"
	case strings.TrimSpace(inv.IdempotencyKey) == "":
		return CodeInvocationInvalid, "invocation names no idempotency key"
	case len(inv.DeclaredEffects) == 0:
		return CodeInvocationInvalid, "invocation declares no effect set"
	case ctx.Err() != nil || !now.Before(inv.Deadline):
		return CodeDeadlineExceeded, "invocation deadline has passed"
	case !slices.Contains(inv.DeclaredEffects, def.EffectClass):
		return CodeEffectUndeclared, "capability effect class " + string(def.EffectClass) + " is not in the declared effect set"
	}
	return "", ""
}

// evidenceOf copies the envelope's recorded fields onto an evidence record.
// A nil envelope leaves the record unchanged.
func (inv *Invocation) evidenceOf(evt InvocationEvidence, def Definition, found bool) InvocationEvidence {
	if inv == nil {
		return evt
	}
	evt.Purpose = inv.Purpose
	evt.IdempotencyKey = inv.IdempotencyKey
	evt.Deadline = inv.Deadline
	if found {
		evt.EffectClass = def.EffectClass
	}
	return evt
}
