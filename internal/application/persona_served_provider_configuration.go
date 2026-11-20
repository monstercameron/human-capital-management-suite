package application

import (
	"context"
	"errors"
	"os"
)

const localPersonaModelDeploymentPath = ".artifacts/lanes/agent-dev/model-deployment.json"
const localPersonaModelSigningPath = ".artifacts/lanes/agent-dev/model-signing.json"

const localPersonaModelPreparationCommand = "go run ./cmd/migrate agent-demo -profile local-dev -tenant ironridge-demo"

// newPersonaServedProviderConfiguration consumes only a qualified deployment.
// The local profile gains persistent signing material after its qualification
// artifact exists. An absent artifact leaves the provider unconfigured.
func newPersonaServedProviderConfiguration(ctx context.Context, cfg ServeConfig, runtime *agentRuntime, env func(string) string) (func(PersonaRuntimeModelOwnerDependencies) (PersonaRuntimeModelComposition, error), error) {
	cfg, deployment, apiKey, configured, err := loadServedAgentModelConfiguration(ctx, cfg, env)
	if err != nil || !configured {
		return nil, err
	}
	return newPersonaServedModelFactory(personaServedModelFactoryInput{Config: cfg, Deployment: deployment, APIKey: apiKey, Runtime: runtime})
}

// personaServedProviderUnavailableReason turns the deliberately quiet
// unconfigured result into an operator-facing local-development instruction.
// Production keeps its existing fail-closed behavior and never advertises a
// local preparation command.
func personaServedProviderUnavailableReason(cfg ServeConfig, env func(string) string) string {
	if cfg.Profile != ServeProfileLocalDev {
		return "configure the persona model deployment and dedicated signing seeds"
	}
	path := cfg.AgentModelConfigFile
	if path == "" {
		path = localAgentDevFile(localPersonaModelDeploymentPath)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "prepared persona model deployment is missing at " + absLocalAgentPath(path) + "; run " + localAgentDemoPreparationCommand(servedLocalAgentDemoTenant(cfg.ServedTenants()))
	}
	if env == nil || localModelAPIKeyMissing(env) {
		return "MODEL_API_KEY is not set for the prepared local persona deployment (or set " + EnvLocalModelProvider + "=fixture for the local test provider)"
	}
	return "prepared persona runtime dependencies are incomplete; rerun " + localAgentDemoPreparationCommand(servedLocalAgentDemoTenant(cfg.ServedTenants()))
}

// Both chat personas and private tasks consume the same qualified deployment.
// Loading it does not bind either runtime or grant publication authority.
func loadServedAgentModelConfiguration(ctx context.Context, cfg ServeConfig, env func(string) string) (ServeConfig, PersonaModelDeployment, string, bool, error) {
	empty := PersonaModelDeployment{}
	if ctx == nil {
		return cfg, empty, "", false, modelConfigurationError("provider composition requires a context")
	}
	if err := ctx.Err(); err != nil {
		return cfg, empty, "", false, err
	}
	// The fixture switch is refused outside local development before anything
	// else is read, so a production process handed it fails closed.
	if cfg.Profile != ServeProfileLocalDev {
		if _, _, err := localModelFixtureEndpoint(cfg, env); err != nil {
			return cfg, empty, "", false, err
		}
	}
	path := cfg.AgentModelConfigFile
	automaticLocal := path == "" && cfg.Profile == ServeProfileLocalDev
	if automaticLocal {
		path = localAgentDevFile(localPersonaModelDeploymentPath)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return cfg, empty, "", false, nil
		} else if err != nil {
			return cfg, empty, "", false, modelConfigurationError("qualified local deployment cannot be read")
		}
	}
	if path == "" {
		return cfg, empty, "", false, nil
	}
	deployment, err := LoadPersonaModelDeployment(path)
	if err != nil {
		return cfg, empty, "", false, err
	}
	if env == nil {
		return cfg, empty, "", false, modelConfigurationError("MODEL_API_KEY environment source is required")
	}
	apiKey := env("MODEL_API_KEY")
	fixtureEndpoint, fixtureKey, err := localModelFixtureEndpoint(cfg, env)
	if err != nil {
		return cfg, empty, "", false, err
	}
	if fixtureEndpoint != "" {
		// Only the HTTP answer is canned: the deployment's profiles, egress
		// terms, pricing and signing all still apply to the call.
		apiKey, deployment.BaseURL = fixtureKey, fixtureEndpoint
	}
	if apiKey == "" {
		return cfg, empty, "", false, modelConfigurationError("MODEL_API_KEY is required for the configured provider")
	}
	// An explicit deployment file no longer switches local signing off: the
	// local profile loads its own seeds whenever none were supplied.
	if automaticLocal || (cfg.Profile == ServeProfileLocalDev && len(cfg.PersonaOutputSigningSeed) == 0 && len(cfg.PersonaWorkloadSigningSeed) == 0) {
		material, err := LoadOrCreateLocalPersonaModelSigningMaterial(localPersonaSigningFile())
		if err != nil {
			return cfg, empty, "", false, err
		}
		cfg.PersonaOutputSigningSeed = material.OutputSeed
		cfg.PersonaWorkloadSigningSeed = material.WorkloadSeed
		deployment.Worker.Cell = cfg.CellID
	}
	if _, _, err := personaModelSigningKeys(cfg.PersonaOutputSigningSeed, cfg.PersonaWorkloadSigningSeed); err != nil {
		return cfg, empty, "", false, err
	}
	return cfg, deployment, apiKey, true, nil
}
