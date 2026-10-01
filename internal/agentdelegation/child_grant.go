package agentdelegation

import (
	"fmt"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CreateChildGrant re-exchanges an authenticated parent step for a durable,
// narrower specialist grant. The caller resolves the child identity and plan
// from published server state before calling this method.
func (s *Service) CreateChildGrant(req GrantRequest, parentRaw string, boundary VerifyRequest) (Grant, error) {
	if s == nil {
		return Grant{}, ErrInvalidRequest
	}
	claims, err := s.Verify(parentRaw, boundary)
	if err != nil {
		return Grant{}, err
	}
	parent, err := s.store.Get(claims.GrantID)
	if err != nil {
		return Grant{}, err
	}
	for ancestor, depth := parent, 0; ; depth++ {
		if depth >= maxActorDepth || (req.TargetAgentID != "" && ancestor.TargetAgentID == req.TargetAgentID) {
			return Grant{}, fmt.Errorf("%w: specialist target cycle", ErrInvalidGrant)
		}
		if ancestor.ParentGrantID == "" {
			break
		}
		ancestor, err = s.store.Get(ancestor.ParentGrantID)
		if err != nil {
			return Grant{}, err
		}
	}
	if req.UserID != claims.Subject || req.Tenant.String() != claims.Tenant || req.Purpose != claims.Purpose || req.OrganizationScopeID != parent.OrganizationScopeID || req.TaskID == parent.TaskID || req.GrantID == parent.GrantID || !subset(req.Skills, parent.Skills) || !subset(skillCapabilities(req.SkillScopes), claims.Scope) {
		return Grant{}, ErrScopeExpanded
	}
	for skill, scope := range req.SkillScopes {
		if !subset(scope, parent.SkillScopes[skill]) {
			return Grant{}, ErrScopeExpanded
		}
	}
	depth := 0
	for actor := &claims.Actor; actor != nil; actor = actor.Act {
		depth++
		if actor.AgentVersion == req.AgentVersion || actor.RunID == req.TaskID {
			return Grant{}, fmt.Errorf("%w: specialist delegation cycle", ErrInvalidGrant)
		}
	}
	if depth >= maxActorDepth {
		return Grant{}, fmt.Errorf("%w: specialist depth exhausted", ErrInvalidGrant)
	}
	if req.NotBefore.IsZero() {
		req.NotBefore = s.now().UTC()
	}
	if req.ExpiresAt.IsZero() {
		req.ExpiresAt = parent.ExpiresAt
	}
	if req.NotBefore.Before(parent.NotBefore) || req.ExpiresAt.After(parent.ExpiresAt) {
		return Grant{}, ErrScopeExpanded
	}
	if !subset(req.UserAuthority.Resources, parent.Authority.Resources) || !subset(req.UserAuthority.Fields, parent.Authority.Fields) || !subset(req.UserAuthority.Purposes, parent.Authority.Purposes) {
		return Grant{}, ErrScopeExpanded
	}
	if parent.SkillAuthorities != nil {
		if req.SkillAuthorities == nil || req.UserAuthority.SkillAuthorities == nil {
			return Grant{}, ErrScopeExpanded
		}
		for skill, scope := range req.SkillAuthorities {
			inherited, ok := parent.SkillAuthorities[skill]
			if !ok || !withinSkill(scope, inherited) {
				return Grant{}, ErrScopeExpanded
			}
		}
	}
	req.parentGrantID, req.parentActor = parent.GrantID, cloneActor(&claims.Actor)
	if parent.SkillAuthorities != nil {
		return s.CreateScopedGrant(req)
	}
	return s.CreateGrant(req)
}

func withinSkill(child, parent trust.SkillAuthority) bool {
	return subset(child.Capabilities, parent.Capabilities) && subset(child.Resources, parent.Resources) && subset(child.Fields, parent.Fields) && subset(child.Purposes, parent.Purposes)
}

// checkParentGrant reloads every ancestor at each exchange and verification.
// An ancestor's revocation, expiry, or changed ceiling invalidates descendants.
func (s *Service) checkParentGrant(child Grant, at time.Time, seen map[string]bool) error {
	for depth := 0; child.ParentGrantID != ""; depth++ {
		if depth >= maxActorDepth || seen[child.GrantID] || seen[child.ParentGrantID] || child.ParentActor == nil {
			return ErrTokenInvalid
		}
		seen[child.GrantID] = true
		parent, err := s.store.Get(child.ParentGrantID)
		if err != nil || parent.Revoked || parent.Authority.Revoked || at.Before(parent.NotBefore) || !at.Before(parent.ExpiresAt) || s.store.CurrentRevocationEpoch(parent.Tenant, parent.UserID) != parent.RevocationEpoch {
			return ErrGrantRevoked
		}
		if child.Tenant != parent.Tenant || child.UserID != parent.UserID || child.Purpose != parent.Purpose || child.OrganizationScopeID != parent.OrganizationScopeID || child.NotBefore.Before(parent.NotBefore) || child.ExpiresAt.After(parent.ExpiresAt) || !subset(child.Skills, parent.Skills) || !subset(child.Authority.Resources, parent.Authority.Resources) || !subset(child.Authority.Fields, parent.Authority.Fields) || !actorChainMatchesGrant(*child.ParentActor, parent) {
			return ErrScopeExpanded
		}
		for skill, scopes := range child.SkillScopes {
			if !subset(scopes, parent.SkillScopes[skill]) {
				return ErrScopeExpanded
			}
		}
		if parent.SkillAuthorities != nil {
			if child.SkillAuthorities == nil {
				return ErrScopeExpanded
			}
			for skill, scopes := range child.SkillAuthorities {
				ancestor, ok := parent.SkillAuthorities[skill]
				if !ok || !withinSkill(scopes, ancestor) {
					return ErrScopeExpanded
				}
			}
		}
		child = parent
	}
	return nil
}

func cloneActor(actor *ActorClaim) *ActorClaim {
	if actor == nil {
		return nil
	}
	out := *actor
	out.Act = cloneActor(actor.Act)
	return &out
}

func sameActor(left, right *ActorClaim) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.AgentVersion == right.AgentVersion && left.InstallationID == right.InstallationID && left.RunID == right.RunID && left.StepID == right.StepID && sameActor(left.Act, right.Act)
}

// InheritedAuthority derives the ceiling for creating a child from a parent
// grant. This projection remains subject to fresh user authority at every step.
func InheritedAuthority(parent Grant) trust.AuthorityScope {
	return trust.AuthorityScope{Tenant: parent.Tenant, OrganizationScopeID: parent.OrganizationScopeID, Capabilities: slices.Clone(parent.Authority.Capabilities), Resources: slices.Clone(parent.Authority.Resources), Fields: slices.Clone(parent.Authority.Fields), Purposes: slices.Clone(parent.Authority.Purposes), SkillAuthorities: trust.CloneSkillAuthorities(parent.SkillAuthorities), Assurance: parent.Authority.RequiredAssurance, NotBefore: parent.NotBefore, ExpiresAt: parent.ExpiresAt}
}

// VerifyAuthority returns the current narrowed resource and field authority
// only after the caller proves possession of an authentic step credential.
func (s *Service) VerifyAuthority(raw string, boundary VerifyRequest) (Claims, trust.EffectiveAuthority, error) {
	claims, err := s.Verify(raw, boundary)
	if err != nil {
		return Claims{}, trust.EffectiveAuthority{}, err
	}
	grant, err := s.store.Get(claims.GrantID)
	if err != nil {
		return Claims{}, trust.EffectiveAuthority{}, err
	}
	current, err := s.effectiveAuthority(grant, s.now().UTC())
	return claims, current, err
}

// CurrentAuthority is for trusted source admission owners holding this
// tenant-scoped service. It resolves a durable grant without minting a token;
// callers still bind their own request identity to that grant independently.
func (s *Service) CurrentAuthority(grantID string) (trust.EffectiveAuthority, error) {
	if s == nil || s.store == nil || s.now == nil {
		return trust.EffectiveAuthority{}, ErrInvalidGrant
	}
	grant, err := s.store.Get(grantID)
	if err != nil {
		return trust.EffectiveAuthority{}, err
	}
	now := s.now().UTC()
	if ValidateGrant(grant) != nil || grant.Revoked || grant.Authority.Revoked || grant.RevocationEpoch != s.store.CurrentRevocationEpoch(grant.Tenant, grant.UserID) || now.Before(grant.NotBefore) || !now.Before(grant.ExpiresAt) {
		return trust.EffectiveAuthority{}, ErrGrantRevoked
	}
	return s.effectiveAuthority(grant, now)
}
