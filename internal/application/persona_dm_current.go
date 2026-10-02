package application

import (
	"context"
	"math"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaDMIdentityReader interface {
	ListPersonaChatIdentities(context.Context, string) ([]agentpersonastore.PersonaChatIdentity, error)
}

// publishedPersonaDM resolves the canonical chat identity from current owner
// stores on each private delivery, then creates or reuses its exact invoker DM.
type publishedPersonaDM struct {
	publications PersonaIdentityPublicationSource
	identities   personaDMIdentityReader
	store        chatcore.Store
	creator      personaDMConversationCreator
	now          func() time.Time
}

func (r *publishedPersonaDM) ResolvePersonaDM(ctx context.Context, principal chatcore.Principal, tenant string) (string, error) {
	if r == nil || ctx == nil || r.publications == nil || r.identities == nil || r.store == nil || r.creator == nil || r.now == nil {
		return "", errPersonaDMResolverUnavailable
	}
	invocation, ok := personaDMInvocationFromContext(ctx)
	verified, authenticated := trust.FromContext(ctx)
	now := r.now().UTC()
	if !ok || !authenticated || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman ||
		verified.Tenant().String() != tenant || verified.Subject() != principal.SubjectID || principal.TenantID != tenant ||
		invocation.TenantID != tenant || invocation.InvokerID != principal.SubjectID || !verified.AuthorizesPurpose("persona-mention") ||
		now.IsZero() || verified.IssuedAt().After(now) || !verified.ExpiresAt().After(now) || !invocation.Grant.ExpiresAt.After(now) {
		return "", errPersonaDMInvocation
	}
	version, err := personaRunVersionNumber(invocation.PersonaVersion)
	if err != nil || version > math.MaxUint32 {
		return "", errPersonaDMInvocation
	}
	published, err := r.publications.PublishedPersona(ctx, values.TenantId(tenant), invocation.PersonaID, uint32(version))
	if err != nil || published.Profile.PersonaID != invocation.PersonaID || published.Profile.Version != uint32(version) {
		return "", chatcore.ErrPermissionDenied
	}
	identities, err := r.identities.ListPersonaChatIdentities(ctx, tenant)
	if err != nil {
		return "", errPersonaDMResolverUnavailable
	}
	member := chatcore.MemberRef{}
	for _, identity := range identities {
		if identity.PersonaID != invocation.PersonaID {
			continue
		}
		if identity.TenantID.String() != tenant || !identity.Active || identity.RevokedAt != nil || !required(identity.AgentID) ||
			identity.RegisteredAt.IsZero() || identity.RegisteredAt.After(now) || member.SubjectID != "" {
			return "", chatcore.ErrPermissionDenied
		}
		member = chatcore.MemberRef{TenantID: tenant, SubjectID: identity.AgentID}
	}
	if member.SubjectID == "" {
		return "", chatcore.ErrPermissionDenied
	}
	resolver, err := NewPersonaDMResolver(r.store, member)
	if err != nil {
		return "", err
	}
	provisioner, err := NewPersonaDMProvisioner(r.creator, resolver, member)
	if err != nil {
		return "", err
	}
	// AGENTUX-030: the conversation is named after the published agent.
	provisioner.DisplayName = published.Profile.DisplayName
	if policies, ok := r.store.(personaDMPolicyStore); ok {
		provisioner.Policies = policies
	}
	return provisioner.EnsurePersonaDM(ctx, principal, tenant)
}
