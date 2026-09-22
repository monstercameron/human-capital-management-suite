package authority

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

// Authority is the server-resolved authority for one capability call. Every
// field is resolved by the caller from durable server state, never from
// caller-chosen request values: a nil field means "none presented", and the
// decision treats it as no authority rather than as a wildcard.
type Authority struct {
	// EffectiveRoles is the server-resolved role set the call is evaluated
	// under: the durable assignment when the caller holds a role store, as
	// RBAC-RT-002 requires. Nil keeps the credential's own roles; a
	// non-nil set, even empty, governs as-is and never falls back.
	EffectiveRoles []string
	// GrantedScopes is the server-resolved client scope grant: the exact
	// capability scopes this caller holds. Nil means none were resolved,
	// which refuses every machine-kind call until INTAPI-001 registers
	// machine clients with explicit grants.
	GrantedScopes []string
	// Delegation is the server-verified effective authority the caller acts
	// under, or nil when the call claims no delegation. Token-carried
	// delegation references alone never populate this: only a grant the
	// server evaluated (see trust.EvaluateDelegation) counts.
	Delegation *trust.EffectiveAuthority
	// StepUp is the step-up obligation evaluated server-side for this call,
	// or nil when no proof was presented.
	StepUp *stepup.Obligation
	// DualApproval is the recorded dual approval resolved from durable
	// votes for this call, or nil when none was recorded. Resolving it
	// from durable state is the caller's duty: this decision checks
	// distinctness and binding, it cannot re-derive durability.
	DualApproval *DualApproval
}

// DualApproval is two recorded approvals for one proposal. The approvers
// must be two distinct subjects and the proposal reference must name the
// recorded decision; anything less is a single approval and does not
// satisfy the high-risk write gate.
type DualApproval struct {
	Tenant      string
	Capability  string
	ProposalRef string
	Approvers   []string
	DecidedAt   time.Time
}

// capabilityDomainPolicy maps the capability data-domain vocabulary onto the
// field-policy domains that govern it, wherever the semantics are exact.
// Domains the field policy does not classify (budget, ledgers, projections,
// the capability registry) have no mapping: a machine call touching one is
// refused until policy classifies it, while a human call is governed by the
// intent-resolution field policy downstream.
var capabilityDomainPolicy = map[string]authz.DataDomain{
	"worker":       authz.DomainCore,
	"employment":   authz.DomainCore,
	"position":     authz.DomainCore,
	"contact":      authz.DomainContact,
	"compensation": authz.DomainCompensation,
}

// domainRepresentative names one classified field per mapped domain. Grants
// are per domain, so one representative ruling decides its whole domain.
var domainRepresentative = map[authz.DataDomain]authz.FieldID{
	authz.DomainCore:         authz.FieldWorkerNumber,
	authz.DomainContact:      authz.FieldWorkEmail,
	authz.DomainCompensation: authz.FieldBaseSalary,
}

// Authorize decides one capability call: the intersection of the caller's
// granted scopes, the capability's scope and the field policy, over a
// server-verified delegation, with step-up or dual approval gating
// high-risk writes. It is pure: the caller resolves every Authority field
// from server state first, and the same inputs always decide the same way.
//
// Humans are authorized by purpose — the credential's purposes are
// server-resolved at verification, and field policy governs their reads at
// intent resolution — while machine kinds (service, agent, integration)
// additionally intersect their kind-gated template grants: a token-claimed
// role outside the caller's kind grants nothing, an unmapped data domain
// refuses, and a scope outside the resolved grant refuses. High-risk writes
// refuse without a bound step-up or a recorded dual approval, for every
// kind. The emitted decision never carries more than the capability's own
// scope, and never a wildcard.
func Authorize(principal *trust.Principal, purpose string, def capability.Definition, auth Authority, now time.Time) capability.Authorization {
	deny := func(reason string) capability.Authorization {
		decision := capability.Authorization{Decision: capability.Deny, Reason: reason}
		if principal != nil {
			decision.SubjectRef = principal.Subject()
			decision.Tenant = principal.Tenant().String()
		}
		return decision
	}
	if principal == nil {
		return deny("no verified principal")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if purpose != "" && !principal.AuthorizesPurpose(purpose) {
		return deny("the principal is not authorized for the resolved purpose of processing")
	}
	if strings.TrimSpace(def.AuthZScopeRef) == "" {
		return deny("the capability names no authorization scope")
	}

	roles := auth.EffectiveRoles
	if roles == nil {
		roles = principal.Roles()
	}

	if auth.Delegation != nil {
		if reason := checkDelegation(principal, def, purpose, auth.Delegation, now); reason != "" {
			return deny(reason)
		}
	}

	if requiresStepUp(def) {
		if !stepUpSatisfies(principal, purpose, def, auth.StepUp, now) &&
			!dualApprovalSatisfies(principal, def, auth.DualApproval, now) {
			return deny("a high-risk write requires a bound step-up or a recorded dual approval")
		}
	}

	if principal.SubjectKind() != trust.SubjectKindHuman {
		if !grantsScope(auth.GrantedScopes, def.AuthZScopeRef) {
			return deny("the capability scope is outside the caller's granted scopes")
		}
		if !machineFieldsAllow(principal, roles, purpose, def) {
			return deny("the field policy denies this capability's data to the caller")
		}
	}

	return capability.Authorization{
		Decision:   capability.Allow,
		SubjectRef: principal.Subject(),
		Tenant:     principal.Tenant().String(),
		Scopes:     []string{def.AuthZScopeRef},
	}
}

// checkDelegation verifies that a server-evaluated delegation actually
// covers this call: this principal as delegate, this tenant, the current
// instant, this capability and this purpose. An empty string admits the
// call; anything else is the refusal reason.
func checkDelegation(principal *trust.Principal, def capability.Definition, purpose string, d *trust.EffectiveAuthority, now time.Time) string {
	switch {
	case d.Delegate != principal.Subject():
		return "the verified delegation names another delegate"
	case d.Tenant != principal.Tenant():
		return "the verified delegation crosses tenant"
	case now.Before(d.NotBefore) || !now.Before(d.ExpiresAt):
		return "the verified delegation is outside its validity window"
	case !contains(d.Capabilities, def.ID):
		return "the verified delegation does not cover this capability"
	case purpose != "" && !contains(d.Purposes, purpose):
		return "the verified delegation does not cover this purpose"
	default:
		return ""
	}
}

// requiresStepUp reports whether def is a write of high risk class: an
// effect that can mutate state or produce an external effect, at the
// critical risk tier. Low-risk writes and every read pass without step-up;
// the P1A gateway's own zero-effect rule still refuses every write behind
// this decision.
func requiresStepUp(def capability.Definition) bool {
	return def.EffectClass.IsWrite() && stepup.RiskForCapabilityClass(def.RiskClass) >= stepup.RiskCritical
}

// stepUpSatisfies re-validates a server-evaluated step-up obligation against
// the live call. The evaluation already proved the proof authentic for its
// operation; this re-checks every coordinate visible here — tenant,
// subject, session, capability, purpose, risk tier, proof validity, recency
// and the principal's current assurance — so a satisfied obligation for
// another call, another moment or another session cannot be replayed into
// this one. A satisfied-but-unrequired obligation never counts: policy must
// have owed the step-up for it to gate a write.
func stepUpSatisfies(principal *trust.Principal, purpose string, def capability.Definition, ob *stepup.Obligation, now time.Time) bool {
	if ob == nil || !ob.Required || !ob.Satisfied {
		return false
	}
	switch {
	case ob.Tenant != principal.Tenant():
		return false
	case ob.Subject != principal.Subject() || ob.SessionRef != principal.SessionRef():
		return false
	case ob.Capability != def.ID || ob.Purpose != purpose:
		return false
	case ob.Risk < stepup.RiskCritical:
		return false
	case ob.EvaluatedAt.After(now) || now.Sub(ob.EvaluatedAt) > ob.Requirement.Recency:
		return false
	case now.Before(ob.IssuedAt) || !now.Before(ob.ExpiresAt):
		return false
	case !principal.Assurance().AtLeast(ob.Requirement.MinAssurance):
		return false
	case !principal.ExpiresAt().After(now):
		return false
	default:
		return true
	}
}

// dualApprovalSatisfies checks recorded dual approval for this call: two
// distinct approvers over a named recorded proposal for this capability in
// this tenant. Resolving the approvers from durable votes is the caller's
// duty; this checks the shape that makes two approvals dual.
func dualApprovalSatisfies(principal *trust.Principal, def capability.Definition, d *DualApproval, now time.Time) bool {
	if d == nil {
		return false
	}
	if d.Tenant == "" || d.Tenant != principal.Tenant().String() {
		return false
	}
	if d.Capability != def.ID || strings.TrimSpace(d.ProposalRef) == "" {
		return false
	}
	distinct := make(map[string]struct{}, len(d.Approvers))
	for _, a := range d.Approvers {
		if strings.TrimSpace(a) == "" {
			return false
		}
		distinct[a] = struct{}{}
	}
	if len(distinct) < 2 {
		return false
	}
	if d.DecidedAt.IsZero() || d.DecidedAt.After(now) {
		return false
	}
	return true
}

// machineFieldsAllow intersects the capability's classified data domains
// with the kind-gated template grants: every mapped domain must be allowed
// under an effective role for the call's purpose. A redacted ruling denies
// — this layer has no masking transform, and disclosing a raw value under a
// masked-only ruling is the exact failure the ruling exists to prevent. A
// capability touching an unmapped domain denies; one naming no domains
// passes vacuously, with the scope leg still enforced.
func machineFieldsAllow(principal *trust.Principal, roles []string, purpose string, def capability.Definition) bool {
	seen := make(map[authz.DataDomain]struct{})
	var fields []authz.FieldID
	for _, domain := range append(append([]string(nil), def.ReadData.DataDomains...), def.WriteData.DataDomains...) {
		mapped, ok := capabilityDomainPolicy[domain]
		if !ok {
			return false
		}
		if _, dup := seen[mapped]; dup {
			continue
		}
		seen[mapped] = struct{}{}
		rep, ok := domainRepresentative[mapped]
		if !ok {
			return false
		}
		fields = append(fields, rep)
	}
	if len(fields) == 0 {
		return true
	}
	decision, err := authz.ResolveFieldsWithRoles(principal, roles, purpose, fields, nil)
	if err != nil {
		return false
	}
	for _, f := range fields {
		if decision.Rulings[f].Effect != authz.EffectAllow {
			return false
		}
	}
	return true
}

func grantsScope(granted []string, scope string) bool {
	return contains(granted, scope)
}

func contains(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}
