package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AuthorityScope is a server-resolved authority snapshot. Empty dimensions
// are intentionally not wildcards: an omitted scope has no authority.
type AuthorityScope struct {
	Tenant              values.TenantId
	OrganizationScopeID string
	Capabilities        []string
	Resources           []string
	Fields              []string
	Purposes            []string
	SkillAuthorities    SkillAuthorities
	Assurance           Assurance
	NotBefore           time.Time
	ExpiresAt           time.Time
}

// SkillAuthority is the current authority for one registered skill. Empty
// resources and purposes are never wildcards; fields may be empty to narrow
// disclosure without denying the capability.
type SkillAuthority struct {
	Capabilities []string
	Resources    []string
	Fields       []string
	Purposes     []string
}

// SkillAuthorities maps each skill to its independently scoped authority.
type SkillAuthorities map[string]SkillAuthority

// CloneSkillAuthorities defensively copies a per-skill authority map.
func CloneSkillAuthorities(in SkillAuthorities) SkillAuthorities {
	if in == nil {
		return nil
	}
	out := make(SkillAuthorities, len(in))
	for skill, authority := range in {
		out[skill] = SkillAuthority{Capabilities: slices.Clone(authority.Capabilities), Resources: slices.Clone(authority.Resources), Fields: slices.Clone(authority.Fields), Purposes: slices.Clone(authority.Purposes)}
	}
	return out
}

// IntersectSkillAuthorities intersects authority dimensions for matching keys.
// A missing key is denied and never treated as a wildcard.
func IntersectSkillAuthorities(sets ...SkillAuthorities) SkillAuthorities {
	if len(sets) == 0 || sets[0] == nil {
		return nil
	}
	out := CloneSkillAuthorities(sets[0])
	for _, set := range sets[1:] {
		if set == nil {
			return nil
		}
		for skill, authority := range out {
			other, ok := set[skill]
			if !ok {
				delete(out, skill)
				continue
			}
			authority.Capabilities = intersect(authority.Capabilities, other.Capabilities)
			authority.Resources = intersect(authority.Resources, other.Resources)
			authority.Fields = intersect(authority.Fields, other.Fields)
			authority.Purposes = intersect(authority.Purposes, other.Purposes)
			out[skill] = authority
		}
	}
	return out
}

// DelegationGrant is a bounded, attributable grant from Delegator to Delegate.
// The grant itself can only narrow the delegator's authority.
type DelegationGrant struct {
	GrantID       string
	RootID        string
	ParentGrantID string
	// Kind records why the grant exists. The zero value normalizes to
	// [GrantKindDirect]; acting roles and vacation coverage set it through
	// [AuthorityAssignment.Grant]. It is attribution only and never widens
	// authority.
	Kind                GrantKind
	Delegator           string
	Delegate            string
	Tenant              values.TenantId
	OrganizationScopeID string
	Capabilities        []string
	Resources           []string
	Fields              []string
	Purposes            []string
	SkillAuthorities    SkillAuthorities
	NotBefore           time.Time
	ExpiresAt           time.Time
	RequiredAssurance   Assurance
	AllowRedelegation   bool
	MaxDepth            uint8
	Revoked             bool
	RevocationEpoch     uint64
}

// DelegationRequest supplies the trusted snapshots needed to evaluate a grant.
// Parent is the effective authority of the immediately preceding grant, when
// this is a re-delegation. CurrentRevocationEpoch is read from the revocation
// store, never from the request originator.
type DelegationRequest struct {
	Grant                  DelegationGrant
	Delegator              AuthorityScope
	Delegate               AuthorityScope
	EvaluatedAt            time.Time
	Parent                 *EffectiveAuthority
	CurrentRevocationEpoch uint64
}

// EffectiveAuthority is the only successful delegation result. Chain holds the
// full attribution from root to leaf and is copied on every accessor.
type EffectiveAuthority struct {
	GrantID             string
	RootID              string
	Kind                GrantKind
	Chain               []string
	Delegator           string
	Delegate            string
	Tenant              values.TenantId
	OrganizationScopeID string
	Capabilities        []string
	Resources           []string
	Fields              []string
	Purposes            []string
	SkillAuthorities    SkillAuthorities
	Assurance           Assurance
	NotBefore           time.Time
	ExpiresAt           time.Time
	DecisionID          string
	// These bounds are carried forward so a later redelegation cannot
	// silently acquire permission or depth that its parent did not grant.
	AllowRedelegation bool
	MaxDepth          uint8
}

var (
	ErrInvalidDelegation        = errors.New("trust: invalid delegation")
	ErrDelegationExpanded       = errors.New("trust: delegation expands authority")
	ErrDelegationTenant         = errors.New("trust: delegation crosses tenant")
	ErrDelegationExpired        = errors.New("trust: delegation is outside its validity window")
	ErrDelegationRevoked        = errors.New("trust: delegation is revoked")
	ErrDelegationCycle          = errors.New("trust: delegation chain is cyclic")
	ErrRedelegationNotPermitted = errors.New("trust: re-delegation is not permitted")
)

// ValidateDelegation validates grant shape and immutable bounds without
// evaluating it against authority snapshots.
func ValidateDelegation(g DelegationGrant) error {
	if !printableASCII(g.GrantID, 1, 200) || (g.RootID != "" && !printableASCII(g.RootID, 1, 200)) ||
		(g.ParentGrantID != "" && !printableASCII(g.ParentGrantID, 1, 200)) ||
		!printableASCII(g.Delegator, 1, 200) || !printableASCII(g.Delegate, 1, 200) || g.Delegator == g.Delegate {
		return fmt.Errorf("%w: grant actors", ErrInvalidDelegation)
	}
	if g.RootID == "" {
		g.RootID = g.GrantID
	}
	if g.ParentGrantID == g.GrantID || g.RootID == g.ParentGrantID {
		return ErrDelegationCycle
	}
	if err := g.Tenant.Validate(); err != nil || g.OrganizationScopeID == "" {
		return fmt.Errorf("%w: tenant or organization scope", ErrInvalidDelegation)
	}
	if g.NotBefore.IsZero() || g.ExpiresAt.IsZero() || !g.ExpiresAt.After(g.NotBefore) {
		return fmt.Errorf("%w: validity", ErrInvalidDelegation)
	}
	if g.RequiredAssurance == AssuranceUnspecified || g.RequiredAssurance > AssuranceHigh {
		return fmt.Errorf("%w: assurance", ErrInvalidDelegation)
	}
	if len(g.Capabilities) == 0 || len(g.Resources) == 0 || len(g.Purposes) == 0 ||
		!printableSet(g.Capabilities) || !printableSet(g.Resources) || !printableSet(g.Fields) || !printableSet(g.Purposes) {
		return fmt.Errorf("%w: empty scope", ErrInvalidDelegation)
	}
	if g.ParentGrantID != "" && g.MaxDepth == 0 {
		return fmt.Errorf("%w: redelegation depth", ErrInvalidDelegation)
	}
	if !g.Kind.valid() {
		return fmt.Errorf("%w: grant kind %q", ErrInvalidDelegation, g.Kind)
	}
	if g.Kind.normalize() == GrantKindCoverage && (g.AllowRedelegation || g.MaxDepth != 0 || g.ParentGrantID != "") {
		// Coverage lends an absent principal's own authority for a window.
		// A stand-in appointing a further stand-in would break attribution
		// back to the person who is actually accountable.
		return ErrRedelegationNotPermitted
	}
	return nil
}

// EvaluateDelegation computes a fail-closed intersection of every trusted
// scope. It never treats empty or absent values as a wildcard.
func EvaluateDelegation(req DelegationRequest) (EffectiveAuthority, error) {
	g := req.Grant
	if err := ValidateDelegation(g); err != nil {
		return EffectiveAuthority{}, err
	}
	if g.RootID == "" {
		g.RootID = g.GrantID
	}
	if req.EvaluatedAt.IsZero() {
		return EffectiveAuthority{}, fmt.Errorf("%w: evaluation time", ErrInvalidDelegation)
	}
	if g.Revoked || g.RevocationEpoch < req.CurrentRevocationEpoch {
		return EffectiveAuthority{}, ErrDelegationRevoked
	}
	if req.EvaluatedAt.Before(g.NotBefore) || !req.EvaluatedAt.Before(g.ExpiresAt) {
		return EffectiveAuthority{}, ErrDelegationExpired
	}
	if g.Tenant != req.Delegator.Tenant || g.Tenant != req.Delegate.Tenant {
		return EffectiveAuthority{}, ErrDelegationTenant
	}
	if g.OrganizationScopeID != req.Delegator.OrganizationScopeID || g.OrganizationScopeID != req.Delegate.OrganizationScopeID {
		return EffectiveAuthority{}, ErrDelegationExpanded
	}
	if !req.Delegator.Assurance.AtLeast(g.RequiredAssurance) || !req.Delegate.Assurance.AtLeast(g.RequiredAssurance) {
		return EffectiveAuthority{}, ErrDelegationExpanded
	}
	if req.Parent != nil {
		if !g.AllowRedelegation || !req.Parent.AllowRedelegation || req.Parent.Delegate != g.Delegator ||
			req.Parent.RootID != g.RootID || g.ParentGrantID != req.Parent.GrantID ||
			len(req.Parent.Chain) == 0 || req.Parent.Chain[len(req.Parent.Chain)-1] != req.Parent.GrantID ||
			len(req.Parent.Chain)+1 > int(g.MaxDepth) || (req.Parent.MaxDepth != 0 && len(req.Parent.Chain)+1 > int(req.Parent.MaxDepth)) {
			return EffectiveAuthority{}, ErrRedelegationNotPermitted
		}
		if req.Parent.GrantID == g.GrantID || slices.Contains(req.Parent.Chain, g.GrantID) {
			return EffectiveAuthority{}, ErrDelegationCycle
		}
	}
	capabilities := intersect(req.Delegator.Capabilities, req.Delegate.Capabilities, g.Capabilities)
	resources := intersect(req.Delegator.Resources, req.Delegate.Resources, g.Resources)
	fields := intersect(req.Delegator.Fields, req.Delegate.Fields, g.Fields)
	purposes := intersect(req.Delegator.Purposes, req.Delegate.Purposes, g.Purposes)
	if req.Parent != nil {
		capabilities = intersect(capabilities, req.Parent.Capabilities)
		resources = intersect(resources, req.Parent.Resources)
		fields = intersect(fields, req.Parent.Fields)
		purposes = intersect(purposes, req.Parent.Purposes)
	}
	if len(capabilities) == 0 || len(resources) == 0 || len(purposes) == 0 {
		return EffectiveAuthority{}, ErrDelegationExpanded
	}
	skillAuthorities := IntersectSkillAuthorities(req.Delegator.SkillAuthorities, req.Delegate.SkillAuthorities, g.SkillAuthorities)
	if g.SkillAuthorities != nil {
		if len(skillAuthorities) == 0 {
			return EffectiveAuthority{}, ErrDelegationExpanded
		}
		for skill, authority := range skillAuthorities {
			if len(authority.Capabilities) == 0 || len(authority.Resources) == 0 || len(authority.Purposes) == 0 {
				delete(skillAuthorities, skill)
			}
		}
		if len(skillAuthorities) == 0 {
			return EffectiveAuthority{}, ErrDelegationExpanded
		}
	}
	if len(fields) == 0 {
		fields = nil
	}
	nb, exp := maxTime(g.NotBefore, req.Delegator.NotBefore, req.Delegate.NotBefore), minTime(g.ExpiresAt, req.Delegator.ExpiresAt, req.Delegate.ExpiresAt)
	if req.Parent != nil {
		nb = maxTime(nb, req.Parent.NotBefore)
		exp = minTime(exp, req.Parent.ExpiresAt)
	}
	if skillAuthorities != nil && req.Parent != nil {
		skillAuthorities = IntersectSkillAuthorities(skillAuthorities, req.Parent.SkillAuthorities)
		if len(skillAuthorities) == 0 {
			return EffectiveAuthority{}, ErrDelegationExpanded
		}
		for skill, authority := range skillAuthorities {
			if len(authority.Capabilities) == 0 || len(authority.Resources) == 0 || len(authority.Purposes) == 0 {
				delete(skillAuthorities, skill)
			}
		}
	}
	if !exp.After(nb) || req.EvaluatedAt.Before(nb) || !req.EvaluatedAt.Before(exp) {
		return EffectiveAuthority{}, ErrDelegationExpired
	}
	chain := []string{g.GrantID}
	if req.Parent != nil {
		chain = append(slices.Clone(req.Parent.Chain), g.GrantID)
	}
	e := EffectiveAuthority{GrantID: g.GrantID, RootID: g.RootID, Kind: g.Kind.normalize(), Chain: chain, Delegator: g.Delegator, Delegate: g.Delegate, Tenant: g.Tenant, OrganizationScopeID: g.OrganizationScopeID, Capabilities: capabilities, Resources: resources, Fields: fields, Purposes: purposes, SkillAuthorities: CloneSkillAuthorities(skillAuthorities), Assurance: minAssurance(req.Delegator.Assurance, req.Delegate.Assurance), NotBefore: nb, ExpiresAt: exp, AllowRedelegation: g.AllowRedelegation, MaxDepth: g.MaxDepth}
	e.DecisionID = delegationDecisionID(e)
	return e, nil
}

// NarrowToCurrentAuthority re-intersects evaluated effective authority with
// the delegate's current authority snapshot at time at. Evaluating a grant
// once is not enough: when the delegator's authority shrinks afterwards — a
// role revoked, a purpose withdrawn — the effective authority must shrink
// with it, never stay at the stale, wider grant. The result carries the
// original chain and attribution with the narrowed sets and window, and a
// fresh decision id naming exactly this narrowing.
//
// It fails closed: a tenant mismatch, an evaluation outside the narrowed
// window, or an empty capability, resource or purpose intersection refuses
// with the same sentinels [EvaluateDelegation] uses. An empty field
// intersection is not a refusal — fields narrow disclosure, never authority.
func NarrowToCurrentAuthority(eff EffectiveAuthority, current AuthorityScope, at time.Time) (EffectiveAuthority, error) {
	if at.IsZero() {
		return EffectiveAuthority{}, fmt.Errorf("%w: evaluation time", ErrInvalidDelegation)
	}
	if eff.Tenant != current.Tenant {
		return EffectiveAuthority{}, ErrDelegationTenant
	}
	if eff.OrganizationScopeID != current.OrganizationScopeID {
		return EffectiveAuthority{}, ErrDelegationExpanded
	}
	capabilities := intersect(eff.Capabilities, current.Capabilities)
	resources := intersect(eff.Resources, current.Resources)
	fields := intersect(eff.Fields, current.Fields)
	purposes := intersect(eff.Purposes, current.Purposes)
	if len(capabilities) == 0 || len(resources) == 0 || len(purposes) == 0 {
		return EffectiveAuthority{}, ErrDelegationExpanded
	}
	skillAuthorities := eff.SkillAuthorities
	if skillAuthorities != nil {
		skillAuthorities = IntersectSkillAuthorities(skillAuthorities, current.SkillAuthorities)
		for skill, authority := range skillAuthorities {
			if len(authority.Capabilities) == 0 || len(authority.Resources) == 0 || len(authority.Purposes) == 0 {
				delete(skillAuthorities, skill)
			}
		}
		if len(skillAuthorities) == 0 {
			return EffectiveAuthority{}, ErrDelegationExpanded
		}
	}
	if len(fields) == 0 {
		fields = nil
	}
	nb := maxTime(eff.NotBefore, current.NotBefore)
	exp := minTime(eff.ExpiresAt, current.ExpiresAt)
	if !exp.After(nb) || at.Before(nb) || !at.Before(exp) {
		return EffectiveAuthority{}, ErrDelegationExpired
	}
	narrowed := eff
	narrowed.Capabilities = capabilities
	narrowed.Resources = resources
	narrowed.Fields = fields
	narrowed.Purposes = purposes
	narrowed.SkillAuthorities = CloneSkillAuthorities(skillAuthorities)
	narrowed.Assurance = minAssurance(eff.Assurance, current.Assurance)
	narrowed.NotBefore = nb
	narrowed.ExpiresAt = exp
	narrowed.DecisionID = delegationDecisionID(narrowed)
	return narrowed, nil
}

func printableSet(set []string) bool {
	for _, s := range set {
		if !printableASCII(s, 1, 200) {
			return false
		}
	}
	return true
}

func intersect(sets ...[]string) []string {
	if len(sets) == 0 || len(sets[0]) == 0 {
		return nil
	}
	out := slices.Clone(sets[0])
	for _, s := range sets[1:] {
		out = slices.DeleteFunc(out, func(v string) bool { return !slices.Contains(s, v) })
	}
	return out
}
func maxTime(ts ...time.Time) time.Time {
	out := ts[0]
	for _, t := range ts[1:] {
		if t.After(out) {
			out = t
		}
	}
	return out
}
func minTime(ts ...time.Time) time.Time {
	out := ts[0]
	for _, t := range ts[1:] {
		if t.Before(out) {
			out = t
		}
	}
	return out
}
func minAssurance(a, b Assurance) Assurance {
	if a < b {
		return a
	}
	return b
}
func delegationDecisionID(e EffectiveAuthority) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s|%v|%v", e.GrantID, e.RootID, e.Kind, e.Tenant, e.Delegate, e.NotBefore.UnixNano(), e.ExpiresAt.UnixNano())
	fmt.Fprintf(h, "|%v", e.Chain)
	for _, s := range [][]string{e.Capabilities, e.Resources, e.Fields, e.Purposes} {
		fmt.Fprintf(h, "|%v", canonicalSet(s))
	}
	keys := make([]string, 0, len(e.SkillAuthorities))
	for skill := range e.SkillAuthorities {
		keys = append(keys, skill)
	}
	slices.Sort(keys)
	for _, skill := range keys {
		a := e.SkillAuthorities[skill]
		fmt.Fprintf(h, "|skill:%s|%v|%v|%v|%v", skill, canonicalSet(a.Capabilities), canonicalSet(a.Resources), canonicalSet(a.Fields), canonicalSet(a.Purposes))
	}
	return "ev:delegation:" + hex.EncodeToString(h.Sum(nil))[:32]
}

func canonicalSet(set []string) []string {
	out := slices.Clone(set)
	slices.Sort(out)
	return slices.Compact(out)
}
