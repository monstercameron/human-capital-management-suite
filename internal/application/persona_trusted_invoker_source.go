package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrPersonaTrustedInvokerUnavailable means that the authenticated principal
// or one of the current authority projections was missing or inconsistent.
// It intentionally does not distinguish membership, role, or skill failures.
var ErrPersonaTrustedInvokerUnavailable = errors.New("persona trusted invoker: current authority unavailable")

// PersonaCurrentMembershipSource supplies the current chat membership for the
// subject in the active invocation context. Implementations must read the
// chat owner store and must not accept membership or role facts from a post.
type PersonaCurrentMembershipSource interface {
	ResolveCurrentMembership(context.Context, string, string) (bool, error)
}

// PersonaInvokerSkillSource supplies the exact skill scopes discovered for a
// verified principal and purpose. It is deliberately narrower than a skill
// catalog: callers cannot turn a published skill into authority themselves.
type PersonaInvokerSkillSource interface {
	ResolveInvokerSkills(context.Context, *trust.Principal, string) (agentinvoke.SkillScopes, error)
}

// TrustedInvokerSourceConfig composes the current chat and discovery facts.
// The principal is always taken from the authenticated context.
type TrustedInvokerSourceConfig struct {
	Membership PersonaCurrentMembershipSource
	Skills     PersonaInvokerSkillSource
	Purpose    string
}

// ServerTrustedInvokerSource resolves the invoker projection used by persona
// admission. Every call re-reads membership and discoverable skills.
type ServerTrustedInvokerSource struct {
	membership PersonaCurrentMembershipSource
	skills     PersonaInvokerSkillSource
	purpose    string
}

// NewServerTrustedInvokerSource constructs a fail-closed trusted invoker
// source. Purpose is bound by composition and cannot be supplied by a request.
func NewServerTrustedInvokerSource(cfg TrustedInvokerSourceConfig) (*ServerTrustedInvokerSource, error) {
	if cfg.Membership == nil || cfg.Skills == nil || strings.TrimSpace(cfg.Purpose) == "" {
		return nil, ErrPersonaTrustedInvokerUnavailable
	}
	return &ServerTrustedInvokerSource{membership: cfg.Membership, skills: cfg.Skills, purpose: cfg.Purpose}, nil
}

var _ TrustedInvokerSource = (*ServerTrustedInvokerSource)(nil)

// ResolveTrustedInvoker resolves only a human principal that is present in
// the authenticated context and currently belongs to chat. Tenant and
// subject arguments select the record; they never provide authority.
func (s *ServerTrustedInvokerSource) ResolveTrustedInvoker(ctx context.Context, tenantID, subjectID string) (TrustedInvoker, error) {
	if s == nil || s.membership == nil || s.skills == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(subjectID) == "" {
		return TrustedInvoker{}, ErrPersonaTrustedInvokerUnavailable
	}
	principal, err := verifiedHumanPrincipal(ctx, tenantID, subjectID, s.purpose)
	if err != nil {
		return TrustedInvoker{}, err
	}
	member, err := s.membership.ResolveCurrentMembership(ctx, tenantID, subjectID)
	if err != nil {
		return TrustedInvoker{}, fmt.Errorf("resolve current chat membership: %w", err)
	}
	if !member {
		return TrustedInvoker{}, ErrPersonaTrustedInvokerUnavailable
	}
	skills, err := s.skills.ResolveInvokerSkills(ctx, principal, s.purpose)
	if err != nil {
		return TrustedInvoker{}, fmt.Errorf("resolve discoverable persona skills: %w", err)
	}
	if len(skills) == 0 {
		return TrustedInvoker{}, ErrPersonaTrustedInvokerUnavailable
	}
	return TrustedInvoker{ID: principal.Subject(), TenantID: principal.Tenant().String(), HumanMember: true, AudienceMember: true, Discoverable: skills.Clone()}, nil
}

// ServerInvokerAuthoritySource resolves the authority used to issue a
// persona delegation grant. It narrows the verified principal's authority to
// the current membership and discovered skill set on every call.
type ServerInvokerAuthoritySource struct {
	Invoker *ServerTrustedInvokerSource
}

// ResolveInvokerAuthority implements InvokerAuthoritySource using only the
// verified principal, current chat membership, and current skill discovery.
func (s *ServerInvokerAuthoritySource) ResolveInvokerAuthority(ctx context.Context, userID string, tenant values.TenantId, purpose string, at time.Time) (agentdelegation.UserAuthority, error) {
	denied := agentdelegation.UserAuthority{UserID: userID}
	if s == nil || s.Invoker == nil || strings.TrimSpace(userID) == "" || tenant.Validate() != nil || strings.TrimSpace(purpose) == "" || at.IsZero() {
		return denied, ErrPersonaTrustedInvokerUnavailable
	}
	if purpose != s.Invoker.purpose {
		return denied, ErrPersonaTrustedInvokerUnavailable
	}
	trusted, err := s.Invoker.ResolveTrustedInvoker(ctx, tenant.String(), userID)
	if err != nil {
		return denied, err
	}
	principal, err := verifiedHumanPrincipal(ctx, tenant.String(), userID, purpose)
	if err != nil {
		return denied, err
	}
	// The legacy UserAuthority contract has separate flat capability and
	// resource lists, while persona discovery carries a map of exact scopes per
	// skill. Flattening that map creates a cross-product and can widen a grant.
	// Refuse until the contract can carry the map end-to-end.
	_ = trusted
	_ = principal
	return denied, fmt.Errorf("%w: delegation contract cannot preserve per-skill scopes", ErrPersonaTrustedInvokerUnavailable)
}

var _ InvokerAuthoritySource = (*ServerInvokerAuthoritySource)(nil)

func verifiedHumanPrincipal(ctx context.Context, tenantID, subjectID, purpose string) (*trust.Principal, error) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != tenantID || principal.Subject() != subjectID || !principal.AuthorizesPurpose(purpose) || strings.TrimSpace(principal.SessionRef()) == "" {
		return nil, ErrPersonaTrustedInvokerUnavailable
	}
	return principal, nil
}
