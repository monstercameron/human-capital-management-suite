package agentaccess

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
)

// SetGrants replaces the grants of a draft revision: which roles, population
// and organization scopes may use which of its skills. Like SetTier it resets
// approval, because whoever approved earlier approved different content, and
// like Create it refuses a grant that omits a role, a population or an
// organization scope, so an omitted filter can never become tenant-wide access.
func (c *Console) SetGrants(tenant, actor, revisionID string, grants []agentconnect.GrantScope) (Revision, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return Revision{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return Revision{}, err
	}
	connection, index, err := c.find(tenant, revisionID)
	if err != nil {
		return Revision{}, err
	}
	list := c.revisions[key(tenant, connection)]
	revision := list[index]
	if revision.Status == StatusPublished || revision.Status == StatusSuperseded {
		return Revision{}, ErrWrongState
	}
	known := map[string]bool{}
	for _, skill := range revision.Skills {
		known[skill.ID] = true
	}
	seen := map[string]bool{}
	next := make([]agentconnect.GrantScope, 0, len(grants))
	for _, grant := range grants {
		if strings.TrimSpace(grant.ID) == "" || seen[grant.ID] || len(grant.Roles) == 0 || strings.TrimSpace(grant.Population) == "" || len(grant.OrganizationScopes) == 0 || len(grant.Skills) == 0 {
			return Revision{}, fmt.Errorf("%w: grant %q needs an id, a role, a population, an organization scope and a skill", ErrInvalid, grant.ID)
		}
		seen[grant.ID] = true
		for _, id := range grant.Skills {
			if !known[id] {
				return Revision{}, fmt.Errorf("%w: grant %q names unknown skill %q", ErrInvalid, grant.ID, id)
			}
		}
		grant.Roles = append([]string(nil), grant.Roles...)
		grant.OrganizationScopes = append([]string(nil), grant.OrganizationScopes...)
		grant.Skills = append([]string(nil), grant.Skills...)
		next = append(next, grant)
	}
	revision.Grants = next
	revision.Status, revision.RequestedBy, revision.ApprovedBy, revision.ApprovedAt, revision.StepUp = StatusDraft, "", "", time.Time{}, false
	revision.Digest = revision.digest()
	if err := c.persistLocked(tenant, actor, "set-grants", revision.ID(), revision); err != nil {
		return Revision{}, err
	}
	list[index] = revision
	return revision, nil
}

// TierFromPage reads the tier a page sent. An unknown or missing tier is the
// most restrictive one, so a malformed request can never lower a skill's tier.
func TierFromPage(value string) agentconnect.SideEffectTier { return tierFromPage(value) }
