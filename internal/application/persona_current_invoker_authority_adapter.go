package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errTrustedSkillInvokerAuthority = errors.New("application: trusted skill invoker authority unavailable")

// TrustedSkillInvokerAuthoritySource binds a current per-skill authority
// projection to current chat membership and discoverable skill scopes.
// Projection must be backed by the server's current policy decision point.
type TrustedSkillInvokerAuthoritySource struct {
	Invoker    *ServerTrustedInvokerSource
	Projection InvokerAuthoritySource
}

// NewTrustedSkillInvokerAuthoritySource requires both authenticated membership
// and a current per-skill authority projector. It has no flat-scope fallback.
func NewTrustedSkillInvokerAuthoritySource(invoker *ServerTrustedInvokerSource, projection InvokerAuthoritySource) (*TrustedSkillInvokerAuthoritySource, error) {
	if invoker == nil || projection == nil {
		return nil, errTrustedSkillInvokerAuthority
	}
	return &TrustedSkillInvokerAuthoritySource{Invoker: invoker, Projection: projection}, nil
}

var _ InvokerAuthoritySource = (*TrustedSkillInvokerAuthoritySource)(nil)

// ResolveInvokerAuthority rechecks the signed-in human, current membership,
// discoverability and exact per-skill authority for every grant decision.
func (s *TrustedSkillInvokerAuthoritySource) ResolveInvokerAuthority(ctx context.Context, userID string, tenant values.TenantId, purpose string, at time.Time) (agentdelegation.UserAuthority, error) {
	denied := agentdelegation.UserAuthority{UserID: userID}
	if s == nil || s.Invoker == nil || s.Projection == nil || ctx == nil || strings.TrimSpace(userID) == "" || tenant.Validate() != nil || strings.TrimSpace(purpose) == "" || at.IsZero() {
		return denied, errTrustedSkillInvokerAuthority
	}
	trusted, err := s.Invoker.ResolveTrustedInvoker(ctx, tenant.String(), userID)
	if err != nil {
		return denied, fmt.Errorf("resolve current persona invoker: %w", err)
	}
	if !trusted.HumanMember || !trusted.AudienceMember || trusted.ID != userID || trusted.TenantID != tenant.String() || len(trusted.Discoverable) == 0 {
		return denied, errTrustedSkillInvokerAuthority
	}
	current, err := s.Projection.ResolveInvokerAuthority(ctx, userID, tenant, purpose, at)
	if err != nil {
		return denied, fmt.Errorf("resolve current per-skill authority: %w", err)
	}
	if !current.Active || current.UserID != userID || current.Authority.Tenant != tenant || current.Authority.OrganizationScopeID == "" || len(current.SkillAuthorities) == 0 || len(current.Authority.SkillAuthorities) == 0 {
		return denied, errTrustedSkillInvokerAuthority
	}
	if !sameSkillAuthorities(current.SkillAuthorities, current.Authority.SkillAuthorities) {
		return denied, errTrustedSkillInvokerAuthority
	}
	principal, err := verifiedHumanPrincipal(ctx, tenant.String(), userID, purpose)
	if err != nil {
		return denied, err
	}
	narrowed := make(trust.SkillAuthorities)
	for skill, authority := range current.SkillAuthorities {
		discoverable, ok := trusted.Discoverable[skill]
		if !ok {
			return denied, errTrustedSkillInvokerAuthority
		}
		allowed := intersectAuthorityValues(authority.Capabilities, discoverable)
		if len(allowed) == 0 {
			continue
		}
		authority.Capabilities = allowed
		narrowed[skill] = authority
	}
	if len(narrowed) == 0 {
		return denied, errTrustedSkillInvokerAuthority
	}
	scope := authorityScopeFromSkills(principal, current.Authority.OrganizationScopeID, narrowed, at)
	return agentdelegation.UserAuthority{
		UserID: userID, Active: true, Authority: scope,
		SkillAuthorities: trust.CloneSkillAuthorities(narrowed),
	}, nil
}

func intersectAuthorityValues(authority, discoverable []string) []string {
	allowed := make(map[string]struct{}, len(discoverable))
	for _, value := range discoverable {
		allowed[value] = struct{}{}
	}
	result := make([]string, 0, len(authority))
	for _, value := range authority {
		if _, ok := allowed[value]; ok {
			result = append(result, value)
		}
	}
	return projectionUniqueSorted(result)
}
