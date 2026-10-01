package agentpersonaeval

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/tools/agenteval"
)

var ErrRunnerConfig = errors.New("agentpersonaeval: invalid composed runner")

// PersonaSafetyFixtureSource runs the AGENT-004 safety, grounding and tool
// selection checks against the served persona stack and returns their observed
// fixture results. Implementations must not derive passing results from case
// expectations or model claims.
type PersonaSafetyFixtureSource interface {
	EvaluatePersonaSafety(context.Context, PersonaVersion, []TaskCase) ([]agentsecurity.EvalFixture, error)
}

// ComposedPersonaRunner joins the served persona case executor with the
// AGENT-004 fixture source and AGENT2-025 synthetic task executor.
type ComposedPersonaRunner struct {
	release         agentsecurity.Release
	personaExecutor TaskExecutor
	safety          PersonaSafetyFixtureSource
	taskExecutor    agenteval.TaskExecutor
}

// NewComposedPersonaRunner requires all runtime components used for issuing
// persona publication evidence. Missing components fail construction.
func NewComposedPersonaRunner(release agentsecurity.Release, personaExecutor TaskExecutor, safety PersonaSafetyFixtureSource, taskExecutor agenteval.TaskExecutor) (*ComposedPersonaRunner, error) {
	if strings.TrimSpace(release.Agent) == "" || strings.TrimSpace(release.AgentBuild) == "" || strings.TrimSpace(release.AgentVersion) == "" ||
		strings.TrimSpace(release.Model) == "" || !validDigest(release.ModelDigest) || strings.TrimSpace(release.Tool) == "" || release.ToolVersion == 0 ||
		!validDigest(release.PromptHash) || personaExecutor == nil || safety == nil || taskExecutor == nil {
		return nil, ErrRunnerConfig
	}
	return &ComposedPersonaRunner{release: release, personaExecutor: personaExecutor, safety: safety, taskExecutor: taskExecutor}, nil
}

// RunPersonaEvaluation executes both the persona case corpus and canonical
// AGENT2-025 suite through their supplied production runtime components.
func (r *ComposedPersonaRunner) RunPersonaEvaluation(ctx context.Context, persona PersonaVersion, cases []TaskCase) (PersonaEvidence, agenteval.Run, error) {
	if r == nil || ctx == nil {
		return PersonaEvidence{}, agenteval.Run{}, ErrRunnerConfig
	}
	release := r.release
	release.PersonaID, release.PersonaVersion = persona.PersonaID, persona.Version
	release.Model, release.ModelDigest, release.PromptHash = persona.Model, persona.ModelDigest, persona.PersonaDigest
	fixtures, err := r.safety.EvaluatePersonaSafety(ctx, persona, cloneTaskCases(sortedCases(cases)))
	if err != nil {
		return PersonaEvidence{}, agenteval.Run{}, fmt.Errorf("%w: AGENT-004 fixture execution: %v", ErrIssuerRun, err)
	}
	if len(fixtures) == 0 {
		return PersonaEvidence{}, agenteval.Run{}, fmt.Errorf("%w: AGENT-004 fixtures are required", ErrIssuerRun)
	}
	if err := validateSafetyFixtures(cases, fixtures); err != nil {
		return PersonaEvidence{}, agenteval.Run{}, err
	}
	personaRun, err := EvaluatePersonaWithObservationEvidence(ctx, persona, cases, r.personaExecutor)
	if err != nil {
		return PersonaEvidence{}, agenteval.Run{}, err
	}
	longRun, err := agenteval.NewEvaluator().Evaluate(ctx, agenteval.Request{Release: release, Suite: agenteval.DefaultSuite(), Fixtures: fixtures, Executor: r.taskExecutor})
	if err != nil {
		return PersonaEvidence{}, agenteval.Run{}, fmt.Errorf("%w: AGENT2-025 execution: %v", ErrIssuerRun, err)
	}
	return personaRun, longRun, nil
}

func validateSafetyFixtures(cases []TaskCase, fixtures []agentsecurity.EvalFixture) error {
	wanted := make(map[string]bool, len(cases))
	for _, testCase := range cases {
		wanted[testCase.ID] = false
	}
	for _, fixture := range fixtures {
		seen, ok := wanted[fixture.Name]
		if !ok || seen {
			return fmt.Errorf("%w: AGENT-004 fixture set does not match persona suite", ErrIssuerRun)
		}
		wanted[fixture.Name] = true
	}
	for _, matched := range wanted {
		if !matched {
			return fmt.Errorf("%w: AGENT-004 fixture set is incomplete", ErrIssuerRun)
		}
	}
	return nil
}
