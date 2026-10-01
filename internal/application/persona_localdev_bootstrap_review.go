package application

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// LocalDevPolicyHelperReviewerBootstrap grants a bounded independent review
// permission through the dedicated authority pool. It never approves a draft.
type LocalDevPolicyHelperReviewerBootstrap struct {
	ReviewDB   dbport.Beginner
	Roles      roleaccess.Store
	Directory  PersonaAudienceDirectory
	TenantUUID func(values.TenantId) uuid.UUID
	Authorizer PersonaCreateAuthorizer
	Now        func() time.Time
}

const localDevPersonaReviewerRole = "local_persona_reviewer"

func (b *LocalDevPolicyHelperReviewerBootstrap) Provision(ctx context.Context, profile string) (string, error) {
	if b == nil || ctx == nil || profile != ServeProfileLocalDev || b.ReviewDB == nil || b.Roles == nil || b.Directory == nil || b.TenantUUID == nil || b.Authorizer == nil || b.Now == nil {
		return "", ErrLocalDevPersonaChatBootstrap
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return "", ErrLocalDevPersonaChatBootstrap
	}
	now := b.Now().UTC()
	if now.IsZero() || now.Before(p.IssuedAt()) || !now.Before(p.ExpiresAt()) {
		return "", ErrLocalDevPersonaChatBootstrap
	}
	pack, ok := demoworkforce.PackFor(p.Tenant().String())
	if !ok {
		return "", ErrLocalDevPersonaChatBootstrap
	}
	owner, steward, err := localDevPolicyHelperOwners(pack, b.TenantUUID(p.Tenant()))
	if err != nil || owner != p.Subject() {
		return "", ErrLocalDevPersonaChatBootstrap
	}
	if err := b.Authorizer.AuthorizePersonaCreate(ctx, PersonaCreateAuthorization{Principal: p, Tenant: p.Tenant(), PersonaID: "hcmnext.local.persona.policy_helper"}); err != nil {
		return "", ErrLocalDevPersonaChatBootstrap
	}
	reviewer, err := localDevPolicyHelperReviewer(pack, b.TenantUUID(p.Tenant()))
	if err != nil || reviewer == owner || reviewer == steward {
		return "", ErrLocalDevPersonaChatBootstrap
	}
	member, err := b.Directory.ResolvePersonaAudienceMember(ctx, p.Tenant().String(), reviewer)
	if err != nil || member.SubjectID != reviewer || len(member.Roles) == 0 || len(member.Populations) != 1 || member.Populations[0] != localDemoPopulationID || member.OrganizationScope == "" {
		return "", ErrLocalDevPersonaChatBootstrap
	}
	// Validate the restricted writer before any role-access side effect.
	if err := b.verifyAuthority(ctx); err != nil {
		return "", err
	}
	snapshot, err := b.Roles.Load(ctx, p.Tenant(), member.OrganizationScope)
	if err != nil {
		return "", err
	}
	role := roleaccess.Role{ID: localDevPersonaReviewerRole, Name: "Local persona reviewer", Description: "Independent local demo persona review; no authoring or publication rights.", Active: true, Reason: "Administer independent persona review for the explicitly selected local demo"}
	roleMissing := true
	for _, current := range snapshot.Roles {
		if current.ID == role.ID {
			if current.Name != role.Name || current.Description != role.Description || current.System || !current.Active {
				return "", ErrLocalDevPersonaChatBootstrap
			}
			roleMissing = false
		}
	}
	permission := roleaccess.PagePermission{RoleID: role.ID, PageID: string(productui.PagePersonaAdmin), View: true, Reason: role.Reason}
	permissionMissing := true
	for _, current := range snapshot.PagePermissions {
		if current.RoleID == role.ID {
			if current.PageID != permission.PageID || !current.View || current.Create || current.Update || current.Delete {
				return "", ErrLocalDevPersonaChatBootstrap
			}
			permissionMissing = false
		}
	}
	feature := roleaccess.FeaturePermission{RoleID: role.ID, PageID: permission.PageID, FeatureID: string(productui.FeatureContent), View: true, Reason: role.Reason}
	featureMissing := true
	for _, current := range snapshot.FeaturePermissions {
		if current.RoleID == role.ID {
			if current.PageID != feature.PageID || current.FeatureID != feature.FeatureID || !current.View || current.Create || current.Update || current.Delete {
				return "", ErrLocalDevPersonaChatBootstrap
			}
			featureMissing = false
		}
	}
	var assignment roleaccess.Assignment
	for _, current := range snapshot.Assignments {
		if current.WorkerRef == reviewer {
			assignment = current
		}
	}
	if assignment.WorkerRef != reviewer || assignment.Version <= 0 {
		return "", ErrLocalDevPersonaChatBootstrap
	}
	assignmentMissing := !slices.Contains(assignment.RoleIDs, role.ID)
	if roleMissing {
		if _, err = b.Roles.SaveRole(ctx, p.Tenant(), owner, role); err != nil {
			return "", err
		}
	}
	if permissionMissing {
		if _, err = b.Roles.SavePagePermission(ctx, p.Tenant(), owner, permission); err != nil {
			return "", err
		}
	}
	if assignmentMissing {
		assignment.RoleIDs = append(slices.Clone(assignment.RoleIDs), role.ID)
		slices.Sort(assignment.RoleIDs)
		assignment.Reason = role.Reason
		if _, err = b.Roles.SaveAssignment(ctx, p.Tenant(), owner, assignment); err != nil {
			return "", err
		}
	}
	if featureMissing {
		if _, err = b.Roles.SaveFeaturePermission(ctx, p.Tenant(), owner, feature); err != nil {
			return "", err
		}
	}
	if err = b.issueGrant(ctx, p.Tenant(), reviewer, owner, now); err != nil {
		return "", err
	}
	return reviewer, nil
}

func localDevPolicyHelperReviewer(pack *demoworkforce.Pack, tenant uuid.UUID) (string, error) {
	workers, err := pack.Plan(tenant)
	if err != nil {
		return "", err
	}
	number := ""
	for _, persona := range pack.Personas {
		if persona.ID == "hiring-manager" {
			number = persona.WorkerNumber
		}
	}
	for _, worker := range workers {
		if worker.Row.WorkerNumber == number && worker.Row.WorkerType == "employee" && worker.Row.LifecycleStatus == "active" {
			return worker.Row.WorkerKey, nil
		}
	}
	return "", ErrLocalDevPersonaChatBootstrap
}

func (b *LocalDevPolicyHelperReviewerBootstrap) verifyAuthority(ctx context.Context) error {
	tx, err := b.ReviewDB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role string
	if err = tx.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		return err
	}
	if role != agentstore.PersonaReviewAuthorityRole {
		return ErrLocalDevPersonaChatBootstrap
	}
	return nil
}

func (b *LocalDevPolicyHelperReviewerBootstrap) issueGrant(ctx context.Context, tenant values.TenantId, reviewer, owner string, now time.Time) error {
	tx, err := b.ReviewDB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role string
	if err = tx.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		return err
	}
	if role != agentstore.PersonaReviewAuthorityRole {
		return ErrLocalDevPersonaChatBootstrap
	}
	tenantID := b.TenantUUID(tenant)
	if err = tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	grantID := "local-demo/persona-review/" + reviewer + "/admin/" + owner + "/v1"
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenantID.String()+"/"+grantID); err != nil {
		return err
	}
	var principal, permission string
	var valid bool
	err = tx.QueryRow(ctx, `SELECT principal_id,permission,granted_at<=CURRENT_TIMESTAMP AND expires_at>CURRENT_TIMESTAMP AND revoked_at IS NULL FROM persona_review_grant WHERE tenant_id=$1 AND grant_id=$2`, tenantID, grantID).Scan(&principal, &permission, &valid)
	if err == nil {
		if principal != reviewer || permission != "persona:review" || !valid {
			return ErrLocalDevPersonaChatBootstrap
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO persona_review_grant(tenant_id,grant_id,principal_id,permission,granted_at,expires_at) VALUES($1,$2,$3,'persona:review',$4,$5)`, tenantID, grantID, reviewer, now, now.Add(24*time.Hour)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
