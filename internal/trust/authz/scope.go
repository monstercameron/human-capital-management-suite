package authz

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// RelationshipKind names the kind of relationship that can authorize access
// to one record.
type RelationshipKind uint8

const (
	// RelationshipUnspecified is the zero value and is never a matched
	// relationship in a decision.
	RelationshipUnspecified RelationshipKind = iota
	// RelationshipSelf is the worker's relationship to their own record. It
	// is derived directly from the principal's identity, not from a
	// supplied fact.
	RelationshipSelf
	// RelationshipManagerChain is the direct-or-indirect reporting
	// relationship a manager holds over a worker.
	RelationshipManagerChain
	// RelationshipHRPartner is an HR business partner's assigned
	// organization or population scope over a worker.
	RelationshipHRPartner
	// RelationshipAssignedPopulation is an explicit population assignment
	// (a named cohort of workers) independent of the reporting hierarchy.
	RelationshipAssignedPopulation
	// RelationshipAdministrative is an administrative role grant (comp
	// admin, auditor) whose authority comes from the role itself, scoped by
	// the tenant/organization boundary, rather than from a relationship
	// fact about this specific worker.
	RelationshipAdministrative
)

var relationshipWire = map[RelationshipKind]string{
	RelationshipSelf:               "SELF",
	RelationshipManagerChain:       "MANAGER_CHAIN",
	RelationshipHRPartner:          "HR_PARTNER",
	RelationshipAssignedPopulation: "ASSIGNED_POPULATION",
	RelationshipAdministrative:     "ADMINISTRATIVE",
}

// String returns the stable wire token, or "RELATIONSHIP_UNSPECIFIED".
func (k RelationshipKind) String() string {
	if s, ok := relationshipWire[k]; ok {
		return s
	}
	return "RELATIONSHIP_UNSPECIFIED"
}

// relationshipRoles maps each relationship-gated kind to the roles it
// authorizes. RelationshipSelf and RelationshipAdministrative are handled
// separately: self-hood is derived from identity, and administrative access
// does not depend on a relationship fact at all.
var relationshipRoles = map[RelationshipKind][]RoleID{
	RelationshipManagerChain:       {RoleManager},
	RelationshipHRPartner:          {RoleHRPartner},
	RelationshipAssignedPopulation: {RoleManager, RoleHRPartner},
}

// relationshipEvaluationOrder fixes the priority a principal's several
// possible relationships to the same subject are considered in, so the
// matched relationship in a decision is deterministic regardless of input
// order.
var relationshipEvaluationOrder = []RelationshipKind{
	RelationshipSelf,
	RelationshipManagerChain,
	RelationshipHRPartner,
	RelationshipAssignedPopulation,
}

// relationshipRuleID is the stable rule-ID suffix for each relationship-
// gated kind, used to build the matched rule ID in a [Decision].
var relationshipRuleID = map[RelationshipKind]string{
	RelationshipManagerChain:       "p1a.scope.manager_chain",
	RelationshipHRPartner:          "p1a.scope.hr_partner",
	RelationshipAssignedPopulation: "p1a.scope.assigned_population",
}

// RelationshipFact is one bitemporal, source-attributed relationship
// projection: a claim that, as of Effective and known no later than
// RecordedAt, Source asserted this relationship kind held over Subject.
// [ResolveAuthorizationScope] never invents a relationship; every grant it
// makes beyond self-hood and administrative role grants traces to a fact
// like this one.
type RelationshipFact struct {
	Kind    RelationshipKind
	Subject values.EntityRef
	// Source attributes the fact to the system of record that projected it
	// (for example "organization.manager_chain.v3"). A fact without a
	// source is not usable: authority must be traceable to who asserted it.
	Source     string
	Effective  values.EffectiveInterval
	RecordedAt values.RecordedAt
	KnownAt    values.KnownAt
}

// Validate reports whether the fact is well formed enough to evaluate: a
// recognized relationship kind, a valid subject, a non-empty source, a valid
// effective interval, and bitemporal fields that satisfy
// [values.ValidateKnowledgeOrder].
func (f RelationshipFact) Validate() error {
	if _, ok := relationshipRoles[f.Kind]; !ok {
		return fmt.Errorf("%w: relationship fact has an unrecognized kind %s", ErrInvalidPolicyInput, f.Kind)
	}
	if err := f.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: relationship fact subject: %v", ErrInvalidPolicyInput, err)
	}
	if f.Source == "" {
		return fmt.Errorf("%w: relationship fact carries no source attribution", ErrInvalidPolicyInput)
	}
	if err := f.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: relationship fact effective interval: %v", ErrInvalidPolicyInput, err)
	}
	if err := values.ValidateKnowledgeOrder(f.KnownAt, f.RecordedAt, false); err != nil {
		return fmt.Errorf("%w: relationship fact: %v", ErrInvalidPolicyInput, err)
	}
	return nil
}

func (f RelationshipFact) coversSubjectAt(subject values.EntityRef, at values.Instant) bool {
	if f.Subject != subject {
		return false
	}
	ok, err := f.Effective.ContainsInstant(at)
	return err == nil && ok
}

// ScopeInput is the caller-supplied projection [ResolveAuthorizationScope]
// evaluates for one subject record.
type ScopeInput struct {
	// Subject is the worker or record the principal wants to reach.
	Subject values.EntityRef
	// EffectiveAt is the instant the scope is evaluated at.
	EffectiveAt values.Instant
	// Relationships is the candidate set of relationship facts between the
	// principal and Subject. Facts about a different subject are ignored
	// rather than rejected, so a caller may pass a wider projection.
	Relationships []RelationshipFact
	// EffectiveRoles is the principal's server-resolved role set, from
	// durable assignments. When non-nil it governs instead of the
	// principal's credential roles; a non-nil empty set authorizes
	// nothing. Nil keeps the legacy credential-role behavior for callers
	// with no role store.
	EffectiveRoles []string
}

// AuthorizationScope is the TRUST-009 result: whether a bitemporal,
// source-attributed relationship (or an administrative role grant)
// authorizes the principal to reach Subject at all.
type AuthorizationScope struct {
	Effect       Effect
	RuleID       string
	Reason       string
	Subject      values.EntityRef
	Relationship RelationshipKind
	// MatchedFact is the relationship fact that justified the grant, or nil
	// when the grant is self-hood or an administrative role grant that does
	// not depend on one.
	MatchedFact   *RelationshipFact
	PolicyVersion string
}

func denyScope(subject values.EntityRef) AuthorizationScope {
	return AuthorizationScope{
		Effect:        EffectDenied,
		RuleID:        "p1a.scope.deny_default",
		Reason:        "relationship_not_established",
		Subject:       subject,
		PolicyVersion: PolicyVersion,
	}
}

// ResolveAuthorizationScope implements TRUST-009: a principal outside every
// manager-chain, HR-partner or assigned-population relationship to Subject,
// and outside every administrative role grant, is denied. It never returns
// an allow built from an unauthorized relationship fact, and a denial always
// uses the same uniform reason token regardless of which relationships were
// considered, so that a caller cannot infer which almost matched.
func ResolveAuthorizationScope(principal *trust.Principal, req ScopeInput) (AuthorizationScope, error) {
	if principal == nil {
		return AuthorizationScope{}, fmt.Errorf("%w: nil principal", ErrInvalidPolicyInput)
	}
	if err := req.Subject.Validate(); err != nil {
		return AuthorizationScope{}, fmt.Errorf("%w: subject: %v", ErrInvalidPolicyInput, err)
	}

	held := RolesForKind(principal.SubjectKind(), effectiveRolesOf(principal, req.EffectiveRoles))
	heldSet := make(map[RoleID]struct{}, len(held))
	for _, r := range held {
		heldSet[r] = struct{}{}
	}

	for _, kind := range relationshipEvaluationOrder {
		switch kind {
		case RelationshipSelf:
			if _, ok := heldSet[RoleWorkerSelf]; !ok {
				continue
			}
			if req.Subject.Id != principal.Subject() {
				// Self-hood is an exact match between the accessed subject's
				// opaque id and the principal's own subject id: it is
				// derived from identity, never from a supplied fact.
				continue
			}
			return AuthorizationScope{
				Effect:        EffectAllow,
				RuleID:        "p1a.scope.self",
				Reason:        "self",
				Subject:       req.Subject,
				Relationship:  RelationshipSelf,
				PolicyVersion: PolicyVersion,
			}, nil
		default:
			roles, ok := relationshipRoles[kind]
			if !ok || !anyHeld(heldSet, roles) {
				continue
			}
			if fact, ok := matchRelationshipFact(req.Relationships, kind, req.Subject, req.EffectiveAt); ok {
				return AuthorizationScope{
					Effect:        EffectAllow,
					RuleID:        relationshipRuleID[kind],
					Reason:        "relationship_established",
					Subject:       req.Subject,
					Relationship:  kind,
					MatchedFact:   &fact,
					PolicyVersion: PolicyVersion,
				}, nil
			}
		}
	}

	for _, role := range administrativeRoleOrder {
		if _, ok := heldSet[role]; ok {
			return AuthorizationScope{
				Effect:        EffectAllow,
				RuleID:        "p1a.scope.administrative." + string(role),
				Reason:        "administrative_role_grant",
				Subject:       req.Subject,
				Relationship:  RelationshipAdministrative,
				PolicyVersion: PolicyVersion,
			}, nil
		}
	}

	return denyScope(req.Subject), nil
}

func anyHeld(held map[RoleID]struct{}, roles []RoleID) bool {
	for _, r := range roles {
		if _, ok := held[r]; ok {
			return true
		}
	}
	return false
}

// matchRelationshipFact returns the first valid fact of kind that covers
// subject at "at", choosing deterministically by source when more than one
// candidate matches.
func matchRelationshipFact(facts []RelationshipFact, kind RelationshipKind, subject values.EntityRef, at values.Instant) (RelationshipFact, bool) {
	var candidates []RelationshipFact
	for _, f := range facts {
		if f.Kind != kind {
			continue
		}
		if f.Validate() != nil {
			continue
		}
		if !f.coversSubjectAt(subject, at) {
			continue
		}
		candidates = append(candidates, f)
	}
	if len(candidates) == 0 {
		return RelationshipFact{}, false
	}
	slices.SortFunc(candidates, func(a, b RelationshipFact) int {
		return cmp.Compare(a.Source, b.Source)
	})
	return candidates[0], true
}

// FilterPopulation resolves scope independently for every request and
// returns only the subjects that are authorized, in the order their inputs
// were given filtered down. It is the query-authorization building block a
// population search uses: a candidate list is filtered down to a bounded,
// authorized subset, and an unauthorized candidate is dropped entirely
// rather than returned with an error attached, so that filtering can never
// leak an unauthorized row.
func FilterPopulation(principal *trust.Principal, requests []ScopeInput) ([]values.EntityRef, error) {
	out := make([]values.EntityRef, 0, len(requests))
	for _, req := range requests {
		scope, err := ResolveAuthorizationScope(principal, req)
		if err != nil {
			return nil, err
		}
		if scope.Effect == EffectAllow {
			out = append(out, req.Subject)
		}
	}
	return out, nil
}
