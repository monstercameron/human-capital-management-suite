package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

const localPersonaModelDeploymentPath = ".artifacts/lanes/agent-dev/model-deployment.json"
const localPersonaModelSigningPath = ".artifacts/lanes/agent-dev/model-signing.json"

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
	path := cfg.AgentModelConfigFile
	automaticLocal := path == "" && cfg.Profile == ServeProfileLocalDev
	if automaticLocal {
		path = filepath.FromSlash(localPersonaModelDeploymentPath)
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
	if apiKey == "" {
		return cfg, empty, "", false, modelConfigurationError("MODEL_API_KEY is required for the configured provider")
	}
	if automaticLocal {
		material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.FromSlash(localPersonaModelSigningPath))
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
