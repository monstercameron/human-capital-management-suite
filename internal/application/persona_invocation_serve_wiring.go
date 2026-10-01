package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

var errPersonaInvocationServeWiring = errors.New("application: persona invocation serve wiring unavailable")

// PersonaInvocationServeConfig is the complete set of server-owned ports
// needed to join committed chat posts to durable AgentRun admission. Callers
// must supply current authority and tenant-scoped durable repositories.
type PersonaInvocationServeConfig struct {
	Chat        personaChatPostWriter
	References  personaReferenceResolver
	Authority   agentinvoke.AuthorityResolver
	Grants      agentinvoke.GrantIssuer
	T0Skills    personaT0SkillPolicy
	Invocations agentinvoke.InvocationRepository
	Run         PersonaRunBindingConfig
	Failures    personaInvocationFailureSink
}

// PersonaInvocationServeWiring exposes the chat post writer that must be
// installed at the served chat boundary. The writer commits the human post
// before attempting persona admission; accepted invocations enter durable
// AgentRun state through RunBinding.
type PersonaInvocationServeWiring struct {
	Chat       *personaChatInvocation
	RunBinding *PersonaRunBinding
}

// NewPersonaInvocationServeWiring joins canonical post-commit invocation to
// durable run admission. Any missing authority, resolver, store, or policy
// dependency fails composition rather than installing a partial path.
func NewPersonaInvocationServeWiring(cfg PersonaInvocationServeConfig) (*PersonaInvocationServeWiring, error) {
	if cfg.Chat == nil || cfg.References == nil || cfg.Authority == nil || cfg.Grants == nil || cfg.T0Skills == nil || cfg.Invocations == nil {
		return nil, errPersonaInvocationServeWiring
	}
	runBinding, err := NewPersonaRunBinding(cfg.Run)
	if err != nil {
		return nil, fmt.Errorf("%w: durable run binding: %v", errPersonaInvocationServeWiring, err)
	}
	chat, err := newPersonaChatInvocation(personaChatInvocationConfig{
		Chat: cfg.Chat, References: cfg.References, Authority: cfg.Authority,
		Grants: cfg.Grants, Runs: runBinding, T0Skills: cfg.T0Skills,
		Repository: cfg.Invocations, Failures: cfg.Failures,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: chat invocation: %v", errPersonaInvocationServeWiring, err)
	}
	return &PersonaInvocationServeWiring{Chat: chat, RunBinding: runBinding}, nil
}

// SendPost commits through the wired chat path and then admits canonical
// persona references. Invocation failures remain post-commit and are recorded
// through the configured failure sink.
func (w *PersonaInvocationServeWiring) SendPost(ctx context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	if w == nil || w.Chat == nil {
		return chatcore.Post{}, errPersonaInvocationServeWiring
	}
	return w.Chat.SendPost(ctx, request)
}
