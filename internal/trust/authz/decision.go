package authz

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrDecisionInvalid is returned by [Decision.Validate] when a decision is
// missing evidence a durable authorization record must carry.
var ErrDecisionInvalid = errors.New("authz: decision fails evidence validation")

// Request is everything [Evaluate] needs to produce one explainable
// authorization [Decision]: the principal, what they are trying to reach and
// under what declared purpose, and the caller-supplied projections
// (organization edges, sharing grants, relationship facts) the three
// resolution stages consume. Nothing in Request is fetched by this package;
// it is all handed in.
type Request struct {
	Principal *trust.Principal
	// EffectiveRoles is the principal's server-resolved role set, from
	// durable assignments. When non-nil it governs the scope and field
	// stages instead of the principal's credential roles; a non-nil empty
	// set authorizes nothing. Nil keeps the legacy credential-role
	// behavior for callers with no role store.
	EffectiveRoles []string
	// Purpose is the declared purpose of use. An empty value falls back to
	// the principal's default purpose.
	Purpose string
	// EffectiveAt is the instant every temporal check is evaluated at.
	EffectiveAt values.Instant
	// Subject is the record being accessed.
	Subject values.EntityRef
	// PrincipalOrg and ResourceOrg, Edges and Sharing feed
	// [ResolveTenantScope] (TRUST-008).
	PrincipalOrg OrgUnitRef
	ResourceOrg  OrgUnitRef
	OrgEdges     []OrgEdge
	Sharing      []SharingGrant
	// Relationships feeds [ResolveAuthorizationScope] (TRUST-009).
	Relationships []RelationshipFact
	// Fields feeds [ResolveFields] (TRUST-010).
	Fields []FieldID
}

// Decision is the TRUST-011 explainable authorization decision: the
// composed result of the tenant, scope and field resolution stages, plus
// everything a durable evidence record needs to prove what was decided and
// why, without ever reproducing a protected value.
type Decision struct {
	// PolicyVersions is the sorted, de-duplicated set of policy versions
	// evaluated to produce this decision. In the P1A bootstrap it is always
	// [PolicyVersion] alone; a later policy engine composing several policy
	// sources reports all of them here.
	PolicyVersions []string
	EvaluatedAt    values.Instant
	Purpose        string
	Assurance      trust.Assurance

	Tenant TenantScopeDecision
	Scope  AuthorizationScope

	// SubjectDisclosable reports whether the principal may learn that
	// Subject exists at all. It is false whenever the tenant or scope stage
	// denies: existence is itself protected.
	SubjectDisclosable bool
	// SubjectDenialReason is the uniform policy reason token for a
	// non-disclosable subject. It never distinguishes which stage denied
	// beyond that one reason.
	SubjectDenialReason string

	// Fields carries a ruling for every field in the request: ALLOW,
	// DENIED, REDACTED, or WITHHELD when the subject is not disclosable.
	Fields map[FieldID]FieldRuling

	// MatchedRules is the sorted, de-duplicated set of every rule ID that
	// fired anywhere in the decision.
	MatchedRules []string
	// Obligations is the sorted, de-duplicated set of every obligation
	// token attached to any field ruling.
	Obligations []string

	// InputsDigest is a canonical digest over every input this decision was
	// computed from. Two calls to [Evaluate] with the same inputs always
	// produce the same digest; any difference in input produces a
	// different one.
	InputsDigest string
	// EvidenceID is the durable identifier for this decision's evidence
	// record, derived from InputsDigest.
	EvidenceID string
}

// evaluate is the one pure evaluator behind [Enforce] and [Simulate]. There
// is no side-effecting enforcement path that could drift from a simulated
// one: both names call this and only this.
func evaluate(req Request) (Decision, error) {
	if req.Principal == nil {
		return Decision{}, fmt.Errorf("%w: nil principal", ErrInvalidPolicyInput)
	}

	purpose := req.Purpose
	if purpose == "" {
		purpose = req.Principal.DefaultPurpose()
	}

	tenantDecision, err := ResolveTenantScope(req.Principal, TenantScopeInput{
		ResourceTenant: req.Subject.Tenant,
		PrincipalOrg:   req.PrincipalOrg,
		ResourceOrg:    req.ResourceOrg,
		Edges:          req.OrgEdges,
		Sharing:        req.Sharing,
		EffectiveAt:    req.EffectiveAt,
	})
	if err != nil {
		return Decision{}, err
	}

	matchedRules := []string{tenantDecision.RuleID}
	matchedRules = append(matchedRules, tenantDecision.MandatoryDenies...)

	subjectDisclosable := tenantDecision.Effect == EffectAllow
	subjectDenialReason := ""
	if !subjectDisclosable {
		subjectDenialReason = tenantDecision.Reason
	}

	var scope AuthorizationScope
	if subjectDisclosable {
		scope, err = ResolveAuthorizationScope(req.Principal, ScopeInput{
			Subject:        req.Subject,
			EffectiveAt:    req.EffectiveAt,
			Relationships:  req.Relationships,
			EffectiveRoles: req.EffectiveRoles,
		})
		if err != nil {
			return Decision{}, err
		}
		matchedRules = append(matchedRules, scope.RuleID)
		if scope.Effect != EffectAllow {
			subjectDisclosable = false
			subjectDenialReason = scope.Reason
		}
	}

	var fieldDecision FieldDecision
	if subjectDisclosable {
		fieldDecision, err = ResolveFieldsWithRoles(req.Principal, effectiveRolesOf(req.Principal, req.EffectiveRoles), purpose, req.Fields, tenantDecision.MandatoryDenies)
		if err != nil {
			return Decision{}, err
		}
		for _, f := range req.Fields {
			matchedRules = append(matchedRules, fieldDecision.Rulings[f].RuleID)
		}
	} else {
		fieldDecision = withholdFields(purpose, req.Fields)
	}

	var obligations []string
	for _, f := range req.Fields {
		obligations = append(obligations, fieldDecision.Rulings[f].Obligations...)
	}

	decision := Decision{
		PolicyVersions:      dedupeSorted([]string{tenantDecision.PolicyVersion, fieldDecision.PolicyVersion, PolicyVersion}),
		EvaluatedAt:         req.EffectiveAt,
		Purpose:             purpose,
		Assurance:           req.Principal.Assurance(),
		Tenant:              tenantDecision,
		Scope:               scope,
		SubjectDisclosable:  subjectDisclosable,
		SubjectDenialReason: subjectDenialReason,
		Fields:              fieldDecision.Rulings,
		MatchedRules:        dedupeSorted(matchedRules),
		Obligations:         dedupeSorted(obligations),
	}
	decision.InputsDigest = canonicalRequestDigest(req, purpose)
	decision.EvidenceID = "ev:authz:" + decision.InputsDigest[:32]
	return decision, nil
}

// Enforce evaluates req and returns the decision an enforcement point (a
// repository, a serializer, a tool) must obey.
func Enforce(req Request) (Decision, error) { return evaluate(req) }

// Simulate evaluates req and returns the decision a policy simulator would
// show before publication. It is [Enforce] under another name: because
// evaluation is pure and has no side effects, a simulated decision and an
// enforced decision for the same inputs are always byte-for-byte identical,
// which is what makes them safe to compare.
func Simulate(req Request) (Decision, error) { return evaluate(req) }

// Validate reports whether d carries every piece of evidence a durable
// authorization record requires: policy versions, the matched rules that
// produced it, an evaluated scope, a field mask for a disclosable subject
// (or a uniform withholding for one that is not), the declared purpose, the
// principal's assurance level, and a digest/evidence identifier. A decision
// failing this check must never be recorded or enforced.
func (d Decision) Validate() error {
	if len(d.PolicyVersions) == 0 {
		return fmt.Errorf("%w: no policy version recorded", ErrDecisionInvalid)
	}
	if d.Purpose == "" {
		return fmt.Errorf("%w: no purpose recorded", ErrDecisionInvalid)
	}
	if d.Assurance == trust.AssuranceUnspecified {
		return fmt.Errorf("%w: no assurance level recorded", ErrDecisionInvalid)
	}
	if len(d.MatchedRules) == 0 {
		return fmt.Errorf("%w: no matched rule recorded", ErrDecisionInvalid)
	}
	if d.InputsDigest == "" || d.EvidenceID == "" {
		return fmt.Errorf("%w: no inputs digest or evidence id recorded", ErrDecisionInvalid)
	}
	if !d.Tenant.Effect.Valid() {
		return fmt.Errorf("%w: tenant scope effect is not recorded", ErrDecisionInvalid)
	}
	if d.Tenant.Effect == EffectDenied && d.Tenant.Reason == "" {
		return fmt.Errorf("%w: tenant scope denial carries no reason", ErrDecisionInvalid)
	}

	if !d.SubjectDisclosable {
		if d.SubjectDenialReason == "" {
			return fmt.Errorf("%w: non-disclosable subject carries no denial reason", ErrDecisionInvalid)
		}
		for field, ruling := range d.Fields {
			if ruling.Effect != EffectWithheld {
				return fmt.Errorf("%w: non-disclosable subject field %q is not withheld", ErrDecisionInvalid, field)
			}
		}
		return nil
	}

	if !d.Scope.Effect.Valid() || d.Scope.Effect != EffectAllow {
		return fmt.Errorf("%w: disclosable subject carries no allowed scope", ErrDecisionInvalid)
	}
	if d.Scope.Relationship == RelationshipUnspecified {
		return fmt.Errorf("%w: disclosable subject carries no matched relationship", ErrDecisionInvalid)
	}
	if d.Fields == nil {
		return fmt.Errorf("%w: no field mask recorded", ErrDecisionInvalid)
	}
	for field, ruling := range d.Fields {
		if !ruling.Effect.Valid() {
			return fmt.Errorf("%w: field %q carries no effect", ErrDecisionInvalid, field)
		}
		if ruling.Effect == EffectRedacted && len(ruling.Obligations) == 0 {
			return fmt.Errorf("%w: field %q is redacted without an obligation", ErrDecisionInvalid, field)
		}
		if (ruling.Effect == EffectDenied || ruling.Effect == EffectWithheld) && ruling.Reason == "" {
			return fmt.Errorf("%w: field %q is refused without a reason", ErrDecisionInvalid, field)
		}
	}
	return nil
}

// Explain renders a redaction-safe, deterministic summary of the decision:
// rule tokens, effects and counts only. It never reproduces a field value,
// and when the subject is not disclosable it collapses every field into one
// uniform withheld count instead of naming which fields or domains were
// involved, so the explanation itself cannot be used to infer whether the
// subject exists or what it would otherwise have granted.
func (d Decision) Explain() string {
	if !d.SubjectDisclosable {
		return fmt.Sprintf(
			"authz decision policy=%s purpose=%s assurance=%s subject_disclosable=false reason=%s fields_withheld=%d rules=%v",
			joinVersions(d.PolicyVersions), d.Purpose, d.Assurance, d.SubjectDenialReason, len(d.Fields), d.MatchedRules,
		)
	}

	fieldNames := make([]FieldID, 0, len(d.Fields))
	for f := range d.Fields {
		fieldNames = append(fieldNames, f)
	}
	slices.Sort(fieldNames)

	counts := map[Effect]int{}
	for _, f := range fieldNames {
		counts[d.Fields[f].Effect]++
	}

	return fmt.Sprintf(
		"authz decision policy=%s purpose=%s assurance=%s subject_disclosable=true scope=%s fields_allow=%d fields_redacted=%d fields_denied=%d obligations=%v rules=%v",
		joinVersions(d.PolicyVersions), d.Purpose, d.Assurance, d.Scope.Relationship,
		counts[EffectAllow], counts[EffectRedacted], counts[EffectDenied], d.Obligations, d.MatchedRules,
	)
}

func joinVersions(v []string) string {
	if len(v) == 0 {
		return ""
	}
	out := v[0]
	for _, s := range v[1:] {
		out += "," + s
	}
	return out
}

// canonicalRequestDigest hashes every input the decision was computed from
// with length-prefixed labeled framing, so that no two distinct inputs can
// collide by field-boundary ambiguity and the same inputs always hash the
// same regardless of caller-supplied slice order.
func canonicalRequestDigest(req Request, purpose string) string {
	h := sha256.New()
	write := func(label, v string) {
		fmt.Fprintf(h, "%s=%d:%s;", label, len(v), v)
	}

	write("principal", req.Principal.Fingerprint())
	roles := slices.Clone(effectiveRolesOf(req.Principal, req.EffectiveRoles))
	slices.Sort(roles)
	fmt.Fprintf(h, "effective_roles[%d]:", len(roles))
	for _, r := range roles {
		write("effective_role", r)
	}
	write("purpose", purpose)
	write("effective_at", req.EffectiveAt.String())
	write("subject", req.Subject.String())
	write("principal_org", req.PrincipalOrg.Tenant.String()+"/"+req.PrincipalOrg.ID)
	write("resource_org", req.ResourceOrg.Tenant.String()+"/"+req.ResourceOrg.ID)

	edges := slices.Clone(req.OrgEdges)
	slices.SortFunc(edges, func(a, b OrgEdge) int {
		ka := a.Child.Tenant.String() + "/" + a.Child.ID + ">" + a.Parent.Tenant.String() + "/" + a.Parent.ID
		kb := b.Child.Tenant.String() + "/" + b.Child.ID + ">" + b.Parent.Tenant.String() + "/" + b.Parent.ID
		if ka < kb {
			return -1
		}
		if ka > kb {
			return 1
		}
		return 0
	})
	fmt.Fprintf(h, "edges[%d]:", len(edges))
	for _, e := range edges {
		write("edge.child", e.Child.Tenant.String()+"/"+e.Child.ID)
		write("edge.parent", e.Parent.Tenant.String()+"/"+e.Parent.ID)
		write("edge.effective", e.Effective.String())
	}

	sharing := slices.Clone(req.Sharing)
	slices.SortFunc(sharing, func(a, b SharingGrant) int {
		ka := a.OwnerTenant.String() + ">" + a.ViewerTenant.String()
		kb := b.OwnerTenant.String() + ">" + b.ViewerTenant.String()
		if ka < kb {
			return -1
		}
		if ka > kb {
			return 1
		}
		return 0
	})
	fmt.Fprintf(h, "sharing[%d]:", len(sharing))
	for _, g := range sharing {
		write("sharing.owner", g.OwnerTenant.String())
		write("sharing.viewer", g.ViewerTenant.String())
		write("sharing.direction", g.Direction.String())
		write("sharing.effective", g.Effective.String())
	}

	relationships := slices.Clone(req.Relationships)
	slices.SortFunc(relationships, func(a, b RelationshipFact) int {
		ka := a.Kind.String() + a.Subject.String() + a.Source
		kb := b.Kind.String() + b.Subject.String() + b.Source
		if ka < kb {
			return -1
		}
		if ka > kb {
			return 1
		}
		return 0
	})
	fmt.Fprintf(h, "relationships[%d]:", len(relationships))
	for _, rel := range relationships {
		write("rel.kind", rel.Kind.String())
		write("rel.subject", rel.Subject.String())
		write("rel.source", rel.Source)
		write("rel.effective", rel.Effective.String())
		write("rel.recorded_at", rel.RecordedAt.String())
		write("rel.known_at", rel.KnownAt.String())
	}

	fields := slices.Clone(req.Fields)
	slices.Sort(fields)
	fmt.Fprintf(h, "fields[%d]:", len(fields))
	for _, f := range fields {
		write("field", string(f))
	}

	return hex.EncodeToString(h.Sum(nil))
}
