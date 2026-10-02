package application

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaInvocationProductionComposition = errors.New("application: production persona invocation composition unavailable")

// PersonaInvocationProductionConfig contains the live serving ports that
// cannot be constructed from the isolated agent database alone. Every
// authority, model, output, delivery, and kill-switch dependency is explicit.
type PersonaInvocationProductionConfig struct {
	AgentStore *agentstore.Store
	TenantUUID func(values.TenantId) uuid.UUID

	Chat       personaChatPostWriter
	References personaReferenceResolver
	Authority  agentinvoke.AuthorityResolver
	Grants     agentinvoke.GrantIssuer
	T0Skills   personaT0SkillPolicy
	Failures   personaInvocationFailureSink
	// Limits is the mention admission (rate, concurrency and daily spend).
	// The served composition binds it to the agent database; a runtime
	// without it is refused, so no served mention runs unmetered.
	Limits *PersonaMentionLimits
	// Conversations reads the conversation and its members so a question in a
	// person's own conversation with an agent is recognized (AGENTUX-075).
	Conversations personaDirectConversationReader
	// Reactions puts the agent's reaction on the question it answers.
	Reactions personaQuestionReactor
	// Steps is the board the runs report their step on (AGENTUX-075).
	Steps *personaRunStepBoard

	Run                PersonaRunStarterConfig
	Fence              PersonaRunSecurityFence
	Leases             PersonaRunSecurityLeaseResolver
	BackgroundRecovery PersonaBackgroundOutputRecovery
}

// PersonaInvocationProductionRuntime is the served chat handoff and its
// execution worker. Wiring.Chat is the post-commit boundary installed into
// streaming chat; Worker is the durable model execution starter it invokes.
type PersonaInvocationProductionRuntime struct {
	Wiring     *PersonaInvocationServeWiring
	Worker     *PersonaRunModelWorker
	Background *PersonaBackgroundDispatcher
}

// NewDatabasePersonaInvocationProductionRuntime binds invocation claims to
// the isolated agent database and composes tenant-scoped durable run stores,
// current admission rechecks, and the fenced model worker. Model work,
// authority, output validation/persistence, reply delivery, and the security
// lease resolver must be supplied by production callers; no in-memory or
// permissive implementation is substituted.
func NewDatabasePersonaInvocationProductionRuntime(cfg PersonaInvocationProductionConfig) (*PersonaInvocationProductionRuntime, error) {
	if cfg.AgentStore == nil || cfg.TenantUUID == nil || isNilPersonaOutputPort(cfg.Chat) || isNilPersonaOutputPort(cfg.References) ||
		isNilPersonaOutputPort(cfg.Authority) || isNilPersonaOutputPort(cfg.Grants) || isNilPersonaOutputPort(cfg.T0Skills) || cfg.Run.Builder == nil ||
		isNilPersonaOutputPort(cfg.Run.Authority) || isNilPersonaOutputPort(cfg.Run.Model) || isNilPersonaOutputPort(cfg.Run.Work) ||
		isNilPersonaOutputPort(cfg.Run.Output) || isNilPersonaOutputPort(cfg.Run.Reply) ||
		cfg.Run.Now == nil || strings.TrimSpace(cfg.Run.WorkerID) == "" || isNilPersonaOutputPort(cfg.Fence) || isNilPersonaOutputPort(cfg.Leases) {
		return nil, errPersonaInvocationProductionComposition
	}
	if err := cfg.Limits.validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", errPersonaInvocationProductionComposition, err)
	}
	invocations, err := agentinvocationstore.NewWithTenantUUID(cfg.AgentStore, func(tenant string) uuid.UUID { return cfg.TenantUUID(values.TenantId(tenant)) })
	if err != nil {
		return nil, fmt.Errorf("%w: durable invocation repository: %v", errPersonaInvocationProductionComposition, err)
	}
	tenantRuntime, err := NewDatabasePersonaRunTenantRuntimeFactory(cfg.AgentStore, cfg.TenantUUID, cfg.Run)
	if err != nil {
		return nil, fmt.Errorf("%w: tenant durable run runtime: %v", errPersonaInvocationProductionComposition, err)
	}
	worker, err := NewPersonaRunModelWorker(PersonaRunModelWorkerConfig{
		Tenants: tenantRuntime, Fence: cfg.Fence, Leases: cfg.Leases,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: fenced persona model worker: %v", errPersonaInvocationProductionComposition, err)
	}
	var background *PersonaBackgroundDispatcher
	if !isNilPersonaOutputPort(cfg.BackgroundRecovery) {
		background, err = NewPersonaBackgroundDispatcher(cfg.AgentStore, cfg.TenantUUID, worker, cfg.BackgroundRecovery)
		if err != nil {
			return nil, fmt.Errorf("%w: background worker recovery: %v", errPersonaInvocationProductionComposition, err)
		}
	}
	chat, err := newPersonaChatInvocation(personaChatInvocationConfig{
		Chat: cfg.Chat, References: cfg.References, Authority: cfg.Authority, Grants: cfg.Grants,
		Runs: worker, T0Skills: cfg.T0Skills, Repository: invocations, Failures: cfg.Failures, Limits: cfg.Limits,
		Conversations: cfg.Conversations, Reactions: cfg.Reactions, Steps: cfg.Steps,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: post-commit invocation: %v", errPersonaInvocationProductionComposition, err)
	}
	return &PersonaInvocationProductionRuntime{
		Wiring: &PersonaInvocationServeWiring{Chat: chat}, Worker: worker, Background: background,
	}, nil
}

var _ agentinvoke.RunStarter = (*PersonaRunModelWorker)(nil)
var _ PersonaRunTenantRuntimeFactory = (*DatabasePersonaRunTenantRuntimeFactory)(nil)
var _ PersonaRunTenantRuntimeValidator = (*DatabasePersonaRunTenantRuntimeFactory)(nil)
var _ PersonaRunLeaseFence = (*agentsecurity.PersonaRunFence)(nil)
