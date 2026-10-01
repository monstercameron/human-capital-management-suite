package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// verifyOnBehalfOf requires independently issued current user authority and
// the actual durable delegation. An active role alone grants no capability.
func (a *CommonAgentAuthority) verifyOnBehalfOf(ctx context.Context, tx dbport.Tx, tenant, principal uuid.UUID, request agentrun.Request, manifestBudget agentrun.Budget, now time.Time) (agentrun.AuthoritySnapshot, error) {
	agentProof, agentScope, err := readCommonAgentBinding(ctx, tx, tenant, principal, request, now)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	userProof, userScope, err := readCommonAgentUserBinding(ctx, tx, tenant, request, now)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	// Release the core read transaction before the delegation repository opens
	// its own tenant transaction, including on single-connection adapters.
	if err := tx.Commit(ctx); err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	factory, err := agentdelegationstore.New(a.cfg.CoreDB, a.cfg.TenantUUID)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	store, err := factory.ForTenant(ctx, values.TenantId(request.Source.TenantID))
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	grant, err := store.Get(request.Principal.DelegatedCredentialRef)
	if err != nil || !commonAgentOBOGrantMatches(grant, request, now, store.CurrentRevocationEpoch(values.TenantId(request.Source.TenantID), request.Principal.InvokerID)) {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("CURRENT_DELEGATION_DENIED")
	}
	current := *userScope.UserAuthority
	if current.Tenant.String() != request.Source.TenantID || current.OrganizationScopeID != grant.OrganizationScopeID ||
		current.SkillAuthorities == nil || current.NotBefore.After(now) || !current.ExpiresAt.After(now) {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("CURRENT_USER_SCOPE_DENIED")
	}
	service, err := agentdelegation.NewService(agentdelegation.Config{Store: store, Now: func() time.Time { return now },
		Authority: agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, purpose string, at time.Time) (agentdelegation.UserAuthority, error) {
			if user != request.Principal.InvokerID || tenant.String() != request.Source.TenantID || purpose != request.Purpose || !at.Equal(now) {
				return agentdelegation.UserAuthority{}, agentdelegation.ErrUserInactive
			}
			return agentdelegation.UserAuthority{UserID: user, Active: true, Authority: current, SkillAuthorities: trust.CloneSkillAuthorities(current.SkillAuthorities)}, nil
		})})
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	// The delegation owner repeats current epoch and authentic parent lineage
	// checks against the independently issued user ceiling. No token is minted.
	effective, err := service.CurrentAuthority(grant.GrantID)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: %v", commonAgentRefusal("CURRENT_DELEGATION_DENIED"), err)
	}
	if request.Deadline.After(effective.ExpiresAt) || !commonAgentSkillsCurrent(grant, effective) {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("CURRENT_SKILL_SCOPE_DENIED")
	}
	ceiling := commonAgentBudgetIntersection(agentScope.BudgetCeiling, userScope.BudgetCeiling, manifestBudget)
	proof, err := json.Marshal([]any{agentProof, agentScope, userProof, userScope, grant})
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	sum := sha256.Sum256(proof)
	return agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal,
		Audience: request.Audience, Context: request.Context, BudgetCeiling: ceiling, GrantRef: grant.GrantID, PolicyDigest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

func commonAgentSkillsCurrent(grant agentdelegation.Grant, current trust.EffectiveAuthority) bool {
	for _, skill := range grant.Skills {
		ceiling, ok := current.SkillAuthorities[skill]
		original := grant.SkillAuthorities[skill]
		if !ok || !commonAgentStringSubset(grant.SkillScopes[skill], ceiling.Capabilities) ||
			!commonAgentStringSubset(original.Resources, ceiling.Resources) || !commonAgentStringSubset(original.Fields, ceiling.Fields) ||
			!commonAgentStringSubset(original.Purposes, ceiling.Purposes) {
			return false
		}
	}
	return true
}

func commonAgentStringSubset(want, allowed []string) bool {
	for _, value := range want {
		if !slices.Contains(allowed, value) {
			return false
		}
	}
	return true
}

func commonAgentOBOGrantMatches(grant agentdelegation.Grant, request agentrun.Request, now time.Time, epoch uint64) bool {
	return agentdelegation.ValidateGrant(grant) == nil && epoch > 0 && epoch != ^uint64(0) && !grant.Revoked && !grant.Authority.Revoked &&
		grant.GrantID == request.Principal.DelegatedCredentialRef && grant.UserID == request.Principal.InvokerID && grant.Tenant.String() == request.Source.TenantID &&
		grant.TargetAgentID == request.Agent.AgentID && commonAgentGrantVersionMatches(grant.AgentVersion, request.Agent) && grant.InstallationID == request.InstallationID &&
		grant.Purpose == request.Purpose && grant.RevocationEpoch == epoch && !grant.NotBefore.After(now) && grant.ExpiresAt.After(now) &&
		!request.Deadline.After(grant.ExpiresAt) && len(grant.Skills) > 0 && grant.SkillAuthorities != nil && grant.Authority.SkillAuthorities != nil &&
		grant.Authority.Delegator == request.Principal.InvokerID && grant.Authority.Tenant == grant.Tenant && grant.Authority.RevocationEpoch == epoch &&
		grant.Authority.OrganizationScopeID == grant.OrganizationScopeID && grant.Authority.NotBefore.Equal(grant.NotBefore) && grant.Authority.ExpiresAt.Equal(grant.ExpiresAt)
}

func commonAgentGrantVersionMatches(version string, agent agentrun.VersionRef) bool {
	return version == agent.Version || version == agent.AgentID+"@"+agent.Version
}

func commonAgentBudgetIntersection(budgets ...agentrun.Budget) agentrun.Budget {
	var result agentrun.Budget
	for i, budget := range budgets {
		if i == 0 {
			result = budget
			continue
		}
		result.MaxCostMicros = commonAgentMinimum(result.MaxCostMicros, budget.MaxCostMicros)
		result.MaxInputTokens = commonAgentMinimum(result.MaxInputTokens, budget.MaxInputTokens)
		result.MaxOutputTokens = commonAgentMinimum(result.MaxOutputTokens, budget.MaxOutputTokens)
	}
	return result
}

func readCommonAgentUserBinding(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, request agentrun.Request, now time.Time) (string, CommonAgentBindingScope, error) {
	rows, err := tx.Query(ctx, `SELECT b.binding_id::text,b.scope,p.revocation_epoch,s.version,s.content_digest
		FROM principal p JOIN authority_binding b ON b.tenant_id=p.tenant_id AND b.principal_id=p.principal_id
		JOIN authority_source s ON s.tenant_id=b.tenant_id AND s.authority_source_id=b.authority_source_id
		WHERE p.tenant_id=$1 AND p.subject=$2 AND p.kind='USER' AND p.lifecycle='ACTIVE'
		AND (p.expires_at IS NULL OR p.expires_at>$3) AND b.valid_from<=$3 AND (b.valid_to IS NULL OR b.valid_to>$3)
		AND s.kind='POLICY_BUNDLE' AND s.valid_interval @> $3::timestamptz`, tenant, request.Principal.InvokerID, now)
	if err != nil {
		return "", CommonAgentBindingScope{}, err
	}
	defer rows.Close()
	var proof string
	var selected CommonAgentBindingScope
	for rows.Next() {
		var id, digest string
		var raw []byte
		var epoch, version int64
		if err := rows.Scan(&id, &raw, &epoch, &version, &digest); err != nil {
			return "", CommonAgentBindingScope{}, err
		}
		var scope CommonAgentBindingScope
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&scope) != nil || decoder.Decode(new(any)) != io.EOF || !commonAgentBindingMatches(scope, request, now) || scope.UserAuthority == nil {
			continue
		}
		if proof != "" || epoch < 1 || version < 1 || !personaRunAuthorityDigest("sha256:"+digest) {
			return "", CommonAgentBindingScope{}, commonAgentRefusal("AMBIGUOUS_USER_AUTHORITY")
		}
		proof, selected = fmt.Sprintf("%s@%d:%d:%s", id, epoch, version, digest), scope
	}
	if err := rows.Err(); err != nil {
		return "", CommonAgentBindingScope{}, err
	}
	if proof == "" {
		return "", CommonAgentBindingScope{}, commonAgentRefusal("CURRENT_USER_BINDING_DENIED")
	}
	return proof, selected, nil
}
