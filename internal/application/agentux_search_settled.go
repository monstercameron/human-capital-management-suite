package application

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// publishedRuntimeSettled reports whether a published persona version already
// has everything the preparation provisions after publication: its service
// principal, its model route and its provider deployment profile. None of that
// needs the version's review or evaluation evidence, which expires; so a rerun
// leaves a settled version alone instead of demanding fresh evidence for it.
// The provider deployment profile is the one piece written here when only it
// is missing, because the file is outside the database and may be absent on a
// machine that has the database.
func (p localAgentDemoPreparation) publishedRuntimeSettled(ctx, adminCtx context.Context, row agentpersonastore.PersonaVersion, profile agentpersona.PersonaProfile, qualified agentmodel.ModelProfile, out *localAgentDemoPreparedAgent) (bool, error) {
	scoped, err := p.personas.Scoped(row.TenantID)
	if err != nil {
		return false, err
	}
	if _, err := scoped.ResolvePersonaAgentPrincipal(ctx, row.PersonaID, row.Version); err != nil {
		if errors.Is(err, agentpersonastore.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	resolver := PersonaRunAgentPrincipalResolver{Bindings: AgentPersonaRunPrincipalBindingReader{Store: p.personas}, Principals: GovernancePersonaPrincipalAuthority{DB: p.core, TenantUUID: p.mapper}, Now: func() time.Time { return p.now }}
	if _, err := resolver.Resolve(ctx, row.TenantID, row.PersonaID, row.Version); err != nil {
		return false, nil
	}
	legalEntity, err := (PersonaRunLegalEntityResolver{DB: p.core, TenantUUID: p.mapper, Now: func() time.Time { return p.now }}).Resolve(adminCtx, agentinvoke.RunRequest{TenantID: row.TenantID.String(), Mode: agentinvoke.OnBehalfOf, InvokerID: localAgentDemoAdmin})
	if err != nil {
		return false, err
	}
	ref, _ := LocalPersonaOpenAIModelPolicyReference()
	if _, err := p.agents.CurrentPersonaModelRoutePolicyForAgent(ctx, p.mapper(row.TenantID), legalEntity, ref.ID, int64(ref.Version), int64(ref.SchemaVersion), ref.Digest, profile.Manifest.Digest, p.now); err != nil {
		if errors.Is(err, agentstore.ErrPersonaModelRouteNotFound) {
			return false, nil
		}
		return false, err
	}
	path := p.deploymentPath
	if strings.TrimSpace(path) == "" {
		path = filepath.FromSlash(localPersonaModelDeploymentPath)
	}
	created, err := ensureLocalAgentDemoDeploymentProfile(path, p.config.Tenant, p.material, qualified)
	if err != nil {
		return false, err
	}
	out.providerDeploymentCreated = out.providerDeploymentCreated || created
	return true, nil
}

// ensureLocalAgentDemoReviewerGrant makes sure the demo's independent reviewer
// holds a persona-review grant that stays current long enough to review and
// publish. The grant bootstrap issues one that lasts a day; a rerun after that
// issues a new grant under a new identifier, through the same restricted
// review-authority role, rather than leaving every later version unreviewable.
// A grant that is still current is left alone.
func ensureLocalAgentDemoReviewerGrant(ctx context.Context, db dbport.Beginner, tenantID uuid.UUID, reviewer, owner string, now time.Time) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role string
	if err := tx.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		return err
	}
	if role != agentstore.PersonaReviewAuthorityRole {
		return ErrPersonaReviewUnavailable
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenantID.String()+"/local-demo-persona-review-grant/"+reviewer); err != nil {
		return err
	}
	var current bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM persona_review_grant WHERE tenant_id=$1 AND principal_id=$2 AND permission='persona:review'
		AND granted_at<=clock_timestamp() AND expires_at>clock_timestamp()+interval '1 hour' AND revoked_at IS NULL)`, tenantID, reviewer).Scan(&current); err != nil {
		return err
	}
	if current {
		return tx.Commit(ctx)
	}
	grantID := fmt.Sprintf("local-demo/persona-review/%s/admin/%s/renewed-%s", reviewer, owner, now.UTC().Format("20060102T150405Z"))
	if _, err := tx.Exec(ctx, `INSERT INTO persona_review_grant(tenant_id,grant_id,principal_id,permission,granted_at,expires_at) VALUES($1,$2,$3,'persona:review',$4,$5) ON CONFLICT DO NOTHING`, tenantID, grantID, reviewer, now.Add(-time.Minute), now.Add(24*time.Hour)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
