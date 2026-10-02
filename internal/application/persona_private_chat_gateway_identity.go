package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

var errPersonaPrivateChatGatewayIdentity = errors.New("application: private persona gateway identity unavailable")

// PersonaPrivateChatDelegationStoreFactory creates a tenant-bound reader for
// the durable on-behalf-of grant attached to an admitted persona run.
type PersonaPrivateChatDelegationStoreFactory interface {
	ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error)
}

// PersonaPrivateChatInstallationReader reads one current installation from
// the tenant's durable persona store.
type PersonaPrivateChatInstallationReader interface {
	GetInstallation(context.Context, string) (agentpersonastore.PersonaInstallation, error)
}

// PersonaPrivateChatInstallationStoreFactory creates a tenant-bound
// installation reader.
type PersonaPrivateChatInstallationStoreFactory interface {
	ForTenant(context.Context, values.TenantId) (PersonaPrivateChatInstallationReader, error)
}

// PersonaPrivateChatWorkloadCredentialSource returns the process-held signed
// worker credential. It must not read a credential from the run request.
type PersonaPrivateChatWorkloadCredentialSource interface {
	PersonaChatWorkerCredential(context.Context) (string, error)
}

// PersonaPrivateChatWorkloadIdentitySource supplies only identities that
// have crossed the configured workload verifier.
type PersonaPrivateChatWorkloadIdentitySource interface {
	ResolvePersonaChatWorker(context.Context) (workload.Identity, error)
}

// VerifiedPersonaPrivateChatWorkloadIdentitySource authenticates a process
// credential through the configured workload verifier.
type VerifiedPersonaPrivateChatWorkloadIdentitySource struct {
	Verifier    *workload.Verifier
	Credentials PersonaPrivateChatWorkloadCredentialSource
}

var _ PersonaPrivateChatWorkloadIdentitySource = (*VerifiedPersonaPrivateChatWorkloadIdentitySource)(nil)

// ResolvePersonaChatWorker verifies the process-held credential and returns
// only the verifier-created identity value.
func (s *VerifiedPersonaPrivateChatWorkloadIdentitySource) ResolvePersonaChatWorker(ctx context.Context) (workload.Identity, error) {
	if s == nil || s.Verifier == nil || isNilPersonaOutputPort(s.Credentials) || ctx == nil || ctx.Err() != nil {
		return workload.Identity{}, errPersonaPrivateChatGatewayIdentity
	}
	credential, err := s.Credentials.PersonaChatWorkerCredential(ctx)
	if err != nil || credential == "" {
		return workload.Identity{}, errPersonaPrivateChatGatewayIdentity
	}
	identity, err := s.Verifier.Verify(ctx, credential)
	if err != nil {
		return workload.Identity{}, errPersonaPrivateChatGatewayIdentity
	}
	return identity, nil
}

// DatabasePersonaPrivateChatGatewayIdentityResolver resolves gateway
// identity only from a current durable grant, current installation, and
// verified worker identity.
type DatabasePersonaPrivateChatGatewayIdentityResolver struct {
	Grants        PersonaPrivateChatDelegationStoreFactory
	Installations PersonaPrivateChatInstallationStoreFactory
	Workload      PersonaPrivateChatWorkloadIdentitySource
	Now           func() time.Time
}

var _ PersonaPrivateChatGatewayIdentityResolver = (*DatabasePersonaPrivateChatGatewayIdentityResolver)(nil)

// NewDatabasePersonaPrivateChatGatewayIdentityResolver binds production
// delegation and persona stores to a verified workload identity source.
func NewDatabasePersonaPrivateChatGatewayIdentityResolver(grants *agentdelegationstore.Store, installations *agentpersonastore.Store, verifier *workload.Verifier, credentials PersonaPrivateChatWorkloadCredentialSource, now func() time.Time) (*DatabasePersonaPrivateChatGatewayIdentityResolver, error) {
	if grants == nil || installations == nil || verifier == nil || isNilPersonaOutputPort(credentials) || now == nil {
		return nil, errPersonaPrivateChatGatewayIdentity
	}
	return &DatabasePersonaPrivateChatGatewayIdentityResolver{
		Grants:        personaPrivateChatDelegationStoreFactory{store: grants},
		Installations: personaPrivateChatInstallationStoreFactory{store: installations},
		Workload:      &VerifiedPersonaPrivateChatWorkloadIdentitySource{Verifier: verifier, Credentials: credentials}, Now: now,
	}, nil
}

type personaPrivateChatDelegationStoreFactory struct{ store *agentdelegationstore.Store }

func (f personaPrivateChatDelegationStoreFactory) ForTenant(ctx context.Context, tenant values.TenantId) (agentdelegation.GrantStore, error) {
	if f.store == nil || ctx == nil {
		return nil, errPersonaPrivateChatGatewayIdentity
	}
	return f.store.ForTenant(ctx, tenant)
}

type personaPrivateChatInstallationStoreFactory struct{ store *agentpersonastore.Store }

func (f personaPrivateChatInstallationStoreFactory) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaPrivateChatInstallationReader, error) {
	if f.store == nil || ctx == nil {
		return nil, errPersonaPrivateChatGatewayIdentity
	}
	return f.store.ForTenant(ctx, tenant)
}

// ResolvePrivateChatGatewayIdentity rereads and validates every durable
// binding needed for the private reply tool. It refuses legacy grants without
// TargetAgentID and never invents the persona.chat_reply capability.
func (r *DatabasePersonaPrivateChatGatewayIdentityResolver) ResolvePrivateChatGatewayIdentity(ctx context.Context, record agentrun.Record, run runstate.Run) (agentsecurity.AgentIdentity, []agentsecurity.DelegationLink, error) {
	if r == nil || ctx == nil || ctx.Err() != nil || r.Grants == nil || r.Installations == nil || isNilPersonaOutputPort(r.Workload) || r.Now == nil || !personaPrivateChatRunTupleMatches(record, run) {
		return agentsecurity.AgentIdentity{}, nil, errPersonaPrivateChatGatewayIdentity
	}
	request := record.Request
	if request.Principal.Mode != agentrun.ModeOnBehalfOf || request.Purpose != "persona-mention" || request.InstallationID == "" || request.Principal.DelegatedCredentialRef == "" {
		return agentsecurity.AgentIdentity{}, nil, errPersonaPrivateChatGatewayIdentity
	}
	tenant := values.TenantId(request.Source.TenantID)
	if tenant.Validate() != nil {
		return agentsecurity.AgentIdentity{}, nil, errPersonaPrivateChatGatewayIdentity
	}
	now := r.Now().UTC()
	if now.IsZero() || ctx.Err() != nil {
		return agentsecurity.AgentIdentity{}, nil, errPersonaPrivateChatGatewayIdentity
	}
	grantStore, err := r.Grants.ForTenant(ctx, tenant)
	if err != nil || grantStore == nil {
		return agentsecurity.AgentIdentity{}, nil, fmt.Errorf("%w: tenant delegation store: %v", errPersonaPrivateChatGatewayIdentity, err)
	}
	grant, err := grantStore.Get(request.Principal.DelegatedCredentialRef)
	if err != nil || !privatePersonaReplyGrantMatches(grant, record, now) {
		return agentsecurity.AgentIdentity{}, nil, errPersonaPrivateChatGatewayIdentity
	}
	epoch := grantStore.CurrentRevocationEpoch(tenant, grant.UserID)
	if epoch == 0 || epoch != grant.RevocationEpoch || grant.Revoked || grant.Authority.Revoked {
		return agentsecurity.AgentIdentity{}, nil, errPersonaPrivateChatGatewayIdentity
	}
	installationStore, err := r.Installations.ForTenant(ctx, tenant)
	if err != nil || installationStore == nil {
		return agentsecurity.AgentIdentity{}, nil, fmt.Errorf("%w: tenant persona store: %v", errPersonaPrivateChatGatewayIdentity, err)
	}
	installation, err := installationStore.GetInstallation(ctx, request.InstallationID)
	if err != nil || !privatePersonaReplyInstallationMatches(installation, record) {
		return agentsecurity.AgentIdentity{}, nil, errPersonaPrivateChatGatewayIdentity
	}
	worker, err := r.Workload.ResolvePersonaChatWorker(ctx)
	if err != nil || !privatePersonaReplyWorkerMatches(worker, now) {
		return agentsecurity.AgentIdentity{}, nil, errPersonaPrivateChatGatewayIdentity
	}
	identity := agentsecurity.AgentIdentity{
		Identity: "workload:" + worker.Issuer() + "/" + worker.Subject() + "#" + worker.Fingerprint(), AgentID: request.Agent.AgentID,
		Tenant: request.Source.TenantID, Purpose: request.Purpose,
		ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 1,
	}
	link := agentsecurity.DelegationLink{
		GrantID: grant.GrantID, Delegator: grant.UserID, Delegate: grant.TargetAgentID,
		Tenant: grant.Tenant.String(), Purpose: grant.Purpose,
		ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 1,
	}
	return identity, []agentsecurity.DelegationLink{link}, nil
}

func privatePersonaReplyGrantMatches(grant agentdelegation.Grant, record agentrun.Record, now time.Time) bool {
	request := record.Request
	const skillID = "persona.chat_reply"
	const capabilityID = "persona.reply"
	const chatScope = "chat.current"
	scope := grant.SkillScopes[skillID]
	bound := personaChatAuthorityResource(request.Source.TenantID, request.Audience.ID, request.Context.ID, request.Source.Ref)
	authority, authorityOK := grant.SkillAuthorities[skillID]
	embedded, embeddedOK := grant.Authority.SkillAuthorities[skillID]
	return grant.GrantID == request.Principal.DelegatedCredentialRef && grant.UserID == request.Principal.InvokerID && grant.Tenant.String() == request.Source.TenantID &&
		request.Persona != nil && grant.AgentVersion == request.Persona.Version && grant.TargetAgentID != "" && grant.TargetAgentID == request.Agent.AgentID && grant.Authority.Delegate == request.Agent.AgentID &&
		grant.Authority.GrantID == grant.GrantID && grant.Authority.RootID == grant.GrantID && grant.Authority.Delegator == grant.UserID &&
		grant.Authority.Tenant == grant.Tenant && slices.Contains(grant.Authority.Purposes, request.Purpose) &&
		grant.Authority.RevocationEpoch == grant.RevocationEpoch && !grant.Authority.NotBefore.After(now) && now.Before(grant.Authority.ExpiresAt) &&
		grant.Authority.NotBefore.Equal(grant.NotBefore) && grant.Authority.ExpiresAt.Equal(grant.ExpiresAt) &&
		grant.InstallationID == request.InstallationID && grant.TaskID == request.Source.Key && grant.Purpose == request.Purpose &&
		!grant.NotBefore.After(now) && now.Before(grant.ExpiresAt) && !grant.Revoked && !grant.Authority.Revoked && grant.RevocationEpoch > 0 &&
		slices.Contains(grant.Skills, skillID) && len(scope) == 1 && authorityOK && embeddedOK &&
		(scope[0] == capabilityID &&
			privatePersonaReplySkillAuthorityMatches(authority, capabilityID, chatScope, request.Purpose) &&
			privatePersonaReplySkillAuthorityMatches(embedded, capabilityID, chatScope, request.Purpose) ||
			// A grant issued from the invoker's chat authority projection names
			// the delegated scope as its capability and binds it to the exact
			// tenant, conversation, thread and invoking post of this run.
			scope[0] == chatScope &&
				privatePersonaReplySkillAuthorityMatches(authority, chatScope, bound, request.Purpose) &&
				privatePersonaReplySkillAuthorityMatches(embedded, chatScope, bound, request.Purpose))
}

func privatePersonaReplySkillAuthorityMatches(authority trust.SkillAuthority, capabilityID, resource, purpose string) bool {
	return len(authority.Capabilities) == 1 && authority.Capabilities[0] == capabilityID &&
		len(authority.Resources) == 1 && authority.Resources[0] == resource && len(authority.Fields) == 0 &&
		len(authority.Purposes) == 1 && authority.Purposes[0] == purpose
}

func privatePersonaReplyInstallationMatches(installation agentpersonastore.PersonaInstallation, record agentrun.Record) bool {
	request := record.Request
	version, err := personaRunVersionNumber(request.Persona.Version)
	if err != nil {
		return false
	}
	if installation.TenantID.String() != request.Source.TenantID || installation.InstallationID != request.InstallationID ||
		installation.PersonaID != request.Persona.ID || installation.PersonaVersion == 0 || installation.PersonaVersion != version ||
		installation.ConversationID != request.Audience.ID || installation.State != agentpersonastore.InstallationActive || installation.Revision <= 0 || installation.RevocationEpoch <= 0 {
		return false
	}
	switch installation.ConversationClass {
	case agentpersonastore.ConversationPrivate, agentpersonastore.ConversationGroupDM, agentpersonastore.ConversationOneToOne:
		return true
	default:
		return false
	}
}

func privatePersonaReplyWorkerMatches(identity workload.Identity, now time.Time) bool {
	return identity.Role() == workload.RoleWorker && identity.Issuer() != "" && identity.Subject() != "" && identity.Cell() != "" &&
		identity.KeyID() != "" && identity.Fingerprint() != "" && identity.ValidAt(now)
}
