package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

var errPersonaRunRequestBuilder = errors.New("application: persona run request builder unavailable")

// PersonaRunRequestFacts is the server-owned, immutable evidence needed to
// admit one persona invocation. A source must resolve every field; zero values
// are never interpreted as defaults.
type PersonaRunRequestFacts struct {
	TenantID       string
	LegalEntityID  string
	Agent          agentrun.VersionRef
	AgentPrincipal string
	PersonaDigest  string
	Audience       agentrun.AudienceScope
	Context        agentrun.ContextScope
	Deadline       time.Time
	Budget         agentrun.Budget
	TriggerID      string
}

// PersonaRunRequestSource resolves current owner facts for a canonical
// invocation. Implementations must read authoritative server state and must
// not copy caller claims into the returned facts.
type PersonaRunRequestSource interface {
	ResolvePersonaRun(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error)
}

// PersonaRunRequestBuilder maps a canonical invocation to the shared durable
// agent-run admission contract.
type PersonaRunRequestBuilder struct {
	source PersonaRunRequestSource
}

var _ personaChatAdmissionRequestBuilder = (*PersonaRunRequestBuilder)(nil)

// NewPersonaRunRequestBuilder constructs a fail-closed builder backed by an
// authoritative facts source.
func NewPersonaRunRequestBuilder(source PersonaRunRequestSource) (*PersonaRunRequestBuilder, error) {
	if source == nil {
		return nil, errPersonaRunRequestBuilder
	}
	return &PersonaRunRequestBuilder{source: source}, nil
}

// BuildPersonaChatAdmission creates one request from server-resolved facts.
// Invocation identity binds the source, audience, context and cause fields;
// mutable chat text and caller-provided display values are never included.
func (b *PersonaRunRequestBuilder) BuildPersonaChatAdmission(ctx context.Context, invocation agentinvoke.RunRequest) (agentrun.Request, error) {
	if b == nil || b.source == nil || ctx == nil || !validPersonaChatRunRequest(invocation) {
		return agentrun.Request{}, errPersonaRunRequestBuilder
	}
	facts, err := b.source.ResolvePersonaRun(ctx, invocation)
	if err != nil {
		return agentrun.Request{}, fmt.Errorf("%w: resolve server facts: %w", errPersonaRunRequestBuilder, err)
	}
	if err := validatePersonaRunFacts(invocation, facts); err != nil {
		return agentrun.Request{}, fmt.Errorf("%w: %v", errPersonaRunRequestBuilder, err)
	}
	if invocation.Grant.TargetAgentID != "" && facts.Agent.AgentID != invocation.Grant.TargetAgentID {
		return agentrun.Request{}, fmt.Errorf("%w: pinned agent identity differs from the delegation target", errPersonaRunRequestBuilder)
	}
	return agentrun.Request{
		Source: agentrun.SourceIdentity{
			TenantID: facts.TenantID,
			Kind:     agentrun.SourcePersonaMention,
			Key:      facts.TriggerID,
			Ref:      invocation.InvokingPostID,
		},
		Persona: &agentrun.PersonaRef{
			ID: invocation.PersonaID, Version: invocation.PersonaVersion, Digest: facts.PersonaDigest,
		},
		LegalEntity:    facts.LegalEntityID,
		Agent:          facts.Agent,
		InstallationID: invocation.InstallationID,
		Principal: agentrun.PrincipalChain{
			Mode:                   agentrun.ModeOnBehalfOf,
			AgentPrincipalID:       facts.AgentPrincipal,
			InvokerID:              invocation.InvokerID,
			DelegatedCredentialRef: invocation.Grant.ID,
		},
		Purpose:  "persona-mention",
		Audience: facts.Audience,
		Context:  facts.Context,
		// Canonicalize before admission is digested so the durable worker's
		// microsecond deadline stays exactly bound to this request.
		Deadline: facts.Deadline.UTC().Truncate(time.Microsecond),
		Budget:   facts.Budget,
		CauseID:  facts.TriggerID,
	}, nil
}

func validatePersonaRunFacts(invocation agentinvoke.RunRequest, facts PersonaRunRequestFacts) error {
	if facts.TenantID != invocation.TenantID || facts.TriggerID != invocation.InvocationID {
		return errors.New("server tenant or trigger does not match canonical invocation")
	}
	if !required(facts.LegalEntityID) || !required(facts.AgentPrincipal) || !required(facts.PersonaDigest) ||
		!personaRequestDigest(facts.PersonaDigest) {
		return errors.New("legal entity, agent principal and persona digest are required")
	}
	if !required(facts.Agent.AgentID) || !required(facts.Agent.Version) || !personaRequestDigest(facts.Agent.Digest) {
		return errors.New("exact agent version and digest are required")
	}
	if facts.Audience.ID != invocation.ConversationID || !required(facts.Audience.SnapshotID) || !personaRequestDigest(facts.Audience.Digest) {
		return errors.New("current audience snapshot is required for the invocation conversation")
	}
	if facts.Context.ID != invocation.ThreadID || !required(facts.Context.SnapshotID) || !personaRequestDigest(facts.Context.Digest) {
		return errors.New("current context snapshot is required for the invocation thread")
	}
	if facts.Deadline.IsZero() {
		return errors.New("deadline is required")
	}
	if facts.Budget.MaxCostMicros == 0 || facts.Budget.MaxInputTokens == 0 || facts.Budget.MaxOutputTokens == 0 {
		return errors.New("positive budget ceilings are required")
	}
	return nil
}

func required(value string) bool { return value != "" && value == strings.TrimSpace(value) }

func personaRequestDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
