package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

var errPersonaRunCurrentAuthority = errors.New("application: current persona run authority unavailable")

// PersonaRunCurrentAuthorityFacts is returned by a tenant-scoped reader after
// resolving current immutable persona, installation, manifest, principal,
// audience, context, policy, and budget records. The reader must populate
// these fields from its stores; request values are lookup keys only.
type PersonaRunCurrentAuthorityFacts struct {
	TenantID       string
	Persona        agentrun.PersonaRef
	Agent          agentrun.VersionRef
	InstallationID string
	Principal      agentrun.PrincipalChain
	Audience       agentrun.AudienceScope
	Context        agentrun.ContextScope
	BudgetCeiling  agentrun.Budget
	GrantRef       string
	PolicyDigest   string
}

// PersonaRunCurrentAuthorityReader loads current authority facts from one
// tenant's trusted stores. Implementations must resolve every reference to
// its exact immutable revision and fail closed on missing or ambiguous state.
type PersonaRunCurrentAuthorityReader interface {
	ReadCurrentPersonaRunAuthority(context.Context, agentrun.Request) (PersonaRunCurrentAuthorityFacts, error)
}

// PersonaRunCurrentAuthorityStore returns a reader isolated to one tenant.
type PersonaRunCurrentAuthorityStore interface {
	ForTenant(context.Context, string) (PersonaRunCurrentAuthorityReader, error)
}

// DatabasePersonaRunAdmissionAuthority re-resolves the current persona run
// authority on every admission and worker recheck. It is safe for background
// execution: it relies on durable request references and tenant stores, not a
// request-scoped human principal in context.
type DatabasePersonaRunAdmissionAuthority struct {
	Store PersonaRunCurrentAuthorityStore
}

var _ agentrun.Authority = (*DatabasePersonaRunAdmissionAuthority)(nil)

// VerifyAdmission returns a snapshot only when current store facts still
// match every immutable identity and scope reference in the accepted request.
func (a *DatabasePersonaRunAdmissionAuthority) VerifyAdmission(ctx context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if a == nil || a.Store == nil || ctx == nil || !validPersonaRunAdmissionRequest(request) {
		return agentrun.AuthoritySnapshot{}, errPersonaRunCurrentAuthority
	}
	reader, err := a.Store.ForTenant(ctx, request.Source.TenantID)
	if err != nil || reader == nil {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: scope tenant authority reader: %v", errPersonaRunCurrentAuthority, err)
	}
	facts, err := reader.ReadCurrentPersonaRunAuthority(ctx, request)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: read current authority: %v", errPersonaRunCurrentAuthority, err)
	}
	if !currentPersonaRunFactsMatch(request, facts) {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: current authority differs from pinned request", agentrun.ErrAuthorityRefusal)
	}
	if !personaRunBudgetWithin(request.Budget, facts.BudgetCeiling) {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: requested budget exceeds current ceiling", agentrun.ErrAuthorityRefusal)
	}
	return agentrun.AuthoritySnapshot{
		Agent: facts.Agent, InstallationID: facts.InstallationID, Principal: facts.Principal,
		Audience: facts.Audience, Context: facts.Context, BudgetCeiling: facts.BudgetCeiling,
		GrantRef: facts.GrantRef, PolicyDigest: facts.PolicyDigest,
	}, nil
}

func validPersonaRunAdmissionRequest(request agentrun.Request) bool {
	return request.Source.Kind == agentrun.SourcePersonaMention && runAuthorityCleanRequired(request.Source.TenantID, 256) &&
		runAuthorityCleanRequired(request.Source.Key, 512) && runAuthorityCleanRequired(request.Source.Ref, 512) &&
		request.Persona != nil && runAuthorityCleanRequired(request.Persona.ID, 256) && runAuthorityCleanRequired(request.Persona.Version, 256) && personaRunAuthorityDigest(request.Persona.Digest) &&
		runAuthorityCleanRequired(request.InstallationID, 256) && runAuthorityCleanRequired(request.Agent.AgentID, 256) && runAuthorityCleanRequired(request.Agent.Version, 256) && personaRunAuthorityDigest(request.Agent.Digest) &&
		runAuthorityCleanRequired(request.Audience.ID, 256) && runAuthorityCleanRequired(request.Audience.SnapshotID, 256) && personaRunAuthorityDigest(request.Audience.Digest) &&
		runAuthorityCleanRequired(request.Context.ID, 256) && runAuthorityCleanRequired(request.Context.SnapshotID, 256) && personaRunAuthorityDigest(request.Context.Digest) &&
		runAuthorityCleanRequired(request.LegalEntity, 256) && runAuthorityCleanRequired(request.CauseID, 512) &&
		request.CauseID == request.Source.Key &&
		runAuthorityCleanRequired(request.Purpose, 4096) && request.Principal.Mode == agentrun.ModeOnBehalfOf &&
		runAuthorityCleanRequired(request.Principal.AgentPrincipalID, 256) && runAuthorityCleanRequired(request.Principal.InvokerID, 256) &&
		runAuthorityCleanRequired(request.Principal.DelegatedCredentialRef, 512) && !request.Deadline.IsZero() &&
		request.Budget.MaxCostMicros > 0 && request.Budget.MaxInputTokens > 0 && request.Budget.MaxOutputTokens > 0
}

func currentPersonaRunFactsMatch(request agentrun.Request, current PersonaRunCurrentAuthorityFacts) bool {
	return current.TenantID == request.Source.TenantID && request.Persona != nil && current.Persona == *request.Persona &&
		current.Agent == request.Agent && current.InstallationID == request.InstallationID && current.Principal == request.Principal &&
		current.Audience == request.Audience && current.Context == request.Context &&
		runAuthorityCleanRequired(current.GrantRef, 512) && personaRunAuthorityDigest(current.PolicyDigest)
}

func runAuthorityCleanRequired(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value
}

func personaRunBudgetWithin(requested, ceiling agentrun.Budget) bool {
	return ceiling.MaxCostMicros > 0 && ceiling.MaxInputTokens > 0 && ceiling.MaxOutputTokens > 0 &&
		requested.MaxCostMicros <= ceiling.MaxCostMicros && requested.MaxInputTokens <= ceiling.MaxInputTokens && requested.MaxOutputTokens <= ceiling.MaxOutputTokens
}

func personaRunAuthorityDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
