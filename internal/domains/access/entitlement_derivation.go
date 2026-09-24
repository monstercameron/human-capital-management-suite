package access

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
	rulesengine "github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DerivationStatus is the fail-closed result for one identity/entitlement
// edge. Only EXPECTED edges are materialized as ExpectedEntitlement records.
type DerivationStatus string

const (
	DerivationExpected    DerivationStatus = "EXPECTED"
	DerivationNotExpected DerivationStatus = "NOT_EXPECTED"
	DerivationConditional DerivationStatus = "CONDITIONAL"
	DerivationUnknown     DerivationStatus = "UNKNOWN"
)

var (
	ErrInvalidDerivation       = errors.New("access: invalid entitlement derivation")
	ErrObservationInput        = errors.New("access: external observation cannot be a derivation fact")
	ErrMissingGovernedFact     = errors.New("access: governed workforce fact is missing")
	ErrInvalidDerivationPolicy = errors.New("access: invalid declared access policy")
	ErrInvalidEntitlementDelta = errors.New("access: invalid entitlement delta")
)

// EmploymentPeriod is a governed, effective-dated employment fact. Ref is
// intentionally opaque: the derivation carries it as evidence without
// interpreting employee-sensitive attributes.
type EmploymentPeriod struct {
	Tenant              values.TenantId
	Ref                 string
	WorkforceIdentityID string
	Effective           values.EffectiveInterval
}

// PositionAssignment is a governed position and org-unit fact.
type PositionAssignment struct {
	Tenant              values.TenantId
	Ref                 string
	WorkforceIdentityID string
	PositionID          string
	OrgUnitRef          string
	Effective           values.EffectiveInterval
}

// AccessPolicyEffect is the closed policy decision vocabulary. DENY wins over
// every allow or conditional policy that applies to the same edge.
type AccessPolicyEffect string

const (
	AccessPolicyAllow       AccessPolicyEffect = "ALLOW"
	AccessPolicyDeny        AccessPolicyEffect = "DENY"
	AccessPolicyConditional AccessPolicyEffect = "CONDITIONAL"
)

// DeclaredAccessPolicy maps governed workforce selectors to one entitlement.
// An empty selector is a tenant policy applying to every active identity;
// non-empty selectors require the corresponding fact to be present at AsOf.
type DeclaredAccessPolicy struct {
	Tenant        values.TenantId
	ID            string
	Version       string
	EntitlementID string
	EmploymentRef string
	PositionRef   string
	OrgUnitRef    string
	Effect        AccessPolicyEffect
	Effective     values.EffectiveInterval
}

func (p DeclaredAccessPolicy) policyRef() string { return p.ID + "@" + p.Version }

// EntitlementBasisRef is a redaction-safe reference to one governed input.
type EntitlementBasisRef struct {
	Kind string
	Ref  string
}

func (b EntitlementBasisRef) Canonical() []byte {
	if strings.TrimSpace(b.Kind) == "" || strings.TrimSpace(b.Ref) == "" {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.access.EntitlementBasisRef", 1).
		String("kind", b.Kind).String("ref", b.Ref).Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// DerivedEntitlementEdge is a deterministic decision edge. OrgUnitRef and
// Basis are retained here because ACCESS-001's ExpectedEntitlement shape has
// only the three original basis columns.
type DerivedEntitlementEdge struct {
	IdentityID    string
	EntitlementID string
	Status        DerivationStatus
	Effective     values.EffectiveInterval
	Basis         []EntitlementBasisRef
	OrgUnitRef    string
	Expected      ExpectedEntitlement
}

func (e DerivedEntitlementEdge) key() string { return e.IdentityID + "\x00" + e.EntitlementID }

// EntitlementDerivationRequest pins every input needed for replay. The
// metadata fields are copied onto generated native ExpectedEntitlement
// revisions; no wall clock or provider state is read.
type EntitlementDerivationRequest struct {
	Graph        Graph
	AsOf         values.Instant
	Employment   []EmploymentPeriod
	Positions    []PositionAssignment
	Policies     []DeclaredAccessPolicy
	Observations []ExternalAccessObservation
	Revision     values.RevisionToken
	KnownAt      values.KnownAt
	Provenance   evidence.Provenance
}

// ExpectedEntitlementCalculation is the complete deterministic result. The
// Expected slice is the native subset; Decisions retains all four statuses.
type ExpectedEntitlementCalculation struct {
	AsOf            values.Instant
	Decisions       []DerivedEntitlementEdge
	Expected        []ExpectedEntitlement
	CanonicalDigest string
}

func (r ExpectedEntitlementCalculation) Digest() string { return r.CanonicalDigest }

func (r ExpectedEntitlementCalculation) Canonical() []byte {
	if r.AsOf.Validate() != nil || r.CanonicalDigest == "" {
		return nil
	}
	edges := append([]DerivedEntitlementEdge(nil), r.Decisions...)
	sort.Slice(edges, func(i, j int) bool { return edges[i].key() < edges[j].key() })
	w := canonicalbytes.New("hcmnext.domains.access.ExpectedEntitlementCalculation", 1).
		Value("as_of", r.AsOf).Count("decisions", len(edges))
	for _, edge := range edges {
		w.String("identity", edge.IdentityID).
			String("entitlement", edge.EntitlementID).
			String("status", string(edge.Status)).
			Value("effective", edge.Effective)
		w.Count("basis", len(edge.Basis))
		for _, basis := range edge.Basis {
			w.Value("basis_ref", basis)
		}
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ExpectedEntitlementExplanation is a bounded trace that contains stable
// identifiers, statuses, and basis kinds only. It never copies raw workforce
// attributes or provider observations.
type ExpectedEntitlementExplanation struct {
	AsOf            values.Instant
	CanonicalDigest string
	ExpectedCount   int
	UnknownCount    int
	Lines           []string
}

func (r ExpectedEntitlementCalculation) Explain() ExpectedEntitlementExplanation {
	lines := make([]string, 0, len(r.Decisions))
	unknown := 0
	for _, edge := range r.Decisions {
		if edge.Status == DerivationUnknown {
			unknown++
		}
		kinds := make([]string, 0, len(edge.Basis))
		for _, basis := range edge.Basis {
			kinds = append(kinds, basis.Kind)
		}
		sort.Strings(kinds)
		lines = append(lines, fmt.Sprintf("%s %s -> %s basis=%s", edge.IdentityID, edge.EntitlementID, edge.Status, strings.Join(kinds, ",")))
	}
	sort.Strings(lines)
	return ExpectedEntitlementExplanation{
		AsOf: r.AsOf, CanonicalDigest: r.CanonicalDigest,
		ExpectedCount: len(r.Expected), UnknownCount: unknown, Lines: lines,
	}
}

func ExplainExpectedEntitlements(r ExpectedEntitlementCalculation) ExpectedEntitlementExplanation {
	return r.Explain()
}

func validateInstantInterval(name string, interval values.EffectiveInterval) error {
	if err := interval.Validate(); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidDerivation, name, err)
	}
	if _, ok := interval.StartInstant(); !ok {
		return fmt.Errorf("%w: %s must be an instant interval", ErrInvalidDerivation, name)
	}
	return nil
}

func (r EntitlementDerivationRequest) validate() error {
	if err := r.Graph.Validate(); err != nil {
		return err
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as_of: %v", ErrInvalidDerivation, err)
	}
	if len(r.Observations) != 0 || len(r.Graph.Observations) != 0 {
		return ErrObservationInput
	}
	if !r.Revision.IsSpecified() || r.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: revision and known_at are required", ErrInvalidDerivation)
	}
	if err := r.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: provenance: %v", ErrInvalidDerivation, err)
	}
	if err := values.ValidateKnowledgeOrder(r.KnownAt, r.Provenance.RecordedAt, false); err != nil {
		return fmt.Errorf("%w: knowledge order: %v", ErrInvalidDerivation, err)
	}
	identityIDs := make(map[string]struct{}, len(r.Graph.Identities))
	for _, identity := range r.Graph.Identities {
		identityIDs[identity.ID] = struct{}{}
	}
	seen := make(map[string]struct{})
	for _, fact := range r.Employment {
		if fact.Tenant != r.Graph.Tenant {
			return fmt.Errorf("%w: employment %s belongs to another tenant", ErrTenantMismatch, fact.Ref)
		}
		if strings.TrimSpace(fact.Ref) == "" || strings.TrimSpace(fact.WorkforceIdentityID) == "" {
			return fmt.Errorf("%w: employment ref and identity are required", ErrInvalidDerivation)
		}
		if _, ok := identityIDs[fact.WorkforceIdentityID]; !ok {
			return fmt.Errorf("%w: employment %s names an unknown identity", ErrMissingGovernedFact, fact.Ref)
		}
		if err := validateInstantInterval("employment "+fact.Ref, fact.Effective); err != nil {
			return err
		}
		if _, duplicate := seen["employment\x00"+fact.Ref]; duplicate {
			return fmt.Errorf("%w: duplicate employment %s", ErrInvalidDerivation, fact.Ref)
		}
		seen["employment\x00"+fact.Ref] = struct{}{}
	}
	seen = make(map[string]struct{})
	for _, fact := range r.Positions {
		if fact.Tenant != r.Graph.Tenant {
			return fmt.Errorf("%w: position %s belongs to another tenant", ErrTenantMismatch, fact.Ref)
		}
		if strings.TrimSpace(fact.Ref) == "" || strings.TrimSpace(fact.WorkforceIdentityID) == "" || strings.TrimSpace(fact.PositionID) == "" {
			return fmt.Errorf("%w: position ref, identity, and position id are required", ErrInvalidDerivation)
		}
		if _, ok := identityIDs[fact.WorkforceIdentityID]; !ok {
			return fmt.Errorf("%w: position %s names an unknown identity", ErrMissingGovernedFact, fact.Ref)
		}
		if err := validateInstantInterval("position "+fact.Ref, fact.Effective); err != nil {
			return err
		}
		if _, duplicate := seen["position\x00"+fact.Ref]; duplicate {
			return fmt.Errorf("%w: duplicate position %s", ErrInvalidDerivation, fact.Ref)
		}
		seen["position\x00"+fact.Ref] = struct{}{}
	}
	entitlements := make(map[string]struct{}, len(r.Graph.Entitlements))
	for _, entitlement := range r.Graph.Entitlements {
		entitlements[entitlement.ID] = struct{}{}
	}
	seen = make(map[string]struct{})
	for _, policy := range r.Policies {
		if policy.Tenant != r.Graph.Tenant {
			return fmt.Errorf("%w: policy %s belongs to another tenant", ErrTenantMismatch, policy.ID)
		}
		if strings.TrimSpace(policy.ID) == "" || strings.TrimSpace(policy.Version) == "" {
			return fmt.Errorf("%w: policy id and version are required", ErrInvalidDerivationPolicy)
		}
		if _, ok := entitlements[policy.EntitlementID]; !ok {
			return fmt.Errorf("%w: policy %s names an unknown entitlement", ErrInvalidDerivationPolicy, policy.ID)
		}
		if policy.Effect != AccessPolicyAllow && policy.Effect != AccessPolicyDeny && policy.Effect != AccessPolicyConditional {
			return fmt.Errorf("%w: policy %s has unknown effect", ErrInvalidDerivationPolicy, policy.ID)
		}
		if err := validateInstantInterval("policy "+policy.ID, policy.Effective); err != nil {
			return err
		}
		if _, duplicate := seen[policy.policyRef()]; duplicate {
			return fmt.Errorf("%w: duplicate policy %s", ErrInvalidDerivationPolicy, policy.policyRef())
		}
		seen[policy.policyRef()] = struct{}{}
	}
	return nil
}

func activeAt(interval values.EffectiveInterval, asOf values.Instant) bool {
	ok, err := interval.ContainsInstant(asOf)
	return err == nil && ok
}

func intersection(intervals ...values.EffectiveInterval) (values.EffectiveInterval, error) {
	if len(intervals) == 0 {
		return values.EffectiveInterval{}, fmt.Errorf("%w: no effective intervals", ErrInvalidDerivation)
	}
	start, _ := intervals[0].StartInstant()
	var end values.Instant
	hasEnd := false
	if candidate, ok := intervals[0].EndInstant(); ok {
		end, hasEnd = candidate, true
	}
	for _, interval := range intervals[1:] {
		candidate, _ := interval.StartInstant()
		if candidate.After(start) {
			start = candidate
		}
		if candidateEnd, ok := interval.EndInstant(); ok && (!hasEnd || candidateEnd.Before(end)) {
			end, hasEnd = candidateEnd, true
		}
	}
	if hasEnd {
		return values.NewInstantInterval(start, end)
	}
	return values.NewOpenInstantInterval(start)
}

func sortedEmployment(facts []EmploymentPeriod, identity string, asOf values.Instant) []EmploymentPeriod {
	result := make([]EmploymentPeriod, 0)
	for _, fact := range facts {
		if fact.WorkforceIdentityID == identity && activeAt(fact.Effective, asOf) {
			result = append(result, fact)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Ref < result[j].Ref })
	return result
}

func sortedPositions(facts []PositionAssignment, identity string, asOf values.Instant) []PositionAssignment {
	result := make([]PositionAssignment, 0)
	for _, fact := range facts {
		if fact.WorkforceIdentityID == identity && activeAt(fact.Effective, asOf) {
			result = append(result, fact)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Ref < result[j].Ref })
	return result
}

type policyMatch struct {
	policy     DeclaredAccessPolicy
	Employment *EmploymentPeriod
	Position   *PositionAssignment
	Evaluation EntitlementBasisRef
}

type derivationFacts struct {
	employment []EmploymentPeriod
	positions  []PositionAssignment
	policyRef  string
	targets    map[string]string
}

func (f derivationFacts) ReadFact(_ context.Context, _ values.EntityRef, field string, _ values.EffectiveInterval) (eligibility.Fact, error) {
	var refs []string
	switch field {
	case "employment_ref":
		for _, fact := range f.employment {
			refs = append(refs, fact.Ref)
		}
	case "position_ref":
		for _, fact := range f.positions {
			if f.targets["org_unit_ref"] == "" || fact.OrgUnitRef == f.targets["org_unit_ref"] {
				refs = append(refs, fact.Ref)
			}
		}
	case "org_unit_ref":
		for _, fact := range f.positions {
			if fact.OrgUnitRef != "" && (f.targets["position_ref"] == "" || fact.Ref == f.targets["position_ref"]) {
				refs = append(refs, fact.OrgUnitRef)
			}
		}
	case "policy_scope":
		refs = []string{f.policyRef}
	default:
		return eligibility.Fact{Presence: values.Unknown[string]("undeclared access selector")}, nil
	}
	if len(refs) == 0 {
		return eligibility.Fact{Presence: values.Unknown[string]("governed selector fact is missing")}, nil
	}
	sort.Strings(refs)
	target := f.targets[field]
	for _, ref := range refs {
		if ref == target {
			return eligibility.Fact{Presence: values.Value(ref)}, nil
		}
	}
	return eligibility.Fact{Presence: values.Value(refs[0])}, nil
}

func evaluatePolicy(policy DeclaredAccessPolicy, employment []EmploymentPeriod, positions []PositionAssignment, identity WorkforceIdentity) (policyMatch, eligibility.Status, error) {
	match := policyMatch{policy: policy}
	children := []eligibility.Condition{{Kind: eligibility.ConditionEquals, Field: "policy_scope", Value: policy.policyRef()}}
	factCatalog := eligibility.FactCatalog{"policy_scope": {Type: eligibility.FieldTypeString, Readable: true}}
	if policy.EmploymentRef != "" {
		children = append(children, eligibility.Condition{Kind: eligibility.ConditionEquals, Field: "employment_ref", Value: policy.EmploymentRef})
		factCatalog["employment_ref"] = eligibility.FactDescriptor{Type: eligibility.FieldTypeString, Readable: true}
	}
	if policy.PositionRef != "" {
		children = append(children, eligibility.Condition{Kind: eligibility.ConditionEquals, Field: "position_ref", Value: policy.PositionRef})
		factCatalog["position_ref"] = eligibility.FactDescriptor{Type: eligibility.FieldTypeString, Readable: true}
	}
	if policy.OrgUnitRef != "" {
		children = append(children, eligibility.Condition{Kind: eligibility.ConditionEquals, Field: "org_unit_ref", Value: policy.OrgUnitRef})
		factCatalog["org_unit_ref"] = eligibility.FactDescriptor{Type: eligibility.FieldTypeString, Readable: true}
	}
	criteria := eligibility.Criteria{Root: eligibility.Condition{Kind: eligibility.ConditionAnd, Children: children}}
	plan, err := eligibility.Compile(criteria, factCatalog, eligibility.RuleCatalog{})
	if err != nil {
		return policyMatch{}, eligibility.StatusUnspecified, err
	}
	subjectMatter := eligibility.SubjectMatterRef{Kind: eligibility.SubjectMatterAction, ID: policy.ID, Revision: policy.Version}
	req := eligibility.Request{Subject: identity.WorkerRef, SubjectMatter: subjectMatter, RequestedInterval: identity.Effective, EffectiveInterval: identity.Effective, Jurisdiction: "GLOBAL", Snapshots: eligibility.Snapshots{PopulationSnapshotRef: "access:" + string(identity.Tenant), FactSnapshotRef: "access:" + string(identity.Tenant), RuleSnapshotRef: "access-policy:" + policy.policyRef()}, Purpose: "access entitlement derivation", Authority: "native access policy"}
	targets := map[string]string{"policy_scope": policy.policyRef(), "employment_ref": policy.EmploymentRef, "position_ref": policy.PositionRef, "org_unit_ref": policy.OrgUnitRef}
	result, err := eligibility.Evaluate(context.Background(), derivationFacts{employment: employment, positions: positions, policyRef: policy.policyRef(), targets: targets}, nil, req, plan)
	if err != nil {
		return policyMatch{}, eligibility.StatusUnspecified, err
	}
	match.Evaluation = EntitlementBasisRef{Kind: "eligibility_evaluation", Ref: result.Digest}
	if result.Status == eligibility.StatusEligible {
		for i := range employment {
			if employment[i].Ref == policy.EmploymentRef && policy.EmploymentRef != "" {
				match.Employment = &employment[i]
				break
			}
		}
		for i := range positions {
			if (policy.PositionRef == "" || positions[i].Ref == policy.PositionRef) && (policy.OrgUnitRef == "" || positions[i].OrgUnitRef == policy.OrgUnitRef) && (policy.PositionRef != "" || policy.OrgUnitRef != "") {
				match.Position = &positions[i]
				break
			}
		}
	}
	return match, result.Status, nil
}

func accessPolicyCompositionTable() rulesengine.Table {
	deny, unknown, conditional, allow := rulesengine.Column{Name: "deny", Kind: rulesengine.KindBool}, rulesengine.Column{Name: "unknown", Kind: rulesengine.KindBool}, rulesengine.Column{Name: "conditional", Kind: rulesengine.KindBool}, rulesengine.Column{Name: "allow", Kind: rulesengine.KindBool}
	falseCell := rulesengine.Equal(rulesengine.BoolValue(false))
	anyCell := rulesengine.Any()
	status := func(id string, denyCell, unknownCell, conditionalCell, allowCell rulesengine.Condition, outcome DerivationStatus) rulesengine.Row {
		return rulesengine.Row{ID: id, Conditions: []rulesengine.Condition{denyCell, unknownCell, conditionalCell, allowCell}, Outputs: []rulesengine.Value{rulesengine.StringValue(string(outcome))}}
	}
	return rulesengine.Table{
		ID: "access-entitlement-policy-composition", Version: "1",
		Inputs:    []rulesengine.Column{deny, unknown, conditional, allow},
		Outputs:   []rulesengine.Column{{Name: "status", Kind: rulesengine.KindString}},
		HitPolicy: rulesengine.HitPolicyFirst,
		Rows: []rulesengine.Row{
			status("deny-dominates", rulesengine.Equal(rulesengine.BoolValue(true)), anyCell, anyCell, anyCell, DerivationNotExpected),
			status("unresolved-fails-closed", falseCell, rulesengine.Equal(rulesengine.BoolValue(true)), anyCell, anyCell, DerivationUnknown),
			status("conditional-pending", falseCell, falseCell, rulesengine.Equal(rulesengine.BoolValue(true)), anyCell, DerivationConditional),
			status("allow", falseCell, falseCell, falseCell, rulesengine.Equal(rulesengine.BoolValue(true)), DerivationExpected),
			status("no-grant", falseCell, falseCell, falseCell, falseCell, DerivationNotExpected),
		},
	}
}

func composePolicyResults(deny, unknown, conditional, allow bool) (DerivationStatus, EntitlementBasisRef, error) {
	table := accessPolicyCompositionTable()
	result, err := rulesengine.Evaluate(table, map[string]rulesengine.Value{"deny": rulesengine.BoolValue(deny), "unknown": rulesengine.BoolValue(unknown), "conditional": rulesengine.BoolValue(conditional), "allow": rulesengine.BoolValue(allow)})
	if err != nil {
		return DerivationUnknown, EntitlementBasisRef{}, err
	}
	if len(result.Matches) != 1 || len(result.Matches[0].Outputs) != 1 {
		return DerivationUnknown, EntitlementBasisRef{}, fmt.Errorf("%w: policy composition returned no unique status", ErrInvalidDerivation)
	}
	trace := result.TableDigest + "/" + strings.Join(result.MatchedRowIDs, ",")
	return DerivationStatus(result.Matches[0].Outputs[0].String()), EntitlementBasisRef{Kind: "rules_evaluation", Ref: trace}, nil
}

func (m policyMatch) basis() []EntitlementBasisRef {
	basis := []EntitlementBasisRef{{Kind: "policy", Ref: m.policy.policyRef()}}
	if m.Employment != nil {
		basis = append(basis, EntitlementBasisRef{Kind: "employment", Ref: m.Employment.Ref})
	}
	if m.Position != nil {
		basis = append(basis, EntitlementBasisRef{Kind: "position", Ref: m.Position.Ref})
		if m.Position.OrgUnitRef != "" {
			basis = append(basis, EntitlementBasisRef{Kind: "org_unit", Ref: m.Position.OrgUnitRef})
		}
	}
	sort.Slice(basis, func(i, j int) bool {
		if basis[i].Kind != basis[j].Kind {
			return basis[i].Kind < basis[j].Kind
		}
		return basis[i].Ref < basis[j].Ref
	})
	return basis
}

func (m policyMatch) effective(identity WorkforceIdentity) (values.EffectiveInterval, error) {
	intervals := []values.EffectiveInterval{identity.Effective, m.policy.Effective}
	if m.Employment != nil {
		intervals = append(intervals, m.Employment.Effective)
	}
	if m.Position != nil {
		intervals = append(intervals, m.Position.Effective)
	}
	return intersection(intervals...)
}

// CalculateExpectedEntitlements derives a native expected-access graph from
// governed facts only. It is pure: it performs no writes, provider calls, or
// observation reconciliation.
func CalculateExpectedEntitlements(request EntitlementDerivationRequest) (ExpectedEntitlementCalculation, error) {
	if err := request.validate(); err != nil {
		return ExpectedEntitlementCalculation{}, err
	}
	identities := append([]WorkforceIdentity(nil), request.Graph.Identities...)
	entitlements := append([]EntitlementDefinition(nil), request.Graph.Entitlements...)
	sort.Slice(identities, func(i, j int) bool { return identities[i].ID < identities[j].ID })
	sort.Slice(entitlements, func(i, j int) bool { return entitlements[i].ID < entitlements[j].ID })
	policies := append([]DeclaredAccessPolicy(nil), request.Policies...)
	sort.Slice(policies, func(i, j int) bool { return policies[i].policyRef() < policies[j].policyRef() })
	accounts := append([]AccountLink(nil), request.Graph.Accounts...)
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })

	result := ExpectedEntitlementCalculation{AsOf: request.AsOf, Decisions: make([]DerivedEntitlementEdge, 0)}
	for _, identity := range identities {
		employment := sortedEmployment(request.Employment, identity.ID, request.AsOf)
		positions := sortedPositions(request.Positions, identity.ID, request.AsOf)
		identityActive := identity.Lifecycle == LifecycleActive && activeAt(identity.Effective, request.AsOf)
		for _, entitlement := range entitlements {
			decision := DerivedEntitlementEdge{IdentityID: identity.ID, EntitlementID: entitlement.ID, Status: DerivationNotExpected, Effective: identity.Effective}
			if !identityActive || entitlement.Lifecycle != LifecycleActive || !activeAt(entitlement.Effective, request.AsOf) {
				result.Decisions = append(result.Decisions, decision)
				continue
			}
			unknown, deny, conditional, allow := false, false, false, false
			matches := make([]policyMatch, 0)
			evaluations := make([]EntitlementBasisRef, 0)
			for _, policy := range policies {
				if policy.EntitlementID != entitlement.ID || !activeAt(policy.Effective, request.AsOf) {
					continue
				}
				match, status, err := evaluatePolicy(policy, employment, positions, identity)
				if err != nil {
					return ExpectedEntitlementCalculation{}, fmt.Errorf("%w: evaluate policy %s: %v", ErrInvalidDerivationPolicy, policy.policyRef(), err)
				}
				evaluations = append(evaluations, match.Evaluation)
				switch status {
				case eligibility.StatusEligible:
					matches = append(matches, match)
					switch policy.Effect {
					case AccessPolicyDeny:
						deny = true
					case AccessPolicyConditional:
						conditional = true
					case AccessPolicyAllow:
						allow = true
					}
				case eligibility.StatusConditional:
					conditional = true
				case eligibility.StatusUnknown:
					unknown = true
				}
			}
			composed, rulesBasis, err := composePolicyResults(deny, unknown, conditional, allow)
			if err != nil {
				return ExpectedEntitlementCalculation{}, err
			}
			decision.Basis = append(decision.Basis, rulesBasis)
			decision.Basis = append(decision.Basis, evaluations...)
			sort.Slice(decision.Basis, func(i, j int) bool {
				if decision.Basis[i].Kind != decision.Basis[j].Kind {
					return decision.Basis[i].Kind < decision.Basis[j].Kind
				}
				return decision.Basis[i].Ref < decision.Basis[j].Ref
			})
			if composed != DerivationExpected && len(matches) == 0 {
				decision.Status = composed
				result.Decisions = append(result.Decisions, decision)
				continue
			}
			decision.Status = composed
			chosen := matches[0]
			decision.Basis = append(decision.Basis, chosen.basis()...)
			if chosen.Position != nil {
				decision.OrgUnitRef = chosen.Position.OrgUnitRef
			}
			decision.Effective, _ = chosen.effective(identity)
			if decision.Status == DerivationExpected {
				accountID := ""
				for _, account := range accounts {
					if account.WorkforceIdentityID == identity.ID && account.System == entitlement.System && account.Lifecycle == LifecycleActive && activeAt(account.Effective, request.AsOf) {
						accountID = account.ID
						break
					}
				}
				policyRef := ""
				employmentRef, positionRef := "", ""
				for _, basis := range decision.Basis {
					switch basis.Kind {
					case "policy":
						policyRef = basis.Ref
					case "employment":
						employmentRef = basis.Ref
					case "position":
						positionRef = basis.Ref
					}
				}
				decision.Expected = ExpectedEntitlement{
					ID: "expected/" + identity.ID + "/" + entitlement.ID, Tenant: request.Graph.Tenant,
					Subject: identity.Subject, System: entitlement.System, WorkforceIdentityID: identity.ID,
					AccountLinkID: accountID, EntitlementID: entitlement.ID, EmploymentRef: employmentRef,
					PositionRef: positionRef, PolicyRef: policyRef, Revision: request.Revision,
					Authority: AuthorityNative, Effective: decision.Effective, KnownAt: request.KnownAt,
					Provenance: request.Provenance, Lifecycle: LifecycleActive,
				}
				result.Expected = append(result.Expected, decision.Expected)
			}
			result.Decisions = append(result.Decisions, decision)
		}
	}
	sort.Slice(result.Decisions, func(i, j int) bool { return result.Decisions[i].key() < result.Decisions[j].key() })
	sort.Slice(result.Expected, func(i, j int) bool { return result.Expected[i].ID < result.Expected[j].ID })
	w := canonicalbytes.New("hcmnext.domains.access.ExpectedEntitlementCalculation", 1).
		Value("as_of", result.AsOf).Count("decisions", len(result.Decisions))
	for _, edge := range result.Decisions {
		w.String("identity", edge.IdentityID).String("entitlement", edge.EntitlementID).String("status", string(edge.Status)).
			Value("effective", edge.Effective).Count("basis", len(edge.Basis))
		for _, basis := range edge.Basis {
			w.Value("basis_ref", basis)
		}
	}
	raw, err := w.Bytes()
	if err != nil {
		return ExpectedEntitlementCalculation{}, err
	}
	result.CanonicalDigest = canonicalbytes.Digest(raw)
	return result, nil
}

// DeriveExpectedEntitlements is the domain-language alias for the calculator.
func DeriveExpectedEntitlements(request EntitlementDerivationRequest) (ExpectedEntitlementCalculation, error) {
	return CalculateExpectedEntitlements(request)
}

type EntitlementDeltaKind string

const (
	EntitlementGrant  EntitlementDeltaKind = "GRANT"
	EntitlementRevoke EntitlementDeltaKind = "REVOKE"
)

// EntitlementDelta is a typed change from the currently expected set to a
// newly calculated set.
type EntitlementDelta struct {
	Kind   EntitlementDeltaKind
	Before ExpectedEntitlement
	After  ExpectedEntitlement
}

func expectedKey(edge ExpectedEntitlement) string {
	return edge.WorkforceIdentityID + "\x00" + edge.EntitlementID
}

// DiffExpectedEntitlements compares native expected records by identity and
// entitlement. A changed basis is represented as a revoke followed by a grant
// in deterministic order.
func DiffExpectedEntitlements(current []ExpectedEntitlement, next ExpectedEntitlementCalculation) ([]EntitlementDelta, error) {
	if next.CanonicalDigest == "" {
		return nil, ErrInvalidEntitlementDelta
	}
	before := make(map[string]ExpectedEntitlement, len(current))
	for _, edge := range current {
		if err := edge.Validate(); err != nil {
			return nil, err
		}
		key := expectedKey(edge)
		if _, exists := before[key]; exists {
			return nil, fmt.Errorf("%w: duplicate current edge %s", ErrInvalidEntitlementDelta, key)
		}
		before[key] = edge
	}
	after := make(map[string]ExpectedEntitlement, len(next.Expected))
	for _, edge := range next.Expected {
		if err := edge.Validate(); err != nil {
			return nil, err
		}
		key := expectedKey(edge)
		if _, exists := after[key]; exists {
			return nil, fmt.Errorf("%w: duplicate calculated edge %s", ErrInvalidEntitlementDelta, key)
		}
		after[key] = edge
	}
	keys := make([]string, 0, len(before)+len(after))
	seen := make(map[string]struct{})
	for key := range before {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range after {
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	deltas := make([]EntitlementDelta, 0)
	for _, key := range keys {
		old, oldOK := before[key]
		fresh, freshOK := after[key]
		switch {
		case !oldOK:
			deltas = append(deltas, EntitlementDelta{Kind: EntitlementGrant, After: fresh})
		case !freshOK:
			deltas = append(deltas, EntitlementDelta{Kind: EntitlementRevoke, Before: old})
		case string(old.Canonical()) != string(fresh.Canonical()):
			deltas = append(deltas, EntitlementDelta{Kind: EntitlementRevoke, Before: old}, EntitlementDelta{Kind: EntitlementGrant, After: fresh})
		}
	}
	return deltas, nil
}

func (r ExpectedEntitlementCalculation) Diff(current []ExpectedEntitlement) ([]EntitlementDelta, error) {
	return DiffExpectedEntitlements(current, r)
}
