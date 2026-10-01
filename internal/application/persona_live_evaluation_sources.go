package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaCandidateDefinitionSource reads only the immutable reviewed candidate
// definition from its production tenant. Execution facts and all business
// data are resolved separately from the reserved synthetic deployment.
type PersonaCandidateDefinitionSource struct {
	Target    agenteval.PersonaEvaluationTarget
	Scope     PersonaCandidateScope
	Versions  PersonaAdminInstallationProfiles
	Manifests personaRunManifestResolverFactory
}

func (s *PersonaCandidateDefinitionSource) Resolve(ctx context.Context) (agentpersonastore.PersonaVersion, agentpersona.PersonaProfile, agentmanifest.Manifest, error) {
	if s == nil || s.Scope == nil || s.Versions == nil || s.Manifests == nil || ctx == nil {
		return agentpersonastore.PersonaVersion{}, agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, agenteval.ErrPersonaEvaluation
	}
	if err := s.Scope.AuthorizeSyntheticPersonaEvaluation(ctx, s.Target); err != nil {
		return agentpersonastore.PersonaVersion{}, agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, err
	}
	version, err := s.Versions.GetVersion(ctx, values.TenantId(s.Target.TenantID), s.Target.PersonaID, s.Target.PersonaVersion)
	var profile agentpersona.PersonaProfile
	if err != nil || version.TenantID.String() != s.Target.TenantID || version.ContentDigest != s.Target.ProfileDigest || json.Unmarshal(version.Profile, &profile) != nil {
		return agentpersonastore.PersonaVersion{}, agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, agenteval.ErrPersonaEvaluation
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != s.Target.ProfileDigest {
		return agentpersonastore.PersonaVersion{}, agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, agenteval.ErrPersonaEvaluation
	}
	resolver, err := s.Manifests.ForTenant(ctx, s.Target.TenantID)
	if err != nil || resolver == nil || resolver.TenantID() != s.Target.TenantID {
		return agentpersonastore.PersonaVersion{}, agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, agenteval.ErrPersonaEvaluation
	}
	manifest, err := resolver.ResolveAgentManifestContext(ctx, profile.Manifest)
	digest, digestErr := manifest.Digest()
	if err != nil || digestErr != nil || digest != profile.Manifest.Digest || manifest.ID != profile.Manifest.ID || manifest.Version != uint64(profile.Manifest.Version) {
		return agentpersonastore.PersonaVersion{}, agentpersona.PersonaProfile{}, agentmanifest.Manifest{}, agenteval.ErrPersonaEvaluation
	}
	return version, profile, manifest, nil
}

// PersonaCandidateRunRequestSource is a dedicated evaluator source. It does
// not require or create a published synthetic persona installation.
type PersonaCandidateRunRequestSource struct {
	Definitions *PersonaCandidateDefinitionSource
	OwnerFacts  PersonaRunOwnerFactsSource
}

func (s *PersonaCandidateRunRequestSource) ResolvePersonaRun(ctx context.Context, invocation agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
	if s == nil || s.Definitions == nil || s.OwnerFacts == nil || !validPersonaRunInvocation(invocation) {
		return PersonaRunRequestFacts{}, agenteval.ErrPersonaEvaluation
	}
	target := s.Definitions.Target
	if invocation.TenantID != target.SyntheticTenantID || invocation.InvokerID != target.InvokerID || invocation.PersonaID != target.PersonaID || !personaRunVersionMatches(target.PersonaVersion, invocation.PersonaVersion) {
		return PersonaRunRequestFacts{}, agenteval.ErrPersonaEvaluation
	}
	_, _, manifest, err := s.Definitions.Resolve(ctx)
	if err != nil {
		return PersonaRunRequestFacts{}, err
	}
	owner, err := s.OwnerFacts.ResolvePersonaRunOwnerFacts(ctx, invocation)
	if err != nil {
		return PersonaRunRequestFacts{}, err
	}
	digest, _ := manifest.Digest()
	facts := PersonaRunRequestFacts{TenantID: target.SyntheticTenantID, LegalEntityID: owner.LegalEntityID,
		Agent:          agentrun.VersionRef{AgentID: manifest.ID, Version: strconv.FormatUint(manifest.Version, 10), Digest: digest},
		AgentPrincipal: owner.AgentPrincipal, PersonaDigest: target.ProfileDigest, Audience: owner.Audience,
		Context: owner.Context, Deadline: owner.Deadline, Budget: owner.Budget, TriggerID: invocation.InvocationID}
	if err := validatePersonaRunFacts(invocation, facts); err != nil {
		return PersonaRunRequestFacts{}, err
	}
	return facts, nil
}

// PersonaCandidateModelWorkSource builds bounded candidate work from actual
// synthetic thread records and the candidate's pinned definition. Route is a
// deployment-owned candidate pin, never a production qualification claim.
type PersonaCandidateModelWorkSource struct {
	Definitions *PersonaCandidateDefinitionSource
	Threads     agentinvoke.ThreadReader
	Leases      *ModelLeaseSource
	Route       PersonaRunModelRoute
	Workload    string
	Now         func() time.Time
	Ledger      *agentbudget.Ledger
}

func (s *PersonaCandidateModelWorkSource) BuildPersonaRunModelWork(ctx context.Context, admission agentrun.Record, run runstate.Run) (PersonaRunModelWork, error) {
	if s == nil || s.Definitions == nil || s.Threads == nil || s.Leases == nil || s.Now == nil || !required(s.Workload) || validatePersonaModelWorkBinding(admission, run) != nil {
		return PersonaRunModelWork{}, fmt.Errorf("%w: candidate model work dependencies or admitted binding", agenteval.ErrPersonaEvaluation)
	}
	target := s.Definitions.Target
	if run.TenantID != target.SyntheticTenantID || admission.Request.Persona == nil || admission.Request.Persona.Digest != target.ProfileDigest || admission.Request.Principal.InvokerID != target.InvokerID ||
		target.ModelDigest != "sha256:"+s.Route.Route.Pin.Primary.ProfileDigest || len(s.Route.Route.Pin.Fallbacks) != 0 {
		return PersonaRunModelWork{}, fmt.Errorf("%w: candidate model work target or route", agenteval.ErrPersonaEvaluation)
	}
	_, profile, manifest, err := s.Definitions.Resolve(ctx)
	if err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("candidate model work definition: %w", err)
	}
	base := &DatabasePersonaRunModelWorkSource{threads: s.Threads, leases: s.Leases, workload: s.Workload, now: s.Now}
	goal, history, refs, err := base.readThreadContext(ctx, admission, profile)
	if err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("candidate model work thread: %w", err)
	}
	route := s.Route
	route.Route.Task.MaxCostMicros = minInt64(route.Route.Task.MaxCostMicros, int64(admission.Request.Budget.MaxCostMicros))
	route.Route.BudgetRemainingMicros = route.Route.Task.MaxCostMicros
	budget := PersonaRunEffectivePolicy{Budget: admission.Request.Budget, Deadline: run.Deadline}
	request, err := base.buildExecutorRequest(ctx, admission, run, profile, manifest, route, budget, goal, history, refs)
	if err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("candidate model work executor request: %w", err)
	}
	request.Route.Task.MaxLatency = minDuration(request.Route.Task.MaxLatency, run.Deadline.Sub(s.Now().UTC()))
	if s.Ledger != nil {
		for _, task := range s.Ledger.Snapshot().Tasks {
			if task.ID != run.ID {
				continue
			}
			if task.TenantID != target.SyntheticTenantID || task.UserID != target.InvokerID {
				return PersonaRunModelWork{}, fmt.Errorf("%w: candidate budget task identity", agenteval.ErrPersonaEvaluation)
			}
			remainingTokens := task.Limit.Tokens - task.Used.Tokens - task.Reserved.Tokens
			remainingCost := task.Limit.SpendMicros - task.Used.SpendMicros - task.Reserved.SpendMicros
			if remainingTokens <= request.Model.Limits.MaxOutputTokens || remainingCost <= 0 {
				return PersonaRunModelWork{}, agentbudget.ErrInvalid
			}
			request.Model.Limits.MaxInputTokens = minInt64(request.Model.Limits.MaxInputTokens, remainingTokens-request.Model.Limits.MaxOutputTokens)
			request.Model.Limits.MaxCostMicros = minInt64(request.Model.Limits.MaxCostMicros, remainingCost)
			request.Route.Task.MaxCostMicros = request.Model.Limits.MaxCostMicros
			request.Route.BudgetRemainingMicros = request.Model.Limits.MaxCostMicros
			request.Route.Task.MaxLatency = minDuration(request.Route.Task.MaxLatency, task.Limit.WallClock-task.Used.WallClock-task.Reserved.WallClock)
		}
	}
	for _, name := range request.Outbound.DeclaredFields {
		request.FieldSources[name] = "synthetic-fixture"
	}
	return PersonaRunModelWork{Request: request}, nil
}
