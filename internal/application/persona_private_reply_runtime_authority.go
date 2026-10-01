package application

import (
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

var errPersonaPrivateReplyAuthorityComposition = errors.New("application: private persona reply authority composition unavailable")

// PersonaPrivateReplyAuthorityComposition supplies current scope, grant,
// installation and worker proofs around the output schema/grounding authority.
type PersonaPrivateReplyAuthorityComposition struct {
	Base               PersonaRunChatReplyAuthoritySource
	Gate               *agentgate.Gate
	ScopeBuilder       PersonaPrivateChatScopeRequestBuilder
	ScopeAuthorizer    agentgate.PrivateChatScopeAuthorizer
	Grants             *agentdelegationstore.Store
	Installations      *agentpersonastore.Store
	WorkloadVerifier   *workload.Verifier
	WorkloadCredential PersonaPrivateChatWorkloadCredentialSource
	Now                func() time.Time
}

// NewPersonaPrivateChatReplyAuthoritySource composes current private-chat
// projection and durable grant identity over the caller's schema and
// grounding source. Its resulting gateway admission is minted by Base.Gateway.
func NewPersonaPrivateChatReplyAuthoritySource(cfg PersonaPrivateReplyAuthorityComposition) (*PersonaPrivateChatReplyAuthoritySource, error) {
	if isNilPersonaOutputPort(cfg.Base) || cfg.Gate == nil || isNilPersonaOutputPort(cfg.ScopeBuilder) || isNilPersonaOutputPort(cfg.ScopeAuthorizer) ||
		cfg.Grants == nil || cfg.Installations == nil || cfg.WorkloadVerifier == nil || isNilPersonaOutputPort(cfg.WorkloadCredential) || cfg.Now == nil {
		return nil, errPersonaPrivateReplyAuthorityComposition
	}
	identity, err := NewDatabasePersonaPrivateChatGatewayIdentityResolver(cfg.Grants, cfg.Installations, cfg.WorkloadVerifier, cfg.WorkloadCredential, cfg.Now)
	if err != nil {
		return nil, errPersonaPrivateReplyAuthorityComposition
	}
	projection := &PersonaPrivateChatSkillAuthorizationAdapter{
		Gate: cfg.Gate, Builder: cfg.ScopeBuilder, Chat: cfg.ScopeAuthorizer,
	}
	return &PersonaPrivateChatReplyAuthoritySource{Base: cfg.Base, Projection: projection, Identity: identity}, nil
}

// NewPersonaPrivateReplyRuntimeWithCurrentAuthority constructs the private
// output runtime only after composing current scope and durable identity
// authority. Shared-channel output stays disabled by the base runtime.
func NewPersonaPrivateReplyRuntimeWithCurrentAuthority(runtime PersonaPrivateReplyRuntimeConfig, authority PersonaPrivateReplyAuthorityComposition) (*PersonaPrivateReplyRuntime, error) {
	if !isNilPersonaOutputPort(runtime.OutputAuthority) {
		return nil, errPersonaPrivateReplyRuntimeUnavailable
	}
	source, err := NewPersonaPrivateChatReplyAuthoritySource(authority)
	if err != nil {
		return nil, errPersonaPrivateReplyRuntimeUnavailable
	}
	runtime.OutputAuthority = source
	return NewPersonaPrivateReplyRuntime(runtime)
}

var _ PersonaRunChatReplyAuthoritySource = (*PersonaPrivateChatReplyAuthoritySource)(nil)
