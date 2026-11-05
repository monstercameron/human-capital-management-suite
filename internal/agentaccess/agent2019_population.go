package agentaccess

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// PopulationSkill is one skill the members of a population could reach under
// a revision. Roles and OrganizationScopes are the union of the matching
// grants' dimensions: they still gate the skill per member, so a population
// preview never implies that every member holds every listed skill.
type PopulationSkill struct {
	Skill
	Roles              []string
	OrganizationScopes []string
}

// PopulationPreview is what the agents of one population's members could do
// under one revision, and what an administrator should look at before
// publishing. Nothing is changed by computing it.
type PopulationPreview struct {
	Population string
	Skills     []PopulationSkill
	Warnings   []string
}

// PreviewPopulation evaluates one revision for one population, using the same
// grant matching the registry uses at call time for the population dimension.
// A grant matches when it names the population or the explicit wildcard; the
// roles and organization scopes it names are reported per skill because they
// still decide per member. The wildcard alone is not a population and is
// refused rather than guessed at.
func (c *Console) PreviewPopulation(tenant, actor, revisionID, population string) (PopulationPreview, error) {
	if err := c.authorize(tenant, actor); err != nil {
		return PopulationPreview{}, err
	}
	population = strings.TrimSpace(population)
	if population == "" || population == agentconnect.AnyScope {
		return PopulationPreview{}, fmt.Errorf("%w: a population name is required", ErrInvalid)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadLocked(tenant); err != nil {
		return PopulationPreview{}, err
	}
	connection, index, err := c.find(tenant, revisionID)
	if err != nil {
		return PopulationPreview{}, err
	}
	revision := c.revisions[key(tenant, connection)][index]
	bySkill := map[string]*PopulationSkill{}
	for _, skill := range revision.Skills {
		bySkill[skill.ID] = &PopulationSkill{Skill: skill}
	}
	for _, grant := range revision.Grants {
		if grant.Population != agentconnect.AnyScope && grant.Population != population {
			continue
		}
		for _, id := range grant.Skills {
			skill, ok := bySkill[id]
			if !ok {
				continue
			}
			skill.Roles = unionScope(skill.Roles, grant.Roles)
			skill.OrganizationScopes = unionScope(skill.OrganizationScopes, grant.OrganizationScopes)
		}
	}
	result := PopulationPreview{Population: population, Warnings: previewWarnings(revision)}
	for _, skill := range revision.Skills {
		if matched := bySkill[skill.ID]; len(matched.Roles) > 0 || len(matched.OrganizationScopes) > 0 {
			matched.Roles = sortedCopy(matched.Roles)
			matched.OrganizationScopes = sortedCopy(matched.OrganizationScopes)
			result.Skills = append(result.Skills, *matched)
		}
	}
	sort.Slice(result.Skills, func(i, j int) bool { return result.Skills[i].ID < result.Skills[j].ID })
	return result, nil
}

func unionScope(have, add []string) []string {
	for _, value := range add {
		found := false
		for _, existing := range have {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			have = append(have, value)
		}
	}
	return have
}

// previewWarnings names what an administrator should look at before
// publishing a revision: high-impact skills granted to everyone, and whether
// publication still needs a second administrator. Person and population
// previews share it so the two answers can never disagree.
func previewWarnings(revision Revision) []string {
	var warnings []string
	for _, grant := range revision.Grants {
		wide := containsWildcard(grant.Roles) && grant.Population == agentconnect.AnyScope
		for _, id := range grant.Skills {
			for _, skill := range revision.Skills {
				if skill.ID == id && highImpact(skill.Tier) && wide {
					warnings = append(warnings, fmt.Sprintf("%s (%s) is granted to everyone.", skill.ID, tierWord(skill.Tier)))
				}
			}
		}
	}
	if revision.RequiresSecondAdmin() && revision.Status != StatusApproved && revision.Status != StatusPublished && revision.Status != StatusSuperseded {
		warnings = append(warnings, "Publishing needs a second administrator.")
	}
	return warnings
}

// SnapshotForPopulation is the console page's data with the preview for one
// population: every revision, newest first, and the effective skills members
// of that population could reach. Without a label the subject names the
// population, so the panel cannot be mistaken for one person's preview.
func (s *AdminSession) SnapshotForPopulation(label, population, previewRevisionID string) (productui.AgentAdminAccessSnapshot, error) {
	revisions, err := s.console.Revisions(s.tenant, s.actor)
	if err != nil {
		return productui.AgentAdminAccessSnapshot{}, err
	}
	var snapshot productui.AgentAdminAccessSnapshot
	for _, revision := range revisions {
		snapshot.Revisions = append(snapshot.Revisions, pageRevision(revision))
	}
	preview, err := s.console.PreviewPopulation(s.tenant, s.actor, previewRevisionID, population)
	if err != nil {
		return productui.AgentAdminAccessSnapshot{}, err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Population " + preview.Population
	}
	connection := productui.AgentAccessConnection{ID: previewRevisionID, Provider: previewRevisionID, LinkState: "linked"}
	for _, skill := range preview.Skills {
		connection.Skills = append(connection.Skills, productui.AgentAccessSkill{ID: skill.ID, Name: skill.ID, Tier: string(skill.Tier), Scope: populationScopeText(skill)})
	}
	snapshot.Preview = productui.AgentEffectiveAccessPreview{SubjectLabel: label, Connections: []productui.AgentAccessConnection{connection}, Warnings: preview.Warnings}
	snapshot.PreviewReady, snapshot.PreviewSubject = true, label
	return snapshot, nil
}

// populationScopeText says which roles and organization scopes still gate a
// skill per member, in the grant rows' vocabulary. The values are tenant
// identifiers and are shown literally, including an explicit wildcard.
func populationScopeText(skill PopulationSkill) string {
	parts := make([]string, 0, 2)
	if len(skill.Roles) > 0 {
		parts = append(parts, "Roles: "+strings.Join(skill.Roles, ", "))
	}
	if len(skill.OrganizationScopes) > 0 {
		parts = append(parts, "Organization scopes: "+strings.Join(skill.OrganizationScopes, ", "))
	}
	return strings.Join(parts, " · ")
}
