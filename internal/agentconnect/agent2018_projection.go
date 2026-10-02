package agentconnect

import (
	"sort"
	"strings"
)

// ConnectionIDs lists the connections registered for one tenant, in a stable
// order. It carries no revision content: a caller asks Revision for that.
func (r *Registry) ConnectionIDs(tenant string) []string {
	if r == nil || strings.TrimSpace(tenant) == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.items))
	for _, item := range r.items {
		if item.revision.TenantID == tenant {
			ids = append(ids, item.revision.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// GrantedSkills returns the skills a revision's grants give the user,
// whether or not the user has linked their account yet. EffectiveSkills is
// what an agent may call right now; this is what the user would be allowed to
// do once linked, so a page can show the reach before anyone links anything.
// It decides nothing about a call: IssueLease still evaluates the link and
// the connection's state at the moment of use.
func (r *Registry) GrantedSkills(user UserContext, connectionID string) ([]SkillExposure, error) {
	if r == nil {
		return nil, ErrInvalid
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[itemKey(user.TenantID, connectionID)]
	if !ok {
		return nil, ErrNotFound
	}
	if err := user.validate(item.revision.TenantID); err != nil {
		return nil, err
	}
	out := make([]SkillExposure, 0, len(item.revision.Skills))
	for _, skill := range item.revision.Skills {
		if allowed(item, user, skill.ID) {
			out = append(out, copySkill(skill))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Matches reports whether the grant gives the user the skill: one of the
// user's roles, their population, an organization scope and the skill must
// all be named by the grant (or by its explicit wildcard). The admin console
// uses it to preview a draft before anything is published.
func (g GrantScope) Matches(user UserContext, skillID string) bool {
	return g.matches(user, skillID)
}
