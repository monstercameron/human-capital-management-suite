package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentskillgrantstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// localAgentDemoPinPurposes names the purposes a demo persona's pinned skill
// requires, from the same pure definitions the served registry publishes.
func localAgentDemoPinPurposes(skillID string) ([]string, bool) {
	switch skillID {
	case personaChatReplySkillID:
		return slices.Clone(personaChatReplySkillDefinition().RequiredPurposes), true
	case personaPolicyHelperSkillID:
		return slices.Clone(personaPolicySearchSkillDefinition().RequiredPurposes), true
	case personaWorkspaceSearchSkillID:
		return slices.Clone(personaWorkspaceSearchSkillDefinition().RequiredPurposes), true
	}
	return nil, false
}

// ensureLocalAgentDemoSkillGrants gives every skill a demo persona pins a
// current administrator grant over the persona's audience. The agent directory
// lists a persona only when discovery returns every pinned skill, and
// discovery returns only granted skills, so a version that pins a skill its
// predecessor did not (Assistant's workspace search) is offered to nobody
// until that skill is granted. A skill whose current grants already cover the
// whole audience is left alone; the count is the grants created.
func ensureLocalAgentDemoSkillGrants(ctx context.Context, core dbport.Beginner, mapper func(values.TenantId) uuid.UUID, tenant values.TenantId, profile agentpersona.PersonaProfile, at time.Time) (int, error) {
	audience := profile.Audience
	if core == nil || mapper == nil || len(audience.Roles) == 0 || len(audience.Populations) != 1 || len(audience.OrganizationScopes) == 0 || profile.Owner == "" {
		return 0, ErrPersonaDraftInvalid
	}
	tenantID := mapper(tenant)
	root, err := agentskillgrantstore.New(core, mapper)
	if err != nil {
		return 0, err
	}
	reader, err := root.Scoped(tenant)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, pin := range profile.SkillPins {
		purposes, known := localAgentDemoPinPurposes(pin.ID)
		if !known {
			return created, fmt.Errorf("%w: no grant purposes known for pinned skill %s", ErrPersonaDraftInvalid, pin.ID)
		}
		grants, err := reader.Grants(ctx, tenant, pin.Key())
		if err != nil {
			return created, err
		}
		covering := grants[:0:0]
		for _, grant := range grants {
			if validPersonaSkillGrant(grant, tenant, pin.Key()) && !slices.ContainsFunc(purposes, func(purpose string) bool { return !grantDimensionCovers(grant.Purposes, purpose) }) {
				covering = append(covering, grant)
			}
		}
		covered := true
		for _, role := range audience.Roles {
			for _, organization := range audience.OrganizationScopes {
				if !tupleGrantCovers(covering, role, audience.Populations[0], organization) {
					covered = false
				}
			}
		}
		if covered {
			continue
		}
		roles := slices.Clone(audience.Roles)
		slices.Sort(roles)
		grantPurposes := append(slices.Clone(purposes), "persona_admin_preview")
		slices.Sort(grantPurposes)
		grantPurposes = slices.Compact(grantPurposes)
		grantID := "local-demo/" + profile.Handle + "/" + pin.ID + "/v" + fmt.Sprint(pin.Version)
		evidence := "local-demo/persona-" + profile.Handle + "/v" + fmt.Sprint(profile.Version) + "/" + pin.Digest
		tx, err := core.Begin(ctx)
		if err != nil {
			return created, err
		}
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			_ = tx.Rollback(ctx)
			return created, err
		}
		rows, err := tx.Exec(ctx, `INSERT INTO agent_skill_grant(tenant_id,grant_id,skill_id,skill_version,roles,population,organization_scopes,purposes,not_before,granted_by,granted_at,admin_evidence_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$9,$11) ON CONFLICT DO NOTHING`,
			tenantID, grantID, pin.ID, int64(pin.Version), roles, audience.Populations[0], slices.Clone(audience.OrganizationScopes), grantPurposes, at.Add(-time.Minute), profile.Owner, evidence)
		if err != nil {
			_ = tx.Rollback(ctx)
			return created, err
		}
		if rows == 0 {
			// The grant row exists but is revoked, expired or narrower than the
			// audience. Grant rows are immutable evidence: refuse rather than
			// rewrite one.
			_ = tx.Rollback(ctx)
			return created, fmt.Errorf("%w: grant %s exists but does not cover the %s audience", ErrLocalDevPersonaChatBootstrap, grantID, profile.DisplayName)
		}
		if err := tx.Commit(ctx); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}
